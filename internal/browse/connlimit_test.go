package browse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fastLimiter builds a limiter with a tiny queue deadline so failure paths
// resolve in milliseconds.
func fastLimiter(limit int) *connLimiter {
	l := newConnLimiter(limit)
	l.wait = 50 * time.Millisecond
	return l
}

func TestConnLimiterCapQueuesBoundedThenFails(t *testing.T) {
	l := fastLimiter(2)
	r1, err := l.acquire(t.Context(), "tab:a")
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	r2, err := l.acquire(t.Context(), "tab:a")
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	start := time.Now()
	_, err = l.acquire(t.Context(), "tab:a")
	if !errors.Is(err, errUpstreamBusy) {
		t.Fatalf("acquire 3 err = %v, want errUpstreamBusy", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("over-cap acquire took %v, want bounded by the 50ms wait", d)
	}
	// Releasing a holder wakes the next acquirer.
	r1()
	r3, err := l.acquire(t.Context(), "tab:a")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	r2()
	r3()
	if n := l.inUse(); n != 0 {
		t.Errorf("inUse = %d after all releases, want 0 (entries must not linger)", n)
	}
}

func TestConnLimiterWaiterWakesOnRelease(t *testing.T) {
	l := newConnLimiter(1)
	l.wait = 2 * time.Second
	ctx := t.Context()
	r1, err := l.acquire(ctx, "tab:w")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		rel, aerr := l.acquire(ctx, "tab:w")
		if aerr == nil {
			rel()
		}
		done <- aerr
	}()
	time.Sleep(30 * time.Millisecond) // let the waiter park
	r1()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter should have acquired after release, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter not woken by release")
	}
}

func TestConnLimiterCancellationAndTimeoutLeakNoRefs(t *testing.T) {
	l := fastLimiter(1)
	r1, _ := l.acquire(t.Context(), "tab:c")
	defer r1()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.acquire(ctx, "tab:c"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire err = %v, want context.Canceled", err)
	}
	// Timeout path (errUpstreamBusy) also drops its ref; with the only holder
	// still out, refs must be exactly 1 and the entry intact, then 0 after.
	if _, err := l.acquire(t.Context(), "tab:c"); !errors.Is(err, errUpstreamBusy) {
		t.Fatalf("over-cap acquire err = %v, want errUpstreamBusy", err)
	}
	if n := l.inUse(); n != 1 {
		t.Fatalf("inUse = %d, want 1 (entry alive for the holder)", n)
	}
	r1()
	if n := l.inUse(); n != 0 {
		t.Fatalf("inUse = %d after holder release, want 0", n)
	}
}

func TestConnLimiterStateKeysAreIndependent(t *testing.T) {
	l := fastLimiter(1)
	r1, err := l.acquire(t.Context(), "tab:one")
	if err != nil {
		t.Fatalf("acquire one: %v", err)
	}
	r2, err := l.acquire(t.Context(), "tab:two")
	if err != nil {
		t.Errorf("different stateKey blocked by tab:one's full semaphore: %v", err)
	} else {
		r2()
	}
	r1()
}

// TestExternalHoldsSlotThroughBodyStream proves the cap covers live upstream
// connections, not request starts: the slot stays held while a slow body
// streams, so the next same-key request 503s until the first completes.
func TestExternalHoldsSlotThroughBodyStream(t *testing.T) {
	gotHeaders := make(chan struct{})
	releaseBody := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(gotHeaders)
		<-releaseBody
		_, _ = io.WriteString(w, "done")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	s.conns = fastLimiter(1)
	host := strings.TrimPrefix(upstream.URL, "http://")
	tgt := target{StateKey: "tab:slow", Scheme: "http", Host: host, Path: "/"}

	first := httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Go(func() {
		s.handleExternal(first, httptest.NewRequest("GET", "/b/tab:slow/http/"+host+"/", nil), tgt)
	})

	<-gotHeaders // first request is mid-stream
	second := httptest.NewRecorder()
	s.handleExternal(second, httptest.NewRequest("GET", "/b/tab:slow/http/"+host+"/", nil), tgt)
	if second.Code != http.StatusServiceUnavailable {
		t.Errorf("over-cap status = %d, want 503", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Errorf("503 must carry Retry-After")
	}

	close(releaseBody)
	wg.Wait()
	third := httptest.NewRecorder()
	s.handleExternal(third, httptest.NewRequest("GET", "/b/tab:slow/http/"+host+"/", nil), tgt)
	if third.Code != http.StatusOK {
		t.Errorf("post-release status = %d, want 200 (slot was returned)", third.Code)
	}
	if n := s.conns.inUse(); n != 0 {
		t.Errorf("inUse = %d after all requests finished, want 0", n)
	}
}

// TestExternalLocalShareOneCapPerStateKey pins the advisor-settled semantics:
// one per-stateKey semaphore covers BOTH modes (same upstream resource).
func TestExternalLocalShareOneCapPerStateKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newTestServer(t)
	s.conns = fastLimiter(1)
	host := strings.TrimPrefix(upstream.URL, "http://")
	tgt := target{StateKey: "tab:shared", Scheme: "http", Host: host, Path: "/", Local: true}

	release, err := s.conns.acquire(t.Context(), "tab:shared")
	if err != nil {
		t.Fatalf("pre-fill: %v", err)
	}
	w := httptest.NewRecorder()
	s.handleLocal(w, httptest.NewRequest("GET", "/b/tab:shared/http/"+host+"/", nil), tgt)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("local over-cap status = %d, want 503", w.Code)
	}
	release()
	w2 := httptest.NewRecorder()
	s.handleLocal(w2, httptest.NewRequest("GET", "/b/tab:shared/http/"+host+"/", nil), tgt)
	if w2.Code != http.StatusOK {
		t.Errorf("local after release status = %d, want 200", w2.Code)
	}
}

func TestExternalBusyClosesNavPair(t *testing.T) {
	s := newTestServer(t)
	s.conns = fastLimiter(1)
	var navs []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { navs = append(navs, ev) })

	release, _ := s.conns.acquire(t.Context(), "tab:nav")
	defer release()

	w := httptest.NewRecorder()
	s.handleExternal(w, httptest.NewRequest("GET", "/b/tab:nav/http/example.com/", nil),
		target{StateKey: "tab:nav", Scheme: "http", Host: "example.com", Path: "/"})
	if len(navs) != 2 {
		t.Fatalf("navs = %d, want the loading+terminal pair for a document 503", len(navs))
	}
	if navs[0].Status != 0 || navs[1].Status != http.StatusServiceUnavailable || navs[1].Error != "upstream busy" {
		t.Errorf("nav pair = %+v, want loading(0) + 503 busy", navs)
	}

	// A failed SUBRESOURCE must stay silent on the address bar (Part 07).
	sub := httptest.NewRequest("GET", "/b/tab:nav/http/example.com/i.png", nil)
	sub.Header.Set("Sec-Fetch-Dest", "image")
	s.handleExternal(httptest.NewRecorder(), sub,
		target{StateKey: "tab:nav", Scheme: "http", Host: "example.com", Path: "/i.png"})
	if len(navs) != 2 {
		t.Errorf("subresource busy emitted navs (total %d), want 0", len(navs))
	}
}
