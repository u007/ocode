package discovery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deadPid returns a pid that has already exited (for stale-lock tests).
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestStrayHelperProcess")
	cmd.Env = append(os.Environ(), strayHelperEnv+"=")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run throwaway process: %v", err)
	}
	return cmd.Process.Pid
}

func TestAcquirePidLockReclaimsFromDeadOwner(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.start.lock")
	pid := deadPid(t)
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	// mtime is now fresh — only pid liveness should reclaim it.
	acquired, release, err := acquirePidLock(lockPath, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !acquired {
		t.Fatal("a lock whose owner is dead must be reclaimed immediately, not waited out")
	}
	defer release()
	data, _ := os.ReadFile(lockPath)
	if strings.TrimSpace(string(data)) != fmt.Sprintf("%d", os.Getpid()) {
		t.Fatalf("reclaimed lock should record new owner pid %d, got %q", os.Getpid(), data)
	}
}

func TestAcquirePidLockRespectsLiveOwner(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.start.lock")
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	acquired, _, err := acquirePidLock(lockPath, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired {
		t.Fatal("a lock held by a live process must not be reclaimed")
	}
}

func TestPidLockReleaseOnlyOwnPid(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.start.lock")

	// Acquire and verify release removes it.
	acquired, release, err := acquirePidLock(lockPath, 10*time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire: acquired=%v err=%v", acquired, err)
	}
	release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("release should remove our own lock")
	}

	// Acquire again, then simulate ownership moving to another pid before we release.
	acquired, release2, err := acquirePidLock(lockPath, 10*time.Minute)
	if err != nil || !acquired {
		t.Fatalf("re-acquire: acquired=%v err=%v", acquired, err)
	}
	// Overwrite with a foreign live pid (our own pid is still live, but file no longer names us).
	// Use a dead pid so the file looks like it moved; the release must not delete it.
	foreign := deadPid(t)
	// First make it look like we died and someone else took over by writing foreign pid
	// that is dead — but to test the live-move case we need a live pid. Use our pid+1
	// if it happens to be alive is unreliable; instead test the dead-foreign case via
	// the stale check: after we overwrite with a different pid, pidLockRelease should
	// see pid != self and leave it alone.
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", foreign)), 0644); err != nil {
		t.Fatalf("overwrite lock: %v", err)
	}
	release2()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("release must not delete a lock that moved to another pid, err=%v", err)
	}
}

func TestPidLockReclaimableFallbackToMtime(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.start.lock")
	// Pre-pid lock (no pid, just garbage) with fresh mtime: not reclaimable.
	if err := os.WriteFile(lockPath, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if pidLockReclaimable(lockPath, 10*time.Minute) {
		t.Fatal("garbage lock with fresh mtime must not be reclaimable")
	}
	// Same garbage but old mtime: reclaimable via stale fallback.
	old := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if !pidLockReclaimable(lockPath, 10*time.Minute) {
		t.Fatal("garbage lock past staleAfter must be reclaimable")
	}
}

func TestEmbedServerArgsMatchWrongModel(t *testing.T) {
	cacheDir := t.TempDir()
	// Simulate a server for a DIFFERENT model than the one we want to start:
	// it still references an artifact under cacheDir, so it must be identified
	// as ocode-owned even though its model token doesn't match the "desired"
	// manifest. This is the M1 fix: wrong-model squat must be reclaimed.
	otherToken := "other-model.gguf"
	// Plant a fake artifact reference under cacheDir/local-test
	artifactPath := filepath.Join(cacheDir, "local-darwin-arm64", otherToken)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	args := []string{"llama-server", "-m", artifactPath, "--port", "11457"}
	if !embedServerArgsMatch(args, 11457, cacheDir) {
		t.Fatal("server referencing a cache artifact must match as ocode-owned regardless of model")
	}
	// Same args but wrong port: must not match.
	if embedServerArgsMatch(args, 11458, cacheDir) {
		t.Fatal("exact port check must fail for wrong port")
	}
	// Foreign server on same port with no cache reference and no known token: must not match.
	foreignArgs := []string{"some-server", "--port", "11457", "--model", "foreign"}
	if embedServerArgsMatch(foreignArgs, 11457, cacheDir) {
		t.Fatal("foreign server with no ocode signature must not match")
	}
}

func TestEmbedServerArgsMatchAnyManifestToken(t *testing.T) {
	cacheDir := t.TempDir()
	// Any known manifest token should match even without cache path.
	for _, m := range localManifests {
		token := m.ExpectedServeID()
		if token == "" {
			continue
		}
		args := []string{"server", "--model", token, "--port", "11457"}
		if !embedServerArgsMatch(args, 11457, cacheDir) {
			t.Fatalf("known manifest token %q should match as ocode-owned", token)
		}
		break // one is enough to prove the path
	}
}

func TestWaitForEmbedHealthTrackedHolderGone(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.start.lock")
	pid := deadPid(t)
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	// No server on 127.0.0.1:1, and lock holder is dead -> should return errEmbedHolderGone immediately
	err := waitForEmbedHealthTracked("http://127.0.0.1:1", ServerManifest{ModelID: "local/bge-m3", Backend: BackendLlamaCpp, Artifacts: []Artifact{{Dest: "bge-m3.gguf"}}}, lockPath, 5)
	if err == nil {
		t.Fatal("expected holder-gone error when lock owner dead and server unhealthy")
	}
	// Should be the sentinel
	if err.Error() != errEmbedHolderGone.Error() {
		t.Fatalf("expected errEmbedHolderGone, got %v", err)
	}
}
