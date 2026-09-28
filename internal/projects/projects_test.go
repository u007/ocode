package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// NewStoreAt must persist additions across instances (same JSON path).
func TestNewStoreAtRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")

	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/a/b"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reloaded, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt reload: %v", err)
	}
	list := reloaded.List()
	if len(list) != 1 || list[0].Path != "/a/b" {
		t.Fatalf("reloaded list = %+v, want [/a/b]", list)
	}
}

// Add is idempotent: re-adding an existing path keeps a single entry.
func TestAddIdempotent(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/x/y"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("/x/y"); err != nil {
		t.Fatal(err)
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("list length = %d, want 1", got)
	}
}

// AddRemote scopes identity to (host, path): the same path on two different
// hosts is two distinct entries, and remote entries never collide with a
// local project at the same path string.
func TestAddRemoteScopedByHost(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("otherhost", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	list := store.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 distinct entries (local + 2 hosts), got %d: %+v", len(list), list)
	}

	// Re-adding the same (host, path) upserts rather than duplicating.
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if got := len(store.List()); got != 3 {
		t.Fatalf("expected AddRemote re-add to upsert, list length = %d, want 3", got)
	}
}

// Local Touch/Rename must never match a remote entry that happens to share
// the same Path string.
func TestLocalOpsNeverMatchRemoteEntry(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	// Touch on a local path identical to the remote one should be a no-op
	// (nothing local exists yet), not silently touch the remote entry.
	if err := store.Touch("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].Host != "devbox" {
		t.Fatalf("local Touch mutated or matched the remote entry: %+v", list)
	}

	if err := store.Rename("/home/user/app", "should not apply"); err == nil {
		t.Fatal("expected Rename to report not-found rather than renaming the remote entry")
	}
}

// Reorder (like Touch/Rename/SetGroup) must never touch a remote entry
// that happens to share a Path string with a local project.
func TestReorderNeverMatchesRemoteEntry(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("/home/user/other"); err != nil {
		t.Fatal(err)
	}

	// Reorder local paths only — but include the shared-path string as if
	// it were meant for the local project. The remote entry's Order must
	// stay untouched (its zero value), never silently reassigned.
	if err := store.Reorder([]string{"/home/user/app", "/home/user/other"}); err != nil {
		t.Fatal(err)
	}

	list := store.List()
	for _, p := range list {
		if p.Host == "devbox" && p.Order != 0 {
			t.Errorf("Reorder mutated remote entry %+v (Order should stay 0)", p)
		}
	}
}

func TestFindLastRemotePicksMostRecent(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/proj/a"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/proj/b"); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchRemote("devbox", "/proj/a"); err != nil {
		t.Fatal(err)
	}

	got, ok := store.FindLastRemote("devbox")
	if !ok || got.Path != "/proj/a" {
		t.Fatalf("FindLastRemote = %+v, ok=%v, want /proj/a", got, ok)
	}

	if _, ok := store.FindLastRemote("no-such-host"); ok {
		t.Fatal("expected no match for an unknown host")
	}
}

// RenameRef renames a remote SSH entry without touching a local or other-host
// entry sharing the same path string.
func TestRenameRefRemoteSSH(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	if err := store.RenameRef(ProjectRef{Host: "devbox", Path: "/home/user/app"}, "renamed-ssh"); err != nil {
		t.Fatalf("RenameRef remote: %v", err)
	}

	for _, p := range store.List() {
		switch {
		case p.Host == "devbox":
			if p.Name != "renamed-ssh" {
				t.Errorf("remote name = %q, want renamed-ssh", p.Name)
			}
		case p.Host == "":
			if p.Name == "renamed-ssh" {
				t.Errorf("local entry was renamed: %+v", p)
			}
		}
	}
}

