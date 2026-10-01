package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/prometheus-udp-relay/internal/compression"
	"github.com/example/prometheus-udp-relay/internal/delta"
	"github.com/example/prometheus-udp-relay/internal/metriccodec"
	"github.com/example/prometheus-udp-relay/internal/protocol"
)

type matches []string

func (m *matches) String() string     { return strings.Join(*m, ",") }
func (m *matches) Set(v string) error { *m = append(*m, v); return nil }

type snapshot struct {
	raw     []byte
	fetched time.Duration
}
type stats struct {
	snapshots, skipped, errors, packets, rawBytes, encodedBytes, compressedBytes              uint64
	samplesObserved, samplesSent                                                              uint64
	lastSuccess, lastFetchNS, lastSerializeNS, lastCompressNS, lastSendNS                     int64
	lastRawBytes, lastEncodedBytes, lastCompressedBytes, lastSamplesObserved, lastSamplesSent int64
	bandwidthBytesPerSecond, bandwidthLimitBytesPerSecond                                     int64
}

func main() {
	var extraMatches matches
	source := flag.String("source-url", "http://prometheus:9090/federate", "Prometheus /federate URL")
	dh := flag.String("dest-host", "127.0.0.1", "receiver host")
	dp := flag.Int("dest-port", 19090, "receiver UDP port")
	interval := flag.Duration("interval", 15*time.Second, "fetch interval")
	mtu := flag.Int("mtu", 1200, "max datagram")
	delay := flag.Duration("packet-delay", time.Millisecond, "delay between packets")
	timeout := flag.Duration("http-timeout", 15*time.Second, "HTTP timeout")
	maxBody := flag.Int64("max-body-bytes", 64<<20, "max federation body")
	comp := flag.String("compression", "gzip-best", "none|gzip-fast|gzip-best")
	fullEvery := flag.Duration("full-sync-interval", 5*time.Minute, "periodic reconciliation interval")
	startupMode := flag.String("startup-mode", "full", "startup behavior: full sends current state immediately; current establishes a baseline and starts transmitting only subsequent changes")
	sendMode := flag.String("send-mode", "delta", "delta sends changed/new/deleted series only; snapshot sends all current samples every interval")
	jobRegex := flag.String("job-regex", ".+", "regular expression for the Prometheus job label")
	metricRegex := flag.String("metric-regex", ".+", "regular expression for metric names; applied after job filtering")
	maxBandwidth := flag.Int64("max-bandwidth-bytes-per-second", 4096, "hard PromBridge UDP payload shaping limit in bytes/second; 0 disables shaping")
	queueSize := flag.Int("snapshot-queue", 1, "bounded snapshot queue; oldest is dropped")
	httpListen := flag.String("metrics-listen", ":9091", "sender self-metrics HTTP address; empty disables")
	sendBuf := flag.Int("udp-send-buffer-bytes", 4<<20, "UDP socket send buffer")
	flag.Var(&extraMatches, "match", "additional Prometheus federation selector; repeatable")
	flag.Parse()

	if *queueSize < 1 {
		log.Fatal("snapshot-queue must be >=1")
	}
	if *jobRegex == "" {
		log.Fatal("job-regex must not be empty")
	}
	if *maxBandwidth < 0 {
		log.Fatal("max-bandwidth-bytes-per-second must be >=0")
	}
	localJobRE, err := regexp.Compile("^(?:" + *jobRegex + ")$")
	if err != nil {
		log.Fatalf("invalid job-regex: %v", err)
	}
	localMetricRE, err := regexp.Compile("^(?:" + *metricRegex + ")$")
	if err != nil {
		log.Fatalf("invalid metric-regex: %v", err)
	}
	tracker, err := delta.New(delta.StartupMode(*startupMode), delta.SendMode(*sendMode))
	if err != nil {
		log.Fatal(err)
	}

	secret := []byte(os.Getenv("PUR_SECRET"))
	if len(secret) == 0 {
		log.Fatal("PUR_SECRET must be set")
	}
	code, err := protocol.CompressionCode(*comp)
	if err != nil {
		log.Fatal(err)
	}
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", *dh, *dp))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetWriteBuffer(*sendBuf)

	selectors := []string{fmt.Sprintf(`{job=~"%s"}`, escapePromQLString(*jobRegex))}
	selectors = append(selectors, extraMatches...)

	client := &http.Client{Timeout: *timeout}
	var st stats
	atomic.StoreInt64(&st.bandwidthLimitBytesPerSecond, *maxBandwidth)
	var senderID [16]byte
	if _, err = rand.Read(senderID[:]); err != nil {
		log.Fatal(err)
	}
	q := make(chan snapshot, *queueSize)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *httpListen != "" {
		go serveMetrics(*httpListen, &st)
	}

	log.Printf("event=sender_started protocol=3 startup_mode=%s send_mode=%s job_regex=%q metric_regex=%q interval=%s full_sync_interval=%s compression=%s max_bandwidth_bytes_per_second=%d", *startupMode, *sendMode, *jobRegex, *metricRegex, interval.String(), fullEvery.String(), *comp, *maxBandwidth)
	go fetchLoop(ctx, client, *source, selectors, *interval, *maxBody, q, &st)
	sendLoop(ctx, conn, q, secret, senderID, code, *comp, *mtu, *delay, *fullEvery, tracker, localJobRE, localMetricRE, *maxBandwidth, &st)
}

