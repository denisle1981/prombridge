package reassembly

import (
	"errors"
	"sync"
	"time"

	"github.com/example/prometheus-udp-relay/internal/protocol"
)

type message struct {
	created time.Time
	updated time.Time
	total   uint32
	count   uint16
	chunks  [][]byte
	seen    int
}

type Store struct {
	mu              sync.Mutex
	ttl             time.Duration
	maxMessageBytes uint32
	messages        map[uint64]*message
}

func New(ttl time.Duration, maxMessageBytes uint32) *Store {
	return &Store{ttl: ttl, maxMessageBytes: maxMessageBytes, messages: map[uint64]*message{}}
}

func (s *Store) Add(p protocol.Packet) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if p.TotalSize == 0 || p.TotalSize > s.maxMessageBytes {
		return nil, false, errors.New("message size exceeds limit")
	}
	m, ok := s.messages[p.MessageID]
	if !ok {
		now := time.Now()
		m = &message{created: now, updated: now, total: p.TotalSize, count: p.ChunkCount, chunks: make([][]byte, p.ChunkCount)}
		s.messages[p.MessageID] = m
	}
	// The timeout is an *idle* timeout, not a maximum total transfer time.
	// A bandwidth-shaped message may legitimately take much longer than ttl
	// to arrive, as long as chunks keep making progress.
	m.updated = time.Now()
	if m.total != p.TotalSize || m.count != p.ChunkCount {
		delete(s.messages, p.MessageID)
		return nil, false, errors.New("inconsistent message metadata")
	}
	if m.chunks[p.ChunkIndex] == nil {
		m.chunks[p.ChunkIndex] = p.Payload
		m.seen++
	}
	if m.seen != int(m.count) {
		return nil, false, nil
	}
	out := make([]byte, 0, m.total)
	for _, c := range m.chunks {
		out = append(out, c...)
	}
	delete(s.messages, p.MessageID)
	if uint32(len(out)) != m.total {
		return nil, false, errors.New("reassembled size mismatch")
	}
	return out, true, nil
}

func (s *Store) cleanupLocked() {
	cutoff := time.Now().Add(-s.ttl)
	for id, m := range s.messages {
		if m.updated.Before(cutoff) {
			delete(s.messages, id)
		}
	}
}
