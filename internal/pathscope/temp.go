package pathscope

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TempRootsForGOOS returns the temp roots the permission engine treats as safe.
// Windows uses the platform temp directory; Unix-like platforms include the
// current temp directory plus the conventional system temp roots.
func TempRootsForGOOS(goos string) []string {
	roots := []string{normalizeRoot(filepath.Clean(os.TempDir()))}
	if goos != "windows" {
		roots = append(roots, normalizeRoot("/tmp"), normalizeRoot("/var/tmp"))
	}
	seen := make(map[string]struct{}, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	return out
}

// IsTempDirUnderRoots reports whether rawPath resolves inside any provided temp root.
func IsTempDirUnderRoots(rawPath string, roots []string) bool {
	path := normalizePath(rawPath)
	if path == "" {
		return false
	}
	for _, td := range roots {
		if td == "" {
			continue
		}
		if pathUnderRoot(path, normalizeRoot(td)) {
			return true
		}
	}
	return false
}

// IsTempDir returns true if the given path is within a well-known system temp
// directory.
func IsTempDir(rawPath string) bool {
	return IsTempDirUnderRoots(rawPath, TempRootsForGOOS(runtime.GOOS))
}

func normalizePath(rawPath string) string {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		dir := filepath.Dir(absPath)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return filepath.Clean(absPath)
		}
		resolved = filepath.Join(resolvedDir, filepath.Base(absPath))
	}
	return filepath.Clean(resolved)
}

func normalizeRoot(root string) string {
	if root == "" {
		return ""
	}
	absPath, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return filepath.Clean(absPath)
	}
	return filepath.Clean(resolved)
}

func pathUnderRoot(path, root string) bool {
	if path == root {
		return true
	}
	rootWithSep := root + string(filepath.Separator)
	return strings.HasPrefix(path, rootWithSep)
}
