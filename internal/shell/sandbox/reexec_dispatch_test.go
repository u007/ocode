package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReexecBinariesDispatchConfiner guards the re-exec contract of the Linux
// confiner: every ocode binary that os.Executable() can resolve to must route
// the hidden `sandbox-confine` subcommand to sandbox.ConfineEntrypoint.
//
// The desktop binary (cmd/ocode-desktop) shipped without that dispatch, so on
// Linux the Landlock confiner re-exec'd ocode-desktop, which fell through to
// the Wails bootstrap and opened a second window instead of confining — every
// sandboxed bash call hung or failed. The root CLI had the case; the desktop
// binary did not.
//
// This is a source-level assertion rather than an executable test on purpose:
// cmd/ocode-desktop links Wails/cgo and is not test-buildable in CI, so the
// only cheap way to catch a missing dispatch is to assert it is present. If a
// new re-exec-capable main package is added, add it to the list below.
func TestReexecBinariesDispatchConfiner(t *testing.T) {
	root, ok := repoRoot()
	if !ok {
		// The raw test binary was run outside the source tree (e.g. copied
		// into a container); there is no source to inspect. `go test` always
		// runs with the package dir as cwd, so this never skips in CI.
		t.Skip("module root not found; source-layout guard skipped")
	}
	for _, rel := range []string{"main.go", "cmd/ocode-desktop/main.go"} {
		path := filepath.Join(root, rel)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(src)
		if !strings.Contains(text, `"`+confinerSubcommand+`"`) {
			t.Errorf("%s does not dispatch the %q hidden subcommand", rel, confinerSubcommand)
		}
		if !strings.Contains(text, "ConfineEntrypoint") {
			t.Errorf("%s never calls sandbox.ConfineEntrypoint", rel)
		}
	}
}

// repoRoot walks up from the test's working directory (the package dir) to the
// module root (the dir containing go.mod). Returns false when no module root is
// reachable, which happens only when the raw test binary runs outside the
// source tree.
func repoRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
