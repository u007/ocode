package secretjob

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/u007/ocode/internal/secretfile"
)

func newTestKey(t *testing.T, dir string) *secretfile.ProjectKey {
	t.Helper()
	keyPath := secretfile.ProjectKeyPath(dir)
	key, err := secretfile.GenerateProjectKey(keyPath, "passphrase")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	return key
}

func TestWalk_EncryptModeSkipsAlreadyEncryptedAndKeyFile(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)
	keyPath := secretfile.ProjectKeyPath(dir)

	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}

	encrypted := filepath.Join(dir, "already.txt")
	ct, err := key.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(encrypted, ct, 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := Walk(dir, keyPath, true)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 1 || files[0] != plain {
		t.Fatalf("expected only %s, got %v", plain, files)
	}
}

func TestWalk_DecryptModeOnlySelectsEncrypted(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)
	keyPath := secretfile.ProjectKeyPath(dir)

	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted := filepath.Join(dir, "sub", "already.txt")
	if err := os.MkdirAll(filepath.Dir(encrypted), 0o700); err != nil {
		t.Fatal(err)
	}
	ct, err := key.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(encrypted, ct, 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := Walk(dir, keyPath, false)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 1 || files[0] != encrypted {
		t.Fatalf("expected only %s, got %v", encrypted, files)
	}
}

func TestWalk_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	keyPath := secretfile.ProjectKeyPath(dir)

	inGit := filepath.Join(dir, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(inGit), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inGit, []byte("git internals"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(outside, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := Walk(dir, keyPath, true)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 1 || files[0] != outside {
		t.Fatalf("expected only %s (git dir must be skipped), got %v", outside, files)
	}
}

func TestTransformFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)

	file := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(file, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := TransformFile(key, file, true); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := TransformFile(key, file, true); err == nil {
		t.Fatal("expected error encrypting an already-encrypted file")
	}

	if err := TransformFile(key, file, false); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("mismatch after round trip: %q", got)
	}
	if err := TransformFile(key, file, false); err == nil {
		t.Fatal("expected error decrypting a plaintext file")
	}
}

func TestManager_ReportsProgressAndCompletes(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)

	var files []string
	for i := 0; i < 3; i++ {
		f := filepath.Join(dir, string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}

	m := NewManager()
	var progressed []string
	doneCh := make(chan error, 1)

	_, err := m.Start(dir, key, files, true, func(jobID string, done, total int, current string) {
		progressed = append(progressed, current)
	}, func(jobID string, err error) {
		doneCh <- err
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("job failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job to finish")
	}

	sort.Strings(progressed)
	sort.Strings(files)
	if len(progressed) != len(files) {
		t.Fatalf("progress reported %d files, want %d", len(progressed), len(files))
	}
	for i := range files {
		if progressed[i] != files[i] {
			t.Fatalf("progress mismatch: got %v, want %v", progressed, files)
		}
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !secretfile.IsEncrypted(data) {
			t.Fatalf("%s was not encrypted by the job", f)
		}
	}
}

func TestManager_CancelStopsBeforeNextFile(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)

	var files []string
	for i := 0; i < 5; i++ {
		f := filepath.Join(dir, string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}

	m := NewManager()
	doneCh := make(chan error, 1)

	// The cancel-on-first-progress callback uses the jobID Start hands it,
	// not Start's own return value: the background goroutine can invoke
	// onProgress before Start returns on this goroutine, so capturing
	// Start's return value here would race.
	_, err := m.Start(dir, key, files, true, func(jobID string, done, total int, current string) {
		if done == 1 {
			m.Cancel(jobID)
		}
	}, func(jobID string, err error) {
		doneCh <- err
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case err := <-doneCh:
		if err != ErrCancelled {
			t.Fatalf("expected ErrCancelled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled job")
	}

	encryptedCount := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if secretfile.IsEncrypted(data) {
			encryptedCount++
		}
	}
	if encryptedCount == 0 || encryptedCount == len(files) {
		t.Fatalf("expected cancellation to stop partway through, got %d/%d encrypted", encryptedCount, len(files))
	}
}

func TestManager_RejectsConcurrentJobForSameRoot(t *testing.T) {
	dir := t.TempDir()
	key := newTestKey(t, dir)

	blocking := make(chan struct{})
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	doneCh := make(chan error, 1)
	_, err := m.Start(dir, key, []string{f}, true, func(jobID string, done, total int, current string) {
		<-blocking
	}, func(jobID string, err error) {
		doneCh <- err
	})
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if _, err := m.Start(dir, key, []string{f}, true, nil, func(jobID string, err error) {}); err == nil {
		t.Fatal("expected error starting a second job for the same root")
	}
	close(blocking)

	// Wait for the first job to actually finish before the test returns, so
	// t.TempDir()'s cleanup doesn't race with its still-in-flight atomic
	// write.
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first job to finish")
	}
}
