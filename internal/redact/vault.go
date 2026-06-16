package redact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// Vault stores per-session secret registry data on disk.
type Vault struct {
	Nonce   string        `json:"nonce"`
	Secrets []vaultRecord `json:"secrets"`
}

type vaultRecord struct {
	Index       int    `json:"index"`
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
		Nonce: reg.Nonce(),
	}
	snapshot := reg.Snapshot()
	vault.Secrets = make([]vaultRecord, 0, len(snapshot))

	for _, indexed := range snapshot {
		vault.Secrets = append(vault.Secrets, vaultRecord{
			Index:       indexed.Index,
			Value:       indexed.Entry.Value,
			Kind:        indexed.Entry.Kind,
			Source:      indexed.Entry.Source,
			FirstSeenAt: indexed.Entry.FirstSeenAt,
		})
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

	var raw struct {
		Nonce   string          `json:"nonce"`
		Secrets json.RawMessage `json:"secrets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal vault: %w", err)
	}

	reg := NewRegistry(raw.Nonce)
	if len(raw.Secrets) == 0 {
		return reg, nil
	}

	if raw.Secrets[0] == '[' {
		var records []vaultRecord
		if err := json.Unmarshal(raw.Secrets, &records); err != nil {
			return nil, fmt.Errorf("unmarshal vault secrets: %w", err)
		}
		sort.Slice(records, func(i, j int) bool { return records[i].Index < records[j].Index })
		for _, entry := range records {
			idx := reg.GetOrAssign(entry.Value, entry.Kind, entry.Source)
			if idx != entry.Index && entry.Index > 0 {
				reg.mu.Lock()
				if cur, ok := reg.entries[idx]; ok {
					delete(reg.valToIdx, cur.Value)
					delete(reg.entries, idx)
				}
				reg.valToIdx[entry.Value] = entry.Index
				reg.entries[entry.Index] = Entry{
					Value:       entry.Value,
					Kind:        entry.Kind,
					Source:      entry.Source,
					FirstSeenAt: entry.FirstSeenAt,
				}
				if entry.Index >= reg.nextIdx {
					reg.nextIdx = entry.Index + 1
				}
				reg.mu.Unlock()
			}
		}
		return reg, nil
	}

	var legacy map[string]vaultRecord
	if err := json.Unmarshal(raw.Secrets, &legacy); err != nil {
		return nil, fmt.Errorf("unmarshal vault secrets: %w", err)
	}
	keys := make([]string, 0, len(legacy))
	for k := range legacy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := legacy[k]
		idx := reg.GetOrAssign(entry.Value, entry.Kind, entry.Source)
		if idx != entry.Index && entry.Index > 0 {
			reg.mu.Lock()
			if cur, ok := reg.entries[idx]; ok {
				delete(reg.valToIdx, cur.Value)
				delete(reg.entries, idx)
			}
			reg.valToIdx[entry.Value] = entry.Index
			reg.entries[entry.Index] = Entry{
				Value:       entry.Value,
				Kind:        entry.Kind,
				Source:      entry.Source,
				FirstSeenAt: entry.FirstSeenAt,
			}
			if entry.Index >= reg.nextIdx {
				reg.nextIdx = entry.Index + 1
			}
			reg.mu.Unlock()
		}
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
