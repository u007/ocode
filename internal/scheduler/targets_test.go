package scheduler

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTargetsSetGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	tg := NewTargets(storePath)

	if _, err := tg.Get("/x/y"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := tg.Set("/proj", 12345); err != nil {
		t.Fatalf("set: %v", err)
	}
	id, err := tg.Get("/proj")
	if err != nil || id != 12345 {
		t.Fatalf("get: id=%d err=%v", id, err)
	}
	// On-disk persistence: new Targets should see the same data.
	tg2 := NewTargets(storePath)
	id, err = tg2.Get("/proj")
	if err != nil || id != 12345 {
		t.Fatalf("re-open: id=%d err=%v", id, err)
	}
}

func TestTargetsSetZeroRemoves(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	tg := NewTargets(storePath)
	_ = tg.Set("/p", 1)
	_ = tg.Set("/p", 0) // remove
	if _, err := tg.Get("/p"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after zero, got %v", err)
	}
}

func TestTargetsAllReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	tg := NewTargets(storePath)
	_ = tg.Set("/a", 1)
	_ = tg.Set("/b", 2)
	all := tg.All()
	if len(all) != 2 || all["/a"] != 1 || all["/b"] != 2 {
		t.Fatalf("All mismatch: %+v", all)
	}
	// Mutating the returned map must not affect the registry.
	delete(all, "/a")
	if id, _ := tg.Get("/a"); id != 1 {
		t.Fatalf("registry mutated via returned map: %d", id)
	}
}
