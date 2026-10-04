package agent

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestMain redirects the managed state dir (truncated tool-results cache,
// cloned-repo cache) for the whole package. Agent-loop tests execute real tool
// calls whose results pass through TruncateToolResult, and without this they
// write <toolID>.txt files into the user's real ~/.local/state/opencode.
// Contract: HARMLESS (no test touches the real state dir) and CROSS-PLATFORM
// (XDG_STATE_HOME is honored before any OS-specific branch). Tests that need
// a specific resolver branch override it with t.Setenv.
func TestMain(m *testing.M) {
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
		for _, key := range []string{homeMarker, "HOME", "USERPROFILE", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
			if err := os.Setenv(key, home); err != nil {
				panic(err)
			}
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
