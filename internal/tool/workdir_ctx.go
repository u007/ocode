package tool

import "context"

// workDirKey carries the agent's resolved working directory (the session's
// project root) through the tool-execution context so path-confining helpers
// (confinedPath) resolve relative paths and check containment against the
// project root rather than the process working directory.
//
// The process cwd is whatever directory ocode was launched from and is
// frequently NOT the project root: ocode may be launched from a parent
// directory, from a multi-project/desktop session, or the TUI may have been
// started elsewhere and then pointed at a project via /cd. Using the process
// cwd as the confinement root wrongly rejects writes to absolute paths that
// live inside the project but outside the process cwd. That made file edits
// performed with a full path silently fail, so they never reached the
// changes tab. See confinedPath for the consumer.
type workDirKey struct{}

// WithWorkDir returns a context carrying wd as the project root for path
// confinement. An empty wd is meaningful and intentional: confinedPath then
// falls back to os.Getwd(), preserving the behavior used by non-agent callers
// (ocodeconfig, standalone helper invocations, and most unit tests).
func WithWorkDir(ctx context.Context, wd string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, workDirKey{}, wd)
}

// workDirFromContext returns the project root stored by WithWorkDir, or "".
func workDirFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	wd, _ := ctx.Value(workDirKey{}).(string)
	return wd
}
