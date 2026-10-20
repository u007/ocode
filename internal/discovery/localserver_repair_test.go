package discovery

import (
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

func TestReinstallLocalServer_CleansDirsAndMarker(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "local-"+runtime.GOOS+"-"+runtime.GOARCH)
	verDir := filepath.Join(binDir, "llama-b9999")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a fake bad binary.
	if err := os.WriteFile(filepath.Join(verDir, "llama-server"), []byte("bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	man, ok := CurrentManifest()
	if !ok {
		t.Skip("no manifest for this platform")
	}
	if len(man.Artifacts) == 0 {
		t.Skip("no artifacts")
	}
	marker := markerPath(filepath.Join(binDir, man.Artifacts[0].Dest))
	if err := os.WriteFile(marker, []byte("stale-sha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reinstall will attempt a real download; skip if no network.
	// We only verify cleanup happened by checking the stale marker is gone
	// even if download fails.
	err := ReinstallLocalServer(dir)
	if err != nil {
		// Network may be unavailable; verify at least cleanup occurred.
		t.Logf("reinstall returned (may be network): %v", err)
	}
	// The stale marker should be gone (either removed before download, or replaced).
	if data, err := os.ReadFile(marker); err == nil {
		if strings.TrimSpace(string(data)) == "stale-sha" {
			t.Fatalf("stale marker survived reinstall")
		}
	}
	// The fake bad version dir should be gone.
	if _, err := os.Stat(verDir); !os.IsNotExist(err) {
		t.Fatalf("fake version dir survived reinstall: %v", err)
	}
}
