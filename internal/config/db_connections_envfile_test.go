package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBEnvFileSurvivesALegacyBinarySave(t *testing.T) {
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
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}, cur.DB.Connections); err != nil {
		t.Fatalf("AddDBConnection: %v", err)
	}

	saveConfigLikeALegacyBinary(t, path)

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
	if len(legacy.Connections) != 1 || legacy.Connections[0].Name != "staging" {
		t.Fatalf("legacy db section = %+v", legacy.Connections)
	}
	if _, ok := raw[DBEnvFilesKey]; !ok {
		t.Fatalf("the legacy save dropped the %q key", DBEnvFilesKey)
	}

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.DB.Connections) != 1 || cfg.DB.Connections[0].EnvFile != ".env.staging" {
		t.Fatalf("EnvFile = %q, want .env.staging (the field must survive a legacy save)", cfg.DB.Connections[0].EnvFile)
	}
}

func TestDBEnvFileMapIsWrittenAndRemoved(t *testing.T) {
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
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}, cur.DB.Connections); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	got, err := dbEnvFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["staging"].File != ".env.staging" {
		t.Fatalf("%s = %v, want staging -> .env.staging", DBEnvFilesKey, got)
	}

	if err := RemoveDBConnection("staging"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]json.RawMessage
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	if _, ok := after[DBEnvFilesKey]; ok {
		t.Fatalf("%s still present after removing the only connection: %s", DBEnvFilesKey, string(raw[DBEnvFilesKey]))
	}
}

func TestReconcileDBEnvFilesAppliesToLiteralConnections(t *testing.T) {
	literal := DBConnection{Name: "literal", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:x"}
	tunnel := DBConnection{Name: "tunnel", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}"}
	conns := []DBConnection{literal, tunnel}
	reconcileDBEnvFiles(conns, map[string]dbEnvFileEntry{
		"literal": {File: ".env.production", Fingerprint: dbConnectionFingerprint(literal)},
		"tunnel":  {File: ".env.staging", Fingerprint: dbConnectionFingerprint(tunnel)},
	})
	if conns[0].EnvFile != ".env.production" {
		t.Fatalf("literal connection EnvFile = %q, want .env.production", conns[0].EnvFile)
	}
	if conns[1].EnvFile != ".env.staging" {
		t.Fatalf("env connection EnvFile = %q, want .env.staging", conns[1].EnvFile)
	}
	if got := dbConnectionEnvFiles(conns); got["literal"].File != ".env.production" || got["tunnel"].File != ".env.staging" {
		t.Fatalf("dbConnectionEnvFiles = %v", got)
	}
}

// preEnvFileConnection is a project connection as a binary that predates
// env_file writes it: every field except EnvFile is kept.
type preEnvFileConnection struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	EncryptedURL string `json:"url,omitempty"`
	EnvTemplate  string `json:"env_template,omitempty"`
}

func TestProjectDBEnvFileSurvivesALegacyBinarySave(t *testing.T) {
	root := t.TempDir()
	conns := []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}
	if err := SaveProjectDBConnections(root, conns); err != nil {
		t.Fatal(err)
	}
	p := ProjectDBConfigPath(root)

	// A binary that predates env_file re-marshals db.json from its own struct:
	// it keeps the connection but drops env_file. It never touches the sidecar.
	var legacy struct {
		Connections []preEnvFileConnection `json:"connections,omitempty"`
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := probeProjectKey(t, p, "connections"); !ok {
		t.Fatalf("legacy rewrite lost the connections")
	}
	if strings.Contains(string(raw), `"env_file"`) {
		t.Fatalf("legacy db.json unexpectedly still has env_file")
	}
	// The sidecar is a file an older binary never touches, so it survives.
	sidecarData, err := os.ReadFile(ProjectDBEnvFilesPath(root))
	if err != nil {
		t.Fatalf("legacy save lost the env-files sidecar: %v", err)
	}
	var sidecar map[string]dbEnvFileEntry
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar["staging"].File != ".env.staging" {
		t.Fatalf("sidecar = %v, want staging -> .env.staging", sidecar)
	}

	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EnvFile != ".env.staging" {
		t.Fatalf("EnvFile after legacy save = %q, want .env.staging", got[0].EnvFile)
	}
}

