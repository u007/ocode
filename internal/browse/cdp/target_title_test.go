package cdp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// titleRecorder collects the TitleEvents a manager emits.
type titleRecorder struct {
	mu     sync.Mutex
	events []TitleEvent
}

func (r *titleRecorder) add(ev TitleEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *titleRecorder) all() []TitleEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TitleEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *titleRecorder) wait(t *testing.T, n int) []TitleEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := r.all()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d title event(s); have %+v", n, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// newTitleTestTarget attaches a sink-backed target to a stub Chrome and
// shortens the poll interval so the watcher's ticks are observable.
func newTitleTestTarget(t *testing.T, key string) (*Target, *stubChrome, *titleRecorder) {
	t.Helper()
	oldInterval := titlePollInterval
	titlePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { titlePollInterval = oldInterval })

	stub := newStubChrome()
	t.Cleanup(stub.Close)
	rec := &titleRecorder{}
	m := NewManager(ManagerOptions{EmitTitle: rec.add})
	exited := make(chan int)
	m.SetLauncher(func(context.Context) (*Conn, <-chan int, func(), error) {
		return stub.Conn(), exited, func() {}, nil
	})
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	tgt, err := m.Attach(context.Background(), key, &testSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return tgt, stub, rec
}

// TestTarget_TitlePollReportsRenderedTitle pins the live source of the tab
// strip's page title. Current Chrome does not send Target.targetInfoChanged
// for a document.title change (measured on 157 headless: the event arrives on
// a URL change and never on a title change), so the per-target poll is what
// carries a rendered title. A title must be reported once, a repeat of the
// same title must not re-report, a changed title must report, a same-title
// navigation must re-report, and an emptied title must report once as an
// explicit clear. The event carries no URL: the probe is already scoped to
// this target's live document.
func TestTarget_TitlePollReportsRenderedTitle(t *testing.T) {
	_, stub, rec := newTitleTestTarget(t, "tab:title")

	// The stub's default getTargetInfo reply has no targetInfo: the probe at
	// attach must not invent a title.
	time.Sleep(50 * time.Millisecond)
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("emitted titles before any page title existed: %+v", got)
	}

	setTitle := func(title, url string) {
		stub.SetResult("Target.getTargetInfo", map[string]any{"targetInfo": map[string]any{
			"type": "page", "title": title, "url": url,
		}})
	}

	setTitle("Example Domain", "https://example.com/")
	first := rec.wait(t, 1)[0]
	if first.StateKey != "tab:title" || first.Title != "Example Domain" || first.URL != "" {
		t.Fatalf("first title event = %+v", first)
	}

	// Many more probes of the same title must not re-emit it.
	time.Sleep(100 * time.Millisecond)
	if got := rec.all(); len(got) != 1 {
		t.Fatalf("emitted %d events for one unchanged title, want 1: %+v", len(got), got)
	}

	setTitle("Second Title", "https://example.com/")
	if got := rec.wait(t, 2)[1].Title; got != "Second Title" {
		t.Fatalf("changed title event = %q, want Second Title", got)
	}

	// A new page that keeps the same title must still re-emit it: the SPA drops
	// pageTitle on every URL change, so an unchanged title would otherwise
	// leave the strip on its fallback (most SPA routes share one title).
	setTitle("Second Title", "https://example.com/next")
	if got := rec.wait(t, 3)[2].Title; got != "Second Title" {
		t.Fatalf("same-title navigation event = %q, want Second Title", got)
	}

	// A page clearing its <title> is reported once, so the strip can fall back.
	setTitle("", "https://example.com/next")
	rec.wait(t, 4)
	time.Sleep(100 * time.Millisecond)
	got := rec.all()
	if len(got) != 4 || got[3].Title != "" {
		t.Fatalf("cleared-title events = %+v, want exactly 4 ending in an empty title", got)
	}
}

// stopHandlers must be safe to call twice: a revoke and a Chrome exit both run
// it, and closing titleStop twice would panic the server.
func TestTarget_StopHandlersTwiceIsSafe(t *testing.T) {
	tgt, _, _ := newTitleTestTarget(t, "tab:stop")
	tgt.stopHandlers()
	tgt.stopHandlers()
}

// A probe already in flight when stopHandlers runs must not emit a title event
// after teardown: the SPA store may already have dropped the surface. probeTitle
// is called directly to stand in for an in-flight poll that completes after
// stop; holding the reply in the stub would deadlock its single-threaded frame
// loop (every later reply queues behind the held one).
func TestTarget_NoTitleEventAfterStop(t *testing.T) {
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	rec := &titleRecorder{}
	m := NewManager(ManagerOptions{EmitTitle: rec.add})
	exited := make(chan int)
	m.SetLauncher(func(context.Context) (*Conn, <-chan int, func(), error) {
		return stub.Conn(), exited, func() {}, nil
	})
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	stub.SetResult("Target.getTargetInfo", map[string]any{"targetInfo": map[string]any{
		"type": "page", "title": "Late", "url": "https://example.com/",
	}})
	tgt, err := m.Attach(context.Background(), "tab:late", &testSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	// Let the attach-time probe settle first: it is legitimate wherever it
	// lands (before or after the stop below is unobservable), and the assert
	// must not depend on that timing.
	rec.wait(t, 1)
	tgt.stopHandlers()
	// A probe completing after stop (here: called directly, standing in for one
	// in flight when stop ran) must be swallowed.
	tgt.probeTitle()
	time.Sleep(50 * time.Millisecond)
	if got := rec.all(); len(got) != 1 {
		t.Fatalf("title events after stop = %+v, want exactly the pre-stop one", got)
	}
}
