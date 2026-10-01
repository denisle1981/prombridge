package compression

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

func Compress(name string, raw []byte) ([]byte, error) {
	switch name {
	case "none":
		return append([]byte(nil), raw...), nil
	case "gzip", "gzip-best":
		return compressGzip(raw, gzip.BestCompression)
	case "gzip-fast":
		return compressGzip(raw, gzip.BestSpeed)
	default:
		return nil, fmt.Errorf("unsupported compression %q", name)
	}
}

func compressGzip(raw []byte, level int) ([]byte, error) {
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, level)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(raw); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func Decompress(name string, data []byte, max int64) ([]byte, error) {
	switch name {
	case "none":
		if int64(len(data)) > max {
			return nil, fmt.Errorf("payload too large")
		}
		return append([]byte(nil), data...), nil
	case "gzip", "gzip-fast", "gzip-best":
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		out, err := io.ReadAll(io.LimitReader(r, max+1))
		if err != nil {
			return nil, err
		}
		if int64(len(out)) > max {
			return nil, fmt.Errorf("payload too large")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported compression %q", name)
	}
}
