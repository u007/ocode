// Package cloudkeystore resolves the install-scoped raw key used to seal
// central cloud-connector credentials. The key never comes from a guessable
// machine identifier and is never written inside a project directory.
package cloudkeystore

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/keychain"
	"github.com/u007/ocode/internal/paths"
)

const (
	keyLen        = 32
	nativeService = "ocode-cloud-keystore"
	nativeAccount = "key"
)

// Injectable so tests can force native-unavailable, unwritable-home and
// native-store paths without depending on the host's installed binaries.
var (
	nativeAvailable = keychain.NativeAvailable
	openNativeStore = func() keychain.Store { return keychain.New() }
	keyFilePath     = defaultKeyFilePath
)

func defaultKeyFilePath() (string, error) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("cloudkeystore: global data dir: %w", err)
	}
	return filepath.Join(dir, "cloud-connectors", "key"), nil
}

// Resolve returns a raw 32-byte key and whether it was persisted. A native OS
// keychain is preferred, then a 0600 file under the global data dir. If neither
// is usable the returned key is random and memory-only (persist=false).
func Resolve() (key []byte, persist bool, err error) {
	if nativeAvailable() {
		if k, ok := resolveNative(); ok {
			return k, true, nil
		}
	}
	if k, ok := resolveFile(); ok {
		return k, true, nil
	}
	k := make([]byte, keyLen)
	if _, err := rand.Read(k); err != nil {
		return nil, false, fmt.Errorf("cloudkeystore: generate memory key: %w", err)
	}
	return k, false, nil
}

// resolveNative uses keychain.New only after NativeAvailable confirmed a native
// store exists, so its machineID-derived fallback can never be selected.
func resolveNative() ([]byte, bool) {
	store := openNativeStore()
	if store == nil {
		return nil, false
	}
	encoded, err := store.Get(nativeService, nativeAccount)
	if err == nil {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && len(raw) == keyLen {
			return raw, true
		}
	} else if err != keychain.ErrNotFound {
		return nil, false
	}
	raw := make([]byte, keyLen)
	if _, err := rand.Read(raw); err != nil {
		return nil, false
	}
	if err := store.Set(nativeService, nativeAccount, base64.StdEncoding.EncodeToString(raw)); err != nil {
		return nil, false
	}
	return raw, true
}

func resolveFile() ([]byte, bool) {
	path, err := keyFilePath()
	if err != nil || path == "" {
		return nil, false
	}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != keyLen {
			return nil, false // corrupt key file; do not use it
		}
		return data, true
	}
	raw := make([]byte, keyLen)
	if _, err := rand.Read(raw); err != nil {
		return nil, false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, false
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		return nil, false
	}
	return raw, true
}
