package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/example/prometheus-udp-relay/internal/compression"
	"github.com/example/prometheus-udp-relay/internal/metriccodec"
	"github.com/example/prometheus-udp-relay/internal/protocol"
	"github.com/example/prometheus-udp-relay/internal/reassembly"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type senderState struct {
	definitions map[uint32]metriccodec.Definition
	samples     map[uint32]metriccodec.Sample
	sequence    uint64
	updated     time.Time
}
type state struct {
	mu      sync.RWMutex
	senders map[[16]byte]*senderState
	payload []byte
	updated time.Time
}

func main() {
	udpListen := flag.String("udp-listen", ":19090", "UDP listen address")
	httpListen := flag.String("http-listen", ":8080", "HTTP listen address")
	ttl := flag.Duration("reassembly-timeout", 30*time.Second, "incomplete message timeout")
	maxCompressed := flag.Uint("max-compressed-bytes", 64<<20, "max compressed batch")
	maxUncompressed := flag.Int64("max-uncompressed-bytes", 256<<20, "max decoded batch")
	maxPacket := flag.Int("max-packet-bytes", 65535, "UDP read buffer")
	recvBuf := flag.Int("udp-receive-buffer-bytes", 8<<20, "UDP receive buffer")
	preserveTimestamps := flag.Bool("preserve-source-timestamps", false, "preserve source sample timestamps on /metrics; false is recommended for delta mode so Prometheus assigns gateway scrape time")
	flag.Parse()
	secret := []byte(os.Getenv("PUR_SECRET"))
	if len(secret) == 0 {
		log.Fatal("PUR_SECRET must be set")
	}
	addr, e := net.ResolveUDPAddr("udp", *udpListen)
	if e != nil {
		log.Fatal(e)
	}
	conn, e := net.ListenUDP("udp", addr)
	if e != nil {
		log.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(*recvBuf)
	store := reassembly.New(*ttl, uint32(*maxCompressed))
	st := state{senders: map[[16]byte]*senderState{}}
	var packets, invalid, completed, unknownSeries, sequenceGaps, wireBytesTotal uint64
	var bandwidthBytesPerSecond int64
	firstSeen := map[uint64]time.Time{}
	messageWireBytes := map[uint64]int64{}
	go func() {
		buf := make([]byte, *maxPacket)
		for {
			n, remote, e := conn.ReadFromUDP(buf)
			if e != nil {
				log.Printf("event=udp_read_failed error=%q", e)
				continue
			}
			atomic.AddUint64(&packets, 1)
			atomic.AddUint64(&wireBytesTotal, uint64(n))
			p, e := protocol.Decode(buf[:n], secret)
			if e != nil {
				atomic.AddUint64(&invalid, 1)
				continue
			}
			messageWireBytes[p.MessageID] += int64(n)
			if _, ok := firstSeen[p.MessageID]; !ok {
				firstSeen[p.MessageID] = time.Now()
				log.Printf("event=snapshot_receive_started protocol=3 message_id=%d source=%q packets=%d compressed_bytes=%d", p.MessageID, remote, p.ChunkCount, p.TotalSize)
			}
			packed, done, e := store.Add(p)
			if e != nil {
				atomic.AddUint64(&invalid, 1)
				delete(firstSeen, p.MessageID)
				log.Printf("event=snapshot_rejected message_id=%d reason=%q", p.MessageID, e)
				continue
			}
			if !done {
				continue
			}
			reassemblyD := time.Since(firstSeen[p.MessageID])
			wireBytes := messageWireBytes[p.MessageID]
			delete(firstSeen, p.MessageID)
			delete(messageWireBytes, p.MessageID)
			if reassemblyD > 0 {
				atomic.StoreInt64(&bandwidthBytesPerSecond, int64(float64(wireBytes)/reassemblyD.Seconds()))
			}
			compName, e := protocol.CompressionName(p.Compression)
			if e != nil {
				atomic.AddUint64(&invalid, 1)
				continue
			}
			decStart := time.Now()
			encoded, e := compression.Decompress(compName, packed, *maxUncompressed)
			decD := time.Since(decStart)
			if e != nil {
				atomic.AddUint64(&invalid, 1)
				log.Printf("event=decompression_failed message_id=%d error=%q", p.MessageID, e)
				continue
			}
			desStart := time.Now()
			batch, e := metriccodec.Decode(encoded)
			desD := time.Since(desStart)
			if e != nil {
				atomic.AddUint64(&invalid, 1)
				log.Printf("event=deserialization_failed message_id=%d error=%q", p.MessageID, e)
				continue
			}
			publishStart := time.Now()
			missing := applyBatch(&st, batch, &sequenceGaps, *preserveTimestamps)
			atomic.AddUint64(&unknownSeries, uint64(missing))
			publishD := time.Since(publishStart)
			atomic.AddUint64(&completed, 1)
			log.Printf("event=snapshot_accepted protocol=3 message_id=%d source=%q sender_id=%s sequence=%d full=%t samples=%d definitions=%d deleted=%d unknown_series=%d compressed_bytes=%d encoded_bytes=%d packets=%d reassembly_duration=%s decompression_duration=%s deserialization_duration=%s publish_duration=%s total_duration=%s", p.MessageID, remote, hex.EncodeToString(batch.SenderID[:]), batch.Sequence, batch.Full, len(batch.Samples), len(batch.Definitions), len(batch.Deleted), missing, len(packed), len(encoded), p.ChunkCount, reassemblyD.Round(time.Millisecond), decD.Round(time.Millisecond), desD.Round(time.Millisecond), publishD.Round(time.Millisecond), (reassemblyD + decD + desD + publishD).Round(time.Millisecond))
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		ready := len(st.payload) > 0
		st.mu.RUnlock()
		if !ready {
			http.Error(w, "no snapshot", 503)
			return
		}
		fmt.Fprintln(w, "ready")
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		payload := append([]byte(nil), st.payload...)
		updated := st.updated
		st.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if len(payload) > 0 {
			w.Write(payload)
		}
		age := -1.0
		if !updated.IsZero() {
			age = time.Since(updated).Seconds()
		}
		fmt.Fprintf(w, "pur_receiver_packets_total %d\npur_receiver_invalid_packets_total %d\npur_receiver_snapshots_total %d\npur_receiver_unknown_series_total %d\npur_receiver_sequence_gaps_total %d\npur_receiver_snapshot_age_seconds %.3f\npur_receiver_snapshot_bytes %d\npur_receiver_wire_bytes_total %d\n# HELP pur_receiver_bandwidth_bytes_per_second Last completed PromBridge message receive throughput in bytes/second.\n# TYPE pur_receiver_bandwidth_bytes_per_second gauge\npur_receiver_bandwidth_bytes_per_second %d\n", atomic.LoadUint64(&packets), atomic.LoadUint64(&invalid), atomic.LoadUint64(&completed), atomic.LoadUint64(&unknownSeries), atomic.LoadUint64(&sequenceGaps), age, len(payload), atomic.LoadUint64(&wireBytesTotal), atomic.LoadInt64(&bandwidthBytesPerSecond))
	})
	log.Printf("event=receiver_started protocol=3 udp_listen=%q http_listen=%q udp_receive_buffer_bytes=%d", *udpListen, *httpListen, *recvBuf)
	log.Fatal(http.ListenAndServe(*httpListen, mux))
}
func applyBatch(st *state, b metriccodec.Batch, gaps *uint64, preserveTimestamps bool) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	ss := st.senders[b.SenderID]
	if ss == nil {
		ss = &senderState{definitions: map[uint32]metriccodec.Definition{}, samples: map[uint32]metriccodec.Sample{}}
		st.senders[b.SenderID] = ss
	}
	if ss.sequence > 0 && b.Sequence > ss.sequence+1 {
		atomic.AddUint64(gaps, b.Sequence-ss.sequence-1)
	}
	if b.Sequence <= ss.sequence {
		return 0
	}
	if b.Full {
		ss.definitions = map[uint32]metriccodec.Definition{}
		ss.samples = map[uint32]metriccodec.Sample{}
	}
	for _, d := range b.Definitions {
		ss.definitions[d.WireID] = d
	}
	for _, id := range b.Deleted {
		delete(ss.definitions, id)
		delete(ss.samples, id)
	}
	missing := 0
	for _, s := range b.Samples {
		if _, ok := ss.definitions[s.WireID]; !ok {
			missing++
			continue
		}
		ss.samples[s.WireID] = s
	}
	ss.sequence = b.Sequence
	ss.updated = time.Now()
	allDefs := map[[16]byte]metriccodec.Definition{}
	allSamples := map[[16]byte]metriccodec.Sample{}
	for _, x := range st.senders {
		for wid, d := range x.definitions {
			allDefs[d.ID] = d
			if s, ok := x.samples[wid]; ok {
				s.ID = d.ID
				allSamples[d.ID] = s
			}
		}
	}
	st.payload = metriccodec.RenderWithTimestamps(allDefs, allSamples, preserveTimestamps)
	st.updated = time.Now()
	return missing
}
