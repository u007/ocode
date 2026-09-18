package agent

import (
	"os"
	"testing"
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
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