// RenameRef renames a WSL entry (host "wsl:<distro>") in isolation.
func TestRenameRefRemoteWSL(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("wsl:Ubuntu", `C:\Users\james\app`); err != nil {
		t.Fatal(err)
	}

	if err := store.RenameRef(ProjectRef{Host: "wsl:Ubuntu", Path: `C:\Users\james\app`}, "wsl-app"); err != nil {
		t.Fatalf("RenameRef WSL: %v", err)
	}

	got := store.List()
	if len(got) != 1 || got[0].Name != "wsl-app" || got[0].Path != `C:\Users\james\app` {
		t.Fatalf("WSL entry after rename = %+v, want path preserved with new name", got)
	}
}

// Remote paths are matched verbatim: a POSIX-style request must not match a
// stored Windows-style path (and must not Clean/rewrite either spelling).
func TestRenameRefRemotePathVerbatim(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("wsl:Ubuntu", `C:\Users\james\app`); err != nil {
		t.Fatal(err)
	}

	if err := store.RenameRef(ProjectRef{Host: "wsl:Ubuntu", Path: "C:/Users/james/app"}, "nope"); err == nil {
		t.Fatal("expected not-found for differently-spelled remote path")
	}
	got := store.List()
	if len(got) != 1 || got[0].Path != `C:\Users\james\app` || got[0].Name == "nope" {
		t.Fatalf("stored remote path was rewritten: %+v", got)
	}
}

// SetGroupRef scopes grouping to (host, path): same path on local, SSH,
// and WSL entries must stay isolated.
func TestSetGroupRefScopedByHost(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("wsl:Ubuntu", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	if err := store.SetGroupRef(ProjectRef{Host: "devbox", Path: "/home/user/app"}, "g"); err != nil {
		t.Fatalf("SetGroupRef remote: %v", err)
	}
	for _, p := range store.List() {
		want := ""
		if p.Host == "devbox" {
			want = "g"
		}
		if p.Group != want {
			t.Errorf("entry %+v group = %q, want %q", p, p.Group, want)
		}
	}
}

// ReorderRefs orders remote entries by scoped identity: two hosts sharing
// the same path string get their own positions.
func TestReorderRefsScopedByHost(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/local"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("wsl:Ubuntu", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	refs := []ProjectRef{
		{Host: "wsl:Ubuntu", Path: "/home/user/app"},
		{Path: "/home/user/local"},
		{Host: "devbox", Path: "/home/user/app"},
	}
	if err := store.ReorderRefs(refs); err != nil {
		t.Fatalf("ReorderRefs: %v", err)
	}
	byKey := map[string]int{}
	for _, p := range store.List() {
		byKey[p.Host+"\x00"+p.Path] = p.Order
	}
	if byKey["wsl:Ubuntu\x00/home/user/app"] != 1 ||
		byKey["\x00/home/user/local"] != 2 ||
		byKey["devbox\x00/home/user/app"] != 3 {
		t.Fatalf("orders = %+v, want wsl=1 local=2 devbox=3", byKey)
	}
}

