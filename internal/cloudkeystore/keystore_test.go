package cloudkeystore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/keychain"
)

// memStore is a stand-in for a native OS keychain in tests.
type memStore struct {
	vals map[string]string
}

func (m *memStore) Get(service, account string) (string, error) {
	if v, ok := m.vals[service+"\x00"+account]; ok {
		return v, nil
	}
	return "", keychain.ErrNotFound
}

func (m *memStore) Set(service, account, secret string) error {
	m.vals[service+"\x00"+account] = secret
	return nil
}

func (m *memStore) Delete(service, account string) error {
	delete(m.vals, service+"\x00"+account)
	return nil
}

// stubResolve saves the injectable vars and restores them when the test ends.
func stubResolve(t *testing.T, native bool, store keychain.Store, pathFn func() (string, error)) {
	t.Helper()
	oldNative, oldOpen, oldPath := nativeAvailable, openNativeStore, keyFilePath
	t.Cleanup(func() {
		nativeAvailable, openNativeStore, keyFilePath = oldNative, oldOpen, oldPath
	})
	nativeAvailable = func() bool { return native }
	openNativeStore = func() keychain.Store { return store }
	if pathFn != nil {
		keyFilePath = pathFn
	}
}

func TestResolveNeverUsesMachineIDFallback(t *testing.T) {
	dir := t.TempDir()
	// An ancestor that is a regular file makes MkdirAll impossible: no key file
	// can be created, so with no native store Resolve must stay memory-only.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	stubResolve(t, false, nil, func() (string, error) {
		return filepath.Join(blocker, "cloud-connectors", "key"), nil
	})

	key, persist, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if persist {
		t.Fatal("persist = true with no native store and an unwritable key path")
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	// A memory-only key is fresh per call, never a deterministic machineID key.
	key2, persist2, err := Resolve()
	if err != nil || persist2 {
		t.Fatalf("second Resolve: persist=%v err=%v", persist2, err)
	}
	if string(key) == string(key2) {
		t.Fatal("memory-only key is reused across calls")
	}
}

func TestResolveUsesKeyFileWhenNoNativeStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cloud-connectors", "key")
	stubResolve(t, false, nil, func() (string, error) { return p, nil })

	key, persist, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !persist || len(key) != 32 {
		t.Fatalf("persist=%v key length=%d", persist, len(key))
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("key file mode = %o, want 0600", got)
	}
	// A second Resolve reuses the persisted key.
	key2, persist2, err := Resolve()
	if err != nil || !persist2 || string(key) != string(key2) {
		t.Fatalf("key file not reused: persist=%v err=%v", persist2, err)
	}
}

func TestResolveUsesNativeStoreWhenAvailable(t *testing.T) {
	store := &memStore{vals: make(map[string]string)}
	stubResolve(t, true, store, nil)

	key, persist, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !persist || len(key) != 32 {
		t.Fatalf("persist=%v key length=%d", persist, len(key))
	}
	key2, persist2, err := Resolve()
	if err != nil || !persist2 || string(key) != string(key2) {
		t.Fatalf("native key not reused: persist=%v err=%v", persist2, err)
	}
}

func TestDefaultKeyFilePathIsOutsideProjectOcode(t *testing.T) {
	p, err := defaultKeyFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("cloud-connectors", "key")) {
		t.Fatalf("unexpected key path %q", p)
	}
	if strings.Contains(p, ".ocode") {
		t.Fatalf("key path must not live inside a project .ocode directory: %q", p)
	}
}
