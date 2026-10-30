package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Two task panes for one workbook (a copied file, or the same file open in two windows) carry
// different per-page-load instance tokens. The newer one evicts the older one; a reconnect with the
// same instance replaces the stream silently.

type sseEvent struct{ name, data string }

// readSSE parses an SSE body into events on the returned channel, which closes at end of stream.
func readSSE(r io.Reader) <-chan sseEvent {
	out := make(chan sseEvent, 16)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		var name, data string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if name != "" {
					out <- sseEvent{name: name, data: data}
				}
				name, data = "", ""
			}
		}
	}()
	return out
}

func nextEvent(t *testing.T, ch <-chan sseEvent) sseEvent {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("stream ended before the expected event")
		}
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an SSE event")
	}
	return sseEvent{}
}

// awaitEnd fails if the stream sends another event, and checks that it ends.
func awaitEnd(t *testing.T, ch <-chan sseEvent) {
	t.Helper()
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatalf("unexpected event %q after the stream should have ended", e.name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end")
	}
}

func openPaneStream(t *testing.T, base, wb, instance string) <-chan sseEvent {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/bridge/stream?workbook="+wb+"&instance="+instance, nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream for instance %q: status %d", instance, resp.StatusCode)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return readSSE(resp.Body)
}

func TestSecondInstanceEvictsFirstWithEventBeforeEnd(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	a := openPaneStream(t, s.URL, "wb1", "pane-a")
	if e := nextEvent(t, a); e.name != "hello" {
		t.Fatalf("first event %q, want hello", e.name)
	}
	b := openPaneStream(t, s.URL, "wb1", "pane-b")
	if e := nextEvent(t, b); e.name != "hello" {
		t.Fatalf("new pane first event %q, want hello", e.name)
	}

	e := nextEvent(t, a)
	if e.name != "evicted" {
		t.Fatalf("evicted pane got %q, want evicted", e.name)
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(e.data), &body); err != nil {
		t.Fatalf("evicted data %q is not JSON: %v", e.data, err)
	}
	if body.Reason != "another task pane opened this workbook" {
		t.Fatalf("evicted reason %q", body.Reason)
	}
	awaitEnd(t, a) // the evicted stream closes after the event

	// The newer pane is still the attached one for the workbook.
	checkOnlyWorkbookAttached(t, s.URL, "wb1")
}

func TestSameInstanceReconnectIsSilent(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	first := openPaneStream(t, s.URL, "wb1", "pane-a")
	if e := nextEvent(t, first); e.name != "hello" {
		t.Fatalf("first event %q, want hello", e.name)
	}
	second := openPaneStream(t, s.URL, "wb1", "pane-a") // same page load reconnecting
	if e := nextEvent(t, second); e.name != "hello" {
		t.Fatalf("reconnect first event %q, want hello", e.name)
	}
	// The replaced stream ends without an evicted event.
	awaitEnd(t, first)
}

func TestStreamRequiresInstance(t *testing.T) {
	s, _ := newTestBridge(t, "http://127.0.0.1:1")
	for _, q := range []string{"?workbook=wb1", "?workbook=wb1&instance="} {
		req, _ := http.NewRequest("GET", s.URL+"/bridge/stream"+q, nil)
		req.Header.Set("X-Bridge-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("stream%s: status %d, want 400", q, resp.StatusCode)
		}
	}
}

// checkOnlyWorkbookAttached reads /bridge/health and fails unless wb is the one connected workbook.
func checkOnlyWorkbookAttached(t *testing.T, base, wb string) {
	t.Helper()
	resp, err := http.Get(base + "/bridge/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h struct {
		Workbooks []string `json:"workbooks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if len(h.Workbooks) != 1 || h.Workbooks[0] != wb {
		t.Fatalf("connected workbooks %v, want exactly [%s]", h.Workbooks, wb)
	}
}
