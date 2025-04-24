package eventsource

import (
	"bufio"
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
	FlushWriter
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
	_, err := e.FlushWriter.Write(newline)
	e.FlushWriter.Flush()
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
		_, err := fmt.Fprintf(e.FlushWriter, "%s\n", field)
		return err
	}

	_, err := fmt.Fprintf(e.FlushWriter, "%s: %s\n", field, value)
	return err
}

// WriteStream writes an event field to the connection. The stream is expected
// to contain valid UTF-8. If it contains newlines, the field will be emitted
// multiple times. Invalid UTF-8 will be coerced into valid UTF-8 using
// unicode replacement character.
func (e *Encoder) WriteStream(field string, stream io.Reader) error {
	if !utf8.ValidString(field) {
		return ErrInvalidEncoding
	}

	scanner := bufio.NewScanner(stream)
	scanner.Split(scanLinesOrValidUTF8)

	writePrefix := true
	writtenBytes := 0
	endsWithLinebreak := false

	for scanner.Scan() {
		var (
			n    int
			err  error
			data = scanner.Bytes()
		)

		hasLineBreak := len(data) > 0 && data[len(data)-1] == '\n'
		if hasLineBreak {
			data = dropCR(data[:len(data)-1])
		}
		if writePrefix {
			if len(data) > 0 {
				n, err = fmt.Fprintf(e.FlushWriter, "%s: ", field)
			} else {
				n, err = fmt.Fprintf(e.FlushWriter, "%s", field)
			}
			if err != nil {
				return err
			}
			writePrefix = false
			writtenBytes += n
		}
		n, err = e.FlushWriter.Write(data)
		if err != nil {
			return err
		}
		writtenBytes += n

		if hasLineBreak {
			n, err = e.FlushWriter.Write(newline)
			if err != nil {
				return err
			}
			writePrefix = true
			writtenBytes += n
		}
		endsWithLinebreak = hasLineBreak && len(data) > 0
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return scanErr
	}

	if !writePrefix {
		_, err := e.FlushWriter.Write(newline)
		return err
	}

	if writtenBytes == 0 || endsWithLinebreak {
		return e.writeField(field, nil)
	}

	return nil
}

func scanLinesOrValidUTF8(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// If we're at EOF and have no data, we're done
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// First, handle line breaks if present
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		// We have a full newline-terminated line
		advance = i + 1
		token = validateUTF8Token(data[:i+1])

		return advance, token, nil
	}

	// If we're at EOF, we have a final, non-terminated line
	if atEOF {
		return len(data), validateUTF8Token(data), nil
	}

	// We're not at EOF and we don't have a complete line.
	// If the buffer is full, we need to return what we have, but
	// ensure we don't split in the middle of a UTF-8 sequence.

	// Find the last position where we can safely cut
	safePos := findLastCompleteUTF8Position(data)

	// If the buffer is almost full, return what we can safely process
	if safePos > 0 && safePos <= len(data) {
		return safePos, validateUTF8Token(data[:safePos]), nil
	}

	// Request more data
	return 0, nil, nil
}

// findLastCompleteUTF8Position finds the last position in the data
// where we can safely cut without breaking a UTF-8 sequence
func findLastCompleteUTF8Position(data []byte) int {
	// Start with the full data length
	n := len(data)
	lookback := 0

	// look at last few bytes to determine whether we have a valid rune
	// no need to look at the rest, as we'll replace invalid data anyway
	for n > 0 && lookback < utf8.UTFMax {
		r, size := utf8.DecodeLastRune(data[:n])

		// If we got a valid rune, we can cut here
		if r != utf8.RuneError || size > 1 {
			// We found a valid last rune or a multi-byte sequence that
			// produced RuneError (which means it's an expected error)
			break
		}

		// Otherwise, we have an invalid single byte
		// Keep going backwards
		n--
		lookback++
	}

	// don't consume last '\r', it could be followed by an '\n'
	// which we might want to drop
	if n > 0 && data[n-1] == '\r' {
		return n - 1
	}

	// the returned chunk still needs UTF-8 validation, but if it's invalid
	// it's not because of split reads
	return n
}

// validateUTF8Token ensures the data contains only valid UTF-8,
// replacing invalid sequences with the UTF-8 replacement character
func validateUTF8Token(data []byte) []byte {
	// If the data is already valid UTF-8, just return it
	if utf8.Valid(data) {
		return data
	}

	return bytes.ToValidUTF8(data, []byte("\uFFFD"))
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
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
