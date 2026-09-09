//go:build !windows

package cdp

import "os"

func replaceHTRFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