func TestLegacyRemoteProjectMigratesStructuredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	data := `[ {"path":"/srv/app","name":"app","host":"alice@example.com"} ]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	got := store.List()[0]
	if got.RemoteKind != "ssh" || got.RemoteUser != "alice" || got.RemoteHost != "example.com" {
		t.Fatalf("migration = %+v", got)
	}
}

func TestUpdateRemotePreservesMetadataAndRejectsCollision(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("alice@old.example", "/srv/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGroupRef(ProjectRef{Host: "alice@old.example", Path: "/srv/app"}, "work"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("bob@example.com", "/srv/other"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateRemote(ProjectRef{Host: "alice@old.example", Path: "/srv/app"}, remote.Target{Kind: remote.KindSSH, User: "carol", Host: "new.example", Port: 2222}, "/srv/new")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Host != "carol@new.example" || updated.RemotePort != 2222 || updated.Path != "/srv/new" || updated.Group != "work" {
		t.Fatalf("updated = %+v", updated)
	}
	_, err = store.UpdateRemote(ProjectRef{Host: "carol@new.example", Path: "/srv/new"}, remote.Target{Kind: remote.KindSSH, Host: "bob@example.com"}, "/srv/other")
	if err == nil {
		t.Fatal("expected collision")
	}
}

func TestUpdateRemoteRejectsWSLPort(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("wsl:Ubuntu", `/home/app`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRemote(ProjectRef{Host: "wsl:Ubuntu", Path: "/home/app"}, remote.Target{Kind: remote.KindWSL, Distro: "Debian", Port: 22}, `/home/app`); err == nil {
		t.Fatal("expected WSL port validation error")
	}
}

// DuplicateAsRemote creates a distinct (host, path) entry that carries the
// source's display name and group, and coexists with the local source.
func TestDuplicateAsRemoteInheritsNameAndGroup(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.RenameRef(ProjectRef{Path: "/home/user/app"}, "My App"); err != nil {
		t.Fatalf("RenameRef: %v", err)
	}
	if err := store.SetGroupRef(ProjectRef{Path: "/home/user/app"}, "work"); err != nil {
		t.Fatalf("SetGroupRef: %v", err)
	}

	dup, err := store.DuplicateAsRemote("devbox", "/home/user/app", 0, "My App", "work")
	if err != nil {
		t.Fatalf("DuplicateAsRemote: %v", err)
	}
	if dup.Host != "devbox" || dup.Path != "/home/user/app" {
		t.Fatalf("duplicate identity = %q:%q, want devbox:/home/user/app", dup.Host, dup.Path)
	}
	if dup.Name != "My App" || dup.Group != "work" {
		t.Fatalf("duplicate name/group = %q/%q, want My App/work", dup.Name, dup.Group)
	}
	if dup.RemoteKind != "ssh" || dup.RemoteHost != "devbox" {
		t.Fatalf("duplicate remote fields = %q/%q, want ssh/devbox", dup.RemoteKind, dup.RemoteHost)
	}

	// The local source is untouched and both entries coexist.
	list := store.List()
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2 (local + duplicate): %+v", len(list), list)
	}
	local := list[0]
	for _, p := range list {
		if p.Host == "" {
			local = p
		}
	}
	if local.Name != "My App" || local.Group != "work" {
		t.Fatalf("local source mutated: %+v", local)
	}
}

// DuplicateAsRemote refuses a target that is already saved (unlike AddRemote,
// which upserts) and reports the sentinel error.
func TestDuplicateAsRemoteRejectsExistingTarget(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DuplicateAsRemote("devbox", "/home/user/app", 0, "", ""); err == nil {
		t.Fatal("expected ErrProjectExists, got nil")
	} else if !errors.Is(err, ErrProjectExists) {
		t.Fatalf("error = %v, want ErrProjectExists", err)
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("list length = %d, want 1 (no new entry on conflict)", got)
	}
}

// DuplicateAsRemote canonicalizes the host before the conflict check, so a
// duplicate of the same target expressed a different way still conflicts.
func TestDuplicateAsRemoteCanonicalizesHost(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("james@devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DuplicateAsRemote("james@devbox", "/home/user/app", 0, "", ""); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("error = %v, want ErrProjectExists", err)
	}
	// A different host is a distinct entry.
	if _, err := store.DuplicateAsRemote("otherhost", "/home/user/app", 0, "", ""); err != nil {
		t.Fatalf("DuplicateAsRemote(otherhost): %v", err)
	}
	if got := len(store.List()); got != 2 {
		t.Fatalf("list length = %d, want 2", got)
	}
}

// DuplicateAsRemote validates the target and requires a path.
func TestDuplicateAsRemoteValidatesTarget(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := store.DuplicateAsRemote("bad/host", "/p", 0, "", ""); err == nil {
		t.Fatal("expected invalid target error")
	}
	if _, err := store.DuplicateAsRemote("devbox", "", 0, "", ""); err == nil {
		t.Fatal("expected missing path error")
	}
	// WSL rejects an SSH port (Validate), mirroring AddRemote.
	if _, err := store.DuplicateAsRemote("wsl:Ubuntu", "/home/app", 22, "", ""); err == nil {
		t.Fatal("expected WSL port validation error")
	}
}
