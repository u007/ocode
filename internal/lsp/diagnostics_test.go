package lsp

import (
	"context"
	"testing"
	"time"
)

func TestDiagnosticStoreClearsOnEmptyPublishDiagnostics(t *testing.T) {
	store := newDiagnosticStore()
	uri := "file:///tmp/example.go"
	path := "/tmp/example.go"
	store.SetURI(uri, []Diagnostic{{
		URI:      uri,
		Path:     path,
		Range:    Range{Start: Position{Line: 4, Character: 2}},
		Severity: SeverityError,
		Message:  "boom",
	}})

	if got := store.FileCount(); got != 1 {
		t.Fatalf("FileCount before clear = %d, want 1", got)
	}
	if got := store.Count(); got != 1 {
		t.Fatalf("Count before clear = %d, want 1", got)
	}

	clear := parseDiagnosticsPayload([]byte(`{"uri":"file:///tmp/example.go","diagnostics":[]}`))
	if len(clear) != 0 {
		t.Fatalf("empty publishDiagnostics should decode to an empty slice, got %#v", clear)
	}
	store.SetURI(uri, clear)

	if !store.IsEmpty() {
		t.Fatalf("store should be empty after empty publishDiagnostics, got %#v", store.All())
	}
	if got := store.FileCount(); got != 0 {
		t.Fatalf("FileCount after clear = %d, want 0", got)
	}
	if got := store.Count(); got != 0 {
		t.Fatalf("Count after clear = %d, want 0", got)
	}
	if snap := store.Snapshot(10); snap.Total != 0 || snap.Files != 0 || len(snap.FirstN) != 0 {
		t.Fatalf("Snapshot after clear = %+v, want empty", snap)
	}
}

func TestDiagnosticStoreIgnoresStaleGenerationWrites(t *testing.T) {
	store := newDiagnosticStore()
	uri := "file:///tmp/example.go"
	path := "tmp/example.go"
	gen := store.Generation()
	if ok := store.SetURIIfGeneration(uri, []Diagnostic{{
		URI:      uri,
		Path:     path,
		Range:    Range{Start: Position{Line: 1, Character: 2}},
		Severity: SeverityWarning,
		Message:  "first",
	}}, gen); !ok {
		t.Fatal("expected initial generation write to succeed")
	}
	if got := store.Count(); got != 1 {
		t.Fatalf("Count after initial write = %d, want 1", got)
	}
	store.BumpGeneration()
	if ok := store.SetURIIfGeneration(uri, []Diagnostic{{
		URI:      uri,
		Path:     path,
		Range:    Range{Start: Position{Line: 9, Character: 9}},
		Severity: SeverityError,
		Message:  "stale",
	}}, gen); ok {
		t.Fatal("expected stale generation write to be ignored")
	}
	all := store.All()
	if len(all) != 1 || all[0].Message != "first" {
		t.Fatalf("store mutated by stale write: %+v", all)
	}
}

func TestDiagnosticStoreWaitURIReturnsFreshPublishOnly(t *testing.T) {
	store := newDiagnosticStore()
	uri := "file:///tmp/wait.go"
	store.SetURI(uri, []Diagnostic{{URI: uri, Path: "/tmp/wait.go", Message: "old"}})
	since := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if got, fresh := store.WaitURI(ctx, uri, since); fresh || got != nil {
		t.Fatalf("WaitURI returned stale publish: fresh=%v got=%#v", fresh, got)
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		store.SetURI(uri, []Diagnostic{{URI: uri, Path: "/tmp/wait.go", Message: "new"}})
	}()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	got, fresh := store.WaitURI(ctx2, uri, since)
	if !fresh || len(got) != 1 || got[0].Message != "new" {
		t.Fatalf("WaitURI after publish: fresh=%v got=%#v", fresh, got)
	}

	// A clean publish (empty list) is still "fresh": the caller must learn
	// the file no longer has diagnostics.
	since2 := time.Now()
	go func() {
		time.Sleep(30 * time.Millisecond)
		store.SetURI(uri, nil)
	}()
	ctx3, cancel3 := context.WithTimeout(context.Background(), time.Second)
	defer cancel3()
	got, fresh = store.WaitURI(ctx3, uri, since2)
	if !fresh || len(got) != 0 {
		t.Fatalf("WaitURI after clean publish: fresh=%v got=%#v", fresh, got)
	}
}