func escapePromQLString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func fetchLoop(ctx context.Context, c *http.Client, source string, ms []string, interval time.Duration, max int64, q chan snapshot, st *stats) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		start := time.Now()
		raw, err := fetch(ctx, c, source, ms, max)
		d := time.Since(start)
		atomic.StoreInt64(&st.lastFetchNS, d.Nanoseconds())
		if err != nil {
			atomic.AddUint64(&st.errors, 1)
			log.Printf("event=federate_fetch_failed error=%q", err)
		} else {
			s := snapshot{raw: raw, fetched: d}
			select {
			case q <- s:
			default:
				select {
				case <-q:
				default:
				}
				q <- s
				log.Printf("event=snapshot_queue_replaced reason=queue_full")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sendLoop(ctx context.Context, conn *net.UDPConn, q chan snapshot, secret []byte, senderID [16]byte, code uint8, comp string, mtu int, delay, fullEvery time.Duration, tracker *delta.Tracker, jobRE, metricRE *regexp.Regexp, maxBandwidth int64, st *stats) {
	var seq uint64
	lastReconcile := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case snap := <-q:
			total := time.Now()
			parseStart := time.Now()
			defs, samples, err := metriccodec.ParseText(snap.raw)
			if err != nil {
				atomic.AddUint64(&st.errors, 1)
				log.Printf("event=parse_failed error=%q", err)
				continue
			}
			defs, samples = metriccodec.FilterByJob(defs, samples, jobRE)
			defs, samples = metriccodec.FilterByMetricName(defs, samples, metricRE)
			parseD := time.Since(parseStart)

			forceFull := !lastReconcile.IsZero() && fullEvery > 0 && time.Since(lastReconcile) >= fullEvery
			result := tracker.Process(defs, samples, forceFull)
			atomic.AddUint64(&st.samplesObserved, uint64(result.Observed))
			atomic.StoreInt64(&st.lastSamplesObserved, int64(result.Observed))

			if result.BaselineOnly {
				lastReconcile = time.Now()
				atomic.AddUint64(&st.skipped, 1)
				atomic.StoreInt64(&st.lastRawBytes, int64(len(snap.raw)))
				atomic.StoreInt64(&st.lastEncodedBytes, 0)
				atomic.StoreInt64(&st.lastCompressedBytes, 0)
				atomic.StoreInt64(&st.lastSamplesSent, 0)
				log.Printf("event=startup_baseline_initialized startup_mode=current samples_observed=%d raw_bytes=%d parse_duration=%s note=%q", result.Observed, len(snap.raw), parseD.Round(time.Millisecond), "no metrics transmitted; subsequent delta changes will be sent")
				continue
			}

			if result.Full {
				lastReconcile = time.Now()
			}
			if len(result.Samples) == 0 && len(result.Definitions) == 0 && len(result.Deleted) == 0 {
				if forceFull {
					lastReconcile = time.Now()
				}
				atomic.AddUint64(&st.skipped, 1)
				atomic.StoreInt64(&st.lastRawBytes, int64(len(snap.raw)))
				atomic.StoreInt64(&st.lastEncodedBytes, 0)
				atomic.StoreInt64(&st.lastCompressedBytes, 0)
				atomic.StoreInt64(&st.lastSamplesSent, 0)
				log.Printf("event=snapshot_skipped reason=no_changes samples_observed=%d samples_sent=0 samples_unchanged=%d raw_bytes=%d parse_duration=%s", result.Observed, result.Unchanged, len(snap.raw), parseD.Round(time.Millisecond))
				continue
			}

			seq++
			serialStart := time.Now()
			batchRaw, err := metriccodec.Encode(metriccodec.Batch{
				SenderID: senderID, Sequence: seq, CreatedAt: time.Now(), Full: result.Full,
				Definitions: result.Definitions, Samples: result.Samples, Deleted: result.Deleted,
			})
			if err != nil {
				atomic.AddUint64(&st.errors, 1)
				log.Printf("event=serialize_failed error=%q", err)
				continue
			}
			serialD := time.Since(serialStart)
			compressStart := time.Now()
			packed, err := compression.Compress(comp, batchRaw)
			if err != nil {
				atomic.AddUint64(&st.errors, 1)
				log.Printf("event=compress_failed error=%q", err)
				continue
			}
			compressD := time.Since(compressStart)
			sendStart := time.Now()
			packets, wireBytes, id, err := sendPackets(conn, packed, secret, code, mtu, delay, maxBandwidth, st)
			if err != nil {
				atomic.AddUint64(&st.errors, 1)
				log.Printf("event=send_failed error=%q", err)
				continue
			}
			sendD := time.Since(sendStart)

			atomic.AddUint64(&st.snapshots, 1)
			atomic.AddUint64(&st.packets, uint64(packets))
			atomic.AddUint64(&st.rawBytes, uint64(len(snap.raw)))
			atomic.AddUint64(&st.encodedBytes, uint64(len(batchRaw)))
			atomic.AddUint64(&st.compressedBytes, uint64(len(packed)))
			atomic.AddUint64(&st.samplesSent, uint64(len(result.Samples)))
			atomic.StoreInt64(&st.lastSuccess, time.Now().Unix())
			atomic.StoreInt64(&st.lastSerializeNS, serialD.Nanoseconds())
			atomic.StoreInt64(&st.lastCompressNS, compressD.Nanoseconds())
			atomic.StoreInt64(&st.lastSendNS, sendD.Nanoseconds())
			atomic.StoreInt64(&st.lastRawBytes, int64(len(snap.raw)))
			atomic.StoreInt64(&st.lastEncodedBytes, int64(len(batchRaw)))
			atomic.StoreInt64(&st.lastCompressedBytes, int64(len(packed)))
			atomic.StoreInt64(&st.lastSamplesSent, int64(len(result.Samples)))
			if sendD > 0 {
				atomic.StoreInt64(&st.bandwidthBytesPerSecond, int64(float64(wireBytes)/sendD.Seconds()))
			}

			log.Printf("event=snapshot_sent protocol=3 message_id=%d sender_id=%s sequence=%d full=%t samples_observed=%d samples_sent=%d samples_unchanged=%d definitions=%d deleted=%d raw_bytes=%d encoded_bytes=%d compressed_bytes=%d wire_bytes=%d packets=%d parse_duration=%s serialization_duration=%s compression_duration=%s send_duration=%s total_duration=%s", id, hex.EncodeToString(senderID[:]), seq, result.Full, result.Observed, len(result.Samples), result.Unchanged, len(result.Definitions), len(result.Deleted), len(snap.raw), len(batchRaw), len(packed), wireBytes, packets, parseD.Round(time.Millisecond), serialD.Round(time.Millisecond), compressD.Round(time.Millisecond), sendD.Round(time.Millisecond), time.Since(total).Round(time.Millisecond))
		}
	}
}

