package desktop

import (
	"os"
	"testing"

	"github.com/u007/ocode/internal/tailscale"
)

// TestMain blocks the real tailscale CLI for the whole binary: a test that
// reaches an exposure helper must never edit the developer's live serve config.
func TestMain(m *testing.M) {
	tailscale.CLIPath = func() string { return "" }
	os.Exit(m.Run())
}
