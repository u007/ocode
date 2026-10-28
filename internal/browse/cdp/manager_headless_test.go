package cdp

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// newHeadlessTestManager returns a manager on a stub Chrome. The stub is closed
// by the test's cleanup.
func newHeadlessTestManager(t *testing.T) (*Manager, *stubChrome) {
	t.Helper()
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	m := newTestManager(t, stub, func(NavEvent) {}, log.New(io.Discard, "", 0))
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	return m, stub
}

func TestEnsureHeadlessAttachesOnceWithoutSink(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	ctx := context.Background()

	first, created, err := m.EnsureHeadless(ctx, "tab:restored")
	if err != nil || !created {
		t.Fatalf("EnsureHeadless = (created=%v, %v), want created", created, err)
	}
	if first.sink != nil {
		t.Fatal("headless target was attached with a FrameSink")
	}
	second, created, err := m.EnsureHeadless(ctx, "tab:restored")
	if err != nil || created || second != first {
		t.Fatalf("second EnsureHeadless = (%p, created=%v, %v), want the same target", second, created, err)
	}
	if n := len(stub.CallsFor("Target.createTarget")); n != 1 {
		t.Fatalf("createTarget calls = %d, want 1", n)
	}
}

// A panel's sink must survive a headless ensure: the panel is showing the tab.
func TestEnsureHeadlessNeverReplacesPanelSink(t *testing.T) {
	m, _ := newHeadlessTestManager(t)
	ctx := context.Background()
	panel := &testSink{}
	if _, err := m.Attach(ctx, "tab:shown", panel); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	got, created, err := m.EnsureHeadless(ctx, "tab:shown")
	if err != nil || created {
		t.Fatalf("EnsureHeadless on a panel tab = (created=%v, %v)", created, err)
	}
	if got.sink != FrameSink(panel) {
		t.Fatal("EnsureHeadless replaced the panel's FrameSink")
	}
}

// Headless ensure and panel attach race for one key. Whichever runs first, the
// key gets exactly one target and the panel's sink is the one left in place.
func TestEnsureHeadlessAndAttachRaceCreatesOneTarget(t *testing.T) {
	for i := 0; i < 15; i++ {
		m, stub := newHeadlessTestManager(t)
		ctx := context.Background()
		panel := &testSink{}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := m.Attach(ctx, "tab:race", panel); err != nil {
				t.Errorf("Attach: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, _, err := m.EnsureHeadless(ctx, "tab:race"); err != nil {
				t.Errorf("EnsureHeadless: %v", err)
			}
		}()
		wg.Wait()
		if n := len(stub.CallsFor("Target.createTarget")); n != 1 {
			t.Fatalf("iteration %d: createTarget calls = %d, want 1", i, n)
		}
		m.mu.Lock()
		got := m.targets["tab:race"]
		m.mu.Unlock()
		if got == nil || got.sink != FrameSink(panel) {
			t.Fatalf("iteration %d: panel sink not left on the target", i)
		}
	}
}

func TestOpenHeadlessReturnsFreshKeyWithoutSink(t *testing.T) {
	m, _ := newHeadlessTestManager(t)
	tgt, key, err := m.OpenHeadless(context.Background())
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}
	if len(key) <= len("tab:") || key[:4] != "tab:" {
		t.Fatalf("key = %q, want a tab: key", key)
	}
	if tgt.sink != nil {
		t.Fatal("opened headless tab has a FrameSink")
	}
	if got, ok := m.LookupTarget(key); !ok || got != tgt {
		t.Fatal("opened tab is not registered under its key")
	}
}

// A headless tab has no FrameSink, so attaching it must not start a screencast:
// Chrome would stream JPEG frames into nothing.
func TestHeadlessAttachStartsNoScreencast(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	if _, _, err := m.EnsureHeadless(context.Background(), "tab:quiet"); err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	if n := len(stub.CallsFor("Page.startScreencast")); n != 0 {
		t.Fatalf("headless attach made %d Page.startScreencast calls, want 0", n)
	}
}

