package redact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Vault stores per-session secret registry data on disk.
type Vault struct {
	Nonce   string                `json:"nonce"`
	Secrets map[string]vaultEntry `json:"secrets"`
}

type vaultEntry struct {
	Value       string `json:"value"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	FirstSeenAt int64  `json:"first_seen_at"`
}

// DefaultVaultBase returns the base directory for vault storage.
// It mirrors the logic in session.GetStorageDir() - always home, never project-local.
func DefaultVaultBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(base, "opencode"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "opencode"), nil
	default: // linux, freebsd, etc
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "opencode"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "opencode"), nil
	}
}

// VaultPath returns the vault file path for a given session.
func VaultPath(base, slug, sessionID string) string {
	return filepath.Join(base, "project", slug, "secrets", sessionID+".vault.json")
}

// SaveVault persists the registry to disk atomically.
func SaveVault(path string, reg *Registry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create vault dir: %w", err)
	}

	vault := Vault{
		Nonce:   reg.Nonce(),
		Secrets: make(map[string]vaultEntry),
	}

	for _, entry := range reg.All() {
		vault.Secrets[entry.Value] = vaultEntry{
			Value:       entry.Value,
			Kind:        entry.Kind,
			Source:      entry.Source,
			FirstSeenAt: entry.FirstSeenAt,
		}
	}

	data, err := json.MarshalIndent(vault, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: temp file + fsync + rename
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create vault tmp file: %w", err)
	}
	defer os.Remove(tmp) // cleanup on failure

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write vault tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync vault tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close vault tmp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename vault tmp: %w", err)
	}

	return nil
}

// LoadVault loads a vault file and populates a registry.
func LoadVault(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var vault Vault
	if err := json.Unmarshal(data, &vault); err != nil {
		return nil, fmt.Errorf("unmarshal vault: %w", err)
	}

	reg := NewRegistry(vault.Nonce)
	for _, entry := range vault.Secrets {
		reg.GetOrAssign(entry.Value, entry.Kind, entry.Source)
	}

	return reg, nil
}

// DeleteVault removes a vault file.
func DeleteVault(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete vault: %w", err)
	}
	return nil
}
