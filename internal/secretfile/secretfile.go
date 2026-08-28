// Package secretfile encrypts and decrypts files with a per-project age
// key, so a file on disk stays ciphertext everywhere except the moment a
// human is actively editing it. Each project has one generated X25519 key,
// itself wrapped under a user passphrase (scrypt) and stored alongside the
// project; unlocking the wrapped key costs one scrypt run per session, while
// per-file Encrypt/Decrypt use the X25519 key directly (ChaCha20-Poly1305,
// no KDF per file).
package secretfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// Header is the ASCII armor prefix that identifies age-encrypted content.
// Any file starting with this line was produced by this package (or by the
// age CLI) and must not be treated as plaintext.
const Header = armor.Header

// ErrKeyExists is returned by GenerateProjectKey when keyPath already holds
// a wrapped key.
var ErrKeyExists = errors.New("secretfile: project key already exists")

// ProjectKeyPath returns the conventional location of a project's wrapped
// key file given its root directory.
func ProjectKeyPath(root string) string {
	return filepath.Join(root, ".ocode", "secret.key.age")
}

// IsEncrypted reports whether data is age-armored ciphertext.
func IsEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte(Header))
}

// ProjectKey encrypts and decrypts files for one project.
type ProjectKey struct {
	identity *age.X25519Identity
}

// GenerateProjectKey creates a new project key, wraps it under passphrase,
// and writes the armored result to keyPath. It fails with ErrKeyExists if
// keyPath already exists, so an existing key is never silently overwritten.
func GenerateProjectKey(keyPath, passphrase string) (*ProjectKey, error) {
	if _, err := os.Stat(keyPath); err == nil {
		return nil, ErrKeyExists
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("secretfile: stat key path: %w", err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("secretfile: generate key: %w", err)
	}

	wrapped, err := wrapIdentity(identity, passphrase)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("secretfile: create key directory: %w", err)
	}
	if err := os.WriteFile(keyPath, wrapped, 0o600); err != nil {
		return nil, fmt.Errorf("secretfile: write key: %w", err)
	}

	return &ProjectKey{identity: identity}, nil
}

// UnlockProjectKey reads the wrapped key at keyPath and decrypts it with
// passphrase.
func UnlockProjectKey(keyPath, passphrase string) (*ProjectKey, error) {
	wrapped, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("secretfile: read key: %w", err)
	}

	scryptIdentity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("secretfile: passphrase: %w", err)
	}

	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(wrapped)), scryptIdentity)
	if err != nil {
		return nil, fmt.Errorf("secretfile: wrong passphrase or corrupt key file: %w", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secretfile: wrong passphrase or corrupt key file: %w", err)
	}

	identity, err := age.ParseX25519Identity(string(raw))
	if err != nil {
		return nil, fmt.Errorf("secretfile: corrupt key file: %w", err)
	}

	return &ProjectKey{identity: identity}, nil
}

func wrapIdentity(identity *age.X25519Identity, passphrase string) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("secretfile: passphrase: %w", err)
	}

	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, recipient)
	if err != nil {
		return nil, fmt.Errorf("secretfile: wrap key: %w", err)
	}
	if _, err := io.WriteString(w, identity.String()); err != nil {
		return nil, fmt.Errorf("secretfile: wrap key: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("secretfile: wrap key: %w", err)
	}
	if err := aw.Close(); err != nil {
		return nil, fmt.Errorf("secretfile: wrap key: %w", err)
	}
	return buf.Bytes(), nil
}

// Encrypt returns the armored age ciphertext for plaintext.
func (k *ProjectKey) Encrypt(plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, k.identity.Recipient())
	if err != nil {
		return nil, fmt.Errorf("secretfile: encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("secretfile: encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("secretfile: encrypt: %w", err)
	}
	if err := aw.Close(); err != nil {
		return nil, fmt.Errorf("secretfile: encrypt: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt returns the plaintext for armored age ciphertext produced by
// Encrypt.
func (k *ProjectKey) Decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(ciphertext)), k.identity)
	if err != nil {
		return nil, fmt.Errorf("secretfile: decrypt: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secretfile: decrypt: %w", err)
	}
	return plaintext, nil
}

// WriteFileAtomic writes data to path atomically: it writes to a sibling
// temp file, fsyncs, then renames over path with the given mode. A crash or
// editor failure mid-write can therefore never truncate or corrupt path.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".secretfile-*")
	if err != nil {
		return fmt.Errorf("secretfile: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("secretfile: write temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("secretfile: sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("secretfile: close temp file: %w", err)
	}
	if err = os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("secretfile: chmod temp file: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("secretfile: rename temp file: %w", err)
	}
	return nil
}

// Session caches unlocked project keys in memory for the process lifetime,
// keyed by project root, so a passphrase is only requested once per project
// per run.
type Session struct {
	mu   sync.Mutex
	keys map[string]*ProjectKey
}

// NewSession returns an empty Session.
func NewSession() *Session {
	return &Session{keys: make(map[string]*ProjectKey)}
}

// Get returns the cached key for root, or nil if it has not been unlocked
// yet this session.
func (s *Session) Get(root string) *ProjectKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[root]
}

// Set caches key for root.
func (s *Session) Set(root string, key *ProjectKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[root] = key
}

// Clear removes the cached key for root, e.g. after the user locks the
// project or the file rejects a decrypt.
func (s *Session) Clear(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, root)
}
