package metriccodec

// This file implements a compact Protocol Buffers wire-compatible encoding
// without generated code or external dependencies. The schema is documented
// in protocol/metric_batch.proto. Unknown fields are skipped on decode.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const protobufProtocolVersion = 2

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

func Encode(b Batch) ([]byte, error) {
	stringsSet := map[string]struct{}{}
	for _, d := range b.Definitions {
		stringsSet[d.Name] = struct{}{}
		for _, l := range d.Labels {
			stringsSet[l.Key] = struct{}{}
			stringsSet[l.Value] = struct{}{}
		}
	}
	dict := make([]string, 0, len(stringsSet))
	for s := range stringsSet {
		dict = append(dict, s)
	}
	sort.Strings(dict)
	ids := make(map[string]uint32, len(dict))
	for i, s := range dict {
		ids[s] = uint32(i + 1)
	}

	out := make([]byte, 0, 128+len(b.Samples)*14)
	out = appendVarintField(out, 1, protobufProtocolVersion)
	out = appendBytesField(out, 2, b.SenderID[:])
	out = appendVarintField(out, 3, b.Sequence)
	out = appendVarintField(out, 4, uint64(b.CreatedAt.UnixMilli()))
	if b.Full {
		out = appendVarintField(out, 5, 1)
	}
	for _, s := range dict {
		out = appendBytesField(out, 6, []byte(s))
	}
	for _, d := range b.Definitions {
		if d.WireID == 0 {
			return nil, errors.New("definition wire_id must be non-zero")
		}
		msg := make([]byte, 0, 32+len(d.Labels)*8)
		msg = appendVarintField(msg, 1, uint64(d.WireID))
		msg = appendBytesField(msg, 2, d.ID[:])
		msg = appendVarintField(msg, 3, uint64(ids[d.Name]))
		for _, l := range d.Labels {
			lr := make([]byte, 0, 12)
			lr = appendVarintField(lr, 1, uint64(ids[l.Key]))
			lr = appendVarintField(lr, 2, uint64(ids[l.Value]))
			msg = appendBytesField(msg, 4, lr)
		}
		out = appendBytesField(out, 7, msg)
	}
	for _, s := range b.Samples {
		if s.WireID == 0 {
			return nil, errors.New("sample wire_id must be non-zero")
		}
		msg := make([]byte, 0, 24)
		msg = appendVarintField(msg, 1, uint64(s.WireID))
		msg = appendFixed64Field(msg, 2, math.Float64bits(s.Value))
		if s.TimestampMS != 0 {
			msg = appendVarintField(msg, 3, uint64(s.TimestampMS))
		}
		out = appendBytesField(out, 8, msg)
	}
	for _, id := range b.Deleted {
		if id != 0 {
			out = appendVarintField(out, 9, uint64(id))
		}
	}
	return out, nil
}

