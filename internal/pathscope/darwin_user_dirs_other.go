//go:build !darwin

package pathscope

// DarwinUserDirs is empty on non-macOS platforms.
func DarwinUserDirs() []string { return nil }

// DarwinUserTempDir is empty on non-macOS platforms.
func DarwinUserTempDir() []string { return nil }
