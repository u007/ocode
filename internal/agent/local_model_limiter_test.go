package agent

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireLocalModelSlotLimitsConcurrency(t *testing.T) {
	dir := t.TempDir()
	const maxParallel = 2
	const workers = 5

	var inFlight int32
	var maxObserved int32
	done := make(chan struct{}, workers)

	for i := 0; i < workers; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release, _, err := acquireLocalModelSlot(ctx, "local/test-model", maxParallel, dir)
			if err != nil {
				t.Errorf("acquireLocalModelSlot: %v", err)
				done <- struct{}{}
				return
			}
			n := atomic.AddInt32(&inFlight, 1)
			for {
				cur := atomic.LoadInt32(&maxObserved)
				if n <= cur || atomic.CompareAndSwapInt32(&maxObserved, cur, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			release()
			done <- struct{}{}
		}()
	}

	for i := 0; i < workers; i++ {
		<-done
	}

	if got := atomic.LoadInt32(&maxObserved); got > maxParallel {
		t.Fatalf("observed %d concurrent holders, want <= %d", got, maxParallel)
	}
}

func TestAcquireLocalModelSlotReleaseIsIdempotentAndFreesSlot(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	release1, _, err := acquireLocalModelSlot(ctx, "local/test-model", 1, dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// A second acquire with the single slot held must block until released.
	acquired := make(chan struct{})
	go func() {
		release2, _, err := acquireLocalModelSlot(context.Background(), "local/test-model", 1, dir)
		if err != nil {
			t.Errorf("second acquire: %v", err)
			return
		}
		close(acquired)
		release2()
	}()

	select {
	case <-acquired:
		t.Fatal("second acquire succeeded while slot was still held")
	case <-time.After(200 * time.Millisecond):
	}

	release1()
	release1() // idempotent: must not panic or double-free

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
}

func TestAcquireLocalModelSlotRespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	release, _, err := acquireLocalModelSlot(context.Background(), "local/test-model", 1, dir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, _, err := acquireLocalModelSlot(ctx, "local/test-model", 1, dir); err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}

func TestSlotReleasingBodyTouchesLockWhileOpen(t *testing.T) {
	dir := t.TempDir()
	slotPath := dir + "/local_test-model.slot0.lock"
	if err := os.WriteFile(slotPath, nil, 0644); err != nil {
		t.Fatalf("create lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(slotPath, old, old); err != nil {
		t.Fatalf("backdate lock: %v", err)
	}

	released := false
	b := &slotReleasingBody{
		ReadCloser: io.NopCloser(strings.NewReader("x")),
		release:    func() { released = true },
		slotPath:   slotPath,
		touchEvery: 15 * time.Millisecond,
		stopTouch:  make(chan struct{}),
	}
	go b.touchLoop()

	time.Sleep(80 * time.Millisecond) // several touch ticks must have fired
	info, err := os.Stat(slotPath)
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if !info.ModTime().After(old.Add(30 * time.Minute)) {
		t.Fatalf("lock mtime not refreshed while body open: mod=%v old=%v", info.ModTime(), old)
	}

	b.Close()
	if !released {
		t.Fatal("Close must release the slot exactly once")
	}
	// After Close the ticker is stopped; capture mtime and ensure it stays put.
	info, _ = os.Stat(slotPath)
	time.Sleep(40 * time.Millisecond)
	info2, _ := os.Stat(slotPath)
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Fatal("touch loop did not stop after Close")
	}
}
