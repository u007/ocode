//go:build darwin

package sysperm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func stubCommandRun(t *testing.T, fn func(context.Context, string, ...string) (string, error)) {
	t.Helper()
	orig := commandRun
	commandRun = fn
	t.Cleanup(func() { commandRun = orig })
}

func TestDetectAccessibilitySeesGrantState(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want Status
	}{
		{"granted", "true\n", nil, StatusGranted},
		{"denied", "false\n", nil, StatusDenied},
		{"error", "", errors.New("boom"), StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubCommandRun(t, func(context.Context, string, ...string) (string, error) {
				return tc.out, tc.err
			})
			if got := detectEntry(context.Background(), Entry{ID: "macos.accessibility"}); got != tc.want {
				t.Fatalf("detectEntry = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestFullDiskAccessIsDeterminate(t *testing.T) {
	got := detectEntry(context.Background(), Entry{ID: "macos.full-disk-access"})
	if got != StatusGranted && got != StatusDenied {
		t.Fatalf("full-disk-access status = %s, want granted or denied", got)
	}
}

func TestProtectedPathNotProbedUntilRequested(t *testing.T) {
	stubCommandRun(t, func(context.Context, string, ...string) (string, error) {
		t.Fatal("a protected path with no recorded request must not run a probe")
		return "", nil
	})
	home, _ := os.UserHomeDir()
	e := Entry{ID: "macos.files.documents", Kind: KindPath, Path: filepath.Join(home, "Documents", "definitely-not-here")}
	if got := detectEntry(context.Background(), e); got != StatusNotDetermined {
		t.Fatalf("status = %s, want not_determined", got)
	}
}

func TestNonProtectedPathProbedWithoutRequest(t *testing.T) {
	stubCommandRun(t, func(context.Context, string, ...string) (string, error) {
		t.Fatal("probing a path must not shell out")
		return "", nil
	})
	dir := t.TempDir()
	if got := detectEntry(context.Background(), Entry{ID: PathID(dir), Kind: KindPath, Path: dir}); got != StatusGranted {
		t.Fatalf("status = %s, want granted for a readable, non-protected path", got)
	}
}

func TestIsProtectedPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(home, "Documents"), true},
		{filepath.Join(home, "Documents", "proj"), true},
		{filepath.Join(home, "Desktop"), true},
		{filepath.Join(home, "Downloads"), true},
		{filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs"), true},
		{filepath.Join("/Volumes", "External"), true},
		{filepath.Join(home, "www", "ocode"), false},
		{filepath.Join(home, "Projects"), false},
	}
	for _, tc := range cases {
		if got := isProtectedPath(tc.path); got != tc.want {
			t.Errorf("isProtectedPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestDirStatus(t *testing.T) {
	dir := t.TempDir()
	if got := dirStatus(dir); got != StatusGranted {
		t.Fatalf("dirStatus(existing dir) = %s, want granted", got)
	}
	if got := dirStatus(filepath.Join(dir, "missing")); got != StatusNotDetermined {
		t.Fatalf("dirStatus(missing) = %s, want not_determined", got)
	}
}

func TestRequestPathDoesNotOpenSettingsWhenGranted(t *testing.T) {
	var opened []string
	stubCommandRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "open" {
			opened = append(opened, args[len(args)-1])
		}
		return "", nil
	})
	dir := t.TempDir()
	res := requestEntry(context.Background(), Entry{ID: PathID(dir), Kind: KindPath, Path: dir})
	if res.Status != StatusGranted {
		t.Fatalf("status = %s, want granted", res.Status)
	}
	if len(opened) != 0 {
		t.Fatalf("opened settings %v for an already-granted path", opened)
	}
}

func TestRequestFullDiskAccessOpensPane(t *testing.T) {
	var opened []string
	stubCommandRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "open" {
			opened = append(opened, args[len(args)-1])
		}
		return "", nil
	})
	res := requestEntry(context.Background(), Entry{ID: "macos.full-disk-access"})
	if !res.OpenedSettings {
		t.Fatal("Full Disk Access request did not open the settings pane")
	}
	if len(opened) == 0 || opened[0] != paneFullDiskAccess {
		t.Fatalf("opened = %v, want %s", opened, paneFullDiskAccess)
	}
	if res.Message == "" {
		t.Fatal("Full Disk Access result has no explanatory message")
	}
}
