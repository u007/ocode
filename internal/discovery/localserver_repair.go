package discovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// localServerBinDir returns the directory holding the cached local embed server
// artifacts for the current platform.
func localServerBinDir(cacheDir string, man ServerManifest) string {
	return filepath.Join(cacheDir, "local-"+man.OS+"-"+man.Arch)
}

// localServerBinaryPath resolves the embed server binary path the same way
// spawnLlamaCppServer does (LaunchArgv[0] with {bin} expanded), so status and
// reinstall agree with the spawn path. Falls back to scanning llama-*/llama-server
// if the manifest path is missing (e.g. a stale cache from an older version).
func localServerBinaryPath(cacheDir string, man ServerManifest) string {
	binDir := localServerBinDir(cacheDir, man)
	if len(man.LaunchArgv) > 0 {
		p := strings.ReplaceAll(man.LaunchArgv[0], "{bin}", binDir)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	// Fallback scan.
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "llama-") {
			continue
		}
		candidate := filepath.Join(binDir, e.Name(), "llama-server")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// LocalServerBinaryPath is the exported form, using CurrentManifest().
func LocalServerBinaryPath(cacheDir string) string {
	man, ok := CurrentManifest()
	if !ok {
		return ""
	}
	return localServerBinaryPath(cacheDir, man)
}

// LocalServerStatus reports the state of the cached local embed server binary
// and its extraction marker. It is meant for human consumption (e.g. the
// /localserver status command).
func LocalServerStatus(cacheDir string) string {
	man, ok := CurrentManifest()
	if !ok {
		return fmt.Sprintf("No local server manifest for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if len(man.Artifacts) == 0 {
		return "Manifest has no artifacts"
	}
	a := man.Artifacts[0]
	binDir := localServerBinDir(cacheDir, man)
	marker := markerPath(filepath.Join(binDir, a.Dest))

	var b strings.Builder
	fmt.Fprintf(&b, "Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "Cache dir: %s\n", binDir)
	fmt.Fprintf(&b, "Archive URL: %s\n", a.URL)
	fmt.Fprintf(&b, "Archive SHA256: %s\n", a.SHA256)

	// Marker
	if data, err := os.ReadFile(marker); err == nil {
		recorded := strings.TrimSpace(string(data))
		fmt.Fprintf(&b, "Marker: %s (recorded SHA: %s)\n", marker, recorded)
		if recorded == a.SHA256 {
			b.WriteString("Marker: valid (matches manifest SHA)\n")
		} else {
			b.WriteString("Marker: STALE (does not match manifest SHA)\n")
		}
	} else if os.IsNotExist(err) {
		fmt.Fprintf(&b, "Marker: missing\n")
	} else {
		fmt.Fprintf(&b, "Marker: error reading: %v\n", err)
	}

	// Binary
	binPath := localServerBinaryPath(cacheDir, man)
	if binPath == "" {
		b.WriteString("Binary: MISSING (no llama-server found)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Binary path: %s\n", binPath)
	info, err := os.Stat(binPath)
	if err != nil {
		fmt.Fprintf(&b, "Binary stat: %v\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "Binary size: %d bytes, mode: %s\n", info.Size(), info.Mode())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(&b, "Binary execution: FAILED: %v\n", err)
		if len(out) > 0 {
			fmt.Fprintf(&b, "Output: %s\n", strings.TrimSpace(string(out)))
		}
		if strings.Contains(err.Error(), "exec format error") || strings.Contains(string(out), "Exec format error") {
			b.WriteString("Hint: the binary's architecture does not match this machine (exec format error). Run /localserver fix to reinstall.\n")
		}
		if ctx.Err() != nil {
			b.WriteString("Hint: the probe timed out (binary may be hung).\n")
		}
	} else {
		b.WriteString("Binary execution: OK\n")
		fmt.Fprintf(&b, "Version: %s\n", strings.TrimSpace(string(out)))
	}
	return b.String()
}

// ReinstallLocalServer removes the cached local embed server binary and
// re-downloads + re-extracts it from the pinned archive. It returns nil on
// success.
func ReinstallLocalServer(cacheDir string) error {
	// Drop the in-process handle so the next EnsureLocalServer re-probes
	// instead of reusing a handle that points at the now-deleted binary.
	StopLocalServer()
	man, ok := CurrentManifest()
	if !ok {
		return fmt.Errorf("no local server manifest for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if len(man.Artifacts) == 0 {
		return fmt.Errorf("manifest has no artifacts")
	}
	binDir := localServerBinDir(cacheDir, man)

	// Remove extracted version directories (llama-*) and the marker for each
	// artifact's Dest.
	entries, err := os.ReadDir(binDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read cache dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "llama-") {
			if err := os.RemoveAll(filepath.Join(binDir, e.Name())); err != nil {
				return fmt.Errorf("remove %s: %w", e.Name(), err)
			}
		}
	}
	for _, a := range man.Artifacts {
		marker := markerPath(filepath.Join(binDir, a.Dest))
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove marker %s: %w", marker, err)
		}
	}

	// Re-download all artifacts.
	for _, a := range man.Artifacts {
		if err := EnsureArtifact(a, binDir); err != nil {
			return fmt.Errorf("reinstall %s: %w", a.Dest, err)
		}
	}
	return nil
}
