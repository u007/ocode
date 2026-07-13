package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStreamEventsMultiLineData verifies that multiple `data:` lines for a
// single SSE event are accumulated (joined by LF) rather than each overwriting
// the previous, per the SSE spec.
func TestStreamEventsMultiLineData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: msg\ndata: line one\ndata: line two\ndata: line three\n\n"))
	}))
	defer srv.Close()

	var got SSEEvent
	n := 0
	err := StreamEvents(context.Background(), srv.URL, "", func(ev SSEEvent) {
		n++
		got = ev
	})
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if n != 1 {
		t.Fatalf("event count = %d, want 1", n)
	}
	if got.Event != "msg" {
		t.Errorf("event = %q, want %q", got.Event, "msg")
	}
	want := "line one\nline two\nline three"
	if got.Data != want {
		t.Errorf("data = %q, want %q", got.Data, want)
	}
}

// TestStreamEventsSingleData verifies a single data line is parsed unchanged.
func TestStreamEventsSingleData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: ping\ndata: hello\n\n"))
	}))
	defer srv.Close()

	var got SSEEvent
	err := StreamEvents(context.Background(), srv.URL, "", func(ev SSEEvent) {
		got = ev
	})
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if got.Event != "ping" || got.Data != "hello" {
		t.Errorf("event = %+v, want {ping hello}", got)
	}
}
