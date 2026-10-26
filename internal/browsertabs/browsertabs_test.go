package browsertabs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleProject(activeID string) ProjectBrowsers {
	return ProjectBrowsers{
		Tabs: []Tab{
			{ID: "b1", Title: "One"},
			{ID: "b2", Title: "Mine", ManualTitle: "Mine"},
		},
		ActiveID: activeID,
		Surfaces: map[string]Surface{
			"b1": {
				URL:          "https://a.example/",
				History:      []string{"https://a.example/"},
				HistoryIndex: 0,
				UserMode:     "chrome",
				PageTitle:    "A Site",
			},
		},
	}
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "browser-tabs.json")
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return s, path
}

func TestApplyBulkRoundTripsStripAndSurfaces(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b2")}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	got := s.All()["/proj-a"]
	if len(got.Tabs) != 2 || got.Tabs[0].ID != "b1" || got.Tabs[1].ManualTitle != "Mine" {
		t.Fatalf("tabs not preserved in order: %+v", got.Tabs)
	}
	if got.ActiveID != "b2" {
		t.Fatalf("ActiveID = %q, want b2", got.ActiveID)
	}
	surf := got.Surfaces["b1"]
	if surf.URL != "https://a.example/" || surf.UserMode != "chrome" || surf.PageTitle != "A Site" {
		t.Fatalf("surface not preserved: %+v", surf)
	}
}

// A PUT names only the projects it knows. A project absent from the patch must
// survive, or a client that never saw another client's project would wipe it.
func TestApplyBulkPreservesProjectsAbsentFromPatch(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{
		"/proj-a":          sampleProject("b1"),
		"ssh-box::/proj-b": sampleProject("b1"),
	}); err != nil {
		t.Fatalf("seed ApplyBulk: %v", err)
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b2"), 1)}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	all := s.All()
	if _, ok := all["ssh-box::/proj-b"]; !ok {
		t.Fatalf("project absent from patch was dropped: keys=%v", keysOf(all))
	}
	if all["/proj-a"].ActiveID != "b2" {
		t.Fatalf("named project not replaced: ActiveID=%q", all["/proj-a"].ActiveID)
	}
}

// An empty strip is the explicit "this project's last tab closed" signal and
// deletes the entry.
func TestApplyBulkEmptyStripDeletesProject(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": {Tabs: []Tab{}, Revision: 1}}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	if _, ok := s.All()["/proj-a"]; ok {
		t.Fatal("empty strip did not delete the project")
	}
}

// atRevision returns pb carrying the revision a client read it at.
func atRevision(pb ProjectBrowsers, rev int64) ProjectBrowsers {
	pb.Revision = rev
	return pb
}

// The first write of a project is stored at revision 1, and each accepted
// write raises it by one, whatever revision the patch carried.
func TestApplyBulkRevisionStartsAtOneAndAdvances(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b1"), 0)}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if got := s.All()["/proj-a"].Revision; got != 1 {
		t.Fatalf("revision after first write = %d, want 1", got)
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b2"), 1)}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got := s.All()["/proj-a"].Revision; got != 2 {
		t.Fatalf("revision after second write = %d, want 2", got)
	}
}

// A stale revision on any key refuses the whole patch. The fresh key in the
// same patch must not be written either, or the client would hold a partial
// view of its own edit.
func TestApplyBulkStaleRevisionWritesNothing(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{
		"/proj-a": sampleProject("b1"),
		"/proj-b": sampleProject("b1"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seeded := s.All()
	revA, revB := seeded["/proj-a"].Revision, seeded["/proj-b"].Revision
	err := s.ApplyBulk(map[string]ProjectBrowsers{
		"/proj-a": atRevision(sampleProject("b2"), revA),
		"/proj-b": atRevision(sampleProject("b2"), 0),
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ApplyBulk error = %v, want *RevisionConflictError", err)
	}
	if conflict.Key != "/proj-b" || conflict.Have != 0 || conflict.Want != revB {
		t.Fatalf("conflict = %+v, want key /proj-b have 0 want %d", conflict, revB)
	}
	all := s.All()
	for key, rev := range map[string]int64{"/proj-a": revA, "/proj-b": revB} {
		if got := all[key]; got.ActiveID != "b1" || got.Revision != rev {
			t.Fatalf("%s written despite conflict: ActiveID=%q Revision=%d", key, got.ActiveID, got.Revision)
		}
	}
}

// A deleted project's revision is never handed out again. Re-creating the
// project, through a second store on the same file, gets a higher revision, so
// a client still holding the old revision is refused.
func TestRecreatedProjectNeverReusesRevision(t *testing.T) {
	s, path := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b1"), 0)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := s.All()["/proj-a"].Revision; got != 1 {
		t.Fatalf("revision after create = %d, want 1", got)
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": {Tabs: []Tab{}, Revision: 1}}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	other, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(other): %v", err)
	}
	if err := other.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b1"), 0)}); err != nil {
		t.Fatalf("re-create: %v", err)
	}
	recreated := other.All()["/proj-a"].Revision
	if recreated <= 1 {
		t.Fatalf("re-created revision = %d, want above 1", recreated)
	}

	err = s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b2"), 1)})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale write after re-create error = %v, want *RevisionConflictError", err)
	}
	if conflict.Have != 1 || conflict.Want != recreated {
		t.Fatalf("conflict = %+v, want have 1 want %d", conflict, recreated)
	}
}

