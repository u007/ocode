//go:build linux

package sandbox

// newWrapper selects the Landlock (with bubblewrap fallback) backend on Linux.
// Task 4 wires the real backend in; until then it degrades to the no-op so
// every target compiles and the fail-closed plumbing can land first.
func newWrapper() Wrapper { return newNoop() }