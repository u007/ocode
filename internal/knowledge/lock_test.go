package knowledge

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestWithBundleLockExecutesFn(t *testing.T) {
	td := t.TempDir()
	executed := false
	err := WithBundleLock(td, func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithBundleLock returned error: %v", err)
	}
	if !executed {
		t.Fatal("fn was not executed")
	}
}

func TestWithBundleLockCreatesLockFile(t *testing.T) {
	td := t.TempDir()
	err := WithBundleLock(td, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("WithBundleLock returned error: %v", err)
	}
	lockPath := filepath.Join(td, ".okf.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file was not created")
	}
}

func TestWithBundleLockReturnsFnError(t *testing.T) {
	td := t.TempDir()
	err := WithBundleLock(td, func() error {
		return os.ErrInvalid
	})
	if err != os.ErrInvalid {
		t.Fatalf("expected os.ErrInvalid, got %v", err)
	}
}

func TestWithBundleLockSerializesConcurrentAccess(t *testing.T) {
	td := t.TempDir()

	var mu sync.Mutex
	// concurrent holds tracks how many goroutines are inside the critical
	// section at once.
	concurrent := 0
	maxConcurrent := 0

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithBundleLock(td, func() error {
				mu.Lock()
				concurrent++
				if concurrent > maxConcurrent {
					maxConcurrent = concurrent
				}
				mu.Unlock()

				// Hold the lock briefly.
				_, _ = os.Stat(td)

				mu.Lock()
				concurrent--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithBundleLock error: %v", err)
			}
		}()
	}
	wg.Wait()

	if maxConcurrent > 1 {
		t.Errorf("expected maxConcurrent <= 1 (serialized), got %d", maxConcurrent)
	}
}

func TestWithBundleLockBoundedWait(t *testing.T) {
	td := t.TempDir()

	// Acquire the lock externally on the lock file (not just create the file).
	lockPath := filepath.Join(td, ".okf.lock")
	f, err := os.OpenFile(lockPath, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Acquire an exclusive flock on the external fd.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("failed to acquire external lock: %v", err)
	}

	// With the lock held externally, WithBundleLock should time out.
	err = WithBundleLock(td, func() error {
		t.Error("fn should not be executed when lock is held")
		return nil
	})
	if err == nil {
		t.Fatal("expected timeout error when lock is held externally")
	}
	// Release the external lock so cleanup doesn't hang.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
