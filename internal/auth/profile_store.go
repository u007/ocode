package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/u007/ocode/internal/paths"
)

var (
	profileStoreMu   sync.Mutex
	profileCache     map[string]map[string]Credential // profile -> provider -> cred
	profileLoaded    bool
	profileCacheInfo os.FileInfo // stat of auth.profiles.json the cache was read from
	profileLoadErr   errLogOnce
)

// ProfileCredentialVersion is the LEGACY name for CredentialVersion, kept so
// the profile-flavoured call sites still read. Both stores now share one
// counter (see CredentialVersion in store.go): a base-store edit — TUI
// /connect, or the web/desktop Connectors settings — must invalidate a cached
// LLM client exactly as a profile edit does, and one counter cannot be missed
// by a caller that reads only one of the two names.
//
// New code should call CredentialVersion; the version is no longer
// profile-specific.
func ProfileCredentialVersion() int64 {
	return CredentialVersion()
}

// SetProfileCredentialVersionForTest is the legacy name for
// SetCredentialVersionForTest. See ProfileCredentialVersion.
func SetProfileCredentialVersionForTest(v int64) {
	SetCredentialVersionForTest(v)
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

// readProfileFile reads auth.profiles.json as it is on disk now. An absent or
// empty file is an empty store. The stat comes before the read, as in
// readAuthFile. The parse error never quotes file content.
func readProfileFile(path string) (map[string]map[string]Credential, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[string]Credential), nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[string]Credential), nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string]map[string]Credential), info, nil
	}
	var raw map[string]map[string]Credential
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse %s: invalid JSON", path)
	}
	if raw == nil {
		raw = make(map[string]map[string]Credential)
	}
	return raw, info, nil
}

// equalProfileStores reports whether two profile maps hold the same credentials.
func equalProfileStores(a, b map[string]map[string]Credential) bool {
	if len(a) != len(b) {
		return false
	}
	for name, m := range a {
		other, ok := b[name]
		if !ok || !maps.Equal(m, other) {
			return false
		}
	}
	return true
}

// setProfileCacheLocked replaces the profile cache with state just read from,
// or written to, disk. Callers hold profileStoreMu.
func setProfileCacheLocked(m map[string]map[string]Credential, info os.FileInfo) {
	profileCache = m
	profileCacheInfo = info
	profileLoaded = true
}

// profileLoadFailed logs a failed load and returns err. Like loadFailed it keeps
// the last good cache and its stamp, so readers keep seeing it and the next call
// retries. Writers still refuse, since they return err from the reload.
func profileLoadFailed(err error) error {
	if err != nil {
		profileLoadErr.log("load profile credentials", err)
	}
	return err
}

// profileReadableLocked refreshes the profile cache and reports whether it holds
// a loaded state to read. As readableLocked does for the base store, a failed
// reload keeps the last good state, and only a never-loaded store reads nothing.
func profileReadableLocked() bool {
	_ = ensureProfileStoreLocked()
	return profileLoaded
}

// reloadProfileLocked reads the profile store from disk. A change against a
// loaded cache bumps the credential version. Callers hold profileStoreMu.
func reloadProfileLocked() error {
	path, err := ProfileAuthPath()
	if err != nil {
		return fmt.Errorf("resolve profile auth path: %w", err)
	}
	disk, info, err := readProfileFile(path)
	if err != nil {
		return err
	}
	changed := profileLoaded && !equalProfileStores(profileCache, disk)
	setProfileCacheLocked(disk, info)
	if changed {
		credentialVersion.Add(1)
	}
	return nil
}

// ensureProfileStoreLocked loads the profile store on first use and afterwards
// reloads it only when another process changed the file (one stat per call).
func ensureProfileStoreLocked() error {
	if !profileLoaded {
		return profileLoadFailed(reloadProfileLocked())
	}
	path, err := ProfileAuthPath()
	if err != nil {
		return profileLoadFailed(fmt.Errorf("resolve profile auth path: %w", err))
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return profileLoadFailed(fmt.Errorf("stat %s: %w", path, err))
	}
	if err != nil {
		info = nil
	}
	if sameAuthFile(info, profileCacheInfo) {
		return nil
	}
	return profileLoadFailed(reloadProfileLocked())
}

