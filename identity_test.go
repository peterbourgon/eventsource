package eventsource

import (
	"io"
	"reflect"
	"strconv"
	"testing"
)

func randomEvents() []Event {
	events := make([]Event, 1000)

	for i := 0; i < 1000; i++ {
		event := Event{Type: "custom"}

		if i%3 == 0 {
			event.Type = "other"
		}

		if i%5 == 0 {
			event.Retry = strconv.FormatInt(int64(i*1000), 10)
		}

		if i%10 == 0 {
			event.ResetID = true
		} else {
			event.ID = strconv.FormatInt(int64(i), 10)
		}

		if i%20 != 0 {
			event.Data = []byte(strconv.FormatInt(int64(i), 10))
		}

		events[i] = event
	}

	return events
}

func TestEncodeDecodeIdentity(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	d, e := NewDecoder(r), NewEncoder(w)

	in := randomEvents()

	go func() {
		defer w.Close()
		for _, event := range in {
			if err := e.Encode(event); err != nil {
				t.Error(err)
			}
		}
	}()

	out := make([]Event, 0, len(in))

	for {
		var e Event
		if d.Decode(&e) != nil {
			break
		}
		out = append(out, e)
	}

	if !reflect.DeepEqual(in, out) {
		t.Fatal("output does not match input")
	}
}
