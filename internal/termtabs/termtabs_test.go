package termtabs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func mustStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return s
}

func list(ids ...string) []Terminal {
	out := make([]Terminal, 0, len(ids))
	for _, id := range ids {
		out = append(out, Terminal{ID: id, Title: "Terminal " + id})
	}
	return out
}

func idsOf(pt ProjectTerminals) []string {
	out := make([]string, 0, len(pt.Terminals))
	for _, term := range pt.Terminals {
		out = append(out, term.ID)
	}
	return out
}

// The whole point of the package: two clients on one server converge on the
// same terminal list, so a terminal opened in one is visible in the other.
func TestApplyBulkThenGetSeesAnotherClientsTerminals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	desktop := mustStore(t, path)
	browser := mustStore(t, path)

	if err := desktop.ApplyBulk(map[string]ProjectTerminals{
		"/srv/app": {Terminals: list("term-1")},
	}); err != nil {
		t.Fatalf("desktop write: %v", err)
	}

	got := idsOf(browser.Get("/srv/app"))
	if len(got) != 1 || got[0] != "term-1" {
		t.Fatalf("second client sees %v, want [term-1]", got)
	}
}

// A client only ever sends the projects it knows about. Omitting a key must
// preserve it, or the browser would wipe the desktop app's terminals on its
// next save — the exact merge internal/tabs documents for session tabs.
func TestApplyBulkPreservesProjectsAbsentFromThePatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	s := mustStore(t, path)

	if err := s.ApplyBulk(map[string]ProjectTerminals{
		"/a": {Terminals: list("t-a")},
		"/b": {Terminals: list("t-b")},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A client that only knows about /a writes only /a.
	if err := s.ApplyBulk(map[string]ProjectTerminals{
		"/a": {Terminals: list("t-a", "t-a2")},
	}); err != nil {
		t.Fatalf("partial write: %v", err)
	}

	all := s.All()
	if got := idsOf(all["/b"]); len(got) != 1 || got[0] != "t-b" {
		t.Fatalf("/b = %v, want [t-b] — an absent key must be preserved", got)
	}
	if got := idsOf(all["/a"]); len(got) != 2 {
		t.Fatalf("/a = %v, want two terminals", got)
	}
}

// An EMPTY list is how a client says "I closed this project's last tab". It
// must delete, not persist an empty entry — and, symmetrically, the frontend
// has to send that explicit empty entry (see the package doc).
func TestApplyBulkEmptyListDeletesTheProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	s := mustStore(t, path)

	if err := s.ApplyBulk(map[string]ProjectTerminals{"/a": {Terminals: list("t-a")}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.ApplyBulk(map[string]ProjectTerminals{"/a": {Terminals: nil}}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := idsOf(s.Get("/a")); len(got) != 0 {
		t.Fatalf("after clear /a = %v, want empty", got)
	}
	if _, ok := s.All()["/a"]; ok {
		t.Fatal("cleared project must not linger in All()")
	}
}

// The remote key is `<host>::<path>` and may itself contain a colon
// (`wsl:Ubuntu`). filepath.Clean would mangle both, so keys must round-trip
// verbatim — this is the one place where copying internal/tabs' Clean call
// would silently merge a remote project with a different one.
func TestKeysWithColonsRoundTripVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	s := mustStore(t, path)

	keys := []string{
		"james@217.216.72.49::/home/james/www/aimsai2",
		"wsl:Ubuntu::/home/dev/app",
		`C:\Users\dev\app`,
		"/srv/app/", // trailing slash must survive too: the client owns the form
	}
	for i, k := range keys {
		want := list("t-" + string(rune('a'+i)))
		if err := s.Set(k, ProjectTerminals{Terminals: want}); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}
	all := s.All()
	if len(all) != len(keys) {
		t.Fatalf("stored %d keys, want %d (they must not collide): %v", len(all), len(keys), all)
	}
	for i, k := range keys {
		if _, ok := all[k]; !ok {
			t.Fatalf("key %q was rewritten; stored keys: %v", k, keysOf(all))
		}
		wantID := "t-" + string(rune('a'+i))
		if got := idsOf(all[k]); len(got) != 1 || got[0] != wantID {
			t.Fatalf("key %q = %v, want [%s]", k, got, wantID)
		}
	}
}

func keysOf(m map[string]ProjectTerminals) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Rename and OSC-title metadata must survive the round trip; a terminal renamed
// in one window has to still be renamed in the other.
func TestTerminalMetadataRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	s := mustStore(t, path)

	want := []Terminal{
		{ID: "t1", Title: "vim", Renamed: true},
		{ID: "t2", Title: "Terminal 2", OSCTitle: "npm run dev"},
	}
	if err := s.Set("/srv/app", ProjectTerminals{Terminals: want}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk map[string]ProjectTerminals
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	stored := onDisk["/srv/app"].Terminals
	if len(stored) != 2 {
		t.Fatalf("got %d terminals, want 2", len(stored))
	}
	if stored[0] != want[0] || stored[1] != want[1] {
		t.Fatalf("round trip changed metadata: %+v", stored)
	}
	// A second store must observe the same file.
	if got := mustStore(t, path).Get("/srv/app").Terminals; len(got) != 2 || got[1].OSCTitle != "npm run dev" {
		t.Fatalf("second store read %+v, want the OSC title preserved", got)
	}
}

// Two Store instances in one process (the desktop server and a dev server
// sharing the data dir) writing concurrently must merge, not clobber. The
// read-modify-write runs under a cross-process file lock, so this also guards
// the lock discipline.
func TestConcurrentWritersMergeAcrossStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	seed := mustStore(t, path)
	if err := seed.ApplyBulk(map[string]ProjectTerminals{"/base": {Terminals: list("t-base")}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := NewStoreAt(path)
			if err != nil {
				errs[i] = err
				return
			}
			key := "/p" + string(rune('a'+i))
			errs[i] = s.ApplyBulk(map[string]ProjectTerminals{key: {Terminals: list("t-" + string(rune('a'+i)))}})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	all := seed.All()
	if len(all) != writers+1 {
		t.Fatalf("concurrent writers lost entries: got %d keys, want %d (%v)", len(all), writers+1, keysOf(all))
	}
	if _, ok := all["/base"]; !ok {
		t.Fatal("the pre-existing project was clobbered by a concurrent writer")
	}
}

// A corrupt or truncated file must not wedge the store: it starts fresh rather
// than failing every request, matching internal/tabs.
func TestCorruptFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	s := mustStore(t, path)
	if got := idsOf(s.Get("/anything")); len(got) != 0 {
		t.Fatalf("corrupt file yielded %v, want empty", got)
	}
	if err := s.Set("/a", ProjectTerminals{Terminals: list("t-a")}); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	if got := idsOf(s.Get("/a")); len(got) != 1 {
		t.Fatalf("write after recovery did not stick: %v", got)
	}
}

func TestSetRejectsEmptyKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminals.json")
	s := mustStore(t, path)
	if err := s.Set("   ", ProjectTerminals{Terminals: list("t-a")}); err == nil {
		t.Fatal("Set with a blank key must error rather than write a junk entry")
	}
}
