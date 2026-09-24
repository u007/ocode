package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, keyLen)
	aad := []byte("item-1")
	plaintext := []byte("correct horse battery staple")

	blob, err := seal(key, aad, plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := open(key, aad, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("open = %q, want %q", got, plaintext)
	}
}

func TestOpenWrongAADFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x22}, keyLen)
	blob, err := seal(key, []byte("item-1"), []byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := open(key, []byte("item-2"), blob); err == nil {
		t.Fatal("open with the wrong AAD succeeded, want failure")
	}
}

func TestOpenTamperedFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, keyLen)
	blob, err := seal(key, []byte("item-1"), []byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// Flip a bit in the ciphertext body (past the nonce).
	blob[len(blob)-1] ^= 0xff
	if _, err := open(key, []byte("item-1"), blob); err == nil {
		t.Fatal("open of a tampered blob succeeded, want failure")
	}
}

func TestWrapUnwrapKeyRoundTrip(t *testing.T) {
	kek := deriveKEK("master-password", bytes.Repeat([]byte{0x44}, saltLen))
	dk := make([]byte, keyLen)
	if _, err := rand.Read(dk); err != nil {
		t.Fatalf("rand: %v", err)
	}
	wrapped, err := wrapKey(kek, dk)
	if err != nil {
		t.Fatalf("wrapKey: %v", err)
	}
	got, err := unwrapKey(kek, wrapped)
	if err != nil {
		t.Fatalf("unwrapKey: %v", err)
	}
	if !bytes.Equal(got, dk) {
		t.Fatalf("unwrapKey = %x, want %x", got, dk)
	}
}

func TestUnwrapWrongKEKFails(t *testing.T) {
	salt := bytes.Repeat([]byte{0x55}, saltLen)
	kek1 := deriveKEK("master-one", salt)
	kek2 := deriveKEK("master-two", salt)
	dk := make([]byte, keyLen)
	if _, err := rand.Read(dk); err != nil {
		t.Fatalf("rand: %v", err)
	}
	wrapped, err := wrapKey(kek1, dk)
	if err != nil {
		t.Fatalf("wrapKey: %v", err)
	}
	_, err = unwrapKey(kek2, wrapped)
	if !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("unwrapKey with wrong KEK = %v, want ErrWrongMaster", err)
	}
}

func TestDeriveKEKDeterministic(t *testing.T) {
	salt := bytes.Repeat([]byte{0x66}, saltLen)
	a := deriveKEK("same-master", salt)
	b := deriveKEK("same-master", salt)
	c := deriveKEK("other-master", salt)

	if !bytes.Equal(a, b) {
		t.Fatal("deriveKEK is not deterministic for identical inputs")
	}
	if bytes.Equal(a, c) {
		t.Fatal("deriveKEK returned the same key for different masters")
	}
	if len(a) != keyLen {
		t.Fatalf("deriveKEK length = %d, want %d", len(a), keyLen)
	}
}