// mutateProfileStoreLocked is the profile store's load-modify-write. Under the
// file lock it re-reads the disk, applies mutate, and writes only when mutate
// reports a change. It fires the save hook and bumps the version after the
// write, never before it. Callers hold profileStoreMu.
func mutateProfileStoreLocked(mutate func(map[string]map[string]Credential) (bool, error)) error {
	path, err := ProfileAuthPath()
	if err != nil {
		return err
	}
	var disk map[string]map[string]Credential
	var info os.FileInfo
	wrote := false
	err = withAuthFileLock(path, func() error {
		cur, curInfo, rerr := readProfileFile(path)
		if rerr != nil {
			return rerr
		}
		changed, merr := mutate(cur)
		if merr != nil {
			return merr
		}
		if !changed {
			disk, info = cur, curInfo
			return nil
		}
		data, serr := json.MarshalIndent(cur, "", "  ")
		if serr != nil {
			return fmt.Errorf("marshal profile auth: %w", serr)
		}
		data = append(data, '\n')
		if werr := writeFileAtomic(path, data); werr != nil {
			return fmt.Errorf("write profile auth store: %w", werr)
		}
		stat, serr := os.Stat(path)
		if serr != nil {
			return fmt.Errorf("stat %s: %w", path, serr)
		}
		disk, info, wrote = cur, stat, true
		return nil
	})
	if err != nil {
		return err
	}
	adoptProfileStateLocked(disk, info, wrote)
	return nil
}

// adoptProfileStateLocked is adoptAuthStateLocked for the profile store: it bumps
// the version when this call wrote or when the re-read differs from the cached
// profile state, so a peer write absorbed by a no-op still invalidates clients.
// Caller holds profileStoreMu.
func adoptProfileStateLocked(disk map[string]map[string]Credential, info os.FileInfo, wrote bool) {
	changed := profileLoaded && !equalProfileStores(profileCache, disk)
	setProfileCacheLocked(disk, info)
	if wrote {
		OnCredentialsSaved.Fire()
	}
	if wrote || changed {
		credentialVersion.Add(1)
	}
}

// GetProfileCredential returns the credential for profile/provider if present.
func GetProfileCredential(profile, provider string) (Credential, bool) {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if !profileReadableLocked() {
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
	return mutateProfileStoreLocked(func(m map[string]map[string]Credential) (bool, error) {
		pm, ok := m[profile]
		if !ok {
			pm = make(map[string]Credential)
			m[profile] = pm
		} else if prev, had := pm[provider]; had && sameCredential(prev, cred) {
			return false, nil
		}
		pm[provider] = cred
		return true, nil
	})
}

// RemoveProfileCredential deletes a provider credential from a profile. A
// provider the profile does not hold writes nothing.
func RemoveProfileCredential(profile, provider string) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	return mutateProfileStoreLocked(func(m map[string]map[string]Credential) (bool, error) {
		pm, ok := m[profile]
		if !ok {
			return false, nil
		}
		if _, had := pm[provider]; !had {
			return false, nil
		}
		delete(pm, provider)
		if len(pm) == 0 {
			delete(m, profile)
		}
		return true, nil
	})
}

// ListProfileCredentials returns provider->cred map for a profile (copy).
func ListProfileCredentials(profile string) map[string]Credential {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if !profileReadableLocked() {
		return nil
	}
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
	names := []string{}
	if !profileReadableLocked() {
		return names
	}
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
	return mutateProfileStoreLocked(func(m map[string]map[string]Credential) (bool, error) {
		if _, ok := m[profile]; !ok {
			return false, nil
		}
		delete(m, profile)
		return true, nil
	})
}

// RenameProfileCredentials moves credentials from old to new profile name.
func RenameProfileCredentials(oldName, newName string) error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	return mutateProfileStoreLocked(func(m map[string]map[string]Credential) (bool, error) {
		pm, ok := m[oldName]
		if !ok {
			return false, fmt.Errorf("profile %q not found in auth sidecar", oldName)
		}
		if _, exists := m[newName]; exists {
			return false, fmt.Errorf("profile %q already exists in auth sidecar", newName)
		}
		m[newName] = pm
		delete(m, oldName)
		return true, nil
	})
}

// ProfileCredentialCount returns number of provider credentials for a profile.
func ProfileCredentialCount(profile string) int {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	if !profileReadableLocked() {
		return 0
	}
	return len(profileCache[profile])
}

// ReloadProfileStore forces reload from disk (useful for tests). A change in
// content bumps the credential version, as any other reload does.
func ReloadProfileStore() error {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	return profileLoadFailed(reloadProfileLocked())
}
