package pathscope

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// DarwinUserDirs returns the per-user macOS temp (T) and cache (C) folders
// under /var/folders — the paths confstr(_CS_DARWIN_USER_TEMP_DIR /
// _CS_DARWIN_USER_CACHE_DIR) hands to mktemp, Python tempfile, Node
// os.tmpdir(), clang/swift module caches. They are found by ownership
// (uid match on /var/folders/*/*) rather than $TMPDIR, so a process launched
// without TMPDIR (launchd, env -i) still grants the dirs its children will
// actually write to. Empty on non-darwin.
func DarwinUserDirs() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	matches, err := filepath.Glob("/var/folders/*/*")
	if err != nil {
		return nil
	}
	uid := uint32(os.Getuid())
	var out []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() {
			continue // intentionally not logged: probing candidate dirs, ENOENT/EACCES expected
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uid {
			continue
		}
		for _, sub := range []string{"T", "C"} {
			p := filepath.Join(m, sub)
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

// DarwinUserTempDir returns only the per-user T folders from DarwinUserDirs.
func DarwinUserTempDir() []string {
	var out []string
	for _, p := range DarwinUserDirs() {
		if filepath.Base(p) == "T" {
			out = append(out, p)
		}
	}
	return out
}
