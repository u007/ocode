package cloudconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
	"github.com/u007/ocode/internal/keychain"
	"github.com/u007/ocode/internal/paths"
)

// Manager owns connector instances and their (encrypted) secrets.
type Manager struct {
	mu             sync.RWMutex
	dataDir        string
	projectRoot    string
	masterPassword string
	keychainStore  keychain.Store
	connectors     map[string]Connector // id -> connector
	configs        map[string]Config    // id -> metadata
	secrets        map[string]SecretBundle
}

// NewManager creates a manager. masterPassword may be empty; in that case secrets
// are kept in memory only and are not persisted across restarts.
func NewManager(dataDir, projectRoot, masterPassword string) *Manager {
	if dataDir == "" {
		if d, err := paths.GlobalDataDir(); err == nil {
			dataDir = d
		}
	}
	m := &Manager{
		dataDir:        dataDir,
		projectRoot:    projectRoot,
		masterPassword: masterPassword,
		keychainStore:  keychain.New(keychain.WithPath(filepath.Join(dataDir, "cloud-secrets.enc")), keychain.WithMasterPassword(masterPassword)),
		connectors:     map[string]Connector{},
		configs:        map[string]Config{},
		secrets:        map[string]SecretBundle{},
	}
	m.load()
	return m
}

func (m *Manager) secretsPath() string {
	base := m.dataDir
	if m.projectRoot != "" {
		base = filepath.Join(m.projectRoot, ".ocode")
	}
	return filepath.Join(base, "cloudconnect-secrets.enc")
}

func (m *Manager) load() {
	// Load metadata from storage_connectors.json.
	cfg, err := config.LoadStorageConnectors(m.projectRoot)
	if err == nil && cfg != nil {
		for id, s := range cfg.Settings {
			m.configs[id] = Config{
				ID:            id,
				Provider:      s.Provider,
				RootPath:      s.RootPath,
				ProjectSlug:   s.ProjectSlug,
				MaxCacheBytes: s.MaxCacheBytes,
			}
		}
	}
	// Load secrets: prefer OS keychain, fall back to encrypted file.
	loaded := false
	if m.keychainStore != nil {
		// keychain stores one JSON blob per connector under service ocode-cloud
		// We store the whole map under account "_all" for simplicity.
		if v, err := m.keychainStore.Get("ocode-cloud", "_all"); err == nil {
			var m2 map[string]SecretBundle
			if json.Unmarshal([]byte(v), &m2) == nil {
				m.secrets = m2
				loaded = true
			}
		}
	}
	if !loaded && m.masterPassword != "" {
		data, err := os.ReadFile(m.secretsPath())
		if err == nil {
			plain, err := encryption.DecryptJSON(string(data), m.masterPassword)
			if err == nil {
				var m2 map[string]SecretBundle
				if json.Unmarshal(plain, &m2) == nil {
					m.secrets = m2
				}
			}
		}
	}
	// Instantiate connectors for configs that have secrets (or need none).
	for id, c := range m.configs {
		sec := m.secrets[id]
		conn, err := newConnector(c, sec)
		if err == nil {
			m.connectors[id] = conn
		}
	}
}

func (m *Manager) saveMetadata() error {
	cfg := &config.StorageConnectorsFile{
		Version:       "v1",
		Settings:      map[string]config.ConnectorSettings{},
		MaxCacheBytes: 500 * 1024 * 1024,
	}
	for id, c := range m.configs {
		cfg.Settings[id] = config.ConnectorSettings{
			Provider:      c.Provider,
			RootPath:      c.RootPath,
			ProjectSlug:   c.ProjectSlug,
			MaxCacheBytes: c.MaxCacheBytes,
		}
	}
	return config.SaveStorageConnectors(m.projectRoot, cfg)
}

func (m *Manager) saveSecrets() error {
	data, err := json.Marshal(m.secrets)
	if err != nil {
		return err
	}
	// Primary: OS keychain (when available).
	if m.keychainStore != nil {
		if err := m.keychainStore.Set("ocode-cloud", "_all", string(data)); err == nil {
			return nil
		}
	}
	// Fallback: encrypted file (requires master password).
	if m.masterPassword == "" {
		return nil // in-memory only
	}
	enc, err := encryption.EncryptJSON(data, m.masterPassword)
	if err != nil {
		return err
	}
	path := m.secretsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(enc), 0600)
}

