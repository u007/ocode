package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/version"
)

func writeBinaryFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("CLI fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolvedFixture(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestPrepareLocalBuildPrebuilt(t *testing.T) {
	// Neither a source checkout nor Go is available, as in a Finder launch.
	t.Chdir(string(filepath.Separator))
	t.Setenv("PATH", t.TempDir())
	for _, platform := range []struct{ os, arch string }{
		{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"},
	} {
		t.Run(platform.os+"-"+platform.arch, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, "ocode.app", "Contents", "MacOS", "ocode")
			writeBinaryFixture(t, exe)
			want := filepath.Join(root, "ocode.app", "Contents", "Resources", "remote-binaries", version.Version, "ocode-"+platform.os+"-"+platform.arch)
			writeBinaryFixture(t, want)
			want = resolvedFixture(t, want)
			build, err := prepareLocalBuild(platform.os, platform.arch, "", exe, false)
			if err != nil {
				t.Fatal(err)
			}
			if build.Path != want || !build.Reused {
				t.Fatalf("got %+v; want borrowed artifact %s", build, want)
			}
			ft := newFakeTransport()
			ft.execResults[shellQuotePath(RemoteBinaryPath(version.Version))+" --version"] = ExecResult{Stdout: version.Version + "\n"}
			if err := InstallBinary(ft, version.Version, build.Path); err != nil {
				t.Fatal(err)
			}
			if string(ft.copyContent) != "CLI fixture" {
				t.Fatal("bundled CLI not uploaded")
			}
			if _, err := os.Stat(want); err != nil {
				t.Fatalf("bundled artifact removed: %v", err)
			}
		})
	}
}

func TestPrepareLocalBuildCLIOnlyReuse(t *testing.T) {
	t.Chdir(string(filepath.Separator))
	t.Setenv("PATH", t.TempDir())
	exe := filepath.Join(t.TempDir(), "ocode") // Packaged GUI is also named ocode.
	writeBinaryFixture(t, exe)
	exe = resolvedFixture(t, exe)
	build, err := prepareLocalBuild(runtime.GOOS, runtime.GOARCH, "", exe, true)
	if err != nil || build.Path != exe || !build.Reused {
		t.Fatalf("CLI reuse = %+v, %v", build, err)
	}
	if _, err := prepareLocalBuild(runtime.GOOS, runtime.GOARCH, "", exe, false); err == nil || !strings.Contains(err.Error(), "make desktop-app") {
		t.Fatalf("desktop executable must not be uploaded as CLI: %v", err)
	}
	// A bundled CLI takes precedence even on a matching platform.
	want := filepath.Join(filepath.Dir(exe), "remote-binaries", version.Version, "ocode-"+runtime.GOOS+"-"+runtime.GOARCH)
	writeBinaryFixture(t, want)
	build, err = prepareLocalBuild(runtime.GOOS, runtime.GOARCH, "", exe, false)
	if err != nil || build.Path != want || !build.Reused {
		t.Fatalf("desktop bundled selection = %+v, %v", build, err)
	}
}

func TestBundledRemoteBinaryRejectsStaleAndEmpty(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "ocode-desktop")
	base := filepath.Join(filepath.Dir(exe), "remote-binaries")
	writeBinaryFixture(t, filepath.Join(base, "old-version", "ocode-linux-amd64"))
	if path, err := bundledRemoteBinary(exe, "linux", "amd64"); err != nil || path != "" {
		t.Fatalf("stale version selected: %q, %v", path, err)
	}
	path := filepath.Join(base, version.Version, "ocode-linux-amd64")
	writeBinaryFixture(t, path)
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := bundledRemoteBinary(exe, "linux", "amd64"); err == nil {
		t.Fatal("empty artifact accepted")
	}
}
