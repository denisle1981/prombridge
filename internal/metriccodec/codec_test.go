package metriccodec

import (
	"bytes"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	raw := []byte("cpu_total{host=\"a\",mode=\"idle\"} 12.5 123\nqueue_size 7\n")
	defs, samples, e := ParseText(raw)
	if e != nil {
		t.Fatal(e)
	}
	var sid [16]byte
	sid[0] = 1
	for i := range defs {
		defs[i].WireID = uint32(i + 1)
	}
	for i := range samples {
		samples[i].WireID = uint32(i + 1)
	}
	b := Batch{SenderID: sid, Sequence: 3, CreatedAt: time.Now(), Full: true, Definitions: defs, Samples: samples}
	enc, e := Encode(b)
	if e != nil {
		t.Fatal(e)
	}
	got, e := Decode(enc)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Definitions) != 2 || len(got.Samples) != 2 {
		t.Fatalf("bad counts")
	}
	mdefs := map[[16]byte]Definition{}
	ms := map[[16]byte]Sample{}
	byWire := map[uint32]Definition{}
	for _, d := range got.Definitions {
		mdefs[d.ID] = d
		byWire[d.WireID] = d
	}
	for _, s := range got.Samples {
		d := byWire[s.WireID]
		s.ID = d.ID
		ms[d.ID] = s
	}
	out := Render(mdefs, ms)
	if !bytes.Contains(out, []byte("cpu_total")) {
		t.Fatalf("bad render %s", out)
	}
}
func TestStableID(t *testing.T) {
	a := []Label{{"b", "2"}, {"a", "1"}}
	b := []Label{{"a", "1"}, {"b", "2"}}
	if SeriesID("x", a) != SeriesID("x", b) {
		t.Fatal("id not stable")
	}
}

func BenchmarkEncodeBatch(b *testing.B) {
	raw := []byte("cpu_total{host=\"windows01\",mode=\"idle\"} 12.5\nqueue_size 7\n")
	defs, samples, err := ParseText(raw)
	if err != nil {
		b.Fatal(err)
	}
	for i := range defs {
		defs[i].WireID = uint32(i + 1)
	}
	for i := range samples {
		samples[i].WireID = uint32(i + 1)
	}
	batch := Batch{Sequence: 1, CreatedAt: time.Now(), Full: true, Definitions: defs, Samples: samples}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(batch); err != nil {
			b.Fatal(err)
		}
	}
}
