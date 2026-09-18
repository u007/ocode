package sysperm

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathIDCleansPath(t *testing.T) {
	if got, want := PathID("/a/b/../c/"), "path:/a/c"; got != want {
		t.Fatalf("PathID = %q, want %q", got, want)
	}
}

func TestConfigGetSetDelete(t *testing.T) {
	cfg := DefaultConfig()
	if ec := cfg.Get("nope"); ec != (EntryConfig{}) {
		t.Fatalf("Get on empty config = %+v, want zero", ec)
	}
	cfg.Set("x", EntryConfig{Enabled: true, Requested: true, Path: "/p", Label: "P"})
	if ec := cfg.Get("x"); !ec.Enabled || !ec.Requested || ec.Path != "/p" || ec.Label != "P" {
		t.Fatalf("Get after Set = %+v", ec)
	}
	cfg.Delete("x")
	if _, ok := cfg.Entries["x"]; ok {
		t.Fatal("Delete left the entry in place")
	}
}

func TestConfigJSONRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Set("macos.files.documents", EntryConfig{Enabled: true, Requested: true})
	cfg.Set(PathID("/tmp/proj"), EntryConfig{Enabled: true, Path: "/tmp/proj", Label: "proj"})

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ec := got.Get("macos.files.documents"); !ec.Enabled || !ec.Requested {
		t.Fatalf("builtin entry round-trip = %+v", ec)
	}
	if ec := got.Get(PathID("/tmp/proj")); !ec.Enabled || ec.Path != "/tmp/proj" || ec.Label != "proj" {
		t.Fatalf("path entry round-trip = %+v", ec)
	}
}

func TestBuildCatalogAppliesIntentAndMergesPaths(t *testing.T) {
	builtins := []Entry{
		{ID: "b1", Label: "Builtin 1", Platform: "test", Supported: true},
		{ID: "b2", Label: "Builtin 2", Platform: "test", Supported: true},
	}
	cfg := DefaultConfig()
	cfg.Set("b1", EntryConfig{Enabled: true, Requested: true})
	cfg.Set(PathID("/x/y"), EntryConfig{Enabled: true, Path: "/x/y", Label: "Y"})

	entries := buildCatalog(cfg, Options{DiscoveredPaths: []string{"/x/y", "/z/w"}}, builtins)

	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}
	b1 := byID["b1"]
	if !b1.Enabled || !b1.Requested || b1.Source != SourceBuiltin {
		t.Fatalf("b1 = %+v, want enabled+requested builtin", b1)
	}
	if b2 := byID["b2"]; b2.Enabled {
		t.Fatalf("b2 = %+v, want disabled", b2)
	}
	xy := byID[PathID("/x/y")]
	if xy.Source != SourceCustom || xy.Label != "Y" || !xy.Enabled || xy.Kind != KindPath {
		t.Fatalf("/x/y = %+v, want custom path with label Y", xy)
	}
	zw := byID[PathID("/z/w")]
	if zw.Source != SourceDiscovered || zw.Enabled {
		t.Fatalf("/z/w = %+v, want disabled discovered path", zw)
	}
}

func TestBuildCatalogDedupesSamePath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Set(PathID("/same"), EntryConfig{Enabled: true, Path: "/same", Label: "custom"})
	entries := buildCatalog(cfg, Options{DiscoveredPaths: []string{"/same"}}, nil)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Source != SourceCustom || entries[0].Label != "custom" {
		t.Fatalf("entry = %+v, want custom label to win", entries[0])
	}
}

func TestReconcileOnlyRequestsEnabledUngranted(t *testing.T) {
	entries := []Entry{
		{ID: "a", Enabled: true, Supported: true, Status: StatusDenied},
		{ID: "b", Enabled: false, Supported: true, Status: StatusDenied},
		{ID: "c", Enabled: true, Supported: true, Status: StatusGranted},
		{ID: "d", Enabled: true, Supported: false, Status: StatusNotRequired},
		{ID: "e", Enabled: true, Supported: true, Status: StatusUnknown},
	}
	var asked []string
	results := Reconcile(context.Background(), entries, func(_ context.Context, e Entry) RequestResult {
		asked = append(asked, e.ID)
		return RequestResult{ID: e.ID, Status: StatusDenied}
	})

	if len(asked) != 2 || asked[0] != "a" || asked[1] != "e" {
		t.Fatalf("asked = %v, want [a e]", asked)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
}

func TestBuiltinEntriesShape(t *testing.T) {
	entries := builtinEntries()
	if len(entries) == 0 {
		t.Fatal("builtinEntries returned nothing")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.ID == "" || e.Label == "" {
			t.Errorf("entry missing id/label: %+v", e)
		}
		if seen[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Platform != runtime.GOOS {
			t.Errorf("entry %q platform = %q, want %q", e.ID, e.Platform, runtime.GOOS)
		}
	}
	if runtime.GOOS != "darwin" {
		for _, e := range entries {
			if e.Supported {
				t.Errorf("entry %q supported on %s, want unsupported", e.ID, runtime.GOOS)
			}
		}
	}
}

func TestPathEntriesSortedAndLabeled(t *testing.T) {
	entries := pathEntries(DefaultConfig(), []string{"/b", "/a"})
	if len(entries) != 2 || entries[0].Path != "/a" || entries[1].Path != "/b" {
		t.Fatalf("pathEntries not sorted: %+v", entries)
	}
	if entries[0].Label != filepath.Base("/a") {
		t.Fatalf("label = %q, want %q", entries[0].Label, filepath.Base("/a"))
	}
}
