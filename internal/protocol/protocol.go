package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Magic      = "PUR3"
	Version    = uint8(3)
	HeaderSize = 4 + 1 + 1 + 8 + 2 + 2 + 4 + 8 + sha256.Size
	MaxChunks  = 65535
)

type Packet struct {
	Compression            uint8
	MessageID              uint64
	ChunkIndex, ChunkCount uint16
	TotalSize              uint32
	UnixMilli              int64
	Payload                []byte
}

func Encode(p Packet, secret []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errors.New("secret must not be empty")
	}
	if p.ChunkCount == 0 || p.ChunkIndex >= p.ChunkCount {
		return nil, errors.New("invalid chunk fields")
	}
	b := make([]byte, HeaderSize+len(p.Payload))
	copy(b[:4], Magic)
	b[4] = Version
	b[5] = p.Compression
	binary.BigEndian.PutUint64(b[6:14], p.MessageID)
	binary.BigEndian.PutUint16(b[14:16], p.ChunkIndex)
	binary.BigEndian.PutUint16(b[16:18], p.ChunkCount)
	binary.BigEndian.PutUint32(b[18:22], p.TotalSize)
	binary.BigEndian.PutUint64(b[22:30], uint64(p.UnixMilli))
	copy(b[HeaderSize:], p.Payload)
	m := hmac.New(sha256.New, secret)
	m.Write(b[:30])
	m.Write(p.Payload)
	copy(b[30:HeaderSize], m.Sum(nil))
	return b, nil
}
func Decode(b, secret []byte) (Packet, error) {
	var p Packet
	if len(b) < HeaderSize {
		return p, errors.New("packet too short")
	}
	if string(b[:4]) != Magic {
		return p, errors.New("invalid magic")
	}
	if b[4] != Version {
		return p, fmt.Errorf("unsupported version %d", b[4])
	}
	m := hmac.New(sha256.New, secret)
	m.Write(b[:30])
	m.Write(b[HeaderSize:])
	if !hmac.Equal(b[30:HeaderSize], m.Sum(nil)) {
		return p, errors.New("invalid hmac")
	}
	p.Compression = b[5]
	p.MessageID = binary.BigEndian.Uint64(b[6:14])
	p.ChunkIndex = binary.BigEndian.Uint16(b[14:16])
	p.ChunkCount = binary.BigEndian.Uint16(b[16:18])
	p.TotalSize = binary.BigEndian.Uint32(b[18:22])
	p.UnixMilli = int64(binary.BigEndian.Uint64(b[22:30]))
	p.Payload = append([]byte(nil), b[HeaderSize:]...)
	if p.ChunkCount == 0 || p.ChunkIndex >= p.ChunkCount {
		return Packet{}, errors.New("invalid chunks")
	}
	return p, nil
}
func CompressionCode(s string) (uint8, error) {
	switch s {
	case "none":
		return 0, nil
	case "gzip-fast":
		return 1, nil
	case "gzip", "gzip-best":
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported compression %q", s)
	}
}
func CompressionName(c uint8) (string, error) {
	switch c {
	case 0:
		return "none", nil
	case 1:
		return "gzip-fast", nil
	case 2:
		return "gzip-best", nil
	default:
		return "", fmt.Errorf("unsupported compression code %d", c)
	}
}
