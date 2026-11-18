package config

import (
	"errors"
	"os"
	"path/filepath"
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

func TestV2FileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &StorageConnectorsFileV2{
		Version: connectorsVersionV2,
		Central: CentralConnector{
			ConnectorID: "kakiit",
			Mode:        "hub",
			Connections: map[string]ConnectionMeta{
				"dropbox": {
					Provider:      "dropbox",
					ConnectionID:  "conn-1",
					ExpiresAt:     "2026-11-18T10:00:00Z",
					RootPath:      "/Apps/ocode",
					MaxCacheBytes: 12345,
					Status:        "connected",
				},
			},
		},
		CredentialsEnvelope: "ENCv2:raw:AAAA:BBBB",
	}
	if err := SaveV2(dir, want); err != nil {
		t.Fatalf("SaveV2: %v", err)
	}
	got, err := LoadV2(dir)
	if err != nil {
		t.Fatalf("LoadV2: %v", err)
	}
	if got.Version != connectorsVersionV2 {
		t.Errorf("version = %q, want %q", got.Version, connectorsVersionV2)
	}
	if got.Central.ConnectorID != "kakiit" || got.Central.Mode != "hub" {
		t.Errorf("central = %+v", got.Central)
	}
	conn, ok := got.Central.Connections["dropbox"]
	if !ok {
		t.Fatal("dropbox connection missing")
	}
	if conn.ConnectionID != "conn-1" || conn.ExpiresAt != "2026-11-18T10:00:00Z" ||
		conn.RootPath != "/Apps/ocode" || conn.MaxCacheBytes != 12345 || conn.Status != "connected" {
		t.Errorf("connection = %+v", conn)
	}
	if got.CredentialsEnvelope != want.CredentialsEnvelope {
		t.Errorf("envelope = %q, want %q", got.CredentialsEnvelope, want.CredentialsEnvelope)
	}
	info, err := os.Stat(filepath.Join(dir, ".ocode", "storage-connectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}
}

func TestLoadV2Defaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadV2(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != connectorsVersionV2 {
		t.Errorf("version = %q, want %q", cfg.Version, connectorsVersionV2)
	}
	if cfg.Central.ConnectorID != "kakiit" || cfg.Central.Mode != "hub" {
		t.Errorf("central = %+v", cfg.Central)
	}
	if cfg.Central.Connections == nil {
		t.Error("connections map must be initialized")
	}
}

func TestLoadV2RefusesV1File(t *testing.T) {
	dir := t.TempDir()
	v1 := &StorageConnectorsFile{
		Version:       connectorsVersion,
		Settings:      map[string]ConnectorSettings{"dropbox": {Provider: "dropbox", RootPath: "/docs"}},
		MaxCacheBytes: 1234,
	}
	if err := SaveStorageConnectors(dir, v1); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	cfg, err := LoadV2(dir)
	if !errors.Is(err, ErrV2MigrationRequired) {
		t.Fatalf("LoadV2 error = %v, want ErrV2MigrationRequired", err)
	}
	if cfg != nil {
		t.Fatalf("LoadV2 returned %+v on a v1 file, want nil", cfg)
	}
}
