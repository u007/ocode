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

// withConfirm replaces readConfirm with one that returns the given answers
// in order, and restores the original on cleanup.
func withConfirm(t *testing.T, answers ...bool) {
	t.Helper()
	orig := readConfirm
	i := 0
	readConfirm = func(prompt string) (bool, error) {
		if i >= len(answers) {
			t.Fatalf("readConfirm called more times than expected (%d)", len(answers))
		}
		v := answers[i]
		i++
		return v, nil
	}
	t.Cleanup(func() { readConfirm = orig })
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

	withPassphrases(t, "correct horse battery staple", "correct horse battery staple")
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

	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"encrypt", file}); err != nil {
		t.Fatalf("first encrypt: %v", err)
	}

	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"encrypt", file}); err == nil {
		t.Fatal("expected error encrypting an already-encrypted file")
	}
}

func TestRun_Encrypt_PassphraseRetypeMismatchFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	withPassphrases(t, "pw", "different")
	if err := Run([]string{"encrypt", file}); err == nil {
		t.Fatal("expected error when retyped passphrase does not match")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if secretfile.IsEncrypted(data) {
		t.Fatal("file must not be encrypted when the retyped passphrase mismatches")
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

func TestRun_EncryptDecrypt_Dir_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "sub", "b.txt")
	if err := os.MkdirAll(filepath.Dir(fileB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileA, []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("bravo"), 0o600); err != nil {
		t.Fatal(err)
	}

	withConfirm(t, true)
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"encrypt", dir}); err != nil {
		t.Fatalf("encrypt dir: %v", err)
	}
	for _, f := range []string{fileA, fileB} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !secretfile.IsEncrypted(data) {
			t.Fatalf("%s was not encrypted", f)
		}
	}

	withConfirm(t, true)
	withPassphrases(t, "pw")
	if err := Run([]string{"decrypt", dir}); err != nil {
		t.Fatalf("decrypt dir: %v", err)
	}
	gotA, err := os.ReadFile(fileA)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "alpha" {
		t.Fatalf("fileA mismatch: %q", gotA)
	}
	gotB, err := os.ReadFile(fileB)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotB) != "bravo" {
		t.Fatalf("fileB mismatch: %q", gotB)
	}
}

func TestRun_Encrypt_Dir_DeclinedConfirmLeavesFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "pw", "pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}

	withConfirm(t, false)
	if err := Run([]string{"encrypt", dir}); err != nil {
		t.Fatalf("expected declining the confirmation to succeed without error, got: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if secretfile.IsEncrypted(data) {
		t.Fatal("file must remain plaintext when confirmation is declined")
	}
}

func TestRun_Rekey_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "old-pw", "old-pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	file := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	withPassphrases(t, "old-pw", "old-pw")
	if err := Run([]string{"encrypt", file}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	withPassphrases(t, "old-pw", "new-pw", "new-pw")
	if err := Run([]string{"rekey", dir}); err != nil {
		t.Fatalf("rekey: %v", err)
	}

	withPassphrases(t, "new-pw")
	if err := Run([]string{"decrypt", file}); err != nil {
		t.Fatalf("decrypt with new passphrase: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("content mismatch after rekey round trip: %q", got)
	}
}

func TestRun_Rekey_WrongOldPassphraseFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "old-pw", "old-pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	withPassphrases(t, "wrong-pw", "new-pw", "new-pw")
	if err := Run([]string{"rekey", dir}); err == nil {
		t.Fatal("expected error rekeying with wrong old passphrase")
	}

	keyPath := secretfile.ProjectKeyPath(dir)
	if _, err := secretfile.UnlockProjectKey(keyPath, "old-pw"); err != nil {
		t.Fatalf("expected key to still unlock with original passphrase after failed rekey: %v", err)
	}
}

func TestRun_Rekey_NewPassphraseMismatchFails(t *testing.T) {
	dir := t.TempDir()
	withPassphrases(t, "old-pw", "old-pw")
	if err := Run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	withPassphrases(t, "old-pw", "new-pw", "different")
	if err := Run([]string{"rekey", dir}); err == nil {
		t.Fatal("expected error when new passphrase retype does not match")
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
