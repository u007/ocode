// Package encryption provides cross-platform AES-256-GCM envelope
// with Argon2id key derivation for connector settings and cache files.
// No secrets are written to disk; only encrypted metadata/cache.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const envelopeVersion = "ENCv1"

// Argon2id parameters (pinned; changing breaks decryption of old envelopes).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
)

// Envelope format: ENCv1:argon2:<salt_b64>:<nonce_b64>:<sealed_b64>
// sealed_b64 = AES-256-GCM(ciphertext || 16-byte auth tag) from Seal.

// DeriveKey produces a 32-byte AES-256 key from a user master password.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// EncryptJSON encrypts plaintext JSON settings using AES-256-GCM.
func EncryptJSON(plaintext []byte, password string) (string, error) {
	if len(password) == 0 {
		return "", fmt.Errorf("password must not be empty")
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	return fmt.Sprintf("%s:argon2:%s:%s:%s",
		envelopeVersion,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(sealed)), nil
}

// DecryptJSON decrypts an envelope produced by EncryptJSON.
func DecryptJSON(envelope string, password string) ([]byte, error) {
	if len(password) == 0 {
		return nil, fmt.Errorf("password must not be empty")
	}
	if !strings.HasPrefix(envelope, envelopeVersion+":") {
		return nil, fmt.Errorf("invalid envelope version (expected %s)", envelopeVersion)
	}
	parts := strings.Split(envelope[len(envelopeVersion)+1:], ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid envelope format: expected 4 parts (argon2 label, salt, nonce, sealed), got %d", len(parts))
	}
	if parts[0] != "argon2" {
		return nil, fmt.Errorf("invalid envelope label: expected argon2, got %s", parts[0])
	}
	salt, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	if len(salt) < 16 {
		return nil, fmt.Errorf("salt too short: %d bytes (min 16)", len(salt))
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	if len(nonce) != 12 {
		return nil, fmt.Errorf("invalid nonce length: %d bytes (expected 12)", len(nonce))
	}
	sealed, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("decode sealed data: %w", err)
	}
	if len(sealed) < 16 {
		return nil, fmt.Errorf("sealed data too short (%d bytes, min 16 for tag)", len(sealed))
	}
	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w (wrong password or corrupted data)", err)
	}
	return plaintext, nil
}