func sendPackets(conn *net.UDPConn, data, secret []byte, code uint8, mtu int, delay time.Duration, maxBandwidth int64, st *stats) (int, int64, uint64, error) {
	if mtu <= protocol.HeaderSize {
		return 0, 0, 0, fmt.Errorf("mtu too small")
	}
	pm := mtu - protocol.HeaderSize
	count := (len(data) + pm - 1) / pm
	if count == 0 {
		count = 1
	}
	if count > protocol.MaxChunks {
		return 0, 0, 0, fmt.Errorf("too many chunks")
	}
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return 0, 0, 0, e
	}
	id := binary.BigEndian.Uint64(b[:])
	var wireBytes int64
	var nextAllowed time.Time
	for i := 0; i < count; i++ {
		a := i * pm
		z := a + pm
		if z > len(data) {
			z = len(data)
		}
		pkt, e := protocol.Encode(protocol.Packet{Compression: code, MessageID: id, ChunkIndex: uint16(i), ChunkCount: uint16(count), TotalSize: uint32(len(data)), UnixMilli: time.Now().UnixMilli(), Payload: data[a:z]}, secret)
		if e != nil {
			return i, wireBytes, id, e
		}
		if maxBandwidth > 0 {
			now := time.Now()
			if !nextAllowed.IsZero() && now.Before(nextAllowed) {
				time.Sleep(nextAllowed.Sub(now))
			}
			spacing := time.Duration(float64(len(pkt)) / float64(maxBandwidth) * float64(time.Second))
			if nextAllowed.IsZero() || time.Now().After(nextAllowed) {
				nextAllowed = time.Now().Add(spacing)
			} else {
				nextAllowed = nextAllowed.Add(spacing)
			}
		}
		if _, e = conn.Write(pkt); e != nil {
			return i, wireBytes, id, e
		}
		wireBytes += int64(len(pkt))
		if delay > 0 && i+1 < count {
			time.Sleep(delay)
		}
	}
	return count, wireBytes, id, nil
}

