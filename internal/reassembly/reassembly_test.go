package reassembly

import (
	"github.com/example/prometheus-udp-relay/internal/protocol"
	"testing"
	"time"
)

func TestReassembly(t *testing.T) {
	s := New(time.Second, 1024)
	chunks := [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}
	for i := 2; i >= 0; i-- {
		out, done, err := s.Add(protocol.Packet{MessageID: 1, ChunkIndex: uint16(i), ChunkCount: 3, TotalSize: 6, Payload: chunks[i]})
		if err != nil {
			t.Fatal(err)
		}
		if i != 0 && done {
			t.Fatal("completed too early")
		}
		if done && string(out) != "abcdef" {
			t.Fatalf("got %q", out)
		}
	}
}

func TestSlowMessageSurvivesWhileChunksKeepArriving(t *testing.T) {
	// ttl is intentionally shorter than the total transfer duration.
	// Reassembly must expire only after a period with no chunk progress.
	s := New(80*time.Millisecond, 1024)
	msgID := uint64(4242)
	chunks := [][]byte{[]byte("aaa"), []byte("bbb"), []byte("ccc")}
	var out []byte
	for i, payload := range chunks {
		p := protocol.Packet{MessageID: msgID, ChunkIndex: uint16(i), ChunkCount: 3, TotalSize: 9, Payload: payload}
		got, done, err := s.Add(p)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && done {
			t.Fatalf("completed too early at chunk %d", i)
		}
		if done {
			out = got
		}
		if i < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if string(out) != "aaabbbccc" {
		t.Fatalf("unexpected reassembly: %q", out)
	}
}
