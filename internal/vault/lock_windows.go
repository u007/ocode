//go:build windows

package vault

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// fileLock owns an exclusive advisory lock over a vault file. The overlapped
// structure must outlive the lock because it is required again to unlock.
type fileLock struct {
	f  *os.File
	ol windows.Overlapped
}

// acquireFileLock takes an exclusive, cross-process advisory lock for path on a
// `<path>.lock` sidecar. This is what makes the vault's load-modify-write safe
// when more than one ocode process (desktop shell, CLI, TUI) edits the same
// vault concurrently: the lock serialises the read-merge-write so no process's
// update is lost. It blocks until the lock becomes available.
func acquireFileLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("vault: open lock file: %w", err)
	}
	l := &fileLock{f: f}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &l.ol); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("vault: close lock file after lock failure: %v", cerr)
		}
		return nil, fmt.Errorf("vault: lock file: %w", err)
	}
	return l, nil
}

// release drops the lock and closes the sidecar. Safe to call once; a nil
// receiver is a no-op.
func (l *fileLock) release() {
	if l == nil || l.f == nil {
		return
	}
	if err := windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &l.ol); err != nil {
		log.Printf("vault: unlock: %v", err)
	}
	if err := l.f.Close(); err != nil {
		log.Printf("vault: close lock file: %v", err)
	}
	l.f = nil
}
