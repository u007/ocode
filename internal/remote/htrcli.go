package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/version"
)

// htrCliName is the remote file name of the HTR companion CLI. It is installed
// beside the ocode binary in the same version directory, so the two upgrade
// together and GCVersions retires both.
const htrCliName = "htrcli"

// RemoteHTRCliPath returns the full remote path to htrcli for an ocode version.
func RemoteHTRCliPath(ver string) string {
	return RemoteBinDir + "/" + ver + "/" + htrCliName
}

// HTRCliInstall describes the htrcli installed for this ocode version on a remote.
type HTRCliInstall struct {
	// Version is htrcli's own build version, as `htrcli --version` reports it
	// on the remote host each time it is checked.
	Version string
	// Digest is the SHA-256 of the installed binary. It decides whether a
	// reinstall is needed: two builds can share a Version (an unclean tree).
	Digest string
	// Installed is true when this call uploaded and activated the binary.
	Installed bool
}

// htrCliDigestCmd prints the SHA-256 of a remote file. GNU hosts ship sha256sum
// and macOS ships shasum; whichever exists answers.
func htrCliDigestCmd(path string) string {
	quoted := shellQuotePath(path)
	return "sha256sum " + quoted + " 2>/dev/null || shasum -a 256 " + quoted
}

func localSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// remoteHTRCliVersion runs the installed htrcli's --version and returns the bare
// version it prints. An empty or failing answer means the binary does not run
// on this host, which is an install failure.
func remoteHTRCliVersion(t Transport, ver string) (string, error) {
	path := RemoteHTRCliPath(ver)
	res, err := t.Exec(shellQuotePath(path) + " --version")
	if err != nil {
		return "", fmt.Errorf("verify %s --version: %w: %s", path, err, res.Stderr)
	}
	reported := strings.TrimSpace(res.Stdout)
	if reported == "" {
		return "", fmt.Errorf("verify %s --version: no version reported", path)
	}
	return reported, nil
}

// remoteHTRCliDigest returns the digest of the installed htrcli, or "" when it
// is absent or no digest tool exists on the remote. An empty digest never
// matches a bundle, so the install path re-uploads and then verifies loudly.
func remoteHTRCliDigest(t Transport, ver string) (string, error) {
	res, err := t.Exec(htrCliDigestCmd(RemoteHTRCliPath(ver)))
	if res.ExitCode != 0 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read remote htrcli digest: %w", err)
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) == 0 {
		return "", fmt.Errorf("read remote htrcli digest: unexpected output %q", res.Stdout)
	}
	return fields[0], nil
}

// EnsureHTRCli installs the htrcli bundled beside the desktop app's remote CLIs
// (`make desktop-remote-binaries`) into the ocode version directory, next to the
// ocode binary. The installed digest is compared with the bundle's on every
// call; a mismatch re-uploads through the same .partial → chmod → mv path as
// ocode and is verified by digest after activation. Every call also runs the
// remote `htrcli --version`, so the reported version is current.
//
// A platform with no bundle returns the zero HTRCliInstall and is logged; the
// remote's HTR automation then reports htrcli as not found.
func EnsureHTRCli(t Transport, goos, goarch string) (HTRCliInstall, error) {
	exe, err := os.Executable()
	if err != nil {
		return HTRCliInstall{}, fmt.Errorf("locate executable for bundled htrcli: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return ensureHTRCliFrom(t, exe, goos, goarch)
}

// ensureHTRCliFrom is EnsureHTRCli with the running executable injected, so the
// bundle lookup can be pointed at a fixture directory.
func ensureHTRCliFrom(t Transport, exe, goos, goarch string) (HTRCliInstall, error) {
	ver := version.Version
	bundled, err := bundledRemoteArtifact(exe, htrCliName, goos, goarch)
	if err != nil {
		return HTRCliInstall{}, err
	}
	if bundled == "" {
		log.Printf("remote htrcli: no bundled %s/%s build for ocode v%s; htrcli is not installed on the remote", goos, goarch, ver)
		return HTRCliInstall{}, nil
	}

	want, err := localSHA256(bundled)
	if err != nil {
		return HTRCliInstall{}, err
	}
	have, err := remoteHTRCliDigest(t, ver)
	if err != nil {
		return HTRCliInstall{}, err
	}
	install := HTRCliInstall{Digest: want}
	if have != want {
		if err := uploadArtifact(t, ver, htrCliName, bundled); err != nil {
			return HTRCliInstall{}, err
		}
		if err := activateArtifact(t, ver, htrCliName); err != nil {
			return HTRCliInstall{}, err
		}
		got, err := remoteHTRCliDigest(t, ver)
		if err != nil {
			return HTRCliInstall{}, err
		}
		if got != want {
			return HTRCliInstall{}, fmt.Errorf("verify %s: remote digest %q, bundle %q — install treated as failed", RemoteHTRCliPath(ver), got, want)
		}
		install.Installed = true
	}

	install.Version, err = remoteHTRCliVersion(t, ver)
	if err != nil {
		return HTRCliInstall{}, err
	}

	linked, err := linkHTRCliForShells(t, ver)
	if err != nil {
		return HTRCliInstall{}, err
	}
	if !linked {
		log.Printf("remote htrcli: ~/.local/bin/htrcli is not an ocode link; left unchanged (login shells may not find htrcli)")
	}
	log.Printf("remote htrcli: %s version %s sha256:%s for ocode v%s (installed=%v)", RemoteHTRCliPath(ver), install.Version, want, ver, install.Installed)
	return install, nil
}

// linkHTRCliForShells points ~/.local/bin/htrcli at the version-matched install.
// The agent shell runs as a login shell, and Debian/Ubuntu /etc/profile resets
// PATH, dropping the version dir the server was launched with; ~/.profile
// re-adds ~/.local/bin, the same convention ocode uses locally (see
// config.EnsureUserBinPath). A file or a symlink outside ~/.ocode/bin is the
// user's own and is never replaced; reports false in that case.
func linkHTRCliForShells(t Transport, ver string) (bool, error) {
	link := `"$HOME/.local/bin/htrcli"`
	target := `"$HOME/.ocode/bin/` + ver + `/htrcli"`
	cmd := `mkdir -p "$HOME/.local/bin" && ` +
		`if { [ -L ` + link + ` ] && case "$(readlink ` + link + `)" in "$HOME/.ocode/bin/"*) true;; *) false;; esac; } || { [ ! -e ` + link + ` ] && [ ! -L ` + link + ` ]; }; then ` +
		`ln -sfn ` + target + ` ` + link + ` && echo linked; else echo foreign; fi`
	res, err := t.Exec(cmd)
	if err != nil {
		return false, fmt.Errorf("link htrcli into ~/.local/bin: %w: %s", err, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout) == "linked", nil
}
