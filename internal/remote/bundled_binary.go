package remote

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/version"
)

// bundledRemoteArtifact resolves the desktop-bundled cross-built file <name>
// (e.g. "ocode" or "htrcli") for goos/goarch, relative to the executable, never
// cwd: Finder starts apps at /. Version directories prevent using stale build
// artifacts. The .app resource location wins over the sibling layout used by
// bare builds. Returns "" when no bundle exists for this version/platform.
func bundledRemoteArtifact(exe, name, goos, goarch string) (string, error) {
	if exe == "" {
		return "", nil
	}
	dir := filepath.Dir(exe)
	roots := []string{filepath.Join(dir, "remote-binaries")}
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		roots = append([]string{filepath.Join(dir, "..", "Resources", "remote-binaries")}, roots...)
	}
	for _, root := range roots {
		path := filepath.Join(root, version.Version, name+"-"+goos+"-"+goarch)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat bundled remote %s: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return "", fmt.Errorf("invalid bundled remote %s %s: expected a non-empty regular file", name, path)
		}
		// Local execute permission is unnecessary: UploadBinary reads the file,
		// and ActivateAndVerify chmods and checks --version on the target.
		return path, nil
	}
	return "", nil
}
