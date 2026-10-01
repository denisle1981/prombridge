package protocol

import "testing"

func TestRoundTrip(t *testing.T) {
	in := Packet{MessageID: 7, ChunkIndex: 1, ChunkCount: 3, TotalSize: 99, UnixMilli: 123, Payload: []byte("hello")}
	b, err := Encode(in, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(b, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageID != in.MessageID || string(out.Payload) != "hello" {
		t.Fatalf("bad roundtrip: %#v", out)
	}
	if _, err := Decode(b, []byte("wrong")); err == nil {
		t.Fatal("expected hmac error")
	}
}
