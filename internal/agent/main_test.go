package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/shell/sandbox"
)

// setHomeTree points every global-config root at one directory tree, so a test
// that overrides HOME gets the isolation it asks for on macOS and Linux alike.
// It replaces a bare t.Setenv("HOME", …) — never add that back. Rationale and
// the failures this fixed are in CLAUDE.md § Coding Standards.
//
// A test that sets only HOME gets a private data/config dir on macOS but shares
// the package-wide one on Linux, because paths.GlobalConfigDir and
// paths.OcodeGlobalDataDir honour the XDG variables everywhere except darwin.
// That let persistent grants written by one test be read back by another.
// Pointing each variable at its real per-platform default keeps the platforms in
// agreement:
//
//	XDG_CONFIG_HOME=<home>/.config      → <home>/.config/opencode
//	XDG_DATA_HOME=<home>/.local/share   → <home>/.local/share/opencode
//
// XDG_STATE_HOME is deliberately NOT set here: TestMain owns it and pins it to a
// directory beside the REAL home, because the permission tests treat the OS temp
// dir as a writable root — a state dir under temp would make `rm -rf ~` an
// in-scope delete and collapse the cache-root cases those tests exist to pin.
func setHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	// Windows resolves from these two instead of the XDG variables, and the
	// same "must not be the same directory" rule applies.
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

// TestMain redirects the managed state dir (truncated tool-results cache,
// cloned-repo cache) for the whole package. Agent-loop tests execute real tool
// calls whose results pass through TruncateToolResult, and without this they
// write <toolID>.txt files into the user's real ~/.local/state/opencode.
// Contract: HARMLESS (no test touches the real state dir) and CROSS-PLATFORM
// (XDG_STATE_HOME is honored before any OS-specific branch). Tests that need
// a specific resolver branch override it with t.Setenv.
func TestMain(m *testing.M) {
	// The hidden `sandbox-confine` re-exec MUST be dispatched here, before
	// anything else. internal/shell/sandbox's Linux backend confines by
	// re-executing os.Executable() with this subcommand — and inside a library
	// package's test binary, os.Executable() is THIS test binary, not a main
	// package. Without this dispatch the re-exec fell through to Go's testing
	// main and re-ran the ENTIRE suite in a child process, recursively, so
	// TestSandboxOSBoundaryGrantsSharedProjectWrites blocked forever in
	// cmd.CombinedOutput(): 31m46s when the CI race job's -timeout killed it,
	// 49m19s in a local linux/arm64 container with no -race at all. That is why
	// internal/agent never completed on Linux and why the race job's timeout was
	// firing while the suite reported ZERO test failures — nothing was wrong
	// except that the run never ended.
	//
	// main.go does this for the real CLI; TestReexecBinariesDispatchConfiner
	// only checks main packages, so nothing covered this binary.
	//
	// ConfineEntrypoint returns 0 for a non-confiner invocation (its own argv[1]
	// guard), and on success it execve's and never returns — so an unconditional
	// call is correct here and needs no exported subcommand constant.
	if code := sandbox.ConfineEntrypoint(os.Args); code != 0 {
		os.Exit(code)
	}

	dir, err := os.MkdirTemp("", "ocode-agent-test-state-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_STATE_HOME", dir); err != nil {
		panic(err)
	}
	// HOME (and the XDG data/config homes derived from it) point at an empty
	// directory too. Agents built by these tests otherwise load the developer's
	// real installed plugins, skills, memory and credentials: their SessionStart
	// hooks are real subprocesses that made timing-sensitive tests flaky, and the
	// results differed from machine to machine. Tests that need a populated home
	// set their own with t.Setenv.
	//
	// It is created beside the real home, NOT under the OS temp dir: the
	// permission tests treat temp as a writable root, so a home inside it turns
	// `rm -rf ~` into an in-scope delete and collapses the cache-root and
	// backup-fact cases that depend on home being outside every writable root.
	// A test that re-executes this binary as a helper process runs TestMain
	// again. The helper inherits the parent's isolated home through the
	// environment, so it must neither create a second one nor remove the
	// parent's: the marker says a home is already in place.
	const homeMarker = "OCODE_AGENT_TEST_HOME"
	home := ""
	if os.Getenv(homeMarker) == "" {
		realHome, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		home, err = os.MkdirTemp(realHome, ".ocode-agent-test-home-")
		if err != nil {
			panic(err)
		}
		// XDG_CONFIG_HOME and XDG_DATA_HOME MUST point at DIFFERENT
		// subtrees of the test home, never the same directory as each other.
		//
		// On Linux paths.GlobalConfigDir() resolves $XDG_CONFIG_HOME/opencode
		// and paths.GlobalDataDir() resolves $XDG_DATA_HOME/opencode. Pointing
		// both variables at `home` collapsed those onto ONE directory, and
		// sandboxSensitivePath's self-escalation guard ("a write under the
		// global CONFIG dir Asks") then covered the whole data dir too — so
		// TestDecideSandboxSharedProjectWritesAllow, which asserts ordinary
		// writes under GlobalDataDir/project/** auto-allow in sandbox mode,
		// got Ask instead of Allow. darwin never saw it, because there
		// GlobalConfigDir is $HOME/.config/opencode and GlobalDataDir is
		// $HOME/.local/share/opencode and the two cannot collide.
		//
		// These are also the real per-platform defaults, so the baseline
		// environment now matches a genuine home rather than a synthetic one.
		for key, value := range map[string]string{
			"USERPROFILE":     home,
			"HOME":            home,
			"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
			"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		} {
			if err := os.Setenv(key, value); err != nil {
				panic(err)
			}
		}
		if err := os.Setenv(homeMarker, home); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	if home != "" {
		// Background workers left running by finished tests (model-registry
		// refresh, session saves) can still be writing here, and a write that
		// lands mid-walk fails RemoveAll with "directory not empty". Retry
		// briefly, and name the directory if it survives so it is not silently
		// left in the developer's home.
		var rmErr error
		for attempt := 0; attempt < 20; attempt++ {
			if rmErr = os.RemoveAll(home); rmErr == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if rmErr != nil {
			fmt.Fprintf(os.Stderr, "agent tests: could not remove test home %s: %v\n", home, rmErr)
		}
	}
	os.Exit(code)
}
