package server

import (
	"encoding/json"
	"testing"
	"time"
)

// TestEventBusPublishFanOut: one publish reaches every subscriber with a
// monotonic seq stamped by the bus.
func TestEventBusPublishFanOut(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe(nil)
	ch2 := bus.Subscribe(nil)
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	bus.Publish("turn_done", "/proj", "ses_1", map[string]string{"ok": "1"})
	bus.Publish("status", "/proj", "", map[string]string{"model": "m"})

	for _, ch := range []chan Envelope{ch1, ch2} {
		first := <-ch
		second := <-ch
		if first.Event != "turn_done" || first.SessionID != "ses_1" || first.Project != "/proj" {
			t.Fatalf("first envelope = %+v", first)
		}
		if second.Event != "status" || second.SessionID != "" {
			t.Fatalf("second envelope = %+v", second)
		}
		if second.Seq <= first.Seq {
			t.Fatalf("seq not monotonic: %d then %d", first.Seq, second.Seq)
		}
	}
}

// TestEventBusSlowSubscriberDoesNotBlock: a subscriber that never reads must
// not stall publishes to a healthy subscriber (bounded buffer + drop).
func TestEventBusSlowSubscriberDoesNotBlock(t *testing.T) {
	bus := NewEventBus()
	slow := bus.Subscribe(nil)
	defer bus.Unsubscribe(slow)
	fast := bus.Subscribe(nil)
	defer bus.Unsubscribe(fast)

	// Overfill the slow subscriber's buffer with a slow consumer (none —
	// never drained) while the fast one reads.
	done := make(chan struct{})
	go func() {
		for i := 0; i < busBufferSize+100; i++ {
			bus.Publish("text", "/proj", "ses_1", map[string]string{"delta": "x"})
		}
		close(done)
	}()

	// Fast subscriber keeps up; the publish loop must finish well within a
	// deadline regardless of the full slow buffer.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("publishes blocked behind a slow subscriber")
	}
	// Drain the fast channel (it may have dropped too, which is fine).
	for {
		select {
		case <-fast:
		default:
			goto drained
		}
	}
drained:
}

// TestEventBusViewedProjects: the subscriber-aware scope tracks which
// projects at least one subscriber declares, and shrinks when the last
// subscriber for a project leaves.
func TestEventBusViewedProjects(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe([]string{"/p", "/q"})
	ch2 := bus.Subscribe([]string{"/q"})

	got := map[string]bool{}
	for _, p := range bus.ViewedProjects() {
		got[p] = true
	}
	if !got["/p"] || !got["/q"] {
		t.Fatalf("viewed projects = %v, want both /p and /q", got)
	}

	bus.Unsubscribe(ch1)
	bus.Unsubscribe(ch2)
	if got := bus.ViewedProjects(); len(got) != 0 {
		t.Fatalf("viewed projects after all leave = %v, want none", got)
	}
}

// TestEventBusEnvelopeJSONShape guards the wire contract the frontend parses:
// event, project, session_id, seq, data field names.
func TestEventBusEnvelopeJSONShape(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(nil)
	defer bus.Unsubscribe(ch)
	bus.Publish("turn_done", "/p", "ses_9", map[string]string{"model": "m"})
	env := <-ch
	var m map[string]any
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"event", "project", "session_id", "seq", "data"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("envelope JSON missing key %q: %v", key, m)
		}
	}
}
