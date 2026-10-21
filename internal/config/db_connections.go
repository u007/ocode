package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrDBConnectionsChanged means the saved set changed between the caller's
// verification and the locked write. The caller must re-verify and retry.
var ErrDBConnectionsChanged = errors.New("saved db connections changed; retry")

// DBDriverPostgres is the only driver a saved DB connection may use in P1.
// SQLite browsing takes a local file path and is not stored here.
const DBDriverPostgres = "postgres"

// DBEnvTemplatesKey is the ocodeconfig.json top-level key that holds the
// environment templates of saved database connections, as a name -> template
// map.
//
// It exists as a SEPARATE key, not just as a field inside the "db" section,
// because the config file is shared between ocode binaries of different
// versions. A binary that predates the env_template field re-marshals the
// "db" section from its own struct when it saves, and would silently drop
// the field — destroying the user's connection.
//
// A top-level key the old binary does not recognise lands in its Extra map
// and is written back verbatim, so the template survives. This is the same
// mechanism that preserves "last_model" (see lastModelKey).
//
// The "db" section ALSO carries an env_template field, so the file is
// readable by a new binary that never learned about this key. The two are
// reconciled on load: the separate key wins, because it is the one that
// survives.
const DBEnvTemplatesKey = "db_env_templates"

// DBConnection is one saved database connection.
//
// Exactly one of EncryptedURL and EnvTemplate is set.
//
// EncryptedURL is an envelope from internal/dbconnect.SealURL; the plaintext
// URL never reaches this struct.
//
// EnvTemplate holds an UNRESOLVED template such as "${DATABASE_URL}". It is
// not a secret — it names environment variables — so it is stored in the
// clear, and it is resolved per project when the connection is unlocked.
// A connection has no EncryptedURL when it has an EnvTemplate, and vice
// versa.
//
// EnvTemplate is the in-section mirror of the entry under
// DBEnvTemplatesKey. Read the template from the map, not from here: the map
// is the copy that survives a save from an older binary.
type DBConnection struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	EncryptedURL string `json:"url,omitempty"`
	// EnvTemplate is a template resolved against the environment at unlock
	// time. It is omitted from the JSON when empty, so a literal connection's
	// on-disk shape is unchanged.
	EnvTemplate string `json:"env_template,omitempty"`
}

// DBConfig is the ocodeconfig.json "db" section.
type DBConfig struct {
	Connections []DBConnection `json:"connections,omitempty"`
}

// dbEnvTemplates returns the templates stored under DBEnvTemplatesKey, or an
// empty map when the key is absent. A malformed value is an error: the caller
// decides how to surface it, and the raw value stays in Extra untouched.
func dbEnvTemplates(extra map[string]json.RawMessage) (map[string]string, error) {
	out := map[string]string{}
	if extra == nil {
		return out, nil
	}
	raw, ok := extra[DBEnvTemplatesKey]
	if !ok || len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]string{}, fmt.Errorf("parse %s: %w", DBEnvTemplatesKey, err)
	}
	return out, nil
}

// reconcileDBEnvTemplates copies the authoritative templates from the
// DBEnvTemplatesKey map onto the env-backed connections.
//
// The map wins over the in-section field: the map is the copy that survives
// a save from an older binary, so it is the one that can be trusted. A
// connection that HAS a template in the map but lost the field (an old
// binary re-saved the "db" section) is repaired here.
//
// Only a connection with no EncryptedURL takes a template. A map entry whose
// connection was deleted by an older binary is stale: applying it to a literal
// connection that later reused the name would give that connection both a URL
// and a template.
func reconcileDBEnvTemplates(conns []DBConnection, templates map[string]string) {
	for i := range conns {
		if conns[i].EncryptedURL != "" {
			continue
		}
		if tmpl, ok := templates[conns[i].Name]; ok && tmpl != "" {
			conns[i].EnvTemplate = tmpl
		}
	}
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
	// Exactly one of the two URL forms must be present. A connection with
	// neither has nothing to open; one with both is ambiguous about which
	// one wins at unlock time.
	switch {
	case conn.EncryptedURL == "" && conn.EnvTemplate == "":
		return fmt.Errorf("db connection %q has no url", conn.Name)
	case conn.EncryptedURL != "" && conn.EnvTemplate != "":
		return fmt.Errorf("db connection %q has both a url and an env template", conn.Name)
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

// dbConnectionTemplates collects the environment templates of the given
// connections, as a name -> template map.
func dbConnectionTemplates(conns []DBConnection) map[string]string {
	out := map[string]string{}
	for _, c := range conns {
		if c.EnvTemplate != "" {
			out[c.Name] = c.EnvTemplate
		}
	}
	return out
}
