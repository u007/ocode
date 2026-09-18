package computer

import (
	"context"
	"os/exec"
	"time"
)

// PermissionReport is the outcome of a user-triggered request for the
// operating-system permissions the computer tool depends on. It is returned by
// RequestPermissions and surfaced verbatim in the settings UI.
type PermissionReport struct {
	// Platform is the runtime.GOOS the report was produced on.
	Platform string `json:"platform"`
	// Granted is true when every grant this platform can *detect* is present:
	// Accessibility and Automation on macOS, or "no explicit grant required" on
	// Windows/Linux. On macOS Screen Recording is not verifiable — a denied
	// grant still produces a wallpaper-only capture with no error — so
	// Granted=true does not guarantee it; the report's Screen Recording line
	// carries that caveat. A Screen Recording probe that *errors* does clear
	// Granted, because that is a detectable problem.
	Granted bool `json:"granted"`
	// Lines are human-readable per-permission status lines, in the order the
	// permissions were requested.
	Lines []string `json:"lines"`
}

// permissionRun executes a helper command and returns its combined output.
// It is a package-level var so tests can stub it and never fire a real OS
// permission prompt.
var permissionRun = func(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// RequestPermissions asks the operating system for the grants the computer tool
// needs on the current platform. It is safe to call on every platform: where no
// explicit grant exists it returns an informational report. Triggering prompts
// is best-effort and never returns an error — the report describes what
// happened.
func RequestPermissions(ctx context.Context) PermissionReport {
	if ctx == nil {
		ctx = context.Background()
	}
	return platformRequestPermissions(ctx)
}

// timeoutContext bounds one permission helper invocation so an unanswered
// macOS consent dialog cannot pin the HTTP request indefinitely.
func timeoutContext(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}
