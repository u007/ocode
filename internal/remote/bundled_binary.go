package remote

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/version"
)

// bundledRemoteBinary resolves relative to the executable, never cwd: Finder
// starts apps at /. Version directories prevent using stale build artifacts.
// The .app resource location wins over the sibling layout used by bare builds.
func bundledRemoteBinary(exe, goos, goarch string) (string, error) {
	if exe == "" {
		return "", nil
	}
	dir := filepath.Dir(exe)
	roots := []string{filepath.Join(dir, "remote-binaries")}
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		roots = append([]string{filepath.Join(dir, "..", "Resources", "remote-binaries")}, roots...)
	}
	for _, root := range roots {
		path := filepath.Join(root, version.Version, "ocode-"+goos+"-"+goarch)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat bundled remote CLI: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return "", fmt.Errorf("invalid bundled remote CLI %s: expected a non-empty regular file", path)
		}
		// Local execute permission is unnecessary: UploadBinary reads the file,
		// and ActivateAndVerify chmods and checks --version on the target.
		return path, nil
	}
	return "", nil
}
