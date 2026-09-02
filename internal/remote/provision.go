package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/version"
)

// RemoteBinDir is the versioned-install root on the remote, relative to the
// remote user's home ("~" expands via the remote login shell — every
// command built here is a single string passed to Transport.Exec /
// ExecInteractive, never argv-split, so that holds).
const RemoteBinDir = "~/.ocode/bin"

// RemoteBinaryPath returns the full remote path to a specific version's
// binary.
func RemoteBinaryPath(ver string) string {
	return RemoteBinDir + "/" + ver + "/ocode"
}

// unameToGoEnv maps `uname -s`/`uname -m` output to GOOS/GOARCH.
func unameToGoEnv(sysOut, machOut string) (goos, goarch string, err error) {
	switch strings.ToLower(strings.TrimSpace(sysOut)) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("unsupported remote platform %q", sysOut)
	}
	switch strings.TrimSpace(machOut) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported remote architecture %q", machOut)
	}
	return goos, goarch, nil
}

// DetectPlatform runs `uname -sm` over t and maps the result to GOOS/GOARCH.
func DetectPlatform(t Transport) (goos, goarch string, err error) {
	res, execErr := t.Exec("uname -sm")
	if execErr != nil {
		return "", "", fmt.Errorf("platform detect: %w", execErr)
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) != 2 {
		return "", "", fmt.Errorf("platform detect: unexpected `uname -sm` output %q", res.Stdout)
	}
	return unameToGoEnv(fields[0], fields[1])
}

// BinaryExists checks whether the versioned binary is already installed and
// executable on the remote.
func BinaryExists(t Transport, ver string) bool {
	res, err := t.Exec("test -x " + shellQuotePath(RemoteBinaryPath(ver)))
	return err == nil && res.ExitCode == 0
}

// LocalBuild is a locally-produced (or reused) binary ready to upload.
type LocalBuild struct {
	Path   string // local filesystem path
	Reused bool   // true when the running binary itself was reused (platform match)
}

// PrepareLocalBuild returns a binary for goos/goarch, either by reusing the
// currently-running executable (when its platform matches) or by
// cross-compiling from a source checkout. moduleDir is the ocode repo root
// (containing go.mod); pass "" to auto-detect from the running executable's
// location and, failing that, the current working directory.
func PrepareLocalBuild(goos, goarch, moduleDir string) (LocalBuild, error) {
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		if exe, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				return LocalBuild{Path: resolved, Reused: true}, nil
			}
		}
	}

	dir := moduleDir
	if dir == "" {
		var err error
		dir, err = findModuleRoot()
		if err != nil {
			return LocalBuild{}, fmt.Errorf("no local binary matches %s/%s and no ocode source checkout found to cross-compile from (%w); install Go and run from the ocode repo, or connect to a matching-platform host", goos, goarch, err)
		}
	}

	out, err := os.CreateTemp("", fmt.Sprintf("ocode-build-%s-%s-*", goos, goarch))
	if err != nil {
		return LocalBuild{}, fmt.Errorf("create build output temp file: %w", err)
	}
	outPath := out.Name()
	out.Close()
	os.Remove(outPath) // go build wants to create it itself

	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", outPath, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return LocalBuild{}, fmt.Errorf("cross-compile %s/%s: %w\n%s", goos, goarch, err, stderr.String())
	}
	return LocalBuild{Path: outPath, Reused: false}, nil
}

func findModuleRoot() (string, error) {
	start := "."
	if wd, err := os.Getwd(); err == nil {
		start = wd
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", start)
		}
		dir = parent
	}
}

func remotePartialPath(ver string) string {
	return RemoteBinDir + "/" + ver + "/.ocode.partial"
}

