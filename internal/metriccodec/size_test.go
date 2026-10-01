package metriccodec

import (
	"fmt"
	"github.com/example/prometheus-udp-relay/internal/compression"
	"testing"
	"time"
)

func TestSyntheticSizeReport(t *testing.T) {
	raw := make([]byte, 0, 2_000_000)
	for i := 0; i < 9054; i++ {
		raw = append(raw, []byte(fmt.Sprintf("windows_metric_%d{instance=\"windows01:9182\",job=\"windows-exporter\",mode=\"idle\",disk=\"C:\",series=\"%d\"} %d %d\n", i%120, i, i, 1780000000000+int64(i)))...)
	}
	defs, samples, err := ParseText(raw)
	if err != nil {
		t.Fatal(err)
	}
	wire := map[[16]byte]uint32{}
	var next uint32 = 1
	for i := range defs {
		defs[i].WireID = next
		wire[defs[i].ID] = next
		next++
	}
	for i := range samples {
		samples[i].WireID = wire[samples[i].ID]
	}
	full, err := Encode(Batch{Sequence: 1, CreatedAt: time.Now(), Full: true, Definitions: defs, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	fullGz, err := compression.Compress("gzip", full)
	if err != nil {
		t.Fatal(err)
	}
	delta, err := Encode(Batch{Sequence: 2, CreatedAt: time.Now(), Full: false, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	deltaGz, err := compression.Compress("gzip", delta)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("raw=%d full_encoded=%d full_compressed=%d delta_encoded=%d delta_compressed=%d defs=%d samples=%d", len(raw), len(full), len(fullGz), len(delta), len(deltaGz), len(defs), len(samples))
}
