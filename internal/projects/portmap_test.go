package projects

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newRemotePortMapStore(t *testing.T) (*Store, ProjectRef) {
	t.Helper()
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("user@host", "/proj"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	return store, ProjectRef{Host: "user@host", Path: "/proj"}
}

func TestAddPortMapUpsertsByRemotePort(t *testing.T) {
	store, ref := newRemotePortMapStore(t)

	if err := store.AddPortMap(ref, 3000, 3000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	// Re-adding the same remote port with a different local port updates
	// the existing entry in place rather than appending a second one.
	if err := store.AddPortMap(ref, 3000, 4000); err != nil {
		t.Fatalf("AddPortMap (update): %v", err)
	}

	maps, err := store.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].LocalPort != 4000 || !maps[0].Enabled {
		t.Fatalf("PortMaps = %+v, want one entry with LocalPort=4000, Enabled=true", maps)
	}
}

func TestPortMapPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	ref := ProjectRef{Host: "user@host", Path: "/proj"}
	if err := store.AddRemote(ref.Host, ref.Path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := store.AddPortMap(ref, 8080, 8081); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}

	reloaded, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt reload: %v", err)
	}
	maps, err := reloaded.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps after reload: %v", err)
	}
	if len(maps) != 1 || maps[0].RemotePort != 8080 || maps[0].LocalPort != 8081 {
		t.Fatalf("PortMaps after reload = %+v, want [{8080 8081 true}]", maps)
	}
}

func TestSetPortMapEnabledTogglesWithoutRemoving(t *testing.T) {
	store, ref := newRemotePortMapStore(t)
	if err := store.AddPortMap(ref, 9000, 9000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := store.SetPortMapEnabled(ref, 9000, false); err != nil {
		t.Fatalf("SetPortMapEnabled(false): %v", err)
	}
	maps, _ := store.PortMaps(ref)
	if len(maps) != 1 || maps[0].Enabled {
		t.Fatalf("PortMaps = %+v, want one disabled entry", maps)
	}
	if err := store.SetPortMapEnabled(ref, 9000, true); err != nil {
		t.Fatalf("SetPortMapEnabled(true): %v", err)
	}
	maps, _ = store.PortMaps(ref)
	if len(maps) != 1 || !maps[0].Enabled {
		t.Fatalf("PortMaps = %+v, want one enabled entry", maps)
	}
}

func TestSetPortMapEnabledUnknownPortErrors(t *testing.T) {
	store, ref := newRemotePortMapStore(t)
	if err := store.SetPortMapEnabled(ref, 1234, true); err == nil {
		t.Fatal("expected error for unknown remote port, got nil")
	}
}

func TestRemovePortMapDeletesEntry(t *testing.T) {
	store, ref := newRemotePortMapStore(t)
	if err := store.AddPortMap(ref, 5000, 5000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := store.RemovePortMap(ref, 5000); err != nil {
		t.Fatalf("RemovePortMap: %v", err)
	}
	maps, err := store.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 0 {
		t.Fatalf("PortMaps = %+v, want empty after removal", maps)
	}
	if err := store.RemovePortMap(ref, 5000); err == nil {
		t.Fatal("expected error removing an already-removed port map, got nil")
	}
}

func TestPortMapOpsRejectLocalProject(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/local/path"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	localRef := ProjectRef{Path: "/local/path"} // Host == "" -> local project
	if err := store.AddPortMap(localRef, 3000, 3000); err == nil {
		t.Fatal("expected error adding a port map to a local (non-remote) project, got nil")
	}
}

// TestReversePortMapPersistsAndRestartsAsReverse: a reverse entry survives a
// reload with its direction, and the runtime conversion every restart path
// uses (reconnect, enable, watchdog retry) keeps it -R. A -L restart of a
// reverse entry would open the wrong direction silently.
func TestReversePortMapPersistsAndRestartsAsReverse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	ref := ProjectRef{Host: "user@host", Path: "/proj"}
	if err := store.AddRemote(ref.Host, ref.Path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := store.AddReversePortMap(ref, 9222, 9222); err != nil {
		t.Fatalf("AddReversePortMap: %v", err)
	}

	reloaded, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt reload: %v", err)
	}
	maps, err := reloaded.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps after reload: %v", err)
	}
	if len(maps) != 1 || !maps[0].Reverse {
		t.Fatalf("PortMaps after reload = %+v, want one reverse entry", maps)
	}
	if rt := maps[0].Runtime(); !rt.Reverse || rt.RemotePort != 9222 || rt.LocalPort != 9222 || !rt.Enabled {
		t.Fatalf("Runtime() = %+v, want reverse 9222->9222 enabled", rt)
	}
}

// TestPortMapJSONOmitsReverseWhenFalse: old -L entries must keep their exact
// on-disk bytes, so no migration is needed and the field appears only for -R.
func TestPortMapJSONOmitsReverseWhenFalse(t *testing.T) {
	forward, err := json.Marshal(PortMap{RemotePort: 3000, LocalPort: 3000, Enabled: true})
	if err != nil {
		t.Fatalf("marshal forward: %v", err)
	}
	if strings.Contains(string(forward), "reverse") {
		t.Fatalf("forward JSON = %s, must not carry a reverse key", forward)
	}
	reverse, err := json.Marshal(PortMap{RemotePort: 9222, LocalPort: 9222, Enabled: true, Reverse: true})
	if err != nil {
		t.Fatalf("marshal reverse: %v", err)
	}
	if !strings.Contains(string(reverse), `"reverse":true`) {
		t.Fatalf("reverse JSON = %s, want reverse:true", reverse)
	}
}

// TestPortMapRefusesDirectionFlipOnSameRemotePort: one remote port means one
// thing. Re-adding it the other way is refused with the sentinel the handlers
// map to 409, and the stored entry is left unchanged.
func TestPortMapRefusesDirectionFlipOnSameRemotePort(t *testing.T) {
	store, ref := newRemotePortMapStore(t)
	if err := store.AddPortMap(ref, 9222, 9222); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	err := store.AddReversePortMap(ref, 9222, 9333)
	if !errors.Is(err, ErrPortMapDirection) {
		t.Fatalf("AddReversePortMap over an -L entry = %v, want ErrPortMapDirection", err)
	}
	maps, err := store.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].Reverse || maps[0].LocalPort != 9222 {
		t.Fatalf("PortMaps after refused flip = %+v, want the original -L entry", maps)
	}
}

// TestReversePortMapReAddUpdatesSameDirection: re-adding a reverse entry in
// the same direction updates the local port in place, like -L does.
func TestReversePortMapReAddUpdatesSameDirection(t *testing.T) {
	store, ref := newRemotePortMapStore(t)
	if err := store.AddReversePortMap(ref, 9222, 9222); err != nil {
		t.Fatalf("AddReversePortMap: %v", err)
	}
	if err := store.AddReversePortMap(ref, 9222, 9333); err != nil {
		t.Fatalf("AddReversePortMap (update): %v", err)
	}
	maps, err := store.PortMaps(ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || !maps[0].Reverse || maps[0].LocalPort != 9333 {
		t.Fatalf("PortMaps = %+v, want one reverse entry with local 9333", maps)
	}
}
