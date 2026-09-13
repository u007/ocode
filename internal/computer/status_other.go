//go:build !darwin && !windows && !linux

package computer

func backendName() string {
	return "unsupported platform"
}
