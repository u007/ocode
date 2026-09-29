package remotecli

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// installStore points the newStore seam at a temp-dir store and restores the
// real seam on cleanup. Every persistence test goes through here so no test in
// this package can write to the developer's real projects.json.
func installStore(t *testing.T) *projects.Store {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	prev := newStore
	newStore = func() (*projects.Store, *projects.GroupStore, error) { return store, nil, nil }
	t.Cleanup(func() { newStore = prev })
	return store
}

// installConnect stubs both connect seams. established drives whether the
// connect reports the host as usable (it calls OnEstablished, exactly as the
// real Connect/ConnectWeb do once the prepare stages pass); returned is the
// error the stubbed connect hands back to Run.
func installConnect(t *testing.T, established bool, returned error) {
	t.Helper()
	stub := func(opts remote.ConnectOptions) error {
		if established && opts.OnEstablished != nil {
			opts.OnEstablished()
		}
		return returned
	}
	prevConnect, prevConnectWeb := connect, connectWeb
	connect, connectWeb = stub, stub
	t.Cleanup(func() { connect, connectWeb = prevConnect, prevConnectWeb })
}

// remoteEntries returns the (host, path) pairs currently persisted, so a test
// can assert on presence without depending on ordering.
func remoteEntries(store *projects.Store) map[string]bool {
	out := map[string]bool{}
	for _, p := range store.List() {
		if p.Host != "" {
			out[p.Host+"|"+p.Path] = true
		}
	}
	return out
}

// The bug: a host that never becomes reachable must not leave a project entry
// behind. Before the fix Run called AddRemote unconditionally after the connect
// returned, so a failed connect persisted a dead project (this is exactly how
// nosuchhost.invalid ended up in a real developer's project list).
func TestRunDoesNotPersistProjectWhenHostNeverEstablishes(t *testing.T) {
	store := installStore(t)
	installConnect(t, false, errors.New("ssh: could not resolve hostname"))

	if err := Run([]string{"nosuchhost.invalid"}); err == nil {
		t.Fatal("expected the connect error to propagate")
	}

	if got := remoteEntries(store); len(got) != 0 {
		t.Errorf("failed connect persisted project entries: %v", got)
	}
}

// Same for --web, which additionally writes the entry BEFORE connecting so the
// PortMapHook can key off it. That pre-write must be rolled back when the host
// never establishes.
func TestRunWebDoesNotPersistProjectWhenHostNeverEstablishes(t *testing.T) {
	store := installStore(t)
	installConnect(t, false, errors.New("ssh: could not resolve hostname"))

	if err := Run([]string{"--web", "nosuchhost.invalid"}); err == nil {
		t.Fatal("expected the connect error to propagate")
	}

	if got := remoteEntries(store); len(got) != 0 {
		t.Errorf("failed --web connect left a pre-connect entry behind: %v", got)
	}
}

// The counter-case that must keep working: a host that DID establish and was
// then disconnected deliberately (Ctrl-C exits Connect with an error) is a
// real project the user wants to reattach to, so the entry is recorded and the
// entry's last_used_at is refreshed.
func TestRunPersistsProjectAfterEstablishedConnect(t *testing.T) {
	store := installStore(t)
	installConnect(t, true, errors.New("session ended"))

	if err := Run([]string{"user@host", "/proj"}); err == nil {
		t.Fatal("expected the disconnect error to propagate")
	}

	if !remoteEntries(store)["user@host|/proj"] {
		t.Errorf("established-then-disconnected host was not persisted: %v", remoteEntries(store))
	}
}

// --web with an established host keeps its entry too (the pre-connect write
// must not be rolled back in that case).
func TestRunWebPersistsProjectAfterEstablishedConnect(t *testing.T) {
	store := installStore(t)
	installConnect(t, true, nil)

	if err := Run([]string{"--web", "user@host"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !remoteEntries(store)["user@host|~"] {
		t.Errorf("established --web host was not persisted: %v", remoteEntries(store))
	}
}

// A failed connect must never delete a project the user already had. Only the
// entry this call created is rolled back.
func TestRunKeepsPreExistingEntryWhenConnectFails(t *testing.T) {
	store := installStore(t)
	if err := store.AddRemote("user@host", "/proj"); err != nil {
		t.Fatalf("seed AddRemote: %v", err)
	}
	installConnect(t, false, errors.New("host went away"))

	if err := Run([]string{"user@host", "/proj"}); err == nil {
		t.Fatal("expected the connect error to propagate")
	}

	if !remoteEntries(store)["user@host|/proj"] {
		t.Errorf("pre-existing project was removed by a failed connect: %v", remoteEntries(store))
	}
}

// A pre-existing entry for the same host but a DIFFERENT path is likewise left
// alone, and the new failing path is not persisted alongside it.
func TestRunKeepsSiblingEntryWhenConnectFails(t *testing.T) {
	store := installStore(t)
	if err := store.AddRemote("user@host", "/keepme"); err != nil {
		t.Fatalf("seed AddRemote: %v", err)
	}
	installConnect(t, false, errors.New("host went away"))

	// No explicit path, so Run defaults to the last-used remote path for this
	// host — which is /keepme, i.e. the pre-existing entry is re-used rather
	// than a second one being created.
	if err := Run([]string{"user@host", "/fresh"}); err == nil {
		t.Fatal("expected the connect error to propagate")
	}

	got := remoteEntries(store)
	if !got["user@host|/keepme"] {
		t.Errorf("pre-existing sibling entry was removed: %v", got)
	}
	if got["user@host|/fresh"] {
		t.Errorf("failed connect persisted a new path: %v", got)
	}
}

func TestShouldPersistRemote(t *testing.T) {
	cases := []struct {
		name        string
		hostKey     string
		established bool
		want        bool
	}{
		{"established", "user@host", true, true},
		{"not established", "user@host", false, false},
		{"empty host", "", true, false},
		{"whitespace host", "   ", true, false},
		{"empty host and not established", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPersistRemote(tc.hostKey, tc.established); got != tc.want {
				t.Errorf("shouldPersistRemote(%q, %v) = %v, want %v", tc.hostKey, tc.established, got, tc.want)
			}
		})
	}
}

// Regression for a vacuous guard: the "never delete a pre-existing project"
// rule only has teeth on the --web path, because that is the only path that
// writes an entry before connecting. A non-web test cannot catch a rollback
// that is too eager, so this one seeds the entry and drives --web.
func TestRunWebKeepsPreExistingEntryWhenConnectFails(t *testing.T) {
	store := installStore(t)
	if err := store.AddRemote("user@host", "~"); err != nil {
		t.Fatalf("seed AddRemote: %v", err)
	}
	installConnect(t, false, errors.New("host went away"))

	if err := Run([]string{"--web", "user@host"}); err == nil {
		t.Fatal("expected the connect error to propagate")
	}

	if !remoteEntries(store)["user@host|~"] {
		t.Errorf("failed --web connect deleted a pre-existing project: %v", remoteEntries(store))
	}
}
