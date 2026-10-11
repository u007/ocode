package config

import (
	"testing"
)

func TestLoadSaveStorageConnectors(t *testing.T) {
	dir := t.TempDir()
	cfg := &StorageConnectorsFile{
		Version:       connectorsVersion,
		Settings:      map[string]ConnectorSettings{"gdrive": {Provider: "google-drive", RootPath: "/docs", MaxCacheBytes: 100}},
		MaxCacheBytes: 500 * 1024 * 1024,
	}
	if err := SaveStorageConnectors(dir, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := LoadStorageConnectors(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.MaxCacheBytes != cfg.MaxCacheBytes {
		t.Errorf("max cache bytes mismatch: got %d, want %d", loaded.MaxCacheBytes, cfg.MaxCacheBytes)
	}
	if len(loaded.Settings) != 1 {
		t.Errorf("expected 1 setting, got %d", len(loaded.Settings))
	}
}

func TestLoadStorageConnectorsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadStorageConnectors(dir)
	if err != nil {
		t.Fatalf("load default failed: %v", err)
	}
	if cfg.MaxCacheBytes != 500*1024*1024 {
		t.Errorf("default max cache bytes unexpected: %d", cfg.MaxCacheBytes)
	}
	if cfg.Version != connectorsVersion {
		t.Errorf("expected version %s, got %s", connectorsVersion, cfg.Version)
	}
}
