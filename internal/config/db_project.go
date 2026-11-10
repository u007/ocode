package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// dbProjectFileName is the per-project DB connection config file.
const dbProjectFileName = "db.json"

// dbProjectConfig is the on-disk shape of .ocode/db.json.
type dbProjectConfig struct {
	Connections []DBConnection `json:"connections,omitempty"`
}

// ProjectDBConfigPath returns the path to a project's DB config file.
func ProjectDBConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".ocode", dbProjectFileName)
}

// LoadProjectDBConnections reads a project's .ocode/db.json. A missing file
// returns nil, nil (no connections). A malformed file is an error.
func LoadProjectDBConnections(projectRoot string) ([]DBConnection, error) {
	if projectRoot == "" {
		return nil, errors.New("project root is required")
	}
	p := ProjectDBConfigPath(projectRoot)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project db config: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var cfg dbProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse project db config %s: %w", p, err)
	}
	return cfg.Connections, nil
}

// SaveProjectDBConnections writes a project's .ocode/db.json atomically.
// It creates the .ocode directory if needed. Caller is responsible for locking.
func SaveProjectDBConnections(projectRoot string, conns []DBConnection) error {
	if projectRoot == "" {
		return errors.New("project root is required")
	}
	dir := filepath.Join(projectRoot, ".ocode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create .ocode dir: %w", err)
	}
	cfg := dbProjectConfig{Connections: conns}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project db config: %w", err)
	}
	lockPath := ProjectDBConfigPath(projectRoot) + ".lock"
	return filelock.WithFileLockTimeout(lockPath, 5*time.Second, func() error {
		tmp := ProjectDBConfigPath(projectRoot) + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o600); err != nil {
			return fmt.Errorf("write project db config: %w", err)
		}
		if err := os.Rename(tmp, ProjectDBConfigPath(projectRoot)); err != nil {
			return fmt.Errorf("rename project db config: %w", err)
		}
		return nil
	})
}

// MergeDBConnections merges project connections over global ones by name.
// Project entries win on name collision.
func MergeDBConnections(global, project []DBConnection) []DBConnection {
	byName := map[string]DBConnection{}
	order := []string{}
	for _, c := range global {
		if _, ok := byName[c.Name]; !ok {
			order = append(order, c.Name)
		}
		byName[c.Name] = c
	}
	for _, c := range project {
		if _, ok := byName[c.Name]; !ok {
			order = append(order, c.Name)
		}
		byName[c.Name] = c
	}
	out := make([]DBConnection, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}
