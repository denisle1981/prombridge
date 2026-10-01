package metriccodec

import (
	"regexp"
	"testing"
)

func TestFilterByMetricName(t *testing.T) {
	var a, b [16]byte
	a[0], b[0] = 1, 2
	defs := []Definition{
		{ID: a, Name: "windows_cpu_time_total"},
		{ID: b, Name: "unwanted_metric"},
	}
	samples := []Sample{{ID: a, Value: 1}, {ID: b, Value: 2}}
	gotD, gotS := FilterByMetricName(defs, samples, regexp.MustCompile(`^(windows_cpu_time_total)$`))
	if len(gotD) != 1 || len(gotS) != 1 || gotD[0].Name != "windows_cpu_time_total" {
		t.Fatalf("unexpected filter result: defs=%v samples=%v", gotD, gotS)
	}
}
