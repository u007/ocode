package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// kdfParams records how the KEK was derived so Unlock can reproduce it.
type kdfParams struct {
	Algo      string `json:"algo"`
	Salt      string `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
}

// diskItem is one vault entry: an opaque sealed blob keyed by its id. The id
// doubles as the AES-GCM AAD, so a blob moved onto a different item fails to
// open.
type diskItem struct {
	ID   string `json:"id"`
	Blob string `json:"blob"`
}

// vaultFile is the on-disk envelope.
type vaultFile struct {
	Version    int        `json:"version"`
	KDF        kdfParams  `json:"kdf"`
	WrappedKey string     `json:"wrapped_key"`
	Items      []diskItem `json:"items"`
}

// readVaultFile loads and parses the vault envelope. A missing file is
// ErrNoVault; malformed JSON is wrapped ErrCorrupt.
func readVaultFile(path string) (*vaultFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoVault
		}
		return nil, fmt.Errorf("vault: read file: %w", err)
	}
	var vf vaultFile
	if err := json.Unmarshal(data, &vf); err != nil {
		return nil, fmt.Errorf("%w: parse vault file: %v", ErrCorrupt, err)
	}
	return &vf, nil
}

// writeVaultFile serialises vf and atomically replaces the file at path with
// mode 0600. The write goes to a temp file in the same directory (so the
// rename stays on one filesystem), which is removed if anything fails before
// the rename.
func writeVaultFile(path string, vf *vaultFile) error {
	data, err := json.MarshalIndent(vf, "", "  ")
	if err != nil {
		return fmt.Errorf("vault: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("vault: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vault-*.tmp")
	if err != nil {
		return fmt.Errorf("vault: create temp: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("vault: remove temp file %s: %v", tmpName, err)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("vault: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("vault: rename temp: %w", err)
	}
	committed = true
	return nil
}
