package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// legacyDBConnection mirrors the DBConnection shape of an ocode binary that
// predates the env_template field. It is used to simulate an OLD binary
// re-saving the shared config file.
//
// The old shape has no env_template field AND no omitempty on url, which is
// why an old binary's save turns a template connection into
// {"name":"env","driver":"postgres","url":""}.
type legacyDBConnection struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	EncryptedURL string `json:"url"`
}

type legacyDBConfig struct {
	Connections []legacyDBConnection `json:"connections,omitempty"`
}

// saveConfigLikeALegacyBinary rewrites the config file the way an ocode
// binary that predates the env_template field would: it re-marshals the "db"
// section from its own (narrower) struct and writes every other top-level
// key back verbatim.
func saveConfigLikeALegacyBinary(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	var legacy legacyDBConfig
	if rawDB, ok := raw["db"]; ok {
		if err := json.Unmarshal(rawDB, &legacy); err != nil {
			t.Fatalf("parse db section: %v", err)
		}
	}
	out := map[string]json.RawMessage{}
	for k, v := range raw {
		if k == "db" {
			continue
		}
		out[k] = v
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal db section: %v", err)
	}
	out["db"] = encoded
	final, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, final, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestDBEnvTemplateSurvivesALegacyBinarySave pins the forward-compatibility
// property that motivated the separate DBEnvTemplatesKey.
//
// The config file is shared between ocode binaries of different versions. A
// binary that predates the env_template field re-marshals the "db" section
// from its own struct on every save, which would silently DESTROY an
// environment-backed connection the first time the user changed an unrelated
// setting in that older binary.
//
// The template therefore lives under its own top-level key, which an old
// binary does not recognise and writes back verbatim, and the new binary
// REPAIRS the "db" section from that key on load.
func TestDBEnvTemplateSurvivesALegacyBinarySave(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ocodeconfig.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cur, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddDBConnection(DBConnection{
		Name: "env", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}",
	}, cur.DB.Connections); err != nil {
		t.Fatalf("AddDBConnection: %v", err)
	}

	// An old binary saves the config for an unrelated reason.
	saveConfigLikeALegacyBinary(t, path)

	// The "db" section lost the field (that is the whole problem) ...
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var legacy legacyDBConfig
	if err := json.Unmarshal(raw["db"], &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Connections) != 1 {
		t.Fatalf("legacy db section = %+v", legacy.Connections)
	}
	if legacy.Connections[0].EncryptedURL != "" {
		t.Fatalf("the legacy save did not strip the url as expected: %+v", legacy.Connections[0])
	}
	// ... but the separate template map survived.
	if _, ok := raw[DBEnvTemplatesKey]; !ok {
		t.Fatalf("the legacy save dropped the %q key: %s", DBEnvTemplatesKey, string(raw["db"]))
	}

	// The new binary reads it back and REPAIRS the connection.
	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.DB.Connections) != 1 {
		t.Fatalf("reloaded connections = %+v", cfg.DB.Connections)
	}
	got := cfg.DB.Connections[0]
	if got.Name != "env" {
		t.Fatalf("reloaded connection = %+v", got)
	}
	if got.EnvTemplate != "${DATABASE_URL}" {
		t.Fatalf("EnvTemplate = %q, want ${DATABASE_URL} (the template must survive a legacy save)", got.EnvTemplate)
	}
}

// TestDBEnvTemplateMapIsWrittenAlongsideTheSection pins that the separate
// map is written when a template connection exists, and is REMOVED when the
// last template connection is deleted (a stale map would resurrect one).
func TestDBEnvTemplateMapIsWrittenAlongsideTheSection(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ocodeconfig.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cur, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddDBConnection(DBConnection{
		Name: "env", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}",
	}, cur.DB.Connections); err != nil {
		t.Fatalf("AddDBConnection: %v", err)
	}
	cur, err = LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddDBConnection(DBConnection{
		Name: "prod", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c",
	}, cur.DB.Connections); err != nil {
		t.Fatalf("AddDBConnection prod: %v", err)
	}

	readMap := func() map[string]string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		blob, ok := raw[DBEnvTemplatesKey]
		if !ok {
			return nil
		}
		var out map[string]string
		if err := json.Unmarshal(blob, &out); err != nil {
			t.Fatalf("parse %s: %v", DBEnvTemplatesKey, err)
		}
		return out
	}

	got := readMap()
	if got["env"] != "${DATABASE_URL}" {
		t.Fatalf("%s = %v, want env -> ${DATABASE_URL}", DBEnvTemplatesKey, got)
	}
	if _, ok := got["prod"]; ok {
		t.Fatalf("%s has a literal connection: %v", DBEnvTemplatesKey, got)
	}

	// Removing the template connection removes the map entry.
	if err := RemoveDBConnection("env"); err != nil {
		t.Fatalf("RemoveDBConnection: %v", err)
	}
	if got := readMap(); len(got) != 0 {
		t.Fatalf("%s = %v after removing the only template connection, want empty", DBEnvTemplatesKey, got)
	}
}

// A template left in the map by a connection an older binary deleted must not
// reach a literal connection that later took the same name.
func TestReconcileSkipsStaleTemplateForLiteralConnection(t *testing.T) {
	conns := []DBConnection{
		{Name: "lit", Driver: DBDriverPostgres, EncryptedURL: "sealed-envelope"},
		{Name: "env", Driver: DBDriverPostgres},
	}
	reconcileDBEnvTemplates(conns, map[string]string{
		"lit": "${STALE}",
		"env": "${DATABASE_URL}",
	})
	if conns[0].EnvTemplate != "" {
		t.Fatalf("literal connection took a stale template: %+v", conns[0])
	}
	if conns[0].EncryptedURL != "sealed-envelope" {
		t.Fatalf("literal connection URL changed: %+v", conns[0])
	}
	if conns[1].EnvTemplate != "${DATABASE_URL}" {
		t.Fatalf("env connection EnvTemplate = %q, want ${DATABASE_URL}", conns[1].EnvTemplate)
	}
}

// A malformed db_env_templates value must not stop the config from loading, and
// a save must keep the unreadable value rather than delete it.
func TestMalformedDBEnvTemplatesSurviveLoadAndSave(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ocodeconfig.json")
	if err := os.WriteFile(path, []byte(`{"db_env_templates": "not-a-map"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("load with a malformed %s: %v", DBEnvTemplatesKey, err)
	}
	if len(cfg.DB.Connections) != 0 {
		t.Fatalf("connections from a malformed map = %+v", cfg.DB.Connections)
	}

	if err := SaveOcodeSTTConfig(STTConfig{Model: "some-model"}); err != nil {
		t.Fatalf("SaveOcodeSTTConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw[DBEnvTemplatesKey]) != `"not-a-map"` {
		t.Fatalf("save changed the unreadable %s: got %s", DBEnvTemplatesKey, string(raw[DBEnvTemplatesKey]))
	}
}
