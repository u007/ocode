package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// dbProjectFileName is the per-project DB connection config file.
const dbProjectFileName = "db.json"

// dbProjectEnvFilesFileName is the env-file sidecar: the selected env file of
// each project connection, keyed by name, with a fingerprint of the connection
// it was chosen for.
//
// db.json already stores env_file itself, so db.json is authoritative. The
// sidecar exists only for an older binary that predates env_file: it re-marshals
// db.json from its own struct and drops the field, but it never touches this
// file, so the selection can be restored on load. Load fills an EMPTY EnvFile
// from the sidecar and never overrides one from db.json.
const dbProjectEnvFilesFileName = "db-env-files.json"

// dbProjectConfig is the on-disk shape of .ocode/db.json.
type dbProjectConfig struct {
	Connections []DBConnection `json:"connections,omitempty"`
}

// ProjectDBConfigPath returns the path to a project's DB config file.
func ProjectDBConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".ocode", dbProjectFileName)
}

// ProjectDBEnvFilesPath returns the path of a project's env-file sidecar.
func ProjectDBEnvFilesPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".ocode", dbProjectEnvFilesFileName)
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
	reconcileDBEnvFiles(cfg.Connections, loadProjectDBEnvFiles(projectRoot))
	return cfg.Connections, nil
}

// loadProjectDBEnvFiles reads the sidecar. db.json is authoritative, so a
// sidecar that cannot be read or parsed is logged and ignored: it must never
// make the connections unloadable. The worst outcome is a missing env_file
// selection on a connection whose db.json lacks one.
func loadProjectDBEnvFiles(projectRoot string) map[string]dbEnvFileEntry {
	p := ProjectDBEnvFilesPath(projectRoot)
	raw, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: ignoring unreadable project env files %s: %v", p, err)
		}
		return map[string]dbEnvFileEntry{}
	}
	if len(raw) == 0 {
		return map[string]dbEnvFileEntry{}
	}
	out := map[string]dbEnvFileEntry{}
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Printf("config: ignoring malformed project env files %s: %v", p, err)
		return map[string]dbEnvFileEntry{}
	}
	return out
}

// writeDBFileAtomic replaces path with data via a temp file in the same
// directory and a rename, so a reader sees the old file or the new one, never a
// torn write. The temp file is removed on every failure path.
func writeDBFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// saveProjectDBEnvFiles rewrites the sidecar to match conns. It runs after
// db.json is committed, under the same lock. Entries for connections that no
// longer exist, or whose identity changed, are dropped here.
func saveProjectDBEnvFiles(projectRoot string, conns []DBConnection) error {
	files := dbConnectionEnvFiles(conns)
	p := ProjectDBEnvFilesPath(projectRoot)
	if len(files) == 0 {
		err := os.Remove(p)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove project env files: %w", err)
		}
		return nil
	}
	raw, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project env files: %w", err)
	}
	if err := writeDBFileAtomic(p, raw); err != nil {
		return fmt.Errorf("write project env files: %w", err)
	}
	return nil
}

// SaveProjectDBConnections writes a project's .ocode/db.json atomically.
// It creates the .ocode directory if needed. Caller is responsible for locking.
func SaveProjectDBConnections(projectRoot string, conns []DBConnection) error {
	if projectRoot == "" {
		return errors.New("project root is required")
	}
	// Validate on write as well as on read: a bad name must never reach disk.
	validated := append([]DBConnection(nil), conns...)
	for i := range validated {
		normalized, err := normalizeDBEnvFile(validated[i].EnvFile)
		if err != nil {
			return err
		}
		validated[i].EnvFile = normalized
	}
	conns = validated
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
	// Both files are written under one lock, so two saves cannot interleave
	// their db.json and sidecar writes. db.json goes first: it is the
	// authoritative copy, so a failure after it leaves a valid, loadable state.
	return filelock.WithFileLockTimeout(lockPath, 5*time.Second, func() error {
		if err := writeDBFileAtomic(ProjectDBConfigPath(projectRoot), raw); err != nil {
			return fmt.Errorf("write project db config: %w", err)
		}
		if err := saveProjectDBEnvFiles(projectRoot, conns); err != nil {
			return fmt.Errorf("project db.json was saved, but its env-files sidecar was not: %w", err)
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
