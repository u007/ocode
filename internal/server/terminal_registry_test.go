package server

import "testing"

func TestTerminalRegistrySetSizeRecordsPtySize(t *testing.T) {
	r := newTerminalRegistry()
	r.register("term-1", terminalProcEntry{Project: "/p", PID: 42})

	if got := r.snapshot()["term-1"]; got.Cols != 0 || got.Rows != 0 {
		t.Fatalf("size before first resize = %dx%d, want 0x0", got.Cols, got.Rows)
	}

	r.setSize("term-1", 200, 61)
	got := r.snapshot()["term-1"]
	if got.Cols != 200 || got.Rows != 61 {
		t.Fatalf("size after resize = %dx%d, want 200x61", got.Cols, got.Rows)
	}
	if got.Project != "/p" || got.PID != 42 {
		t.Fatalf("setSize clobbered the entry: %+v", got)
	}
}

func TestTerminalRegistrySetSizeIgnoresUnregisteredID(t *testing.T) {
	r := newTerminalRegistry()
	gen := r.generation()

	r.setSize("term-gone", 80, 24)

	if _, ok := r.snapshot()["term-gone"]; ok {
		t.Fatal("setSize created an entry for an unregistered terminal")
	}
	if r.generation() != gen {
		t.Fatal("setSize on an unregistered terminal moved the generation")
	}
}
