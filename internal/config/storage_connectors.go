// Package config provides settings persistence for connector framework.
// It operates independently of ocodeconfig.json (separate atomic writer,
// no concurrent-write conflict with the main settings file).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

// ConnectorSettings holds non-secret connector metadata (per-project override).
// Real secrets (OAuth tokens, S3 keys) live in OS keychain, never here.
type ConnectorSettings struct {
	Provider      string `json:"provider"`        // e.g. google-drive, s3, onedrive, gcs
	RootPath      string `json:"root_path"`       // cloud root / bucket / folder
	ProjectSlug   string `json:"project_slug"`    // optional link to project
	MaxCacheBytes int64  `json:"max_cache_bytes"` // cache size limit (0 = unlimited)
}

// StorageConnectorsFile is the persistent settings file format.
// It is saved as plaintext JSON. Encryption is planned but not implemented, so
// nothing secret may be stored in this file.
type StorageConnectorsFile struct {
	Version       string                       `json:"version"`
	Settings      map[string]ConnectorSettings `json:"settings"` // key = provider id
	MaxCacheBytes int64                        `json:"max_cache_bytes"`
}

const connectorsVersion = "v1"

func connectorsFilePath(projectRoot string) string {
	if projectRoot != "" {
		return filepath.Join(projectRoot, ".ocode", "storage-connectors.json")
	}
	globalDir, _ := paths.GlobalDataDir()
	return filepath.Join(globalDir, "storage-connectors.json")
}

func LoadStorageConnectors(projectRoot string) (*StorageConnectorsFile, error) {
	path := connectorsFilePath(projectRoot)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &StorageConnectorsFile{
			Version:       connectorsVersion,
			Settings:      make(map[string]ConnectorSettings),
			MaxCacheBytes: 500 * 1024 * 1024, // 500 MB default
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read connectors settings: %w", err)
	}
	// Decryption is handled by the caller (encryption package);
	// this loader works on plaintext JSON (pre-save) or encrypted envelope.
	var cfg StorageConnectorsFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse connectors settings: %w", err)
	}
	if cfg.Settings == nil {
		cfg.Settings = make(map[string]ConnectorSettings)
	}
	if cfg.MaxCacheBytes == 0 {
		cfg.MaxCacheBytes = 500 * 1024 * 1024
	}
	return &cfg, nil
}

func SaveStorageConnectors(projectRoot string, cfg *StorageConnectorsFile) error {
	if cfg == nil {
		return fmt.Errorf("connectors config is nil")
	}
	cfg.Version = connectorsVersion
	path := connectorsFilePath(projectRoot)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create connectors settings dir: %w", err)
	}
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal connectors settings: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write connectors settings temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename connectors settings: %w", err)
	}
	return nil
}
