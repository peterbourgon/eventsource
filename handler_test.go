package eventsource

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

var emptyHandler Handler = func(string, *Encoder, <-chan bool) {}

type testCloseNotifier struct {
	closed chan bool
	http.ResponseWriter
}

func (n testCloseNotifier) Close() {
	n.closed <- true
}

func (n testCloseNotifier) CloseNotify() <-chan bool {
	return n.closed
}

func TestHandlerAcceptable(t *testing.T) {
	t.Parallel()

	table := []struct {
		accept string
		result bool
	}{
		{"", true},
		{"text/event-stream", true},
		{"text/*", true},
		{"*/*", true},
		{"text/event-stream; q=1.0", true},
		{"text/*; q=1.0", true},
		{"*/*; q=1.0", true},
		{"text/html; q=1.0, text/*; q=0.8", true},
		{"text/html; q=1.0, image/gif; q=0.6, image/jpeg; q=0.6", false},
	}

	for i, tt := range table {
		if exp, got := tt.result, emptyHandler.acceptable(tt.accept); exp != got {
			t.Errorf("%d. expected acceptable(%q) == %t, got %t", i, tt.accept, exp, got)
		}
	}
}

func TestHandlerValidatesAcceptHeader(t *testing.T) {
	t.Parallel()

	w, r := httptest.NewRecorder(), &http.Request{Header: map[string][]string{
		"Accept": {"text/html"},
	}}
	emptyHandler.ServeHTTP(w, r)

	if w.Code != http.StatusNotAcceptable {
		t.Fatal("handler did not set 406 status")
	}
}

func TestHandlerSetsContentType(t *testing.T) {
	t.Parallel()

	w, r := httptest.NewRecorder(), &http.Request{Header: map[string][]string{
		"Accept": {"text/event-stream"},
	}}
	emptyHandler.ServeHTTP(w, r)

	if w.Result().Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("handler did not set appropriate content type")
	}

	if w.Code != http.StatusOK {
		t.Fatal("handler did not set 200 status")
	}
}

func TestHandlerEncode(t *testing.T) {
	t.Parallel()

	handler := func(_ string, enc *Encoder, _ <-chan bool) {
		enc.Encode(Event{Data: []byte("hello")})
	}

	w, r := httptest.NewRecorder(), &http.Request{Header: map[string][]string{
		"Accept": {"text/event-stream"},
	}}

	Handler(handler).ServeHTTP(w, r)

	var event Event
	NewDecoder(w.Body).Decode(&event)

	if !reflect.DeepEqual(event, Event{Type: "message", Data: []byte("hello")}) {
		t.Error("unexpected handler output")
	}
}

func TestHandlerCloseNotify(t *testing.T) {
	t.Parallel()

	done := make(chan bool, 1)
	handler := func(_ string, _ *Encoder, stop <-chan bool) {
		<-stop
		done <- true
	}

	w, r := httptest.NewRecorder(), &http.Request{Header: map[string][]string{
		"Accept": {"text/event-stream"},
	}}
	closer := testCloseNotifier{make(chan bool, 1), w}
	go Handler(handler).ServeHTTP(closer, r)

	closer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("handler was not notified of close")
	}
}
