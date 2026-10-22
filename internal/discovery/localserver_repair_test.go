package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalServerBinaryPath_Missing(t *testing.T) {
	dir := t.TempDir()
	got := LocalServerBinaryPath(dir)
	if got != "" {
		t.Fatalf("expected empty path for missing cache, got %q", got)
	}
}

func TestLocalServerStatus_MissingBinary(t *testing.T) {
	dir := t.TempDir()
	out := LocalServerStatus(dir)
	if !strings.Contains(out, "MISSING") {
		t.Fatalf("expected MISSING in status, got:\n%s", out)
	}
}

func TestLocalServerStatus_WrongArchBinary(t *testing.T) {
	// Create a fake cache layout with a non-executable/wrong-arch file.
	dir := t.TempDir()
	binDir := filepath.Join(dir, "local-"+runtime.GOOS+"-"+runtime.GOARCH)
	verDir := filepath.Join(binDir, "llama-b9999")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a tiny text file named llama-server (will fail exec).
	binPath := filepath.Join(verDir, "llama-server")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho nope\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a valid marker so the marker check passes.
	man, ok := CurrentManifest()
	if !ok {
		t.Skip("no manifest for this platform")
	}
	if len(man.Artifacts) == 0 {
		t.Skip("no artifacts")
	}
	marker := markerPath(filepath.Join(binDir, man.Artifacts[0].Dest))
	if err := os.WriteFile(marker, []byte(man.Artifacts[0].SHA256+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := LocalServerStatus(dir)
	// The text file won't report version 9777; execution should fail or report wrong output.
	t.Logf("status output:\n%s", out)
	if !strings.Contains(out, "Binary path:") {
		t.Fatalf("expected Binary path in status, got:\n%s", out)
	}
}

// reinstallFixture lays out an installed (bad) llama-b9999 tree plus a stale
// marker under cacheDir and returns the paths a reinstall must replace.
func reinstallFixture(t *testing.T) (cacheDir, binDir, oldBin, marker string, man ServerManifest) {
	t.Helper()
	man, ok := CurrentManifest()
	if !ok || len(man.Artifacts) == 0 || man.Artifacts[0].Archive == ArchiveNone {
		t.Skip("no archive manifest for this platform")
	}
	cacheDir = t.TempDir()
	binDir = localServerBinDir(cacheDir, man)
	verDir := filepath.Join(binDir, "llama-b9999")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldBin = filepath.Join(verDir, "llama-server")
	if err := os.WriteFile(oldBin, []byte("bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker = markerPath(filepath.Join(binDir, man.Artifacts[0].Dest))
	if err := os.WriteFile(marker, []byte("stale-sha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cacheDir, binDir, oldBin, marker, man
}

func stubEnsureArtifact(t *testing.T, fn func(a Artifact, dir string) error) {
	t.Helper()
	prev := ensureArtifactFn
	ensureArtifactFn = fn
	t.Cleanup(func() { ensureArtifactFn = prev })
}

func TestReinstallLocalServer_SwapsInNewTree(t *testing.T) {
	cacheDir, binDir, oldBin, marker, man := reinstallFixture(t)
	stubEnsureArtifact(t, func(a Artifact, dir string) error {
		if a.Archive == ArchiveNone {
			return nil
		}
		ver := filepath.Join(dir, "llama-bNEW")
		if err := os.MkdirAll(ver, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(ver, "llama-server"), []byte("good"), 0o755); err != nil {
			return err
		}
		return writeExtractionMarker(markerPath(filepath.Join(dir, a.Dest)), a.SHA256)
	})

	if err := ReinstallLocalServer(cacheDir); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(oldBin); !os.IsNotExist(err) {
		t.Fatalf("old version dir survived reinstall: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(binDir, "llama-bNEW", "llama-server"))
	if err != nil || string(got) != "good" {
		t.Fatalf("new binary not installed: %q, %v", got, err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(data)) != man.Artifacts[0].SHA256 {
		t.Fatalf("marker not refreshed: %q, %v", data, err)
	}
	assertNoStaging(t, binDir)
}

// A failed download must leave the previous (possibly merely stale) install in
// place: /localserver fix may not turn a degraded install into no install.
func TestReinstallLocalServer_FailedDownloadKeepsOldInstall(t *testing.T) {
	cacheDir, binDir, oldBin, marker, _ := reinstallFixture(t)
	stubEnsureArtifact(t, func(a Artifact, dir string) error {
		// Leave partial output in the scratch dir, like a half-extracted archive.
		_ = os.MkdirAll(filepath.Join(dir, "llama-bPARTIAL"), 0o755)
		return errors.New("offline")
	})

	if err := ReinstallLocalServer(cacheDir); err == nil {
		t.Fatal("expected an error from the failed download")
	}
	if got, err := os.ReadFile(oldBin); err != nil || string(got) != "bad" {
		t.Fatalf("old binary lost after failed reinstall: %q, %v", got, err)
	}
	if data, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(data)) != "stale-sha" {
		t.Fatalf("old marker lost after failed reinstall: %q, %v", data, err)
	}
	assertNoStaging(t, binDir)
}

// The cross-process embed start lock serialises a reinstall against a spawn.
func TestReinstallLocalServer_RefusesWhileStartLockHeld(t *testing.T) {
	cacheDir, _, oldBin, _, _ := reinstallFixture(t)
	acquired, release, err := acquireEmbedStartLock(cacheDir)
	if err != nil || !acquired {
		t.Fatalf("setup: acquire start lock: acquired=%v err=%v", acquired, err)
	}
	defer release()
	called := false
	stubEnsureArtifact(t, func(Artifact, string) error { called = true; return nil })

	err = ReinstallLocalServer(cacheDir)
	if err == nil || !strings.Contains(err.Error(), "another ocode process") {
		t.Fatalf("expected a start-lock refusal, got %v", err)
	}
	if called {
		t.Fatal("download ran while the start lock was held by another process")
	}
	if _, statErr := os.Stat(oldBin); statErr != nil {
		t.Fatalf("old install touched despite refusal: %v", statErr)
	}
}

func TestInstallStaged_RollsBackWhenInstallFails(t *testing.T) {
	binDir := t.TempDir()
	stage := t.TempDir()
	oldBin := filepath.Join(binDir, "llama-old", "llama-server")
	if err := os.MkdirAll(filepath.Dir(oldBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"llama-a", "llama-b"} {
		if err := os.MkdirAll(filepath.Join(stage, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Fail the second install rename, after llama-a is already in place.
	prev := renameFn
	renameFn = func(from, to string) error {
		if filepath.Base(to) == "llama-b" && filepath.Dir(to) == binDir {
			return errors.New("injected")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { renameFn = prev })

	if err := installStaged(binDir, stage, []string{"llama-old"}); err == nil {
		t.Fatal("expected installStaged to fail")
	}
	if got, err := os.ReadFile(oldBin); err != nil || string(got) != "old" {
		t.Fatalf("old install not restored: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(binDir, "llama-a")); !os.IsNotExist(err) {
		t.Fatalf("partially installed llama-a survived rollback: %v", err)
	}
	assertNoStaging(t, binDir)
}

func TestLocalServerBinaryPath_FallbackScanFindsInstalledBinary(t *testing.T) {
	man, ok := CurrentManifest()
	if !ok {
		t.Skip("no manifest for this platform")
	}
	cacheDir := t.TempDir()
	// A path the manifest's LaunchArgv does not name forces the fallback scan.
	verDir := filepath.Join(localServerBinDir(cacheDir, man), "llama-b0000")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := llamaServerNames()[0]
	want := filepath.Join(verDir, name)
	if err := os.WriteFile(want, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := localServerBinaryPath(cacheDir, man); got != want {
		t.Fatalf("fallback scan = %q, want %q", got, want)
	}
}

func TestLlamaServerNames_WindowsHasExe(t *testing.T) {
	names := llamaServerNames()
	if runtime.GOOS == "windows" {
		if names[0] != "llama-server.exe" {
			t.Fatalf("windows names = %v, want .exe first", names)
		}
		return
	}
	if len(names) != 1 || names[0] != "llama-server" {
		t.Fatalf("names = %v", names)
	}
}

func assertNoStaging(t *testing.T, binDir string) {
	t.Helper()
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagingPrefix) {
			t.Fatalf("reinstall left scratch dir %q behind", e.Name())
		}
	}
}
