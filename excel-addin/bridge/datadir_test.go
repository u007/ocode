package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolateHome points HOME and XDG_DATA_HOME into a fresh temp tree so no test touches the real home.
// Each test sets both explicitly; the bridge has no TestMain that could pre-set them.
func isolateHome(t *testing.T, xdg string) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", xdg)
	return home
}

func TestGlobalDataDirHonoursXDGOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_DATA_HOME is honoured on linux only (paths.GlobalDataDir ignores it on darwin)")
	}
	xdg := filepath.Join(t.TempDir(), "xdg")
	isolateHome(t, xdg)
	got, err := globalDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "opencode"); got != want {
		t.Fatalf("globalDataDir() = %q, want %q", got, want)
	}
	state, err := defaultStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "opencode", "excel", "state"); state != want {
		t.Fatalf("defaultStateDir() = %q, want %q", state, want)
	}
	ws, err := defaultWorkspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "opencode", "excel", "workspaces"); ws != want {
		t.Fatalf("defaultWorkspaceRoot() = %q, want %q", ws, want)
	}
}

func TestGlobalDataDirFallsBackToLocalShare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows resolves %LOCALAPPDATA%, not the home fallback")
	}
	// On linux an unset XDG_DATA_HOME falls back to ~/.local/share. On darwin XDG is ignored altogether,
	// so the same expectation holds even when it is set.
	xdg := ""
	if runtime.GOOS == "darwin" {
		xdg = filepath.Join(t.TempDir(), "ignored")
	}
	home := isolateHome(t, xdg)
	got, err := globalDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "opencode"); got != want {
		t.Fatalf("globalDataDir() = %q, want %q", got, want)
	}
}

func TestWorkspaceEndpointDefaultsUnderDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default paths are checked on linux and darwin")
	}
	xdg := ""
	if runtime.GOOS == "linux" {
		xdg = filepath.Join(t.TempDir(), "xdg")
	}
	home := isolateHome(t, xdg)

	h, _, err := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key}) // WorkspaceRoot left empty
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	req, _ := http.NewRequest("POST", s.URL+"/bridge/workspace/wb-9", nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workspace: status %d %v", resp.StatusCode, m)
	}

	base := filepath.Join(home, ".local", "share", "opencode")
	if runtime.GOOS == "linux" {
		base = filepath.Join(xdg, "opencode")
	}
	wantRoot := filepath.Join(base, "excel", "workspaces")
	if m["root"] != wantRoot || m["path"] != filepath.Join(wantRoot, "wb-9") {
		t.Fatalf("workspace root=%q path=%q, want root %q", m["root"], m["path"], wantRoot)
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "wb-9", "CLAUDE.md")); err != nil {
		t.Fatalf("workspace not created under the data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ocode-excel")); err == nil {
		t.Fatal("the old ~/.ocode-excel default was used")
	}
}