// A file written before revisions existed has no counter. It loads with the
// counter seeded to 0, so its projects take revision 1 on their next write.
func TestLegacyFileWithoutCounterLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser-tabs.json")
	legacy := `{"/proj-a":{"tabs":[{"id":"b1","title":"One"}],"active_id":"b1","surfaces":{}}}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	got, ok := s.All()["/proj-a"]
	if !ok || len(got.Tabs) != 1 || got.Revision != 0 {
		t.Fatalf("legacy project not loaded at revision 0: ok=%v %+v", ok, got)
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b2"), 0)}); err != nil {
		t.Fatalf("ApplyBulk over legacy project: %v", err)
	}
	if rev := s.All()["/proj-a"].Revision; rev != 1 {
		t.Fatalf("revision after first write over legacy file = %d, want 1", rev)
	}
}

// Deleting a project is a write, so it needs the revision the store holds.
func TestApplyBulkDeleteNeedsMatchingRevision(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": {Tabs: []Tab{}, Revision: 0}})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale delete error = %v, want *RevisionConflictError", err)
	}
	if _, ok := s.All()["/proj-a"]; !ok {
		t.Fatal("stale delete removed the project")
	}
	if err := s.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": {Tabs: []Tab{}, Revision: 1}}); err != nil {
		t.Fatalf("matching delete: %v", err)
	}
	if _, ok := s.All()["/proj-a"]; ok {
		t.Fatal("matching delete did not remove the project")
	}
}

// Two writers share one file. The second writer's stale copy is refused, it
// re-reads the current revision, and its retry then lands on top of the first.
func TestApplyBulkSecondWriterRetriesAfterConflict(t *testing.T) {
	a, path := newTestStore(t)
	b, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(b): %v", err)
	}
	if err := a.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b1")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	readByB := b.All()["/proj-a"]

	if err := a.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b2"), 1)}); err != nil {
		t.Fatalf("writer A: %v", err)
	}

	stale := atRevision(sampleProject("b1"), readByB.Revision)
	err = b.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": stale})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("writer B stale write error = %v, want *RevisionConflictError", err)
	}

	fresh := b.All()["/proj-a"]
	if fresh.ActiveID != "b2" || fresh.Revision != 2 {
		t.Fatalf("writer B re-read ActiveID=%q Revision=%d, want b2 and 2", fresh.ActiveID, fresh.Revision)
	}
	if err := b.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": atRevision(sampleProject("b1"), fresh.Revision)}); err != nil {
		t.Fatalf("writer B retry: %v", err)
	}
	if got := a.All()["/proj-a"]; got.ActiveID != "b1" || got.Revision != 3 {
		t.Fatalf("writer A did not see retry: ActiveID=%q Revision=%d", got.ActiveID, got.Revision)
	}
}

// Keys are opaque. A host-qualified key with a Windows drive letter or a WSL
// host must come back byte-identical, which filepath.Clean would not do.
func TestKeysAreOpaqueAndNotCleaned(t *testing.T) {
	s, _ := newTestStore(t)
	key := `wsl:Ubuntu::/home/x//proj/`
	if err := s.ApplyBulk(map[string]ProjectBrowsers{key: sampleProject("")}); err != nil {
		t.Fatalf("ApplyBulk: %v", err)
	}
	if _, ok := s.All()[key]; !ok {
		t.Fatalf("key was rewritten; keys=%v", keysOf(s.All()))
	}
}

// Two store instances on one file model two server processes. The second must
// see the first's write without restarting.
func TestSecondProcessSeesWriteWithoutReload(t *testing.T) {
	a, path := newTestStore(t)
	b, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(b): %v", err)
	}
	if err := a.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b1")}); err != nil {
		t.Fatalf("ApplyBulk(a): %v", err)
	}
	if _, ok := b.All()["/proj-a"]; !ok {
		t.Fatal("write from process A not visible to process B")
	}
}

