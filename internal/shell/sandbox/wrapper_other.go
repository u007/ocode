//go:build !darwin && !linux

package sandbox

// newWrapper selects the backend for this GOOS. On non-darwin/non-linux it is
// always the no-op.
func newWrapper() Wrapper { return newNoop() }
