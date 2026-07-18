package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDrainerDrainsAndCallsSink(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	ob := NewOutbox(storePath)
	_ = ob.Append(Delivery{JobID: "a", Result: "hello"})
	_ = ob.Append(Delivery{JobID: "b", Result: "world"})

	var got int32
	d := NewDrainer(ob, func(d Delivery) {
		atomic.AddInt32(&got, 1)
	})
	d.Period = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	defer d.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&got) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&got) != 2 {
		t.Fatalf("sink not called twice, got %d", atomic.LoadInt32(&got))
	}
	// Outbox should be empty after drain.
	entries, _ := ob.Peek()
	if len(entries) != 0 {
		t.Fatalf("outbox should be empty, got %d", len(entries))
	}
}

func TestDrainerNilSinkLogs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	ob := NewOutbox(storePath)
	_ = ob.Append(Delivery{JobID: "x", Result: "y"})
	d := NewDrainer(ob, nil)
	d.Period = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	defer d.Stop()
	// Wait for drain.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := ob.Peek()
		if len(entries) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("outbox never drained with nil sink")
}

func TestDrainerStopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ob := NewOutbox(filepath.Join(dir, "j.json"))
	d := NewDrainer(ob, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	d.Stop()
	d.Stop() // must not panic
}
