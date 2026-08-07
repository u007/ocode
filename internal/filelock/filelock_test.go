package filelock

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWithFileLockExecutesFn(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")
	executed := false
	err := WithFileLock(lockPath, func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithFileLock returned error: %v", err)
	}
	if !executed {
		t.Fatal("fn was not executed")
	}
}

func TestWithFileLockCreatesLockFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")
	if err := WithFileLock(lockPath, func() error { return nil }); err != nil {
		t.Fatalf("WithFileLock returned error: %v", err)
	}
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file was not created")
	}
}

func TestWithFileLockReturnsFnError(t *testing.T) {
	err := WithFileLock(filepath.Join(t.TempDir(), "test.lock"), func() error {
		return os.ErrInvalid
	})
	if err != os.ErrInvalid {
		t.Fatalf("expected os.ErrInvalid, got %v", err)
	}
}

func TestWithFileLockSerializesConcurrentAccess(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")

	var mu sync.Mutex
	concurrent := 0
	maxConcurrent := 0

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithFileLock(lockPath, func() error {
				mu.Lock()
				concurrent++
				if concurrent > maxConcurrent {
					maxConcurrent = concurrent
				}
				mu.Unlock()

				// Hold the lock briefly.
				time.Sleep(5 * time.Millisecond)

				mu.Lock()
				concurrent--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithFileLock error: %v", err)
			}
		}()
	}
	wg.Wait()

	if maxConcurrent > 1 {
		t.Errorf("expected maxConcurrent <= 1 (serialized), got %d", maxConcurrent)
	}
}

func TestWithFileLockBoundedWait(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")

	f, err := os.OpenFile(lockPath, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Acquire an exclusive flock on the external fd, then verify the helper
	// times out rather than proceeding.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("failed to acquire external lock: %v", err)
	}
	start := time.Now()
	err = WithFileLock(lockPath, func() error {
		t.Error("fn should not be executed when lock is held")
		return nil
	})
	if err == nil {
		t.Fatal("expected timeout error when lock is held externally")
	}
	if elapsed := time.Since(start); elapsed < Timeout-2*time.Second {
		t.Errorf("bounded wait returned after %v, expected to hold near %v", elapsed, Timeout)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