// Register adds or updates a connector.
func (m *Manager) Register(cfg Config, sec SecretBundle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, err := newConnector(cfg, sec)
	if err != nil {
		return err
	}
	m.configs[cfg.ID] = cfg
	m.secrets[cfg.ID] = sec
	m.connectors[cfg.ID] = conn
	if err := m.saveMetadata(); err != nil {
		return err
	}
	return m.saveSecrets()
}

// Remove deletes a connector.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.connectors, id)
	delete(m.configs, id)
	delete(m.secrets, id)
	if err := m.saveMetadata(); err != nil {
		return err
	}
	if m.keychainStore != nil {
		_ = m.keychainStore.Delete("ocode-cloud", "_all")
	}
	return m.saveSecrets()
}

// List returns metadata for all connectors.
func (m *Manager) List() []Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Config, 0, len(m.configs))
	for _, c := range m.configs {
		out = append(out, c)
	}
	return out
}

// Get returns a connector by id.
func (m *Manager) Get(id string) (Connector, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.connectors[id]
	return c, ok
}

// Browse lists entries at a virtual path for a connector.
func (m *Manager) Browse(ctx context.Context, id, path string) ([]Entry, error) {
	c, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown connector %q", id)
	}
	return c.List(ctx, path)
}

// Read downloads a file's content.
func (m *Manager) Read(ctx context.Context, id, path string) (io.ReadCloser, error) {
	c, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown connector %q", id)
	}
	return c.Read(ctx, path)
}

// Metadata returns entry metadata.
func (m *Manager) Metadata(ctx context.Context, id, path string) (*Entry, error) {
	c, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown connector %q", id)
	}
	return c.Metadata(ctx, path)
}

func newConnector(cfg Config, sec SecretBundle) (Connector, error) {
	switch cfg.Provider {
	case "google-drive":
		return NewGoogleDriveConnector(cfg.ID, cfg.RootPath, sec)
	case "onedrive":
		return NewOneDriveConnector(cfg.ID, cfg.RootPath, sec)
	case "gcs":
		return NewGCSConnector(cfg.ID, cfg.RootPath, sec)
	case "s3":
		return NewS3Connector(cfg.ID, cfg.RootPath, sec)
	case "dropbox":
		return NewDropboxConnector(cfg.ID, cfg.RootPath, sec)
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

// Write uploads content to a virtual path for a connector, resolving conflicts.
func (m *Manager) Write(ctx context.Context, connectorID, path string, r io.Reader, size int64) error {
	c, ok := m.Get(connectorID)
	if !ok {
		return fmt.Errorf("unknown connector %q", connectorID)
	}
	final := ResolveConflictPath(c, path)
	return c.Write(ctx, final, r, size)
}

// Delete removes a virtual path for a connector.
func (m *Manager) Delete(ctx context.Context, connectorID, path string) error {
	c, ok := m.Get(connectorID)
	if !ok {
		return fmt.Errorf("unknown connector %q", connectorID)
	}
	return c.Delete(ctx, path)
}

// ResolveConflictPath checks whether path exists on conn; if so, returns a
// renamed path "<name>.<timestamp>" to keep both copies.
func ResolveConflictPath(conn Connector, path string) string {
	if conn == nil {
		return path
	}
	if _, err := conn.Metadata(context.Background(), path); err != nil {
		return path
	}
	dir := ""
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
		base = path[i+1:]
	}
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	if dir == "" {
		return base + "." + ts
	}
	return dir + "/" + base + "." + ts
}

// ResolveConflictPath is also available as a Manager method.
func (m *Manager) ResolveConflictPath(conn Connector, path string) string {
	return ResolveConflictPath(conn, path)
}

// DefaultManager is the process-wide cloud connector manager, set by the
// server host at startup. Tools and other non-HTTP callers read it.
var DefaultManager *Manager
