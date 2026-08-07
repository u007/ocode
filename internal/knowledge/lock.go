package knowledge

import (
	"path/filepath"

	"github.com/u007/ocode/internal/filelock"
)

const (
	// lockFileName is the name of the lock file created in the bundle root.
	lockFileName = ".okf.lock"
)

// WithBundleLock acquires an exclusive advisory lock on <root>/.okf.lock,
// executes fn, and releases the lock on return. It delegates to the shared
// filelock helper so the flock implementation exists exactly once.
func WithBundleLock(root string, fn func() error) (err error) {
	return filelock.WithFileLock(filepath.Join(root, lockFileName), fn)
}
