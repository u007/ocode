// Package vault implements ocode's password vault: an encrypted file whose
// contents are protected by a master password.
//
// The master password is never stored. Instead a random 32-byte data key (DK)
// is generated once and wrapped (encrypted) under an Argon2id-derived
// key-encryption key (KEK); every vault item is sealed whole with its own id
// as AES-GCM additional authenticated data. Unlocking re-derives the KEK from
// the supplied master, unwraps the DK, and decrypts the items in memory.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters and envelope constants. These are persisted in the vault
// file's kdf block so a future change can stay backward compatible.
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024 // KiB
	kdfThreads = 4
	keyLen     = 32
	saltLen    = 16
	keyWrapAAD = "ocode-vault-key"
)

// Sentinel errors shared by the vault package. Callers (the server handlers)
// map these onto HTTP status codes.
var (
	ErrLocked      = errors.New("vault is locked")
	ErrWrongMaster = errors.New("invalid master password")
	ErrNoVault     = errors.New("vault does not exist")
	ErrExists      = errors.New("vault already exists")
	ErrNotFound    = errors.New("vault item not found")
	ErrCorrupt     = errors.New("vault data is corrupt")
)

// deriveKEK stretches a master password into a 32-byte key-encryption key with
// Argon2id. The same master + salt always yields the same KEK.
func deriveKEK(master string, salt []byte) []byte {
	return argon2.IDKey([]byte(master), salt, kdfTime, kdfMemory, kdfThreads, keyLen)
}

// seal encrypts plaintext with AES-256-GCM under key, authenticating aad as
// additional data. The returned blob is nonce || ciphertext+tag.
func seal(key, aad, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("vault: read nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// open is the inverse of seal. A blob shorter than the GCM nonce is rejected
// as ErrCorrupt before touching the cipher.
func open(key, aad, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, ErrCorrupt
	}
	nonce, ciphertext := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("vault: open: %w", err)
	}
	return plaintext, nil
}

// wrapKey seals the data key under the KEK and returns it base64-encoded.
func wrapKey(kek, dk []byte) (string, error) {
	blob, err := seal(kek, []byte(keyWrapAAD), dk)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

// unwrapKey is the inverse of wrapKey. Any decode or authentication failure is
// collapsed to ErrWrongMaster so a caller cannot distinguish a bad password
// from a corrupted wrap — there is no oracle beyond success or failure. A
// decoded DK of the wrong length is likewise rejected.
func unwrapKey(kek []byte, wrapped string) ([]byte, error) {
	blob, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, ErrWrongMaster
	}
	dk, err := open(kek, []byte(keyWrapAAD), blob)
	if err != nil || len(dk) != keyLen {
		return nil, ErrWrongMaster
	}
	return dk, nil
}

// newGCM builds an AES-256-GCM AEAD for a 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: new gcm: %w", err)
	}
	return gcm, nil
}
