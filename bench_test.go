package eventsource_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/peterbourgon/eventsource"
)

func BenchmarkEncoder(b *testing.B) {
	event := eventsource.Event{
		ID:   "my-event-id",
		Type: "EventTypeFoo",
		Data: []byte(`my event data goes here`),
	}

	enc := eventsource.NewEncoder(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := enc.Encode(event); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoder(b *testing.B) {
	buf := &bytes.Buffer{}
	enc := eventsource.NewEncoder(buf)
	if err := enc.Encode(eventsource.Event{
		ID:   "my-event-id",
		Type: "EventTypeFoo",
		Data: []byte(`my event data goes here`),
	}); err != nil {
		b.Fatalf("encode event: %v", err)
	}

	r := &infiniteReader{data: buf.Bytes()}
	dec := eventsource.NewDecoder(r)
	ev := eventsource.Event{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := dec.Decode(&ev); err != nil {
			b.Fatal(err)
		}
	}

	b.SetBytes(int64(buf.Len()))
}

type infiniteReader struct {
	data   []byte
	cursor int
}

func (ir *infiniteReader) Read(p []byte) (n int, err error) {
	for n < len(p) {
		p[n] = ir.data[ir.cursor]
		ir.cursor = (ir.cursor + 1) % len(ir.data)
		n = n + 1
	}
	return n, nil
}
