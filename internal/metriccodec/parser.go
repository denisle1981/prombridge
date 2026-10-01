package metriccodec

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ParseText(raw []byte) ([]Definition, []Sample, error) {
	defs := make([]Definition, 0, 1024)
	samples := make([]Sample, 0, 4096)
	seen := map[[16]byte]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ts, err := parseLine(line)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %q: %w", line, err)
		}
		id := SeriesID(name, labels)
		if !seen[id] {
			defs = append(defs, Definition{ID: id, Name: name, Labels: labels})
			seen[id] = true
		}
		samples = append(samples, Sample{ID: id, Value: value, TimestampMS: ts})
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	return defs, samples, nil
}

func SeriesID(name string, labels []Label) [16]byte {
	cp := append([]Label(nil), labels...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Key < cp[j].Key })
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	for _, l := range cp {
		h.Write([]byte(l.Key))
		h.Write([]byte{'='})
		h.Write([]byte(l.Value))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	var id [16]byte
	copy(id[:], sum[:16])
	return id
}

func parseLine(line string) (string, []Label, float64, int64, error) {
	split := metricValueSplit(line)
	if split < 0 {
		return "", nil, 0, 0, fmt.Errorf("missing value")
	}
	left := strings.TrimSpace(line[:split])
	right := strings.Fields(line[split:])
	if len(right) == 0 {
		return "", nil, 0, 0, fmt.Errorf("missing value")
	}
	value, err := strconv.ParseFloat(right[0], 64)
	if err != nil {
		return "", nil, 0, 0, err
	}
	var ts int64
	if len(right) > 1 {
		ts, err = strconv.ParseInt(right[1], 10, 64)
		if err != nil {
			return "", nil, 0, 0, err
		}
	}
	name := left
	var labels []Label
	if i := strings.IndexByte(left, '{'); i >= 0 {
		if !strings.HasSuffix(left, "}") {
			return "", nil, 0, 0, fmt.Errorf("bad labels")
		}
		name = left[:i]
		labels, err = parseLabels(left[i+1 : len(left)-1])
		if err != nil {
			return "", nil, 0, 0, err
		}
	}
	return name, labels, value, ts, nil
}

func metricValueSplit(line string) int {
	inLabels := false
	inQuote := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' && inLabels {
			inQuote = !inQuote
			continue
		}
		if !inQuote {
			if r == '{' {
				inLabels = true
				continue
			}
			if r == '}' {
				inLabels = false
				continue
			}
			if !inLabels && (r == ' ' || r == '\t') {
				return i
			}
		}
	}
	return -1
}

func parseLabels(s string) ([]Label, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out := []Label{}
	for len(s) > 0 {
		s = strings.TrimSpace(s)
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("bad label")
		}
		key := strings.TrimSpace(s[:eq])
		s = strings.TrimSpace(s[eq+1:])
		if len(s) == 0 || s[0] != '"' {
			return nil, fmt.Errorf("label value must be quoted")
		}
		s = s[1:]
		var b strings.Builder
		escaped := false
		end := -1
		for i, r := range s {
			if escaped {
				switch r {
				case 'n':
					b.WriteByte('\n')
				case '\\', '"':
					b.WriteRune(r)
				default:
					b.WriteRune(r)
				}
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				end = i
				break
			}
			b.WriteRune(r)
		}
		if end < 0 {
			return nil, fmt.Errorf("unterminated label")
		}
		out = append(out, Label{Key: key, Value: b.String()})
		s = s[end+1:]
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		if s[0] != ',' {
			return nil, fmt.Errorf("expected comma")
		}
		s = s[1:]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
