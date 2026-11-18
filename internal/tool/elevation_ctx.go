package tool

import "context"

// elevationSecretKey is the private key for the elevation-secret getter in a
// tool-call context. The value is a getter closure, never the secret bytes:
// the owner of the store decides when (and whether) a Take consumes the
// single-use secret, so merely building or marshalling a context cannot leak
// it, and the bytes never enter a transcript, log or SSE frame.
type elevationSecretKey struct{}

// elevationNoSecretMessage is the fail-closed message shared by the exec.go
// guard and the Unix PTY executor.
const elevationNoSecretMessage = "elevation approved but no password is available; refusing to run the command unprivileged"

// WithElevationSecret returns a context carrying getter as the source of the
// elevation password for re-executing an approved tool call. Passing a nil
// getter is equivalent to attaching nothing (ElevationSecretFromContext
// returns nil). Like WithTodoScope, this attaches only a closure.
func WithElevationSecret(ctx context.Context, getter func() []byte) context.Context {
	if getter == nil {
		return ctx
	}
	return context.WithValue(ctx, elevationSecretKey{}, getter)
}

// ElevationSecretFromContext returns the getter attached by
// WithElevationSecret, or nil when the call carries no elevation context.
// Callers must handle nil — an ordinary approval provides no password.
func ElevationSecretFromContext(ctx context.Context) func() []byte {
	if ctx == nil {
		return nil
	}
	getter, _ := ctx.Value(elevationSecretKey{}).(func() []byte)
	return getter
}

// elevationFlagKey is the private key for the independent elevation marker.
// It is deliberately separate from the secret getter: the getter is attached
// to every approved call, but only a leading sudo/su approval sets this flag.
type elevationFlagKey struct{}

// WithElevationFlag marks ctx as an approved elevation (a leading sudo/su),
// independent of whether a password is available. On Windows no password is
// collected (UAC authenticates), yet the call must still run on the elevated
// path; on Unix a missing password must fail closed instead of falling through
// to the ordinary path and running the command unprivileged.
func WithElevationFlag(ctx context.Context, isElevation bool) context.Context {
	return context.WithValue(ctx, elevationFlagKey{}, isElevation)
}

// ElevationFlagFromContext reports whether WithElevationFlag marked this call
// as an elevation. False means an ordinary call and the normal execution path.
func ElevationFlagFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, _ := ctx.Value(elevationFlagKey{}).(bool)
	return flag
}
