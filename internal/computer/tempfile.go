package computer

import (
	"os"
)

// tempPNGPath returns a unique file path under os.TempDir()
// with prefix ocode-computer- and .png suffix.
func tempPNGPath() (string, error) {
	f, err := os.CreateTemp("", "ocode-computer-*.png")
	if err != nil {
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// readAndRemove reads the file at path and removes it.
func readAndRemove(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, os.Remove(path)
}
