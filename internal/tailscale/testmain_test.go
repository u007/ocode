package tailscale

import (
	"os"
	"testing"
)

// TestMain blocks the real tailscale CLI for the whole binary: a test that
// reaches an exposure helper must never edit the developer's live serve config.
// Tests that exercise the argv path opt in via isolateCLI.
func TestMain(m *testing.M) {
	CLIPath = func() string { return "" }
	os.Exit(m.Run())
}