// UploadBinary uploads local (a LocalBuild.Path) to the version dir's
// ".partial" name, creating the remote dir if needed. It does not activate
// the binary — see ActivateAndVerify.
func UploadBinary(t Transport, ver, localPath string) error {
	remoteDir := RemoteBinDir + "/" + ver
	if _, err := t.Exec("mkdir -p " + shellQuotePath(remoteDir)); err != nil {
		return fmt.Errorf("prepare remote dir %s: %w", remoteDir, err)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local build %s: %w", localPath, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat local build %s: %w", localPath, err)
	}

	if err := t.Copy(f, info.Size(), remotePartialPath(ver)); err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}

// ActivateAndVerify chmods the uploaded ".partial" binary executable,
// atomically renames it into place, and verifies it by running
// `ocode --version` remotely — a mismatch is treated as install failure,
// per the spec ("never silently launched").
func ActivateAndVerify(t Transport, ver string) error {
	partial := remotePartialPath(ver)
	final := RemoteBinaryPath(ver)

	installCmd := fmt.Sprintf("chmod +x %s && mv %s %s", shellQuotePath(partial), shellQuotePath(partial), shellQuotePath(final))
	if res, err := t.Exec(installCmd); err != nil || res.ExitCode != 0 {
		if err == nil {
			err = fmt.Errorf("exit %d", res.ExitCode)
		}
		return fmt.Errorf("install %s: %w: %s", final, err, res.Stderr)
	}

	verify, err := t.Exec(shellQuotePath(final) + " --version")
	if err != nil {
		return fmt.Errorf("verify %s --version: %w", final, err)
	}
	got := strings.TrimSpace(verify.Stdout)
	if got != ver {
		return fmt.Errorf("verify %s --version: remote reports %q, expected %q — install treated as failed", final, got, ver)
	}
	return nil
}

// InstallBinary uploads and activates local in one call — the combination
// EnsureBinary uses when progress isn't rendered stage-by-stage (e.g. from
// tests or programmatic callers).
func InstallBinary(t Transport, ver, localPath string) error {
	if err := UploadBinary(t, ver, localPath); err != nil {
		return err
	}
	return ActivateAndVerify(t, ver)
}

// EnsureBinary is the full stage-3 flow: check for an existing install,
// otherwise reuse-or-cross-compile and install one. Returns whether a fresh
// install happened (false when the remote already had it).
func EnsureBinary(t Transport, moduleDir string) (installed bool, err error) {
	ver := version.Version
	if BinaryExists(t, ver) {
		return false, nil
	}
	goos, goarch, err := DetectPlatform(t)
	if err != nil {
		return false, err
	}
	build, err := PrepareLocalBuild(goos, goarch, moduleDir)
	if err != nil {
		return false, err
	}
	if !build.Reused {
		defer os.Remove(build.Path)
	}
	if err := InstallBinary(t, ver, build.Path); err != nil {
		return false, err
	}
	return true, nil
}

// GCVersions keeps only the two newest version directories under
// RemoteBinDir on the remote, deleting the rest. "Newest" is lexicographic
// (ocode versions are dotted-numeric and sort correctly as strings for any
// same-width numbering; ties/odd version strings are broken by keeping
// whichever `ls` returns last, which is acceptable since GC is best-effort
// cleanup, never correctness-critical).
func GCVersions(t Transport) error {
	res, err := t.Exec("ls -1 " + shellQuotePath(RemoteBinDir) + " 2>/dev/null || true")
	if err != nil {
		return nil // best-effort; a listing failure is not a connect failure
	}
	var dirs []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			dirs = append(dirs, line)
		}
	}
	if len(dirs) <= 2 {
		return nil
	}
	sort.Strings(dirs)
	stale := dirs[:len(dirs)-2]
	for _, d := range stale {
		// Defensive: never rm a name that isn't a plain path segment (a
		// version string legitimately contains dots, e.g. "0.9.3" — only
		// reject "/" (path traversal into another dir) and "." / ".."
		// (the dir itself or its parent)).
		if d == "" || d == "." || d == ".." || strings.Contains(d, "/") {
			continue
		}
		_, _ = t.Exec("rm -rf " + shellQuotePath(RemoteBinDir+"/"+d))
	}
	return nil
}
