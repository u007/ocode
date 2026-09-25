package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureExecutablePathMergesUnixLoginAndToolchainDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix login-shell PATH probe")
	}
	home := t.TempDir()
	goBin := filepath.Join(home, "go", "bin")
	cargoBin := filepath.Join(home, ".cargo", "bin")
	nvmBin := filepath.Join(home, ".nvm", "versions", "node", "v22.17.0", "bin")
	for _, dir := range []string{goBin, cargoBin, nvmBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := map[string]string{
		"PATH":    "/usr/bin:/bin:/usr/bin",
		"SHELL":   "/bin/zsh",
		"NVM_DIR": filepath.Join(home, ".nvm"),
	}
	probeCalls := 0
	got, changed := ensureExecutablePath(executablePathOptions{
		goos:    "darwin",
		pathSep: ":",
		home:    home,
		current: env["PATH"],
		getenv:  func(key string) string { return env[key] },
		exists: func(path string) bool {
			if !strings.HasPrefix(path, home) {
				return false
			}
			_, err := os.Stat(path)
			return err == nil
		},
		join: filepath.Join,
		probe: func(ctx context.Context, shell string, args ...string) (string, error) {
			probeCalls++
			if loginShell(shell) == "" {
				return "", fmt.Errorf("unsupported shell = %q", shell)
			}
			if len(args) != 3 || args[0] != "-l" || args[1] != "-c" {
				return "", fmt.Errorf("args = %v", args)
			}
			return "/opt/homebrew/bin:/usr/local/bin", nil
		},
	})
	if !changed {
		t.Fatal("ensureExecutablePath changed = false, want true")
	}
	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", probeCalls)
	}

	want := strings.Join([]string{
		"/usr/bin",
		"/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
		goBin,
		cargoBin,
		nvmBin,
	}, ":")
	if got != want {
		t.Fatalf("effective PATH = %q, want %q", got, want)
	}
}

func TestEnsureExecutablePathWindowsUsesUserDirsWithoutShellProbe(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	goBin := filepath.Join(home, "go", "bin")
	cargoBin := filepath.Join(home, ".cargo", "bin")
	npmBin := filepath.Join(appData, "npm")
	pnpmBin := filepath.Join(localAppData, "pnpm")
	for _, dir := range []string{goBin, cargoBin, npmBin, pnpmBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := map[string]string{
		"PATH":              `C:\Windows\System32;C:\Windows`,
		"APPDATA":           appData,
		"LOCALAPPDATA":      localAppData,
		"USERPROFILE":       home,
		"ChocolateyInstall": filepath.Join(home, "chocolatey"),
	}
	probeCalled := false
	got, changed := ensureExecutablePath(executablePathOptions{
		goos:    "windows",
		pathSep: ";",
		home:    home,
		current: env["PATH"],
		getenv:  func(key string) string { return env[key] },
		exists: func(path string) bool {
			if !strings.HasPrefix(path, home) {
				return false
			}
			_, err := os.Stat(path)
			return err == nil
		},
		join: filepath.Join,
		probe: func(context.Context, string, ...string) (string, error) {
			probeCalled = true
			return "", nil
		},
	})
	if !changed {
		t.Fatal("ensureExecutablePath changed = false, want true")
	}
	if probeCalled {
		t.Fatal("Windows PATH hydration invoked a Unix shell probe")
	}

	want := strings.Join([]string{
		`C:\Windows\System32`,
		`C:\Windows`,
		goBin,
		cargoBin,
		npmBin,
		pnpmBin,
	}, ";")
	if got != want {
		t.Fatalf("effective Windows PATH = %q, want %q", got, want)
	}
}

func TestEnsureExecutablePathUpdatesProcessEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix login-shell probe is not used on Windows")
	}
	home := t.TempDir()
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", "/usr/bin:/bin")

	if !EnsureExecutablePath() {
		t.Fatal("EnsureExecutablePath() = false, want true")
	}
	if !strings.Contains(os.Getenv("PATH"), goBin) {
		t.Fatalf("hydrated PATH %q does not contain %q", os.Getenv("PATH"), goBin)
	}
}

func TestExtractExecutablePathIgnoresShellStartupNoise(t *testing.T) {
	raw := "shell profile output\n" + executablePathProbePrefix + "/opt/homebrew/bin:/usr/local/bin" + executablePathProbeSuffix + "\n"
	if got, want := extractExecutablePath(raw), "/opt/homebrew/bin:/usr/local/bin"; got != want {
		t.Fatalf("extractExecutablePath() = %q, want %q", got, want)
	}
	if got := extractExecutablePath(executablePathProbePrefix + "only-prefix"); got != "" {
		t.Fatalf("extractExecutablePath() with missing suffix = %q, want empty", got)
	}
}

func TestEnsureExecutablePathFallsBackWhenLoginShellProbeFails(t *testing.T) {
	home := t.TempDir()
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}

	got, changed := ensureExecutablePath(executablePathOptions{
		goos:    "linux",
		pathSep: string(os.PathListSeparator),
		home:    home,
		current: "/usr/bin",
		getenv: func(key string) string {
			if key == "SHELL" {
				return "/bin/bash"
			}
			return ""
		},
		exists: func(path string) bool {
			if !strings.HasPrefix(path, home) {
				return false
			}
			_, err := os.Stat(path)
			return err == nil
		},
		join: filepath.Join,
		probe: func(context.Context, string, ...string) (string, error) {
			return "", fmt.Errorf("shell startup timed out")
		},
	})
	if !changed {
		t.Fatal("ensureExecutablePath changed = false, want true")
	}
	if want := "/usr/bin" + string(os.PathListSeparator) + goBin; got != want {
		t.Fatalf("fallback PATH = %q, want %q", got, want)
	}
}
