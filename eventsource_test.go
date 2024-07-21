package eventsource

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestEventSource200NoContentType(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	retry := time.Millisecond
	es := New(req, retry)

	es.connect()

	if es.err == nil {
		t.Fatalf("200 OK with no Content-Type should have errored, but didn't")
	}
}

func TestEventSourceHeaders(t *testing.T) {
	headers := make(chan http.Header, 1)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers <- r.Header
		}),
	)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	retry := time.Duration(-1)
	es := New(req, retry)

	es.connect()

	h := <-headers

	if want, have := "text/event-stream", h.Get("accept"); want != have {
		t.Errorf("Accept: want %q, have %q", want, have)
	}
	if want, have := "no-cache", h.Get("cache-control"); want != have {
		t.Errorf("Cache-Control: want %q, have %q", want, have)
	}
}

func TestEventSource204(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(204)
		}),
	)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	retry := time.Millisecond
	es := New(req, retry)

	es.connect()
	if es.err == nil {
		t.Fatalf("204 No Content should have errored, but didn't")
	}
}

func TestEventSourceEmphemeral500(t *testing.T) {
	fail := true

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail {
				w.WriteHeader(500)
				fail = false
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
		}),
	)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	retry := time.Millisecond
	es := New(req, retry)

	es.connect()
	if es.err != nil {
		t.Fatalf("500 Internal Server Error should have reconnected, but didn't; error: %v", es.err)
	}
}

func TestEventSourceRead(t *testing.T) {
	fail := make(chan struct{})
	more := make(chan bool, 1)
	server := testServer(func(w responseWriter, r *http.Request) {
		select {
		case <-fail:
			w.WriteHeader(204)
			return
		default:
			//
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		var id int

		if lastID := r.Header.Get("Last-Event-Id"); lastID != "" {
			if i, err := strconv.ParseInt(lastID, 10, 64); err == nil {
				id = int(i) + 1
			}
		}

		for {
			if !<-more {
				break
			}
			fmt.Fprintf(w, "id: %d\ndata: message %d\n\n", id, id)
			w.Flush()
			id++
		}
	})
	defer server.Close()
	defer close(more)

	es := New(request(server.URL), 10*time.Millisecond)
	more <- true

	event, err := es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if event.ID != "0" {
		t.Fatalf("expected id = 0, got %s", event.ID)
	}

	if event.Type != "message" {
		t.Fatalf("expected event = message, got %s", event.Type)
	}

	if !bytes.Equal([]byte("message 0"), event.Data) {
		t.Fatalf("expected data = message 0, got %s", event.Data)
	}

	more <- true
	event, err = es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if event.ID != "1" {
		t.Fatalf("expected id = 1, got %s", event.ID)
	}

	if event.Type != "message" {
		t.Fatalf("expected event = message, got %s", event.Type)
	}

	if !bytes.Equal([]byte("message 1"), event.Data) {
		t.Fatalf("expected data = message 1, got %s", event.Data)
	}

	more <- false // stop handler
	more <- true  // start handler

	event, err = es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if event.ID != "2" {
		t.Fatalf("expected id = 2, got %s", event.ID)
	}

	if event.Type != "message" {
		t.Fatalf("expected event = message, got %s", event.Type)
	}

	if !bytes.Equal([]byte("message 2"), event.Data) {
		t.Fatalf("expected data = message 2, got %s", event.Data)
	}

	more <- false // stop handler
	close(fail)

	if _, err := es.Read(); err == nil {
		t.Fatal("expected fatal err")
	}
}

func TestEventSourceChangeRetry(t *testing.T) {
	server := testServer(func(w responseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		NewEncoder(w).Encode(Event{
			Retry: "10000",
			Data:  []byte("foo"),
		})
	})

	defer server.Close()

	es := New(request(server.URL), -1)

	event, err := es.Read()
	if err != nil {
		t.Fatal(err)
	}
	if event.Retry != "10000" {
		t.Error("event retry not set")
	}
	if es.retry != (10 * time.Second) {
		t.Fatal("expected retry to be updated, but wasn't")
	}
}

func TestEventSourceBOM(t *testing.T) {
	server := testServer(func(w responseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		w.Write([]byte("\xEF\xBB\xBF"))
		NewEncoder(w).Encode(Event{Type: "custom", Data: []byte("foo")})
	})
	defer server.Close()

	es := New(request(server.URL), -1)

	event, err := es.Read()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(event, Event{Type: "custom", Data: []byte("foo")}) {
		t.Fatal("message was unsuccessfully decoded with BOM")
	}
}

type responseWriter interface {
	http.ResponseWriter
	http.Flusher
}

func testServer(f func(responseWriter, *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f(w.(responseWriter), r)
	}))
}

func request(url string) *http.Request {
	req, _ := http.NewRequest("GET", url, nil)
	return req
}
