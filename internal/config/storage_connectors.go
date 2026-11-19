// Package config provides settings persistence for connector framework.
// It operates independently of ocodeconfig.json (separate atomic writer,
// no concurrent-write conflict with the main settings file).
package config

import (
	"encoding/json"
	"errors"
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

// v2 format: one central connector (kakiit) holds metadata in plaintext; all
// token material lives in a raw-key ENCv2 envelope. The v1 reader above stays
// exported so Task 4a can migrate legacy files; no migration happens here.
const connectorsVersionV2 = "v2"

type StorageConnectorsFileV2 struct {
	Version             string           `json:"version"`
	Central             CentralConnector `json:"central"`
	CredentialsEnvelope string           `json:"credentials_envelope"`
}

type CentralConnector struct {
	ConnectorID string                    `json:"connector_id"` // "kakiit"
	Mode        string                    `json:"mode"`         // "hub" | "local"
	Connections map[string]ConnectionMeta `json:"connections"`  // key = provider
}

type ConnectionMeta struct {
	Provider      string `json:"provider"`
	ConnectionID  string `json:"connection_id"`
	ExpiresAt     string `json:"expires_at"` // RFC3339
	RootPath      string `json:"root_path"`
	MaxCacheBytes int64  `json:"max_cache_bytes"`
	Status        string `json:"status"`
}

// ErrV2MigrationRequired means the on-disk connectors file is v1. Hub
// handlers must migrate before writing, or they would overwrite v1 metadata.
var ErrV2MigrationRequired = errors.New("connectors file requires v1 to v2 migration")

func LoadV2(projectRoot string) (*StorageConnectorsFileV2, error) {
	path := connectorsFilePath(projectRoot)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &StorageConnectorsFileV2{
			Version: connectorsVersionV2,
			Central: CentralConnector{
				ConnectorID: "kakiit",
				Mode:        "hub",
				Connections: make(map[string]ConnectionMeta),
			},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read v2 connectors settings: %w", err)
	}
	var f StorageConnectorsFileV2
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse v2 connectors settings: %w", err)
	}
	if f.Version != connectorsVersionV2 {
		return nil, ErrV2MigrationRequired
	}
	if f.Central.Connections == nil {
		f.Central.Connections = make(map[string]ConnectionMeta)
	}
	return &f, nil
}

func SaveV2(projectRoot string, f *StorageConnectorsFileV2) error {
	if f == nil {
		return fmt.Errorf("v2 connectors config is nil")
	}
	f.Version = connectorsVersionV2
	if f.Central.Connections == nil {
		f.Central.Connections = make(map[string]ConnectionMeta)
	}
	path := connectorsFilePath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("create connectors settings dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal v2 connectors settings: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write v2 connectors settings temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename v2 connectors settings: %w", err)
	}
	return nil
}
