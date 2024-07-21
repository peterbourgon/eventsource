package eventsource

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"
)

// A Decoder reads and decodes EventSource events from an input stream.
type Decoder struct {
	r *bufio.Reader

	checkedBOM bool
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r: bufio.NewReader(r),
	}
}

func (d *Decoder) checkBOM() {
	r, _, err := d.r.ReadRune()

	if err != nil {
		return // let other other callers handle this
	}

	if r != 0xFEFF { // utf8 byte order mark
		d.r.UnreadRune()
	}

	d.checkedBOM = true
}

var colon = []byte{':'}

// ReadField reads a single line from the stream and parses it as a field. A
// complete event is signalled by an empty key and value. The returned error
// may either be an error from the stream, or an ErrInvalidEncoding if the
// value is not valid UTF-8.
func (d *Decoder) ReadField() (field string, value []byte, err error) {
	if !d.checkedBOM {
		d.checkBOM()
	}

	var buf []byte

	for {
		line, isPrefix, err := d.r.ReadLine()

		if err != nil {
			return "", nil, err
		}

		buf = append(buf, line...)

		if !isPrefix {
			break
		}
	}

	if len(buf) == 0 {
		return "", nil, nil
	}

	f, v, _ := bytes.Cut(buf, colon)

	// §7. If value starts with a U+0020 SPACE character, remove it from value.
	if len(v) > 0 && v[0] == ' ' {
		v = v[1:]
	}

	fstr := string(f)

	if !utf8.ValidString(fstr) || !utf8.Valid(v) {
		return "", nil, ErrInvalidEncoding
	}

	return fstr, v, nil
}

// Decode reads the next event from its input and stores it in the provided
// Event pointer.
func (d *Decoder) Decode(e *Event) error {
	var wroteData bool

	e.Type = "message" // set default event type

	for {
		field, value, err := d.ReadField()
		if err != nil {
			return fmt.Errorf("read field: %w", err)
		}

		if len(field) == 0 && len(value) == 0 {
			break
		}

		switch field {
		case "id":
			e.ID = string(value)
			if len(e.ID) == 0 {
				e.ResetID = true
			}

		case "retry":
			e.Retry = string(value)

		case "event":
			e.Type = string(value)

		case "data":
			if wroteData {
				e.Data = append(e.Data, '\n')
			} else {
				wroteData = true
			}
			e.Data = append(e.Data, value...)
		}
	}

	return nil
}