func probeProjectKey(t *testing.T, path, key string) (json.RawMessage, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	v, ok := raw[key]
	return v, ok
}

func TestSaveProjectDBConnectionsWritesEnvFilesMap(t *testing.T) {
	root := t.TempDir()
	conns := []DBConnection{{Name: "prod", Driver: DBDriverPostgres, EnvFile: ".env.production"}}
	if err := SaveProjectDBConnections(root, conns); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ProjectDBEnvFilesPath(root))
	if err != nil {
		t.Fatalf("no env-files sidecar: %v", err)
	}
	var files map[string]dbEnvFileEntry
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	if files["prod"].File != ".env.production" {
		t.Fatalf("%s = %v, want prod -> .env.production", DBEnvFilesKey, files)
	}
	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EnvFile != ".env.production" {
		t.Fatalf("reloaded EnvFile = %q, want .env.production", got[0].EnvFile)
	}
}

// writeProjectDBJSON overwrites db.json with conns, the way a binary that
// predates env_file (or a crash between the two writes) leaves it.
func writeProjectDBJSON(t *testing.T, root string, conns []DBConnection) {
	t.Helper()
	raw, err := json.MarshalIndent(dbProjectConfig{Connections: conns}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectDBConfigPath(root), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertNoOcodeTempFiles fails if a temp file was left in .ocode.
func assertNoOcodeTempFiles(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".ocode"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind in .ocode: %s", e.Name())
		}
	}
}

// blockPathWithDir replaces path with a non-empty directory, so an atomic
// rename onto it fails while the temp write itself succeeds.
func blockPathWithDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectSidecarFailureLeavesDBJSONSavedAndLoadable(t *testing.T) {
	root := t.TempDir()
	first := []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}
	if err := SaveProjectDBConnections(root, first); err != nil {
		t.Fatal(err)
	}
	blockPathWithDir(t, ProjectDBEnvFilesPath(root))

	second := []DBConnection{
		{Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.production"},
		{Name: "reports", Driver: DBDriverMySQL, EncryptedURL: "ENCv1:x"},
	}
	if err := SaveProjectDBConnections(root, second); err == nil {
		t.Fatal("want an error when the sidecar cannot be written")
	}

	// db.json is written first and is authoritative: the new set is on disk.
	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatalf("load must not fail on a broken sidecar: %v", err)
	}
	if len(got) != 2 || got[0].Name != "staging" || got[0].EnvFile != ".env.production" || got[1].Name != "reports" {
		t.Fatalf("db.json not authoritative after sidecar failure: %+v", got)
	}
	assertNoOcodeTempFiles(t, root)
}

func TestProjectDBJSONFailureLeavesSidecarUntouched(t *testing.T) {
	root := t.TempDir()
	first := []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}
	if err := SaveProjectDBConnections(root, first); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ProjectDBEnvFilesPath(root))
	if err != nil {
		t.Fatal(err)
	}

	blockPathWithDir(t, ProjectDBConfigPath(root))
	second := []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.production",
	}}
	if err := SaveProjectDBConnections(root, second); err == nil {
		t.Fatal("want an error when db.json cannot be written")
	}
	after, err := os.ReadFile(ProjectDBEnvFilesPath(root))
	if err != nil {
		t.Fatalf("sidecar disappeared: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("sidecar changed although db.json was not saved:\nbefore=%s\nafter=%s", before, after)
	}
	assertNoOcodeTempFiles(t, root)
}

func TestRecreatedProjectConnectionDoesNotReuseEnvFile(t *testing.T) {
	root := t.TempDir()
	if err := SaveProjectDBConnections(root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}); err != nil {
		t.Fatal(err)
	}
	// The connection is deleted and re-created under the same name, pointing at
	// a different database. The stale sidecar entry is still on disk.
	writeProjectDBJSON(t, root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${REPORTS_DATABASE_URL}",
	}})

	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EnvFile != "" {
		t.Fatalf("re-created connection inherited EnvFile %q, want none", got[0].EnvFile)
	}
	// The next save drops the stale entry.
	if err := SaveProjectDBConnections(root, got); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ProjectDBEnvFilesPath(root)); !os.IsNotExist(err) {
		t.Fatalf("stale sidecar survived the next save: %v", err)
	}
}

