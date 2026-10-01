package metriccodec

import (
	"regexp"
	"testing"
)

func TestFilterByJob(t *testing.T) {
	raw := []byte("a{job=\"windows-exporter\",instance=\"w1\"} 1\nb{job=\"node-exporter\"} 2\nc 3\n")
	defs, samples, err := ParseText(raw)
	if err != nil {
		t.Fatal(err)
	}
	d, s := FilterByJob(defs, samples, regexp.MustCompile(`^windows-.*$`))
	if len(d) != 1 || len(s) != 1 || d[0].Name != "a" {
		t.Fatalf("unexpected filter result defs=%d samples=%d", len(d), len(s))
	}
}
