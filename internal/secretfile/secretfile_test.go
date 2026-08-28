package secretfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateProjectKey(filepath.Join(t.TempDir(), "project.key.age"), "correct horse battery staple")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}

	plaintext := []byte("super secret content")
	ciphertext, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := key.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestIsEncrypted(t *testing.T) {
	key, err := GenerateProjectKey(filepath.Join(t.TempDir(), "project.key.age"), "pw")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	ciphertext, err := key.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if !IsEncrypted(ciphertext) {
		t.Fatal("expected ciphertext to be detected as encrypted")
	}
	if IsEncrypted([]byte("plain text file content")) {
		t.Fatal("expected plain text to not be detected as encrypted")
	}
	if IsEncrypted(nil) {
		t.Fatal("expected empty data to not be detected as encrypted")
	}
}

func TestGenerateProjectKey_ExistingFileFails(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "project.key.age")
	if _, err := GenerateProjectKey(keyPath, "pw"); err != nil {
		t.Fatalf("first GenerateProjectKey: %v", err)
	}

	if _, err := GenerateProjectKey(keyPath, "pw"); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("expected ErrKeyExists, got %v", err)
	}
}

func TestUnlockProjectKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "project.key.age")
	original, err := GenerateProjectKey(keyPath, "correct horse battery staple")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}

	unlocked, err := UnlockProjectKey(keyPath, "correct horse battery staple")
	if err != nil {
		t.Fatalf("UnlockProjectKey: %v", err)
	}

	plaintext := []byte("round trip through wrapped key")
	ciphertext, err := original.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := unlocked.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt with unlocked key: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("mismatch: got %q, want %q", got, plaintext)
	}
}

func TestUnlockProjectKey_WrongPassphraseFails(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "project.key.age")
	if _, err := GenerateProjectKey(keyPath, "right passphrase"); err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}

	if _, err := UnlockProjectKey(keyPath, "wrong passphrase"); err == nil {
		t.Fatal("expected error unlocking with wrong passphrase, got nil")
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	key, err := GenerateProjectKey(filepath.Join(t.TempDir(), "project.key.age"), "pw")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	ciphertext, err := key.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := []byte(string(ciphertext))
	// Flip a byte in the middle of the armored body, past the header line.
	idx := bytes.IndexByte(tampered, '\n') + 20
	if idx >= len(tampered) {
		t.Fatalf("ciphertext too short to tamper: %d bytes", len(tampered))
	}
	tampered[idx] ^= 0xFF

	if _, err := key.Decrypt(tampered); err == nil {
		t.Fatal("expected error decrypting tampered ciphertext, got nil")
	}
}

func TestWriteFileAtomic_WritesContentAndPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := WriteFileAtomic(path, []byte("replaced"), 0o640); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "replaced" {
		t.Fatalf("content mismatch: got %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode mismatch: got %o, want %o", info.Mode().Perm(), 0o640)
	}
}

func TestWriteFileAtomic_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")

	if err := WriteFileAtomic(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "secret.txt" {
		t.Fatalf("expected only secret.txt in dir, got %v", entries)
	}
}

func TestProjectKeyPath(t *testing.T) {
	got := ProjectKeyPath("/home/user/myproject")
	want := filepath.Join("/home/user/myproject", ".ocode", "secret.key.age")
	if got != want {
		t.Fatalf("ProjectKeyPath: got %q, want %q", got, want)
	}
}

func TestSession_CachesKeyPerRoot(t *testing.T) {
	s := NewSession()

	if s.Get("/project/a") != nil {
		t.Fatal("expected no cached key before Set")
	}

	key, err := GenerateProjectKey(filepath.Join(t.TempDir(), "project.key.age"), "pw")
	if err != nil {
		t.Fatalf("GenerateProjectKey: %v", err)
	}
	s.Set("/project/a", key)

	if s.Get("/project/a") != key {
		t.Fatal("expected cached key to be returned for root")
	}
	if s.Get("/project/b") != nil {
		t.Fatal("expected no cached key for a different root")
	}

	s.Clear("/project/a")
	if s.Get("/project/a") != nil {
		t.Fatal("expected key to be cleared")
	}
}
