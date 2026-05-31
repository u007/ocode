package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// CredentialKind distinguishes how a credential was obtained.
type CredentialKind string

const (
	KindAPIKey CredentialKind = "api" // matches opencode's "type": "api" convention
	KindOAuth  CredentialKind = "oauth"
)

// Credential is a single saved provider credential.
//
// JSON tags match the opencode auth.json format so both tools can share
// the same file at ~/.local/share/opencode/auth.json.
type Credential struct {
	Kind         CredentialKind `json:"type"`                   // "api_key" or "oauth" — serialised as "type" for opencode compat
	Key          string         `json:"key,omitempty"`          // API key
	AccessToken  string         `json:"access_token,omitempty"` // OAuth
	RefreshToken string         `json:"refresh_token,omitempty"`
	ExpiresAt    int64          `json:"expires_at,omitempty"` // unix seconds
	Account      string         `json:"account,omitempty"`
	BaseURL      string         `json:"baseURL,omitempty"`    // endpoint override
	AccountID    string         `json:"account_id,omitempty"` // Cloudflare
}

type credentialJSON struct {
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	Key          string `json:"key,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	Account      string `json:"account,omitempty"`
	BaseURL      string `json:"baseURL,omitempty"`
	BaseURLOld   string `json:"base_url,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

func normalizeCredentialKind(kind string) CredentialKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", string(KindAPIKey), "api_key":
		return KindAPIKey
	case string(KindOAuth):
		return KindOAuth
	default:
		return CredentialKind(kind)
	}
}

func (c *Credential) UnmarshalJSON(data []byte) error {
	var raw credentialJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kind := raw.Type
	if kind == "" {
		kind = raw.Kind
	}
	*c = Credential{
		Kind:         normalizeCredentialKind(kind),
		Key:          raw.Key,
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ExpiresAt:    raw.ExpiresAt,
		Account:      raw.Account,
		BaseURL:      raw.BaseURL,
		AccountID:    raw.AccountID,
	}
	if c.BaseURL == "" {
		c.BaseURL = raw.BaseURLOld
	}
	return nil
}

func (c Credential) MarshalJSON() ([]byte, error) {
	return json.Marshal(credentialJSON{
		Type:         string(normalizeCredentialKind(string(c.Kind))),
		Key:          c.Key,
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		ExpiresAt:    c.ExpiresAt,
		Account:      c.Account,
		BaseURL:      c.BaseURL,
		AccountID:    c.AccountID,
	})
}

var (
	storeMu     sync.Mutex
	cache       map[string]Credential // provider → Credential
	cacheLoaded bool                  // true after a successful loadStoreLocked
)

// authPath returns the opencode-compatible credentials path:
//
//	Linux / macOS: ~/.local/share/opencode/auth.json
//	Windows:       %LOCALAPPDATA%\opencode\auth.json
func authPath() (string, error) {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(localAppData, "opencode", "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json"), nil
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json"), nil
}

// legacyAuthPath returns the old ocode auth path at ~/.config/ocode/auth.json
// for one-time migration.
func legacyAuthPath() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(appdata, "ocode", "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ocode", "auth.json"), nil
}

// readLegacyOcodeFormat attempts to parse the legacy ocode auth.json which
// wraps credentials under {"credentials": {…}}. If successful it fills dst
// and returns true.
func readLegacyOcodeFormat(data []byte, dst map[string]Credential) bool {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return false
	}
	raw, ok := top["credentials"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var legacy map[string]Credential
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return false
	}
	for k, v := range legacy {
		dst[k] = v
	}
	return true
}

// LoadStore reads auth.json into memory. Missing file is not an error.
func LoadStore() error {
	storeMu.Lock()
	defer storeMu.Unlock()

	return loadStoreLocked()
}

func loadStoreLocked() error {
	path, err := authPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}

	// Ensure the directory exists; MkdirAll is a no-op if it already does.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}

	cache = map[string]Credential{}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", path, err)
		}
		// File doesn't exist yet. Try legacy path for one-time migration.
		if legacy, e := legacyAuthPath(); e == nil {
			if legacyData, e2 := os.ReadFile(legacy); e2 == nil {
				// Try ocode legacy format first: {"credentials": {…}}
				if readLegacyOcodeFormat(legacyData, cache) {
					mergeOpencodeCredentials(cache)
					if err := persistLocked(); err == nil {
						os.Remove(legacy)
					}
					cacheLoaded = true
					return nil
				}
				// Try opencode flat format.
				if e2 := json.Unmarshal(legacyData, &cache); e2 == nil {
					mergeOpencodeCredentials(cache)
					if err := persistLocked(); err == nil {
						os.Remove(legacy)
					}
					cacheLoaded = true
					return nil
				}
			}
		}
		// No existing file anywhere — seed with opencode credentials (if any).
		mergeOpencodeCredentials(cache)
		cacheLoaded = true
		// Write the empty seed file so the path is materialised on disk.
		if err := persistLocked(); err != nil {
			return fmt.Errorf("seed auth file: %w", err)
		}
		return nil
	}

	// File exists at the new path. Try flat opencode format first, then
	// legacy ocode wrapper format.
	if err := json.Unmarshal(data, &cache); err != nil {
		// Flat format failed; try legacy wrapper before giving up.
		if !readLegacyOcodeFormat(data, cache) {
			return fmt.Errorf("parse %s: unknown format", path)
		}
	}
	mergeOpencodeCredentials(cache)
	cacheLoaded = true
	return nil
}

// opencodeLegacyAuthPath returns the path to opencode's own auth.json at
// the XDG data directory (for merging legacy credentials from opencode).
func opencodeLegacyAuthPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "opencode", "auth.json")
		}
		return ""
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// mergeOpencodeCredentials reads opencode's auth.json and fills in any
// providers not already present in dst (ocode credentials take precedence).
// For the common case where authPath() == opencodeLegacyAuthPath(), the file
// is the same one already loaded into cache, so this is a harmless no-op loop.
func mergeOpencodeCredentials(dst map[string]Credential) {
	path := opencodeLegacyAuthPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]Credential
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for id, v := range raw {
		if _, exists := dst[id]; exists {
			continue // ocode credential wins
		}
		dst[id] = v
	}
}

func ensureLoadedLocked() error {
	if cacheLoaded {
		return nil
	}
	return loadStoreLocked()
}

// Get returns the stored credential for a provider, if any.
func Get(provider string) (Credential, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return Credential{}, false
	}
	c, ok := cache[provider]
	return c, ok
}

// List returns a copy of all stored credentials keyed by provider.
func List() map[string]Credential {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return map[string]Credential{}
	}
	out := make(map[string]Credential, len(cache))
	for k, v := range cache {
		out[k] = v
	}
	return out
}

// Set writes a credential for a provider, persisting to disk at 0600.
func Set(provider string, cred Credential) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return err
	}
	cache[provider] = cred
	cacheLoaded = true
	return persistLocked()
}

// Remove deletes a credential.
func Remove(provider string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return err
	}
	delete(cache, provider)
	cacheLoaded = true
	return persistLocked()
}

func persistLocked() error {
	path, err := authPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write auth tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename auth file: %w", err)
	}
	// Ensure mode is tight even if file pre-existed with wider perms.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod auth file: %w", err)
	}
	return nil
}
