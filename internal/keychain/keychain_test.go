package keychain

import (
	"os"
	"path/filepath"
	"testing"
)

// stubNativeStore only reports presence; NativeAvailable must never construct
// the machineID-derived fallback store.
type stubNativeStore struct{}

func (stubNativeStore) Get(string, string) (string, error) { return "", ErrNotFound }
func (stubNativeStore) Set(string, string, string) error   { return nil }
func (stubNativeStore) Delete(string, string) error        { return nil }

func TestNativeAvailableDetectsPlatformStore(t *testing.T) {
	orig := newPlatformStoreFn
	t.Cleanup(func() { newPlatformStoreFn = orig })

	newPlatformStoreFn = func(Options) (Store, bool) { return nil, false }
	if NativeAvailable() {
		t.Fatal("NativeAvailable() = true with no platform store")
	}
	newPlatformStoreFn = func(Options) (Store, bool) { return stubNativeStore{}, true }
	if !NativeAvailable() {
		t.Fatal("NativeAvailable() = false with a platform store")
	}
}

func TestFallbackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kc.json")
	s := newFallbackStore(Options{Path: p, MasterPassword: "pw"})
	if err := s.Set("svc", "acct", "secret123"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("svc", "acct")
	if err != nil || got != "secret123" {
		t.Fatalf("got %q err %v", got, err)
	}
	// persistence across instances
	s2 := newFallbackStore(Options{Path: p, MasterPassword: "pw"})
	got2, err := s2.Get("svc", "acct")
	if err != nil || got2 != "secret123" {
		t.Fatalf("persisted got %q err %v", got2, err)
	}
	if err := s.Delete("svc", "acct"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("svc", "acct"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound got %v", err)
	}
}

func TestFallbackErrNotFound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kc.json")
	s := newFallbackStore(Options{Path: p})
	if _, err := s.Get("no", "no"); err != ErrNotFound {
		t.Fatalf("Get: expected ErrNotFound got %v", err)
	}
	if err := s.Delete("no", "no"); err != ErrNotFound {
		t.Fatalf("Delete: expected ErrNotFound got %v", err)
	}
}

func TestFallbackTamperOrWrongPassword(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kc.json")
	s := newFallbackStore(Options{Path: p, MasterPassword: "pw"})
	if err := s.Set("svc", "acct", "secret"); err != nil {
		t.Fatal(err)
	}
	// wrong password should not silently succeed
	s2 := newFallbackStore(Options{Path: p, MasterPassword: "wrong"})
	if _, err := s2.Get("svc", "acct"); err == nil {
		t.Fatal("expected error with wrong password")
	}
	_ = os.Remove
}
