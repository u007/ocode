//go:build darwin

package computer

func backendName() string {
	return "macOS: screencapture + CGEvent"
}
