package encryption

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// rawTestKey is a 32-byte raw AES key for the ENCv2:raw envelope (no KDF).
var rawTestKey = []byte("0123456789abcdef0123456789abcdef")

func TestSealWithKeyRoundTrip(t *testing.T) {
	sealed, err := SealWithKey([]byte("secret"), rawTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, "ENCv2:raw:") {
		t.Fatalf("bad envelope prefix: %q", sealed)
	}
	got, err := OpenWithKey(sealed, rawTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Fatalf("got %q, want secret", got)
	}
	// A fresh nonce must be used for each seal.
	sealed2, err := SealWithKey([]byte("secret"), rawTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if sealed2 == sealed {
		t.Fatal("nonce reused across seals")
	}
}

func TestOpenWithKeyRejectsBadEnvelopes(t *testing.T) {
	sealed, err := SealWithKey([]byte("secret"), rawTestKey)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sealed, ":")
	if len(parts) != 4 {
		t.Fatalf("unexpected envelope parts: %d", len(parts))
	}
	// Drop the sealed-data field: truncated envelope must be rejected.
	if _, err := OpenWithKey(strings.Join(parts[:3], ":"), rawTestKey); err == nil {
		t.Fatal("truncated envelope accepted")
	}
	// Wrong version label must be rejected.
	if _, err := OpenWithKey("ENCv1:raw:"+parts[2]+":"+parts[3], rawTestKey); err == nil {
		t.Fatal("non-ENCv2 envelope accepted")
	}
	// Wrong key must fail authentication.
	wrong := bytes.Repeat([]byte{0x01}, 32)
	if _, err := OpenWithKey(sealed, wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestSealWithKeyRejectsInvalidKeyLength(t *testing.T) {
	if _, err := SealWithKey([]byte("x"), []byte("too-short")); err == nil {
		t.Fatal("short raw key accepted")
	}
	if _, err := OpenWithKey("ENCv2:raw:AAAA:BBBB", []byte("too-short")); err == nil {
		t.Fatal("short raw key accepted on open")
	}
}

// TestENCv1StillDecrypts pins a fixed ENCv1 vector so the password+Argon2id
// reader remains compatible after the raw-key ENCv2 addition.
func TestENCv1StillDecrypts(t *testing.T) {
	const vector = "ENCv1:argon2:Zml4ZWQtc2FsdC12MS12ZWN0b3Ih:Zml4ZWRub25jZTEy:JBnPwz6X82j6A6VVThOTE9ZiLJ6ZYo/BkJxhthB+zUu+TBs2jdA2dgRCqS87bsbEJ5HZ3A=="
	got, err := DecryptJSON(vector, "vector-password")
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"provider":"dropbox","token":"abc"}`; string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte(`{"provider":"google-drive","root":"/docs","project":"test"}`)
	encrypted, err := EncryptJSON(plaintext, "test-master-password")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted string is empty")
	}
	decrypted, err := DecryptJSON(encrypted, "test-master-password")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch: got %s, want %s", string(decrypted), string(plaintext))
	}
}

func TestDecryptWrongPasswordFails(t *testing.T) {
	plaintext := []byte(`{"provider":"s3","bucket":"my-bucket"}`)
	encrypted, err := EncryptJSON(plaintext, "correct-password")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	_, err = DecryptJSON(encrypted, "wrong-password")
	if err == nil {
		t.Fatal("expected decryption to fail with wrong password, but it succeeded")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	plaintext := []byte(`secret config`)
	enc, err := EncryptJSON(plaintext, "pw")
	if err != nil {
		t.Fatal(err)
	}
	// Mutate ciphertext portion (between nonce and tag segments by splitting envelope)
	parts := strings.Split(enc, ":")
	if len(parts) != 5 {
		t.Fatalf("unexpected envelope parts: %d", len(parts))
	}
	ct, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatal(err)
	}
	ct[0] ^= 0xFF // flip bits
	parts[3] = base64.StdEncoding.EncodeToString(ct)
	tampered := strings.Join(parts, ":")
	_, err = DecryptJSON(tampered, "pw")
	if err == nil {
		t.Fatal("expected decryption failure on tampered ciphertext")
	}
}

func TestTruncatedEnvelopeFails(t *testing.T) {
	_, err := DecryptJSON("ENCv1:incomplete", "pw")
	if err == nil {
		t.Fatal("expected error for truncated envelope")
	}
}

func TestWrongVersionRejected(t *testing.T) {
	_, err := DecryptJSON("BADv1:argon2:xyz:xyz:xyz:xyz", "pw")
	if err == nil {
		t.Fatal("expected error for wrong version prefix")
	}
}

func TestSamePlaintextDifferentCiphertext(t *testing.T) {
	plaintext := []byte(`same`)
	a, _ := EncryptJSON(plaintext, "pw")
	b, _ := EncryptJSON(plaintext, "pw")
	if a == b {
		t.Fatal("two encryptions of same plaintext must differ (random salt/nonce)")
	}
}

func TestEmptyAndNil(t *testing.T) {
	_, err := EncryptJSON([]byte{}, "pw")
	if err != nil {
		t.Fatalf("empty plaintext encrypt failed: %v", err)
	}
	enc, err := EncryptJSON([]byte{1, 2}, "pw")
	if err != nil {
		t.Fatalf("small plaintext encrypt failed: %v", err)
	}
	decrypted, err := DecryptJSON(enc, "pw")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != string([]byte{1, 2}) {
		t.Errorf("small plaintext mismatch")
	}
}

func TestBadNonceLengthFails(t *testing.T) {
	enc, err := EncryptJSON([]byte("x"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(enc, ":")
	if len(parts) != 5 {
		t.Fatalf("unexpected envelope parts: %d", len(parts))
	}
	// Replace nonce (index 3: ENCv1, argon2, salt, nonce, sealed) with shorter value
	parts[3] = base64.StdEncoding.EncodeToString([]byte("short"))
	tampered := strings.Join(parts, ":")
	_, err = DecryptJSON(tampered, "pw")
	if err == nil {
		t.Fatal("expected failure for bad nonce length")
	}
}

func TestGarbledBase64Fails(t *testing.T) {
	enc, err := EncryptJSON([]byte("x"), "pw")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(enc, ":")
	parts[1] = "!!!not_base64!!!"
	tampered := strings.Join(parts, ":")
	_, err = DecryptJSON(tampered, "pw")
	if err == nil {
		t.Fatal("expected failure for garbled base64")
	}
}
