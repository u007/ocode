package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleVaultFile() *vaultFile {
	return &vaultFile{
		Version: 1,
		KDF: kdfParams{
			Algo:      "argon2id",
			Salt:      "c2FsdA==",
			Time:      kdfTime,
			MemoryKiB: kdfMemory,
			Threads:   kdfThreads,
		},
		WrappedKey: "d3JhcHBlZA==",
		Items:      []diskItem{{ID: "i1", Blob: "YmxvYg=="}},
	}
}

func TestWriteReadVaultFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	in := sampleVaultFile()

	if err := writeVaultFile(path, in); err != nil {
		t.Fatalf("writeVaultFile: %v", err)
	}
	got, err := readVaultFile(path)
	if err != nil {
		t.Fatalf("readVaultFile: %v", err)
	}
	if got.Version != in.Version {
		t.Errorf("Version = %d, want %d", got.Version, in.Version)
	}
	if got.KDF != in.KDF {
		t.Errorf("KDF = %+v, want %+v", got.KDF, in.KDF)
	}
	if got.WrappedKey != in.WrappedKey {
		t.Errorf("WrappedKey = %q, want %q", got.WrappedKey, in.WrappedKey)
	}
	if len(got.Items) != 1 || got.Items[0] != in.Items[0] {
		t.Errorf("Items = %+v, want %+v", got.Items, in.Items)
	}
}

func TestWriteVaultFileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := writeVaultFile(path, sampleVaultFile()); err != nil {
		t.Fatalf("writeVaultFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
}

func TestReadVaultFileMissingIsErrNoVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	_, err := readVaultFile(path)
	if !errors.Is(err, ErrNoVault) {
		t.Fatalf("readVaultFile(missing) = %v, want ErrNoVault", err)
	}
}

// TestFileLockSerializes pins the cross-process safety property: while one
// holder owns the lock, a second acquire on the same path must block until the
// first releases.
func TestFileLockSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")

	first, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}

	acquired := make(chan error, 1)
	go func() {
		second, err := acquireFileLock(path)
		if err == nil {
			second.release()
		}
		acquired <- err
	}()

	select {
	case err := <-acquired:
		first.release()
		t.Fatalf("second lock acquired while first was held (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: the second acquire is blocked.
	}

	first.release()

	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("second lock failed after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after the first was released")
	}
}
