package config

import (
	"errors"
	"fmt"
)

// ErrDBConnectionsChanged means the saved set changed between the caller's
// verification and the locked write. The caller must re-verify and retry.
var ErrDBConnectionsChanged = errors.New("saved db connections changed; retry")

// DBDriverPostgres is the only driver a saved DB connection may use in P1.
// SQLite browsing takes a local file path and is not stored here.
const DBDriverPostgres = "postgres"

// DBConnection is one saved database connection. EncryptedURL is an envelope
// from internal/dbconnect.SealURL; the plaintext URL never reaches this struct.
type DBConnection struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	EncryptedURL string `json:"url"`
}

// DBConfig is the ocodeconfig.json "db" section.
type DBConfig struct {
	Connections []DBConnection `json:"connections,omitempty"`
}

// AddDBConnection appends a saved connection under the ocodeconfig lock, so a
// concurrent session's other settings are kept. Names are unique.
//
// verified is the saved set the caller checked the master password against,
// before taking the lock (Argon2id is too slow to run under it). If the saved
// set no longer matches verified, nothing is written and ErrDBConnectionsChanged
// is returned.
func AddDBConnection(conn DBConnection, verified []DBConnection) error {
	if conn.Name == "" {
		return fmt.Errorf("db connection name is required")
	}
	if conn.Driver != DBDriverPostgres {
		return fmt.Errorf("unsupported db driver %q", conn.Driver)
	}
	if conn.EncryptedURL == "" {
		return fmt.Errorf("db connection %q has no encrypted url", conn.Name)
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if !sameDBConnections(cfg.DB.Connections, verified) {
			return ErrDBConnectionsChanged
		}
		for _, existing := range cfg.DB.Connections {
			if existing.Name == conn.Name {
				return fmt.Errorf("db connection %q already exists", conn.Name)
			}
		}
		cfg.DB.Connections = append(cfg.DB.Connections, conn)
		return nil
	})
}

// sameDBConnections reports whether two saved sets hold the same entries in the same order.
func sameDBConnections(a, b []DBConnection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RemoveDBConnection deletes a saved connection by name under the ocodeconfig lock.
func RemoveDBConnection(name string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		kept := cfg.DB.Connections[:0:0]
		for _, existing := range cfg.DB.Connections {
			if existing.Name != name {
				kept = append(kept, existing)
			}
		}
		if len(kept) == len(cfg.DB.Connections) {
			return fmt.Errorf("db connection %q not found", name)
		}
		cfg.DB.Connections = kept
		return nil
	})
}
