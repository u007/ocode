//go:build darwin

package sandbox

// newWrapper selects the Seatbelt backend on macOS. Task 3 wires the real
// backend in; until then it degrades to the no-op so every target compiles and
// the fail-closed plumbing can land first.
func newWrapper() Wrapper { return newNoop() }