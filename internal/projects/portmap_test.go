package projects

import (
	"path/filepath"
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
