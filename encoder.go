package eventsource

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"
)

// FlushWriter groups Write and Flush.
type FlushWriter interface {
	io.Writer
	Flush()
}

type noopFlusher struct {
	io.Writer
}

func (noopFlusher) Flush() {}

// Encoder writes EventSource events to an output stream.
type Encoder struct {
	w FlushWriter
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	if w, ok := w.(FlushWriter); ok {
		return &Encoder{w}
	}
	return &Encoder{noopFlusher{w}}
}

var newline = []byte{'\n'}

// Flush an empty line to signal event is complete, and flush the writer.
func (e *Encoder) Flush() error {
	_, err := e.w.Write(newline)
	e.w.Flush()
	return err
}

// WriteField writes an event field to the connection. If the provided value
// contains newlines, multiple fields will be emitted. If the returned error is
// not nil, it will be either ErrInvalidEncoding or an error from the
// connection.
func (e *Encoder) WriteField(field string, value []byte) error {
	if !utf8.ValidString(field) || !utf8.Valid(value) {
		return ErrInvalidEncoding
	}

	for _, line := range bytes.Split(value, newline) {
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		if err := e.writeField(field, line); err != nil {
			return fmt.Errorf("write field: %w", err)
		}
	}

	return nil
}

func (e *Encoder) writeField(field string, value []byte) error {
	if len(value) == 0 {
		_, err := fmt.Fprintf(e.w, "%s\n", field)
		return err
	}

	_, err := fmt.Fprintf(e.w, "%s: %s\n", field, value)
	return err
}

// Encode writes an event to the connection.
func (e *Encoder) Encode(event Event) error {
	if event.ResetID || len(event.ID) > 0 {
		if err := e.WriteField("id", []byte(event.ID)); err != nil {
			return err
		}
	}

	if len(event.Retry) > 0 {
		if err := e.WriteField("retry", []byte(event.Retry)); err != nil {
			return err
		}
	}

	if len(event.Type) > 0 {
		if err := e.WriteField("event", []byte(event.Type)); err != nil {
			return err
		}
	}

	if err := e.WriteField("data", event.Data); err != nil {
		return err
	}

	return e.Flush()
}
