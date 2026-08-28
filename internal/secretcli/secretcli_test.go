package secretcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/secretfile"
)

// withPassphrases replaces readPassphrase with one that returns the given
// values in order, and restores the original on cleanup.
func withPassphrases(t *testing.T, values ...string) {
	t.Helper()
	orig := readPassphrase
	i := 0
	readPassphrase = func(prompt string) (string, error) {
		if i >= len(values) {
			t.Fatalf("readPassphrase called more times than expected (%d)", len(values))
		}
		v := values[i]
		i++
		return v, nil
	}
	t.Cleanup(func() { readPassphrase = orig })
}

func TestRun_Init_CreatesKeyFile(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "correct horse battery staple", "correct horse battery staple")

	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	keyPath := secretfile.ProjectKeyPath(dir)
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected key file at %s: %v", keyPath, err)
	}
}

func TestRun_Init_PassphraseMismatchFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw1", "pw2")

	if err := Run([]string{"init", dir}); err == nil {
		t.Fatal("expected error on passphrase mismatch")
	}

	if _, err := os.Stat(secretfile.ProjectKeyPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("expected no key file to be created, stat err = %v", err)
	}
}

func TestRun_Init_ExistingKeyFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	withPassphrases(t, "pw2", "pw2")
	if err := Run([]string{"init", dir}); err == nil {
		t.Fatal("expected error initializing an already-initialized project")
	}
}

func TestRun_EncryptDecrypt_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "correct horse battery staple", "correct horse battery staple")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "secret.txt")
	plaintext := "top secret configuration\n"
	if err := os.WriteFile(file, []byte(plaintext), 0o640); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	withPassphrases(t, "correct horse battery staple")
	if err := Run([]string{"encrypt", file}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	encrypted, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if !secretfile.IsEncrypted(encrypted) {
		t.Fatal("expected file to be encrypted after Run encrypt")
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed: got %o, want %o", info.Mode().Perm(), 0o640)
	}

	withPassphrases(t, "correct horse battery staple")
	if err := Run([]string{"decrypt", file}); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read decrypted file: %v", err)
	}
	if string(got) != plaintext {
		t.Fatalf("content mismatch after round trip: got %q, want %q", got, plaintext)
	}
}

func TestRun_Encrypt_AlreadyEncryptedFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	withPassphrases(t, "pw")
	if err := Run([]string{"encrypt", file}); err != nil {
		t.Fatalf("first encrypt: %v", err)
	}

	withPassphrases(t, "pw")
	if err := Run([]string{"encrypt", file}); err == nil {
		t.Fatal("expected error encrypting an already-encrypted file")
	}
}

func TestRun_Encrypt_MissingKeyFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := Run([]string{"encrypt", file})
	if err == nil {
		t.Fatal("expected error when no project key exists")
	}
	if !strings.Contains(err.Error(), "secret init") {
		t.Fatalf("expected error to mention 'secret init', got: %v", err)
	}
}

func TestRun_Decrypt_NotEncryptedFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(file, []byte("plain data"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	withPassphrases(t, "pw")
	if err := Run([]string{"decrypt", file}); err == nil {
		t.Fatal("expected error decrypting a file that isn't encrypted")
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	if err := Run([]string{"nope"}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRun_NoArgs(t *testing.T) {
	if err := Run(nil); err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}
