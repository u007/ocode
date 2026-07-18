package scheduler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutboxAppendDrainRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	o := NewOutbox(storePath)

	for i := 0; i < 3; i++ {
		if err := o.Append(Delivery{JobID: "abc", JobName: "t", Result: "ok"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, err := o.Drain()
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("drain len: want 3 got %d", len(got))
	}
	// Second drain should be empty (file truncated).
	again, err := o.Drain()
	if err != nil {
		t.Fatalf("drain2: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("drain2 len: want 0 got %d", len(again))
	}
}

func TestOutboxPeekDoesNotTruncate(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	o := NewOutbox(storePath)
	if err := o.Append(Delivery{JobID: "a", Result: "x"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := o.Peek(); err != nil {
		t.Fatalf("peek: %v", err)
	}
	// File should still exist.
	if _, err := os.Stat(o.Path()); err != nil {
		t.Fatalf("expected file to remain after Peek: %v", err)
	}
}

func TestOutboxMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	o := NewOutbox(filepath.Join(dir, "nope.json"))
	got, err := o.Drain()
	if err != nil {
		t.Fatalf("drain missing: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil slice, got %v", got)
	}
}
