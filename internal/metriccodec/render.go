package metriccodec

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func Render(defs map[[16]byte]Definition, samples map[[16]byte]Sample) []byte {
	return RenderWithTimestamps(defs, samples, false)
}

func RenderWithTimestamps(defs map[[16]byte]Definition, samples map[[16]byte]Sample, preserveTimestamps bool) []byte {
	ids := make([][16]byte, 0, len(samples))
	for id := range samples {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	var b strings.Builder
	for _, id := range ids {
		d, ok := defs[id]
		if !ok {
			continue
		}
		s := samples[id]
		b.WriteString(d.Name)
		if len(d.Labels) > 0 {
			b.WriteByte('{')
			for i, l := range d.Labels {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(l.Key)
				b.WriteString("=\"")
				b.WriteString(escape(l.Value))
				b.WriteByte('"')
			}
			b.WriteByte('}')
		}
		b.WriteByte(' ')
		b.WriteString(strconv.FormatFloat(s.Value, 'g', -1, 64))
		if preserveTimestamps && s.TimestampMS > 0 {
			fmt.Fprintf(&b, " %d", s.TimestampMS)
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
func escape(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(s)
}
