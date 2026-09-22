package tabs

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(%s): %v", path, err)
	}
	return s
}

func pt(ids ...string) ProjectTabs {
	tabs := make([]Tab, 0, len(ids))
	for _, id := range ids {
		tabs = append(tabs, Tab{ID: id, Title: id})
	}
	return ProjectTabs{Tabs: tabs, Active: ids[len(ids)-1]}
}

// A later merge must not drop a project the caller didn't mention. This is
// the whole-map-clobber regression: two clients with divergent project sets
// would otherwise overwrite each other's tabs.json.
func TestApplyBulkPreservesProjectsAbsentFromPatch(t *testing.T) {
	s := newTestStore(t, filepath.Join(t.TempDir(), "tabs.json"))

	if err := s.ApplyBulk(map[string]ProjectTabs{"/mail-archive": pt("m1")}); err != nil {
		t.Fatalf("ApplyBulk(mail): %v", err)
	}
	if err := s.ApplyBulk(map[string]ProjectTabs{"/ocode": pt("o1")}); err != nil {
		t.Fatalf("ApplyBulk(ocode): %v", err)
	}

	all := s.All()
	if _, ok := all["/mail-archive"]; !ok {
		t.Fatalf("/mail-archive must survive a merge that doesn't mention it: %+v", all)
	}
	if _, ok := all["/ocode"]; !ok {
		t.Fatalf("/ocode should be stored: %+v", all)
	}
}

// Two store instances model two server processes sharing one tabs.json (the
// desktop .app plus a dev server). Each writes its own project; neither may
// lose the other's, and a read must pick up the other process's write.
func TestApplyBulkMergesAcrossStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs.json")
	storeA := newTestStore(t, path)
	storeB := newTestStore(t, path)

	if err := storeA.ApplyBulk(map[string]ProjectTabs{"/mail-archive": pt("m1")}); err != nil {
		t.Fatalf("storeA.ApplyBulk: %v", err)
	}
	// storeB still has an empty in-memory cache; its merge must reload the
	// file so storeA's project survives.
	if err := storeB.ApplyBulk(map[string]ProjectTabs{"/ocode": pt("o1")}); err != nil {
		t.Fatalf("storeB.ApplyBulk: %v", err)
	}

	for name, s := range map[string]*Store{"A": storeA, "B": storeB} {
		all := s.All()
		if _, ok := all["/mail-archive"]; !ok {
			t.Fatalf("store%s lost /mail-archive: %+v", name, all)
		}
		if _, ok := all["/ocode"]; !ok {
			t.Fatalf("store%s lost /ocode: %+v", name, all)
		}
	}
}

// An empty tab list is the explicit deletion signal (how a client persists
// "the last tab of this project was closed").
func TestApplyBulkEmptyListDeletes(t *testing.T) {
	s := newTestStore(t, filepath.Join(t.TempDir(), "tabs.json"))
	if err := s.ApplyBulk(map[string]ProjectTabs{"/a": pt("a1"), "/b": pt("b1")}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	if err := s.ApplyBulk(map[string]ProjectTabs{"/a": {Tabs: nil}}); err != nil {
		t.Fatalf("ApplyBulk(empty): %v", err)
	}
	all := s.All()
	if _, ok := all["/a"]; ok {
		t.Fatalf("/a should be deleted by the empty entry: %+v", all)
	}
	if _, ok := all["/b"]; !ok {
		t.Fatalf("/b must survive: %+v", all)
	}
}

func TestSetEmptyDeletes(t *testing.T) {
	s := newTestStore(t, filepath.Join(t.TempDir(), "tabs.json"))
	if err := s.Set("/a", pt("a1")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("/a", ProjectTabs{}); err != nil {
		t.Fatalf("Set(empty): %v", err)
	}
	if _, ok := s.All()["/a"]; ok {
		t.Fatalf("/a should be cleared by an empty Set")
	}
}

// A read must not leave temp files behind (atomic writes are renamed away).
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tabs.json")
	s := newTestStore(t, path)
	if err := s.ApplyBulk(map[string]ProjectTabs{"/a": pt("a1")}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
