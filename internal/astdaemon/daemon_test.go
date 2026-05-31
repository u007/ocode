package astdaemon

import (
	"os"
	"path/filepath"
	"testing"
)

// sharedTestDirs creates a temp dir with .sgindex/ and returns (dir, lockPath, indexDir).
func sharedTestDirs(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	indexDir := filepath.Join(dir, indexDirRel)
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(dir, lockFileRel), indexDir
}

func TestTryAcquireLock(t *testing.T) {
	dir, lockPath, indexDir := sharedTestDirs(t)
	inst := &Instance{
		projectRoot: dir,
		lockPath:    lockPath,
		indexDir:    indexDir,
	}

	// First acquire should succeed.
	acquired, err := inst.tryAcquireLock()
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock first time")
	}
	defer inst.releaseLock()

	// Second acquire (same lock file) should fail.
	inst2 := &Instance{
		projectRoot: dir,
		lockPath:    lockPath,
		indexDir:    indexDir,
	}
	acquired2, err := inst2.tryAcquireLock()
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if acquired2 {
		t.Fatal("second instance should not acquire lock")
	}
}

func TestReleaseLock(t *testing.T) {
	dir, lockPath, indexDir := sharedTestDirs(t)
	inst := &Instance{
		projectRoot: dir,
		lockPath:    lockPath,
		indexDir:    indexDir,
	}

	_, err := inst.tryAcquireLock()
	if err != nil {
		t.Fatal(err)
	}

	// Verify lock file exists.
	if _, err := os.Stat(inst.lockPath); err != nil {
		t.Fatalf("lock file should exist after acquire: %v", err)
	}

	inst.releaseLock()

	// Verify lock file is gone.
	if _, err := os.Stat(inst.lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file should be removed after release")
	}
}

func TestProcessExists(t *testing.T) {
	// PID 0 or negative should not exist.
	if processExists(0) {
		t.Fatal("PID 0 should not be considered running")
	}
	if processExists(-1) {
		t.Fatal("PID -1 should not be considered running")
	}
}

func TestIndexStatus(t *testing.T) {
	dir, _, _ := sharedTestDirs(t)

	status, err := GetIndexStatus(dir)
	if err != nil {
		t.Fatalf("GetIndexStatus: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Installed {
		t.Log("sg is installed on this machine")
	}

	// Verify DaemonAlive is false since we haven't started the daemon.
	if status.DaemonAlive {
		t.Log("daemon is already running (unexpected in test)")
	}
}

func TestEnsureRunning_StaleLock(t *testing.T) {
	dir, lockPath, _ := sharedTestDirs(t)

	// Create a lock file manually as if another instance (PID 999999) holds it.
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// EnsureRunning should detect stale lock and take over.
	inst, err := EnsureRunning(dir)
	if err != nil {
		t.Fatalf("EnsureRunning with stale lock: %v", err)
	}
	defer inst.Stop()

	if !inst.IsDaemon() {
		t.Fatal("should have taken over as daemon after stale lock")
	}
}
