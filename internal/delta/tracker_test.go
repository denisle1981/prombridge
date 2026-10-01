package delta

import (
	"github.com/example/prometheus-udp-relay/internal/metriccodec"
	"testing"
)

func parse(t *testing.T, s string) ([]metriccodec.Definition, []metriccodec.Sample) {
	d, v, err := metriccodec.ParseText([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d, v
}

func TestStartupCurrentAndDelta(t *testing.T) {
	tr, _ := New(StartupCurrent, SendDelta)
	d1, s1 := parse(t, "a{job=\"j\"} 1\nb{job=\"j\"} 2\n")
	r := tr.Process(d1, s1, false)
	if !r.BaselineOnly || len(r.Samples) != 0 {
		t.Fatalf("first current-mode scrape must be baseline only: %+v", r)
	}
	d2, s2 := parse(t, "a{job=\"j\"} 1\nb{job=\"j\"} 3\n")
	r = tr.Process(d2, s2, false)
	if len(r.Samples) != 1 || len(r.Definitions) != 1 || r.Changed != 1 {
		t.Fatalf("expected one changed new-to-gateway series: %+v", r)
	}
}

func TestStartupFullDeltaSkipsUnchanged(t *testing.T) {
	tr, _ := New(StartupFull, SendDelta)
	d, s := parse(t, "a{job=\"j\"} 1\nb{job=\"j\"} 2\n")
	r := tr.Process(d, s, false)
	if !r.Full || len(r.Samples) != 2 {
		t.Fatalf("bad initial full: %+v", r)
	}
	d, s = parse(t, "a{job=\"j\"} 1\nb{job=\"j\"} 2\n")
	r = tr.Process(d, s, false)
	if len(r.Samples) != 0 || r.Unchanged != 2 {
		t.Fatalf("unchanged samples should not be sent: %+v", r)
	}
}

func TestPeriodicFullCurrentDoesNotResurrectBaselineOnly(t *testing.T) {
	tr, _ := New(StartupCurrent, SendDelta)
	d, s := parse(t, "a{job=\"j\"} 1\nb{job=\"j\"} 2\n")
	tr.Process(d, s, false)
	d, s = parse(t, "a{job=\"j\"} 9\nb{job=\"j\"} 2\n")
	tr.Process(d, s, false)
	r := tr.Process(d, s, true)
	if !r.Full || len(r.Samples) != 1 || len(r.Definitions) != 1 {
		t.Fatalf("full reconciliation should include only active series: %+v", r)
	}
}
