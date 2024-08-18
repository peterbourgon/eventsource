// Package eventsource provides the building blocks for consuming and building
// EventSource services.
package eventsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

var (
	// ErrClosed indicates the event source has been permanently closed.
	ErrClosed = errors.New("closed")

	// ErrInvalidEncoding indicates invalid UTF-8 event data.
	ErrInvalidEncoding = errors.New("invalid UTF-8 sequence")
)

// Event can be written to an event stream, and read from an event source.
type Event struct {
	Type    string
	ID      string
	Retry   string
	Data    []byte
	ResetID bool
}

// EventSource consumes server sent events over HTTP with automatic recovery.
type EventSource struct {
	client      HTTPClient
	request     *http.Request
	retry       time.Duration
	err         error
	r           io.ReadCloser
	dec         *Decoder
	lastEventID string
}

// New calls NewConfig with the provided request and retry interval, and a
// default HTTP client.
func New(req *http.Request, retry time.Duration) *EventSource {
	return NewConfig(Config{
		Request: req,
		Retry:   retry,
	})
}

// Config enumerates the input parameters for an EventSource client.
type Config struct {
	Client  HTTPClient
	Request *http.Request
	Retry   time.Duration
}

// HTTPClient models an [http.Client].
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// NewConfig prepares an EventSource client. The connection is automatically
// managed, using req to connect, and retrying from recoverable errors after
// waiting the provided retry duration.
func NewConfig(config Config) *EventSource {
	if config.Client == nil {
		config.Client = http.DefaultClient
	}

	if config.Retry <= 0 {
		config.Retry = time.Second
	}

	config.Request.Header.Set("Accept", "text/event-stream")
	config.Request.Header.Set("Cache-Control", "no-cache")

	return &EventSource{
		client:  config.Client,
		retry:   config.Retry,
		request: config.Request,
	}
}

// Close the source. Any further calls to Read() will return ErrClosed.
func (es *EventSource) Close() {
	if es.r != nil {
		es.r.Close()
	}
	es.err = ErrClosed
}

// Connect to an event source, validate the response, and gracefully handle
// reconnects.
func (es *EventSource) connect() {
	for es.err == nil {
		if es.r != nil {
			es.r.Close()
			<-time.After(es.retry)
		}

		es.request.Header.Set("Last-Event-Id", es.lastEventID)

		resp, err := es.client.Do(es.request)
		switch {
		case errors.Is(err, context.Canceled): // canceled contexts are fatal
			es.err = err
			continue

		case err != nil: // other execution errors are assumed to be non-fatal
			continue

		case resp.StatusCode >= 500: // 5xx are assumed to be temporary
			resp.Body.Close()
			continue

		case resp.StatusCode == 204: // 204 No Content is assumed to be fatal
			resp.Body.Close()
			es.err = ErrClosed
			continue

		case resp.StatusCode == 200:
			ct := resp.Header.Get("content-type")
			mt, _, _ := mime.ParseMediaType(ct)
			if mt != "text/event-stream" {
				resp.Body.Close()
				es.err = fmt.Errorf("invalid response Content-Type (%s)", ct)
				continue
			}
			es.r = resp.Body
			es.dec = NewDecoder(es.r)
			return

		default:
			es.err = fmt.Errorf("endpoint returned unrecoverable status %s", resp.Status)
			continue
		}
	}
}

// Read an event from EventSource. If an error is returned, the EventSource
// will not reconnect, and any further call to Read() will return the same
// error.
func (es *EventSource) Read() (Event, error) {
	if es.r == nil {
		es.connect()
	}

	for es.err == nil {
		var e Event

		err := es.dec.Decode(&e)
		if errors.Is(err, ErrInvalidEncoding) {
			continue
		}
		if err != nil {
			es.connect()
			continue
		}

		if len(e.Data) == 0 {
			continue
		}

		if len(e.ID) > 0 || e.ResetID {
			es.lastEventID = e.ID
		}

		if len(e.Retry) > 0 {
			if retry, err := strconv.Atoi(e.Retry); err == nil {
				es.retry = time.Duration(retry) * time.Millisecond
			}
		}

		return e, nil
	}

	return Event{}, es.err
}
