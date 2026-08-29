package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/paths"
)

var (
	profileStoreMu sync.Mutex
	profileCache   map[string]map[string]Credential // profile -> provider -> cred
	profileLoaded  bool
	profileVersion atomic.Int64
)

// ProfileCredentialVersion returns a counter bumped on every profile
// credential mutation (set/remove/delete/rename). Callers that cache
// something built from profile credentials (e.g. an LLM client keyed by
// profile name) can snapshot this value at build time and compare it later
// to detect an in-place credential edit that a same-name profile comparison
// would otherwise miss.
func ProfileCredentialVersion() int64 {
	return profileVersion.Load()
}

// ProfileAuthPath returns the ocode-only sidecar path for per-profile credentials:
//
//	<OcodeGlobalDataDir>/auth.profiles.json  (0600)
func ProfileAuthPath() (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "auth.profiles.json"), nil
}

func loadProfileStoreLocked() error {
	path, err := ProfileAuthPath()
	if err != nil {
		return fmt.Errorf("resolve profile auth path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create profile auth dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			profileCache = make(map[string]map[string]Credential)
			profileLoaded = true
			return nil
		}
		return fmt.Errorf("read profile auth store: %w", err)
	}
	if len(data) == 0 {
		profileCache = make(map[string]map[string]Credential)
		profileLoaded = true
		return nil
	}
	var raw map[string]map[string]Credential
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse profile auth store: %w", err)
	}
	if raw == nil {
		raw = make(map[string]map[string]Credential)
	}
	profileCache = raw
	profileLoaded = true
	return nil
}

func ensureProfileStoreLocked() error {
	if profileLoaded {
		return nil
	}
	return loadProfileStoreLocked()
}

func writeProfileStoreLocked() error {
	path, err := ProfileAuthPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profileCache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write profile auth tmp: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		// best effort
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename profile auth: %w", err)
	}
	return nil
}

// GetProfileCredential returns the credential for profile/provider if present.
func GetProfileCredential(profile, provider string) (Credential, bool) {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if err := ensureProfileStoreLocked(); err != nil {
		return Credential{}, false
	}
	m, ok := profileCache[profile]
	if !ok {
		return Credential{}, false
	}
	c, ok := m[provider]
	return c, ok
}

// SetProfileCredential upserts a credential for profile/provider.
func SetProfileCredential(profile, provider string, cred Credential) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if err := ensureProfileStoreLocked(); err != nil {
		return err
	}
	if profileCache == nil {
		profileCache = make(map[string]map[string]Credential)
	}
	m, ok := profileCache[profile]
	if !ok {
		m = make(map[string]Credential)
		profileCache[profile] = m
	}
	m[provider] = cred
	profileVersion.Add(1)
	return writeProfileStoreLocked()
}

// RemoveProfileCredential deletes a provider credential from a profile.
func RemoveProfileCredential(profile, provider string) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if err := ensureProfileStoreLocked(); err != nil {
		return err
	}
	m, ok := profileCache[profile]
	if !ok {
		return nil
	}
	delete(m, provider)
	if len(m) == 0 {
		delete(profileCache, profile)
	}
	profileVersion.Add(1)
	return writeProfileStoreLocked()
}

// ListProfileCredentials returns provider->cred map for a profile (copy).
func ListProfileCredentials(profile string) map[string]Credential {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	_ = ensureProfileStoreLocked()
	m, ok := profileCache[profile]
	if !ok {
		return nil
	}
	out := make(map[string]Credential, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ListProfiles returns sorted profile names that have at least one credential.
func ListProfileNames() []string {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	_ = ensureProfileStoreLocked()
	names := make([]string, 0, len(profileCache))
	for n := range profileCache {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// DeleteProfileCredentials removes all credentials for a profile.
func DeleteProfileCredentials(profile string) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if err := ensureProfileStoreLocked(); err != nil {
		return err
	}
	delete(profileCache, profile)
	profileVersion.Add(1)
	return writeProfileStoreLocked()
}

// RenameProfileCredentials moves credentials from old to new profile name.
func RenameProfileCredentials(oldName, newName string) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if err := ensureProfileStoreLocked(); err != nil {
		return err
	}
	m, ok := profileCache[oldName]
	if !ok {
		return fmt.Errorf("profile %q not found in auth sidecar", oldName)
	}
	if _, exists := profileCache[newName]; exists {
		return fmt.Errorf("profile %q already exists in auth sidecar", newName)
	}
	profileCache[newName] = m
	delete(profileCache, oldName)
	profileVersion.Add(1)
	return writeProfileStoreLocked()
}

// ProfileCredentialCount returns number of provider credentials for a profile.
func ProfileCredentialCount(profile string) int {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	_ = ensureProfileStoreLocked()
	return len(profileCache[profile])
}

// ReloadProfileStore forces reload from disk (useful for tests).
func ReloadProfileStore() error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	profileLoaded = false
	return loadProfileStoreLocked()
}

// lockProfileAuth acquires a file lock similar to ocodeconfig lock.
func lockProfileAuth() (func(), error) {
	path, err := ProfileAuthPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Second {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}
