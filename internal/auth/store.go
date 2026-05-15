package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// CredentialKind distinguishes how a credential was obtained.
type CredentialKind string

const (
	KindAPIKey CredentialKind = "api_key"
	KindOAuth  CredentialKind = "oauth"
)

// Credential is a single saved provider credential.
type Credential struct {
	Kind         CredentialKind `json:"kind"`
	Key          string         `json:"key,omitempty"`           // API key
	AccessToken  string         `json:"access_token,omitempty"`  // OAuth
	RefreshToken string         `json:"refresh_token,omitempty"` // OAuth
	ExpiresAt    int64          `json:"expires_at,omitempty"`    // OAuth (unix seconds)
	Account      string         `json:"account,omitempty"`       // e.g. "user@example.com"
	BaseURL      string         `json:"base_url,omitempty"`      // optional endpoint override
}

// authFile is the on-disk representation.
type authFile struct {
	Credentials map[string]Credential `json:"credentials"`
}

var (
	storeMu sync.Mutex
	cache   *authFile
)

// authPath returns ~/.config/ocode/auth.json (or %APPDATA%\ocode\auth.json on Windows).
func authPath() (string, error) {
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

	cache = &authFile{Credentials: map[string]Credential{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			mergeOpencodeCredentials(cache)
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := json.Unmarshal(data, cache); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if cache.Credentials == nil {
		cache.Credentials = map[string]Credential{}
	}
	mergeOpencodeCredentials(cache)
	return nil
}

// opencodeAuthPath returns the path to opencode's auth.json (XDG data dir).
func opencodeAuthPath() string {
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

// mergeOpencodeCredentials reads opencode's auth.json and fills in any providers
// not already present in dst (ocode credentials take precedence).
func mergeOpencodeCredentials(dst *authFile) {
	path := opencodeAuthPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // opencode not installed or no credentials — silently skip
	}
	// opencode format: { "providerID": {"type":"api","key":"..."} | {"type":"oauth","access_token":"..."} }
	var raw map[string]struct {
		Type         string `json:"type"`
		Key          string `json:"key"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
		Account      string `json:"account"`
		BaseURL      string `json:"baseURL"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for id, oc := range raw {
		if _, exists := dst.Credentials[id]; exists {
			continue // ocode credential wins
		}
		switch oc.Type {
		case "api":
			if oc.Key != "" {
				dst.Credentials[id] = Credential{Kind: KindAPIKey, Key: oc.Key, BaseURL: oc.BaseURL}
			}
		case "oauth":
			if oc.AccessToken != "" {
				dst.Credentials[id] = Credential{
					Kind:         KindOAuth,
					AccessToken:  oc.AccessToken,
					RefreshToken: oc.RefreshToken,
					ExpiresAt:    oc.ExpiresAt,
					Account:      oc.Account,
					BaseURL:      oc.BaseURL,
				}
			}
		}
	}
}

func ensureLoadedLocked() error {
	if cache != nil {
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
	c, ok := cache.Credentials[provider]
	return c, ok
}

// List returns a copy of all stored credentials keyed by provider.
func List() map[string]Credential {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return map[string]Credential{}
	}
	out := make(map[string]Credential, len(cache.Credentials))
	for k, v := range cache.Credentials {
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
	cache.Credentials[provider] = cred
	return persistLocked()
}

// Remove deletes a credential.
func Remove(provider string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return err
	}
	delete(cache.Credentials, provider)
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