func Decode(data []byte) (Batch, error) {
	var b Batch
	var dict []string
	for len(data) > 0 {
		field, wt, n, err := consumeTag(data)
		if err != nil {
			return b, err
		}
		data = data[n:]
		switch field {
		case 1:
			v, m, err := consumeVarintType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			if v != protobufProtocolVersion {
				return b, fmt.Errorf("unsupported protobuf batch version %d", v)
			}
		case 2:
			v, m, err := consumeBytesType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			if len(v) != 16 {
				return b, errors.New("sender_id must be 16 bytes")
			}
			copy(b.SenderID[:], v)
		case 3:
			v, m, err := consumeVarintType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			b.Sequence = v
		case 4:
			v, m, err := consumeVarintType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			b.CreatedAt = time.UnixMilli(int64(v))
		case 5:
			v, m, err := consumeVarintType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			b.Full = v != 0
		case 6:
			v, m, err := consumeBytesType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			if len(dict) >= 1_000_000 {
				return b, errors.New("dictionary limit exceeded")
			}
			dict = append(dict, string(v))
		case 7:
			v, m, err := consumeBytesType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			d, err := decodeDefinition(v, dict)
			if err != nil {
				return b, err
			}
			b.Definitions = append(b.Definitions, d)
		case 8:
			v, m, err := consumeBytesType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			s, err := decodeSample(v)
			if err != nil {
				return b, err
			}
			b.Samples = append(b.Samples, s)
		case 9:
			v, m, err := consumeVarintType(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
			b.Deleted = append(b.Deleted, uint32(v))
		default:
			m, err := skipValue(data, wt)
			if err != nil {
				return b, err
			}
			data = data[m:]
		}
		if len(b.Definitions) > 2_000_000 || len(b.Samples) > 5_000_000 || len(b.Deleted) > 2_000_000 {
			return b, errors.New("batch count limit exceeded")
		}
	}
	return b, nil
}

func decodeDefinition(data []byte, dict []string) (Definition, error) {
	var d Definition
	for len(data) > 0 {
		f, wt, n, err := consumeTag(data)
		if err != nil {
			return d, err
		}
		data = data[n:]
		switch f {
		case 1:
			v, m, e := consumeVarintType(data, wt)
			if e != nil {
				return d, e
			}
			data = data[m:]
			d.WireID = uint32(v)
		case 2:
			v, m, e := consumeBytesType(data, wt)
			if e != nil {
				return d, e
			}
			data = data[m:]
			if len(v) != 16 {
				return d, errors.New("series_id must be 16 bytes")
			}
			copy(d.ID[:], v)
		case 3:
			v, m, e := consumeVarintType(data, wt)
			if e != nil {
				return d, e
			}
			data = data[m:]
			if v == 0 || v > uint64(len(dict)) {
				return d, errors.New("bad metric name dictionary id")
			}
			d.Name = dict[v-1]
		case 4:
			v, m, e := consumeBytesType(data, wt)
			if e != nil {
				return d, e
			}
			data = data[m:]
			l, e := decodeLabelRef(v, dict)
			if e != nil {
				return d, e
			}
			d.Labels = append(d.Labels, l)
		default:
			m, e := skipValue(data, wt)
			if e != nil {
				return d, e
			}
			data = data[m:]
		}
	}
	if d.WireID == 0 || d.Name == "" {
		return d, errors.New("incomplete definition")
	}
	return d, nil
}
func decodeLabelRef(data []byte, dict []string) (Label, error) {
	var l Label
	var ki, vi uint64
	for len(data) > 0 {
		f, wt, n, e := consumeTag(data)
		if e != nil {
			return l, e
		}
		data = data[n:]
		switch f {
		case 1:
			ki, n, e = consumeVarintType(data, wt)
			if e != nil {
				return l, e
			}
			data = data[n:]
		case 2:
			vi, n, e = consumeVarintType(data, wt)
			if e != nil {
				return l, e
			}
			data = data[n:]
		default:
			n, e = skipValue(data, wt)
			if e != nil {
				return l, e
			}
			data = data[n:]
		}
	}
	if ki == 0 || vi == 0 || ki > uint64(len(dict)) || vi > uint64(len(dict)) {
		return l, errors.New("bad label dictionary id")
	}
	l.Key = dict[ki-1]
	l.Value = dict[vi-1]
	return l, nil
}
func decodeSample(data []byte) (Sample, error) {
	var s Sample
	for len(data) > 0 {
		f, wt, n, e := consumeTag(data)
		if e != nil {
			return s, e
		}
		data = data[n:]
		switch f {
		case 1:
			v, m, e := consumeVarintType(data, wt)
			if e != nil {
				return s, e
			}
			data = data[m:]
			s.WireID = uint32(v)
		case 2:
			if wt != wireFixed64 || len(data) < 8 {
				return s, errors.New("invalid sample value")
			}
			s.Value = math.Float64frombits(binary.LittleEndian.Uint64(data[:8]))
			data = data[8:]
		case 3:
			v, m, e := consumeVarintType(data, wt)
			if e != nil {
				return s, e
			}
			data = data[m:]
			s.TimestampMS = int64(v)
		default:
			m, e := skipValue(data, wt)
			if e != nil {
				return s, e
			}
			data = data[m:]
		}
	}
	if s.WireID == 0 {
		return s, errors.New("sample wire_id must be non-zero")
	}
	return s, nil
}

func appendTag(dst []byte, field int, wt int) []byte { return appendUvarint(dst, uint64(field<<3|wt)) }
func appendVarintField(dst []byte, field int, v uint64) []byte {
	dst = appendTag(dst, field, wireVarint)
	return appendUvarint(dst, v)
}
func appendBytesField(dst []byte, field int, v []byte) []byte {
	dst = appendTag(dst, field, wireBytes)
	dst = appendUvarint(dst, uint64(len(v)))
	return append(dst, v...)
}
func appendFixed64Field(dst []byte, field int, v uint64) []byte {
	dst = appendTag(dst, field, wireFixed64)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return append(dst, b[:]...)
}
func appendUvarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}
func consumeTag(data []byte) (int, int, int, error) {
	v, n, e := consumeUvarint(data)
	if e != nil {
		return 0, 0, 0, e
	}
	return int(v >> 3), int(v & 7), n, nil
}
func consumeUvarint(data []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < 10; i++ {
		if i >= len(data) {
			return 0, 0, errors.New("truncated varint")
		}
		c := data[i]
		if c < 0x80 {
			return v | uint64(c)<<uint(7*i), i + 1, nil
		}
		v |= uint64(c&0x7f) << uint(7*i)
	}
	return 0, 0, errors.New("varint overflow")
}
func consumeVarintType(data []byte, wt int) (uint64, int, error) {
	if wt != wireVarint {
		return 0, 0, errors.New("wrong protobuf wire type")
	}
	return consumeUvarint(data)
}
func consumeBytesType(data []byte, wt int) ([]byte, int, error) {
	if wt != wireBytes {
		return nil, 0, errors.New("wrong protobuf wire type")
	}
	n, m, e := consumeUvarint(data)
	if e != nil {
		return nil, 0, e
	}
	if n > uint64(len(data)-m) {
		return nil, 0, errors.New("truncated bytes field")
	}
	return data[m : m+int(n)], m + int(n), nil
}
func skipValue(data []byte, wt int) (int, error) {
	switch wt {
	case wireVarint:
		_, n, e := consumeUvarint(data)
		return n, e
	case wireFixed64:
		if len(data) < 8 {
			return 0, errors.New("truncated fixed64")
		}
		return 8, nil
	case wireBytes:
		_, n, e := consumeBytesType(data, wt)
		return n, e
	case wireFixed32:
		if len(data) < 4 {
			return 0, errors.New("truncated fixed32")
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported wire type %d", wt)
	}
}
