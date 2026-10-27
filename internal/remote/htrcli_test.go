package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/version"
)

// fakeHTRCliVersion is what the fake remote's `htrcli --version` prints.
const fakeHTRCliVersion = "htrcli-test-1.2.3"

// digestReportingTransport behaves like a remote whose htrcli install reflects
// the bytes last uploaded: the digest query and `--version` answer from what
// landed on disk. corrupt makes the digest lie; noVersion makes --version print
// nothing.
type digestReportingTransport struct {
	*fakeTransport
	corrupt   bool
	noVersion bool
}

func (d *digestReportingTransport) Exec(command string) (ExecResult, error) {
	installed := RemoteHTRCliPath(version.Version)
	if command == shellQuotePath(installed)+" --version" {
		if d.copyContent == nil {
			return ExecResult{ExitCode: 127, Stderr: "no such file"}, fmt.Errorf("exit 127")
		}
		if d.noVersion {
			return ExecResult{}, nil
		}
		return ExecResult{Stdout: fakeHTRCliVersion + "\n"}, nil
	}
	if command == htrCliDigestCmd(installed) {
		if d.copyContent == nil {
			return ExecResult{ExitCode: 1}, nil
		}
		sum := sha256.Sum256(d.copyContent)
		digest := hex.EncodeToString(sum[:])
		if d.corrupt {
			digest = strings.Repeat("0", len(digest))
		}
		return ExecResult{Stdout: digest + "  " + installed + "\n"}, nil
	}
	return d.fakeTransport.Exec(command)
}

// htrCliFixture places a bundled htrcli for goos/goarch beside a fake desktop
// executable and returns that executable's path.
func htrCliFixture(t *testing.T, goos, goarch string) string {
	t.Helper()
	root := t.TempDir()
	exe := filepath.Join(root, "ocode-desktop")
	writeBinaryFixture(t, exe)
	writeBinaryFixture(t, filepath.Join(root, "remote-binaries", version.Version, htrCliName+"-"+goos+"-"+goarch))
	return exe
}

func newDigestTransport() *digestReportingTransport {
	return &digestReportingTransport{fakeTransport: newFakeTransport()}
}

func TestEnsureHTRCliInstallsBundleThenSkipsWhenCurrent(t *testing.T) {
	exe := htrCliFixture(t, "linux", "amd64")
	ft := newDigestTransport()

	first, err := ensureHTRCliFrom(ft, exe, "linux", "amd64")
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !first.Installed || first.Digest == "" || first.Version != fakeHTRCliVersion {
		t.Fatalf("first install = %+v; want installed, digest set, version %q", first, fakeHTRCliVersion)
	}
	wantPartial := RemoteBinDir + "/" + version.Version + "/.htrcli.partial"
	if ft.copyDestPath != wantPartial {
		t.Fatalf("uploaded to %q; want %q", ft.copyDestPath, wantPartial)
	}
	activate := "chmod +x " + shellQuotePath(wantPartial) + " && mv " + shellQuotePath(wantPartial) + " " + shellQuotePath(RemoteHTRCliPath(version.Version))
	if !slices.Contains(ft.execCalls, activate) {
		t.Fatalf("activation command %q not run; calls: %v", activate, ft.execCalls)
	}

	again, err := ensureHTRCliFrom(ft, exe, "linux", "amd64")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if again.Installed || again.Digest != first.Digest || again.Version != fakeHTRCliVersion {
		t.Fatalf("second call = %+v; want the same digest and version, not reinstalled", again)
	}
}

func TestEnsureHTRCliRejectsDigestMismatchAfterActivation(t *testing.T) {
	exe := htrCliFixture(t, "linux", "amd64")
	ft := newDigestTransport()
	ft.corrupt = true
	_, err := ensureHTRCliFrom(ft, exe, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "install treated as failed") {
		t.Fatalf("corrupt install must fail loudly, got %v", err)
	}
}

func TestEnsureHTRCliRejectsBinaryThatReportsNoVersion(t *testing.T) {
	exe := htrCliFixture(t, "linux", "amd64")
	ft := newDigestTransport()
	ft.noVersion = true
	_, err := ensureHTRCliFrom(ft, exe, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "no version reported") {
		t.Fatalf("an htrcli that reports no version must fail the install, got %v", err)
	}
}