func fetch(ctx context.Context, c *http.Client, source string, ms []string, max int64) ([]byte, error) {
	u, e := url.Parse(source)
	if e != nil {
		return nil, e
	}
	q := u.Query()
	for _, m := range ms {
		q.Add("match[]", m)
	}
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, e := c.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("source returned %s", resp.Status)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if int64(len(b)) > max {
		return nil, fmt.Errorf("body exceeds limit")
	}
	return b, e
}

func serveMetrics(addr string, s *stats) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w,
			"pur_sender_snapshots_total %d\n"+
				"pur_sender_snapshots_skipped_total %d\n"+
				"pur_sender_errors_total %d\n"+
				"pur_sender_packets_total %d\n"+
				"pur_sender_raw_bytes_total %d\n"+
				"pur_sender_encoded_bytes_total %d\n"+
				"pur_sender_compressed_bytes_total %d\n"+
				"pur_sender_samples_observed_total %d\n"+
				"pur_sender_samples_sent_total %d\n"+
				"pur_sender_last_raw_bytes %d\n"+
				"pur_sender_last_encoded_bytes %d\n"+
				"pur_sender_last_compressed_bytes %d\n"+
				"pur_sender_last_samples_observed %d\n"+
				"pur_sender_last_samples_sent %d\n"+
				"pur_sender_last_success_timestamp_seconds %d\n"+
				"pur_sender_last_fetch_duration_seconds %.6f\n"+
				"pur_sender_last_serialization_duration_seconds %.6f\n"+
				"pur_sender_last_compression_duration_seconds %.6f\n"+
				"pur_sender_last_send_duration_seconds %.6f\n"+
				"# HELP pur_sender_bandwidth_bytes_per_second Configured/active PromBridge send rate while transmitting.\n"+
				"# TYPE pur_sender_bandwidth_bytes_per_second gauge\n"+
				"pur_sender_bandwidth_bytes_per_second %d\n"+
				"# HELP pur_sender_bandwidth_limit_bytes_per_second Hard PromBridge UDP payload shaping limit.\n"+
				"# TYPE pur_sender_bandwidth_limit_bytes_per_second gauge\n"+
				"pur_sender_bandwidth_limit_bytes_per_second %d\n",
			atomic.LoadUint64(&s.snapshots), atomic.LoadUint64(&s.skipped), atomic.LoadUint64(&s.errors), atomic.LoadUint64(&s.packets),
			atomic.LoadUint64(&s.rawBytes), atomic.LoadUint64(&s.encodedBytes), atomic.LoadUint64(&s.compressedBytes),
			atomic.LoadUint64(&s.samplesObserved), atomic.LoadUint64(&s.samplesSent),
			atomic.LoadInt64(&s.lastRawBytes), atomic.LoadInt64(&s.lastEncodedBytes), atomic.LoadInt64(&s.lastCompressedBytes),
			atomic.LoadInt64(&s.lastSamplesObserved), atomic.LoadInt64(&s.lastSamplesSent), atomic.LoadInt64(&s.lastSuccess),
			float64(atomic.LoadInt64(&s.lastFetchNS))/1e9, float64(atomic.LoadInt64(&s.lastSerializeNS))/1e9,
			float64(atomic.LoadInt64(&s.lastCompressNS))/1e9, float64(atomic.LoadInt64(&s.lastSendNS))/1e9,
			atomic.LoadInt64(&s.bandwidthBytesPerSecond), atomic.LoadInt64(&s.bandwidthLimitBytesPerSecond))
	})
	log.Printf("event=sender_metrics_started address=%q", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