// A second process's write merges into the file instead of overwriting the
// first's projects.
func TestConcurrentInstancesMergeInsteadOfClobbering(t *testing.T) {
	a, path := newTestStore(t)
	b, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(b): %v", err)
	}
	if err := a.ApplyBulk(map[string]ProjectBrowsers{"/proj-a": sampleProject("b1")}); err != nil {
		t.Fatalf("ApplyBulk(a): %v", err)
	}
	if err := b.ApplyBulk(map[string]ProjectBrowsers{"/proj-b": sampleProject("b1")}); err != nil {
		t.Fatalf("ApplyBulk(b): %v", err)
	}
	fresh, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt(fresh): %v", err)
	}
	all := fresh.All()
	if _, ok := all["/proj-a"]; !ok {
		t.Fatalf("process B's write clobbered process A's project; keys=%v", keysOf(all))
	}
	if _, ok := all["/proj-b"]; !ok {
		t.Fatalf("process B's project missing; keys=%v", keysOf(all))
	}
}

// A corrupt file loads as an empty store (NewStoreAt logs the reason), and the
// parse error is still returned by load so it is never hidden.
func TestCorruptFileIsReportedByLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser-tabs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if got := len(s.All()); got != 0 {
		t.Fatalf("corrupt file should load as empty, got %d projects", got)
	}
	s.mu.Lock()
	loadErr := s.load()
	s.mu.Unlock()
	if loadErr == nil || !strings.Contains(loadErr.Error(), "parse") {
		t.Fatalf("load error = %v, want a parse error", loadErr)
	}
}

func TestValidateAcceptsWellFormedProject(t *testing.T) {
	if err := sampleProject("b2").Validate(); err != nil {
		t.Fatalf("Validate rejected a well-formed project: %v", err)
	}
	if err := (ProjectBrowsers{Tabs: []Tab{{ID: "b1", Title: "New tab"}}}).Validate(); err != nil {
		t.Fatalf("a background-opened strip with no active tab must be valid: %v", err)
	}
}

func TestValidateRejectsMalformedProjects(t *testing.T) {
	ok := sampleProject("b1")
	cases := map[string]func(p *ProjectBrowsers){
		"tab without id":         func(p *ProjectBrowsers) { p.Tabs[0].ID = "" },
		"duplicate tab id":       func(p *ProjectBrowsers) { p.Tabs[1].ID = "b1" },
		"blank title":            func(p *ProjectBrowsers) { p.Tabs[0].Title = "   " },
		"active id not in strip": func(p *ProjectBrowsers) { p.ActiveID = "ghost" },
		"surface for unknown tab": func(p *ProjectBrowsers) {
			p.Surfaces["ghost"] = Surface{URL: "https://x.example/", History: []string{"https://x.example/"}}
		},
		"javascript url":     func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.URL = "javascript:alert(1)"; p.Surfaces["b1"] = s },
		"data url":           func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.URL = "data:text/html,hi"; p.Surfaces["b1"] = s },
		"hostless https url": func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.URL = "https:///path"; p.Surfaces["b1"] = s },
		"bad history entry": func(p *ProjectBrowsers) {
			s := p.Surfaces["b1"]
			s.History = []string{"file:///etc/passwd"}
			p.Surfaces["b1"] = s
		},
		"history index past end": func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.HistoryIndex = 1; p.Surfaces["b1"] = s },
		"empty history":          func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.History = nil; p.Surfaces["b1"] = s },
		"unknown user mode":      func(p *ProjectBrowsers) { s := p.Surfaces["b1"]; s.UserMode = "tor"; p.Surfaces["b1"] = s },
		"negative revision":      func(p *ProjectBrowsers) { p.Revision = -1 },
		"too many tabs": func(p *ProjectBrowsers) {
			p.Tabs = make([]Tab, MaxTabs+1)
			for i := range p.Tabs {
				p.Tabs[i] = Tab{ID: string(rune('a' + i)), Title: "t"}
			}
		},
		"oversized url": func(p *ProjectBrowsers) {
			s := p.Surfaces["b1"]
			s.URL = "https://a.example/" + strings.Repeat("x", MaxURLBytes)
			p.Surfaces["b1"] = s
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := ProjectBrowsers{Tabs: append([]Tab(nil), ok.Tabs...), ActiveID: ok.ActiveID, Surfaces: map[string]Surface{}}
			for k, v := range ok.Surfaces {
				p.Surfaces[k] = v
			}
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate accepted a malformed project")
			}
		})
	}
}

func keysOf(m map[string]ProjectBrowsers) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