func TestEnsureHTRCliSkipsPlatformWithoutBundle(t *testing.T) {
	exe := htrCliFixture(t, "linux", "amd64")
	ft := newDigestTransport()
	got, err := ensureHTRCliFrom(ft, exe, "linux", "arm64")
	if err != nil || got != (HTRCliInstall{}) {
		t.Fatalf("no bundle = %+v, %v; want the zero value with no error", got, err)
	}
	if ft.copyContent != nil {
		t.Fatal("nothing should be uploaded when no bundle exists")
	}
}

func TestEnsureHTRCliBundleLookupIgnoresStaleVersion(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ocode-desktop")
	writeBinaryFixture(t, exe)
	writeBinaryFixture(t, filepath.Join(root, "remote-binaries", "old-version", htrCliName+"-linux-amd64"))
	got, err := ensureHTRCliFrom(newDigestTransport(), exe, "linux", "amd64")
	if err != nil || got != (HTRCliInstall{}) {
		t.Fatalf("stale bundle must be skipped, not installed: %+v, %v", got, err)
	}
}

func TestLocalSHA256MatchesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "htrcli")
	if err := os.WriteFile(path, []byte("HTR fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("HTR fixture"))
	got, err := localSHA256(path)
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Fatalf("localSHA256 = %q, %v", got, err)
	}
}

func TestLaunchServerCmdPutsVersionDirOnPath(t *testing.T) {
	cmd := launchServerCmd("0.8.85", "")
	if !strings.Contains(cmd, `PATH="$HOME/.ocode/bin/0.8.85":"$PATH"`) {
		t.Fatalf("launch command must put the version dir first on PATH so the agent shell finds htrcli: %q", cmd)
	}
}

// localShellTransport runs Exec commands with sh under a fake HOME, so the
// ~/.local/bin link logic is exercised for real rather than string-matched.
type localShellTransport struct {
	*fakeTransport
	home string
}

func (l *localShellTransport) Exec(command string) (ExecResult, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = []string{"HOME=" + l.home, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	res := ExecResult{Stdout: string(out)}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
		res.Stderr = string(exitErr.Stderr)
		return res, err
	}
	return res, err
}

func TestLinkHTRCliForShellsOwnership(t *testing.T) {
	ver := version.Version
	cases := []struct {
		name       string
		setup      func(t *testing.T, home string)
		wantLinked bool
	}{
		{name: "fresh home", setup: func(t *testing.T, home string) {}, wantLinked: true},
		{name: "replaces ocode's own stale link", setup: func(t *testing.T, home string) {
			mustMkdir(t, filepath.Join(home, ".local", "bin"))
			mustSymlink(t, filepath.Join(home, ".ocode", "bin", "old", "htrcli"), filepath.Join(home, ".local", "bin", "htrcli"))
		}, wantLinked: true},
		{name: "leaves a user's regular file alone", setup: func(t *testing.T, home string) {
			mustMkdir(t, filepath.Join(home, ".local", "bin"))
			if err := os.WriteFile(filepath.Join(home, ".local", "bin", "htrcli"), []byte("mine"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, wantLinked: false},
		{name: "leaves a user's symlink outside ~/.ocode alone", setup: func(t *testing.T, home string) {
			mustMkdir(t, filepath.Join(home, ".local", "bin"))
			mustSymlink(t, filepath.Join(home, "go", "bin", "htrcli"), filepath.Join(home, ".local", "bin", "htrcli"))
		}, wantLinked: false},
		{name: "leaves a dangling user symlink alone", setup: func(t *testing.T, home string) {
			mustMkdir(t, filepath.Join(home, ".local", "bin"))
			mustSymlink(t, filepath.Join(home, "gone", "htrcli"), filepath.Join(home, ".local", "bin", "htrcli"))
		}, wantLinked: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)
			lt := &localShellTransport{fakeTransport: newFakeTransport(), home: home}
			linked, err := linkHTRCliForShells(lt, ver)
			if err != nil {
				t.Fatalf("link: %v", err)
			}
			if linked != tc.wantLinked {
				t.Fatalf("linked = %v; want %v", linked, tc.wantLinked)
			}
			if !tc.wantLinked {
				return
			}
			got, err := os.Readlink(filepath.Join(home, ".local", "bin", "htrcli"))
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(home, ".ocode", "bin", ver, "htrcli"); got != want {
				t.Fatalf("link target = %q; want %q", got, want)
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(target))
	mustMkdir(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
