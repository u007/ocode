package sysperm

import (
	"context"
	"os/exec"
	"time"
)

// commandRun executes a helper command and returns its combined output. It is
// a package-level var so tests can stub it and never fire a real OS permission
// prompt.
var commandRun = func(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// runBounded runs a helper under a timeout so an unanswered consent dialog
// cannot pin the caller indefinitely.
func runBounded(ctx context.Context, d time.Duration, name string, args ...string) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return commandRun(rctx, name, args...)
}
