package eventsource

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

var longline = string(bytes.Repeat([]byte{'a'}, 4096))

func TestDecoderReadField(t *testing.T) {
	for i, tt := range []struct {
		in    string
		field string
		value []byte
		err   error
	}{
		{in: "\n"},
		{in: "id", field: "id"},
		{in: "id:", field: "id"},
		{in: "id:1", field: "id", value: []byte("1")},
		{in: "id: 1", field: "id", value: []byte("1")},
		{in: "data: " + longline, field: "data", value: []byte(longline)},
		{in: "\xFF\xFE\xFD", err: ErrInvalidEncoding},
		{in: "data: \xFF\xFE\xFD", err: ErrInvalidEncoding},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			dec := NewDecoder(bytes.NewBufferString(tt.in))
			field, value, err := dec.ReadField()
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("error: want %q, have %q", tt.err, err)
			}
			if tt.err == nil && err != nil {
				t.Fatalf("error: %v", err)
			}
			if want, have := tt.field, field; want != have {
				t.Errorf("field: want %q, have %q", want, have)
			}
			if want, have := tt.value, value; !bytes.Equal(want, have) {
				t.Errorf("value: want %q, have %q", want, have)
			}
		})
	}
}

func TestDecoderDecode(t *testing.T) {
	table := []struct {
		in  string
		out Event
	}{
		{"event: type\ndata\n\n", Event{Type: "type"}},
		{"id: 123\ndata\n\n", Event{Type: "message", ID: "123"}},
		{"retry: 10000\ndata\n\n", Event{Type: "message", Retry: "10000"}},
		{"data: data\n\n", Event{Type: "message", Data: []byte("data")}},
		{"id\ndata\n\n", Event{Type: "message", ResetID: true}},
	}

	for i, tt := range table {
		dec := NewDecoder(bytes.NewBufferString(tt.in))

		var event Event
		if err := dec.Decode(&event); err != nil {
			t.Errorf("%d. %s", i, err)
			continue
		}

		if !reflect.DeepEqual(event, tt.out) {
			t.Errorf("%d. expected %#v, got %#v", i, tt.out, event)
		}
	}
}