func TestUnchangedProjectConnectionKeepsEnvFileAfterRewrite(t *testing.T) {
	root := t.TempDir()
	conn := DBConnection{Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging"}
	if err := SaveProjectDBConnections(root, []DBConnection{conn}); err != nil {
		t.Fatal(err)
	}
	stripped := conn
	stripped.EnvFile = ""
	writeProjectDBJSON(t, root, []DBConnection{stripped})

	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EnvFile != ".env.staging" {
		t.Fatalf("unchanged connection EnvFile = %q, want .env.staging", got[0].EnvFile)
	}
}

func TestProjectConnectionOwnEnvFileWinsOverSidecar(t *testing.T) {
	root := t.TempDir()
	if err := SaveProjectDBConnections(root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}); err != nil {
		t.Fatal(err)
	}
	// Same identity, so the sidecar entry matches, but db.json carries its own
	// selection, which must be kept.
	writeProjectDBJSON(t, root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.production",
	}})

	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EnvFile != ".env.production" {
		t.Fatalf("EnvFile = %q, want db.json's .env.production", got[0].EnvFile)
	}
}

func TestProjectMalformedSidecarIsIgnored(t *testing.T) {
	root := t.TempDir()
	if err := SaveProjectDBConnections(root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}}); err != nil {
		t.Fatal(err)
	}
	// db.json lost env_file (a pre-env_file binary rewrote it), so the sidecar
	// is the only source, and it is torn.
	writeProjectDBJSON(t, root, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}",
	}})
	if err := os.WriteFile(ProjectDBEnvFilesPath(root), []byte(`{"staging": `), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProjectDBConnections(root)
	if err != nil {
		t.Fatalf("a torn sidecar must not fail the load: %v", err)
	}
	if len(got) != 1 || got[0].Name != "staging" || got[0].EnvFile != "" {
		t.Fatalf("got %+v, want the connection with no EnvFile", got)
	}
}

func TestReconcileDBEnvFilesRejectsInvalidStoredName(t *testing.T) {
	conn := DBConnection{Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}"}
	conns := []DBConnection{conn}
	reconcileDBEnvFiles(conns, map[string]dbEnvFileEntry{
		"staging": {File: "../secrets", Fingerprint: dbConnectionFingerprint(conn)},
	})
	if conns[0].EnvFile != "" {
		t.Fatalf("traversal name applied: EnvFile = %q", conns[0].EnvFile)
	}
}

func TestGlobalDBEnvFileNotReusedByRecreatedConnection(t *testing.T) {
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
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}", EnvFile: ".env.staging",
	}, cur.DB.Connections); err != nil {
		t.Fatal(err)
	}

	// A legacy binary deletes the connection and re-creates it under the same
	// name with another template, leaving the env-files map untouched.
	rewriteGlobalDBSection(t, path, []DBConnection{{
		Name: "staging", Driver: DBDriverPostgres, EnvTemplate: "${REPORTS_DATABASE_URL}",
	}})

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DB.Connections) != 1 || cfg.DB.Connections[0].EnvFile != "" {
		t.Fatalf("re-created global connection inherited EnvFile %q, want none", cfg.DB.Connections[0].EnvFile)
	}
}

// rewriteGlobalDBSection replaces the db section and the env-template map of
// ocodeconfig.json, leaving every other key (including the env-files map) as is.
func rewriteGlobalDBSection(t *testing.T, path string, conns []DBConnection) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	db, err := json.Marshal(DBConfig{Connections: conns})
	if err != nil {
		t.Fatal(err)
	}
	raw["db"] = db
	tmpl, err := json.Marshal(dbConnectionTemplates(conns))
	if err != nil {
		t.Fatal(err)
	}
	raw[DBEnvTemplatesKey] = tmpl
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
