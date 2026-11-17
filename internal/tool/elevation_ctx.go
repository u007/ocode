package tool

import "context"

// elevationSecretKey is the private key for the elevation-secret getter in a
// tool-call context. The value is a getter closure, never the secret bytes:
// the owner of the store decides when (and whether) a Take consumes the
// single-use secret, so merely building or marshalling a context cannot leak
// it, and the bytes never enter a transcript, log or SSE frame.
type elevationSecretKey struct{}

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
