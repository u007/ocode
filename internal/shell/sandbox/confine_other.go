//go:build !linux

package sandbox

// confineEntrypoint is a no-op on non-Linux platforms (the Landlock confiner
// is linux-only).
func confineEntrypoint([]string) int { return 0 }
