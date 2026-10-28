package cdp

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"
)

// TestAttachKeepsChromeLaunchOutsideAttachMu: a slow first launch must not hold
// attachMu, so an attach for another key is not serialized behind it. The
// launch itself is still serialized by launchMu, which is the point: only one
// Chrome process exists, but every attach is not blocked while it starts.
func TestAttachKeepsChromeLaunchOutsideAttachMu(t *testing.T) {
	stub := newStubChrome()
	t.Cleanup(stub.Close)
	m := NewManager(ManagerOptions{Log: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	exited := make(chan int)
	launchStarted := make(chan struct{})
	release := make(chan struct{})
	var launches int32
	m.SetLauncher(func(context.Context) (*Conn, <-chan int, func(), error) {
		if atomic.AddInt32(&launches, 1) == 1 {
			close(launchStarted)
			<-release
		}
		return stub.Conn(), exited, func() {}, nil
	})

	type attachResult struct {
		tgt     *Target
		created bool
		err     error
	}
	firstDone := make(chan attachResult, 1)
	go func() {
		tgt, created, err := m.EnsureHeadless(context.Background(), "tab:first")
		firstDone <- attachResult{tgt, created, err}
	}()

	select {
	case <-launchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the first launch never started")
	}

	// The load-bearing assertion: while the first launch is in flight, attachMu
	// is free. Before the pre-flight it was held for the whole launch, so this
	// TryLock failed and every other attach waited on a slow Chrome start.
	if !m.attachMu.TryLock() {
		t.Fatal("attachMu is held across the Chrome launch; a slow launch blocks every attach")
	}
	m.attachMu.Unlock()

	secondStarted := make(chan struct{})
	secondDone := make(chan attachResult, 1)
	go func() {
		close(secondStarted)
		tgt, created, err := m.EnsureHeadless(context.Background(), "tab:second")
		secondDone <- attachResult{tgt, created, err}
	}()
	<-secondStarted

	close(release)
	first := <-firstDone
	second := <-secondDone
	if first.err != nil || !first.created {
		t.Fatalf("first attach = (created=%v, %v)", first.created, first.err)
	}
	if second.err != nil || !second.created {
		t.Fatalf("second attach = (created=%v, %v)", second.created, second.err)
	}
	if first.tgt == second.tgt {
		t.Fatal("two state keys share one target")
	}
	for key, want := range map[string]*Target{"tab:first": first.tgt, "tab:second": second.tgt} {
		got, ok := m.LookupTarget(key)
		if !ok || got != want {
			t.Fatalf("%s is not registered under its own target", key)
		}
	}
}