// A panel attach on the same target starts the screencast exactly once, and the
// panel's sink receives frames.
func TestPanelAttachOnHeadlessTargetStartsScreencastOnce(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	ctx := context.Background()
	tgt, _, err := m.EnsureHeadless(ctx, "tab:later")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	panel := &testSink{}
	if _, err := m.Attach(ctx, "tab:later", panel); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if n := len(stub.CallsFor("Page.startScreencast")); n != 1 {
		t.Fatalf("Page.startScreencast calls = %d, want exactly 1", n)
	}
	stub.InjectEvent(tgt.sessionID, "Page.screencastFrame", map[string]any{
		"data": "AAAA", "sessionId": 1,
		"metadata": map[string]any{"deviceWidth": 4, "deviceHeight": 4},
	})
	waitFor(t, func() bool {
		panel.mu.Lock()
		defer panel.mu.Unlock()
		return len(panel.frames) == 1
	})
}

func TestCloseTargetTearsDownAndForgetsKey(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	ctx := context.Background()
	tgt, key, err := m.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}
	if err := m.CloseTarget(ctx, key); err != nil {
		t.Fatalf("CloseTarget: %v", err)
	}
	if _, ok := m.LookupTarget(key); ok {
		t.Fatal("closed tab is still registered under its key")
	}
	closed := stub.CallsFor("Target.closeTarget")
	if len(closed) != 1 || !strings.Contains(string(closed[0].Params), tgt.targetID) {
		t.Fatalf("Target.closeTarget calls = %+v, want one for %s", closed, tgt.targetID)
	}
	disposed := stub.CallsFor("Target.disposeBrowserContext")
	if len(disposed) != 1 || !strings.Contains(string(disposed[0].Params), tgt.browserContextID) {
		t.Fatalf("Target.disposeBrowserContext calls = %+v, want one for %s", disposed, tgt.browserContextID)
	}
}

func TestCloseTargetAbsentKeyErrorsWithoutChromeCall(t *testing.T) {
	m, stub := newHeadlessTestManager(t)
	before := len(stub.Calls())
	err := m.CloseTarget(context.Background(), "tab:never")
	if !errors.Is(err, ErrNoTarget) {
		t.Fatalf("CloseTarget(absent) = %v, want an error wrapping ErrNoTarget", err)
	}
	if after := len(stub.Calls()); after != before {
		t.Fatalf("closing an absent key made %d Chrome calls, want 0", after-before)
	}
}

// The idle shutdown arms only once the last target is gone, and then closes
// Chrome. Two tabs: closing the first must leave the timer unarmed.
func TestCloseTargetArmsIdleShutdownAfterLastClose(t *testing.T) {
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	m := NewManager(ManagerOptions{IdleTimeout: 50 * time.Millisecond, Log: log.New(io.Discard, "", 0)})
	exited := make(chan int)
	m.SetLauncher(func(context.Context) (*Conn, <-chan int, func(), error) {
		return stub.Conn(), exited, func() {}, nil
	})
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	ctx := context.Background()
	_, first, err := m.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}
	_, second, err := m.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}

	if err := m.CloseTarget(ctx, first); err != nil {
		t.Fatalf("CloseTarget(first): %v", err)
	}
	m.mu.Lock()
	armedAfterFirst := m.idleTimer != nil
	m.mu.Unlock()
	if armedAfterFirst {
		t.Fatal("idle shutdown armed while a tab is still open")
	}

	if err := m.CloseTarget(ctx, second); err != nil {
		t.Fatalf("CloseTarget(second): %v", err)
	}
	m.mu.Lock()
	armed := m.idleTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Fatal("idle shutdown not armed after the last tab closed")
	}
	waitFor(t, func() bool { return len(stub.CallsFor("Browser.close")) == 1 })
}
