package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakePane answers every call with handler(call) until detach.
func fakePane(t *testing.T, h *Hub, wb string, handler func(CallEvent) Result) (detach func(), cancels <-chan string) {
	t.Helper()
	events, done, det := h.Attach(wb)
	cc := make(chan string, 8)
	go func() {
		for {
			select {
			case ev := <-events:
				switch ev.Name {
				case "call":
					c := ev.Data.(CallEvent)
					if r := handler(c); r.ID != "skip" {
						r.ID = c.ID
						h.Deliver(wb, r)
					}
				case "cancel":
					cc <- ev.Data.(map[string]string)["id"]
				}
			case <-done:
				return
			}
		}
	}()
	return det, cc
}

func TestHubRoundTrip(t *testing.T) {
	h := NewHub()
	det, _ := fakePane(t, h, "wb1", func(c CallEvent) Result {
		return Result{OK: true, Content: "ran " + c.Tool + " " + string(c.Args)}
	})
	defer det()
	r, err := h.Call(context.Background(), "wb1", "read_range", json.RawMessage(`{"a":1}`), time.Second)
	if err != nil || !r.OK || r.Content != `ran read_range {"a":1}` {
		t.Fatalf("got %+v, %v", r, err)
	}
	// A single connected pane is used when no workbook is named.
	if _, err := h.Call(context.Background(), "", "describe_workbook", json.RawMessage(`{}`), time.Second); err != nil {
		t.Fatalf("implicit workbook: %v", err)
	}
}

func TestHubNoPaneAndAmbiguity(t *testing.T) {
	h := NewHub()
	if _, err := h.Call(context.Background(), "", "x", nil, time.Second); !errors.Is(err, ErrNoPane) {
		t.Fatalf("want ErrNoPane, got %v", err)
	}
	d1, _ := fakePane(t, h, "a", func(CallEvent) Result { return Result{OK: true} })
	d2, _ := fakePane(t, h, "b", func(CallEvent) Result { return Result{OK: true} })
	defer d1()
	defer d2()
	_, err := h.Call(context.Background(), "", "x", nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "several workbooks") {
		t.Fatalf("ambiguity must be an error naming the workbooks, got %v", err)
	}
	if _, err := h.Call(context.Background(), "zzz", "x", nil, time.Second); !errors.Is(err, ErrNoPane) {
		t.Fatalf("unknown workbook: %v", err)
	}
}

func TestHubTimeoutTellsPaneToCancel(t *testing.T) {
	h := NewHub()
	det, cancels := fakePane(t, h, "wb", func(CallEvent) Result { return Result{ID: "skip"} }) // never answers
	defer det()
	_, err := h.Call(context.Background(), "wb", "read_range", json.RawMessage(`{}`), 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("want timeout, got %v", err)
	}
	select {
	case id := <-cancels:
		if id == "" {
			t.Fatal("empty cancel id")
		}
	case <-time.After(time.Second):
		t.Fatal("pane was not told to cancel")
	}
	// A late result after the timeout is dropped, not an error.
	if h.Deliver("wb", Result{ID: "c1", OK: true}) {
		t.Fatal("late result must not be accepted")
	}
}

func TestHubContextCancelAbandons(t *testing.T) {
	h := NewHub()
	det, cancels := fakePane(t, h, "wb", func(CallEvent) Result { return Result{ID: "skip"} })
	defer det()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if _, err := h.Call(ctx, "wb", "x", nil, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	select {
	case <-cancels:
	case <-time.After(time.Second):
		t.Fatal("pane not told to cancel after ctx cancel")
	}
}

func TestHubPaneReplacedOrGoneFailsPendingCalls(t *testing.T) {
	h := NewHub()
	_, _, detach := h.Attach("wb") // nobody reads events; the call is queued then pending
	errc := make(chan error, 1)
	go func() {
		_, err := h.Call(context.Background(), "wb", "x", nil, 5*time.Second)
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	detach()
	select {
	case err := <-errc:
		// The pending call is failed with ErrPaneGone, delivered as a Result error.
		if err != nil && !errors.Is(err, ErrPaneGone) {
			t.Fatalf("unexpected error %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("call hung after the pane disconnected")
	}
}

func TestHubRejectsResultFromAnotherWorkbook(t *testing.T) {
	h := NewHub()
	det, _ := fakePane(t, h, "a", func(CallEvent) Result { return Result{ID: "skip"} })
	defer det()
	d2, _ := fakePane(t, h, "b", func(CallEvent) Result { return Result{ID: "skip"} })
	defer d2()
	go func() {
		time.Sleep(30 * time.Millisecond)
		if h.Deliver("b", Result{ID: "c1", OK: true, Content: "forged"}) {
			t.Error("workbook b must not be able to answer a call made to workbook a")
		}
		h.Deliver("a", Result{ID: "c1", OK: true, Content: "real"})
	}()
	r, err := h.Call(context.Background(), "a", "x", nil, time.Second)
	if err != nil || r.Content != "real" {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestHubInFlightLimitAndClamp(t *testing.T) {
	h := NewHub()
	h.MaxInFlight = 2
	det, _ := fakePane(t, h, "wb", func(CallEvent) Result { return Result{ID: "skip"} })
	defer det()
	for i := 0; i < 2; i++ {
		go h.Call(context.Background(), "wb", "x", nil, time.Second)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := h.Call(context.Background(), "wb", "x", nil, time.Second); !errors.Is(err, ErrCallLimit) {
		t.Fatalf("want ErrCallLimit, got %v", err)
	}
	r := clamp(Result{Content: strings.Repeat("x", maxResultBytes+10)})
	if len(r.Content) <= maxResultBytes || !strings.Contains(r.Content, "truncated") {
		t.Fatal("oversized result must be truncated with a visible marker")
	}
}
