//go:build linux

package sandbox

import (
	"os"
	"testing"
)

// TestMain doubles as the re-exec target for the Landlock confiner. The real
// confinement tests (landlock_linux_test.go) invoke Wrap, which re-execs
// os.Executable() — the test binary itself — as `sandbox-confine`. A Go test
// binary that does not dispatch that hidden subcommand simply runs the whole
// suite again, so the wrapped command would recurse forever instead of being
// confined. Routing the subcommand through confineEntrypoint here lets the
// real Landlock/bwrap confinement tests run.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == confinerSubcommand {
		os.Exit(confineEntrypoint(os.Args))
	}
	os.Exit(m.Run())
}
