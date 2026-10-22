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
		for _, name := range llamaServerNames() {
			candidate := filepath.Join(binDir, e.Name(), name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

// llamaServerNames lists the file names the llama-server binary can have in an
// extracted release: the Windows build carries an .exe suffix.
func llamaServerNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"llama-server.exe", "llama-server"}
	}
	return []string{"llama-server"}
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

// ensureArtifactFn is the artifact downloader; a package var so tests can
// simulate a download without the network.
var ensureArtifactFn = EnsureArtifact

// renameFn is os.Rename; a package var so tests can inject a mid-swap failure.
var renameFn = os.Rename

// stagingPrefix names the scratch directories a reinstall creates inside the
// cache dir. The fallback binary scan only looks at llama-* entries, so these
// never read as an installed version.
const stagingPrefix = ".reinstall-"

// ReinstallLocalServer re-downloads and re-extracts the pinned local embed
// server archive. The new tree is built in a scratch directory first and only
// then swapped over the installed one, so a failed download (offline, bad SHA)
// leaves the previous install untouched rather than turning a degraded install
// into no install. It returns nil on success.
//
// It holds localMu (so a concurrent EnsureLocalServer in this process waits
// instead of spawning from a half-swapped tree) and the cross-process embed
// start lock (so another ocode cannot spawn mid-swap).
func ReinstallLocalServer(cacheDir string) error {
	man, ok := CurrentManifest()
	if !ok {
		return fmt.Errorf("no local server manifest for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if len(man.Artifacts) == 0 {
		return fmt.Errorf("manifest has no artifacts")
	}
	binDir := localServerBinDir(cacheDir, man)

	localMu.Lock()
	defer localMu.Unlock()
	// Drop the in-process handle so the next EnsureLocalServer re-probes
	// instead of reusing a handle that points at the replaced binary.
	localBase, localModelID = "", ""

	acquired, release, err := acquireEmbedStartLock(cacheDir)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("another ocode process is starting the local embed server; try again in a moment")
	}
	defer release()

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	removeStaleStaging(binDir)

	stage, err := os.MkdirTemp(binDir, stagingPrefix+"stage-")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stage)

	// Only archive artifacts are re-extracted: the raw model files (.gguf) are
	// large, SHA-checked by EnsureArtifact and unaffected by a broken binary,
	// so they are left in place and re-verified after the swap.
	var raw []Artifact
	for _, a := range man.Artifacts {
		if a.Archive == ArchiveNone {
			raw = append(raw, a)
			continue
		}
		if err := ensureArtifactFn(a, stage); err != nil {
			return fmt.Errorf("reinstall %s: %w", a.Dest, err)
		}
	}

	// Everything the new install replaces: every extracted llama-* version dir
	// (a version bump leaves old ones behind) and each archive's marker.
	var retire []string
	if entries, err := os.ReadDir(binDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "llama-") {
				retire = append(retire, e.Name())
			}
		}
	}
	for _, a := range man.Artifacts {
		if a.Archive != ArchiveNone {
			retire = append(retire, filepath.Base(markerPath(a.Dest)))
		}
	}
	if err := installStaged(binDir, stage, retire); err != nil {
		return err
	}

	for _, a := range raw {
		if err := ensureArtifactFn(a, binDir); err != nil {
			return fmt.Errorf("reinstall %s: %w", a.Dest, err)
		}
	}
	return nil
}

// removeStaleStaging deletes scratch directories a crashed reinstall left
// behind. The caller holds the embed start lock, so none is in use.
func removeStaleStaging(binDir string) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagingPrefix) {
			os.RemoveAll(filepath.Join(binDir, e.Name()))
		}
	}
}

// installStaged swaps the top-level entries of stage into binDir. Entries in
// retire, and any installed entry the stage would overwrite, are first moved
// aside; if any rename fails the moves are undone so the previous install
// stays intact. A rename of a running executable fails on Windows, which
// surfaces here as an error before anything is lost.
func installStaged(binDir, stage string, retire []string) error {
	staged, err := os.ReadDir(stage)
	if err != nil {
		return fmt.Errorf("read staging dir: %w", err)
	}
	backup, err := os.MkdirTemp(binDir, stagingPrefix+"old-")
	if err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	// On success this holds the replaced install; on rollback it is empty.
	defer os.RemoveAll(backup)

	names := append([]string(nil), retire...)
	for _, e := range staged {
		names = append(names, e.Name())
	}
	var retired, installed []string
	rollback := func() {
		for _, n := range installed {
			os.RemoveAll(filepath.Join(binDir, n))
		}
		for _, n := range retired {
			renameFn(filepath.Join(backup, n), filepath.Join(binDir, n))
		}
	}

	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		src := filepath.Join(binDir, n)
		if _, err := os.Lstat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			rollback()
			return fmt.Errorf("inspect %s: %w", n, err)
		}
		if err := renameFn(src, filepath.Join(backup, n)); err != nil {
			rollback()
			return fmt.Errorf("replace %s (is the embed server still running?): %w", n, err)
		}
		retired = append(retired, n)
	}
	for _, e := range staged {
		if err := renameFn(filepath.Join(stage, e.Name()), filepath.Join(binDir, e.Name())); err != nil {
			rollback()
			return fmt.Errorf("install %s: %w", e.Name(), err)
		}
		installed = append(installed, e.Name())
	}
	return nil
}
