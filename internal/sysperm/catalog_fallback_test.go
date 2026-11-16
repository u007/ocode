package sysperm

import "testing"

func TestGenericEntriesWindows(t *testing.T) {
	entries := genericEntries("windows")
	if len(entries) != 1 {
		t.Fatalf("genericEntries(windows) = %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.ID != "windows.no-explicit-grant" || e.Platform != "windows" {
		t.Fatalf("entry = %+v, want windows.no-explicit-grant", e)
	}
	if e.Supported || e.Status != StatusNotRequired {
		t.Fatalf("entry = %+v, want unsupported and not_required", e)
	}
}

func TestGenericEntriesLinux(t *testing.T) {
	entries := genericEntries("linux")
	if len(entries) != 1 {
		t.Fatalf("genericEntries(linux) = %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.ID != "linux.no-explicit-grant" || e.Platform != "linux" {
		t.Fatalf("entry = %+v, want linux.no-explicit-grant", e)
	}
	if e.Supported || e.Status != StatusNotRequired {
		t.Fatalf("entry = %+v, want unsupported and not_required", e)
	}
}

func TestGenericEntriesUnknownOSNamesTheOS(t *testing.T) {
	entries := genericEntries("plan9")
	if len(entries) != 1 {
		t.Fatalf("genericEntries(plan9) = %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.ID != "plan9.unsupported" || e.Platform != "plan9" || e.Supported {
		t.Fatalf("entry = %+v, want a plan9.unsupported informational row", e)
	}
	if e.Status != StatusNotRequired {
		t.Fatalf("status = %q, want %q", e.Status, StatusNotRequired)
	}
}
