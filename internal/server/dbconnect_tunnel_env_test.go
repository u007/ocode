package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The process environment holds a secret. A tunnel field that names it must
// never resolve from there, in any unlock outcome.
const (
	dbTunnelSecretVar   = "OCODE_TEST_SECRET"
	dbTunnelSecretValue = "s3cret"
)

func assertNoTunnelSecret(t *testing.T, where, s string) {
	t.Helper()
	if strings.Contains(s, dbTunnelSecretValue) {
		t.Fatalf("%s leaks the process-env secret: %s", where, s)
	}
}

func saveDBConnectionForTest(t *testing.T, c config.DBConnection) {
	t.Helper()
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DB.Connections = append(cfg.DB.Connections, c)
	if err := config.SaveOcodeConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

// A connection a cloned repo ships in .ocode/db.json: a ${VAR} tunnel host and
// no env file. It must be refused rather than resolved from the process env.
const dbTunnelRepoConnJSON = `{"connections":[{"name":"ship","driver":"postgres",` +
	`"env_template":"${DATABASE_URL}",` +
	`"tunnel":{"host":"${OCODE_TEST_SECRET}","remote_host":"db.internal","remote_port":5432}}]}`

func TestDBUnlockRefusesTunnelEnvWithoutEnvFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbTunnelSecretVar, dbTunnelSecretValue)
	t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/db")

	p := config.ProjectDBConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(dbTunnelRepoConnJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-noenv", "project_path": root,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d body=%s; want 400 when nothing can be granted", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dbTunnelNeedsEnvFileMsg) {
		t.Fatalf("unlock body = %s, want the env-file refusal", rec.Body.String())
	}
	assertNoTunnelSecret(t, "unlock response", rec.Body.String())
	h.dbMu.Lock()
	_, granted := h.dbGrants["s-noenv"]["ship"]
	h.dbMu.Unlock()
	if granted {
		t.Fatal("a tunnel with ${VAR} and no env file was granted")
	}
}

// A selected env file that does not define the variable must leave it unset,
// even though the process environment exports it.
func TestDBUnlockTunnelEnvFileDoesNotFallBackToProcessEnv(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbTunnelSecretVar, dbTunnelSecretValue)
	body := "DATABASE_URL=postgres://u:p@db.internal:5432/db\nSSH_HOST=bastion.example\n"
	if err := os.WriteFile(filepath.Join(root, ".env.staging"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	saveDBConnectionForTest(t, config.DBConnection{
		Name: "staging", Driver: config.DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".staging",
		Tunnel: &config.DBTunnel{Host: "${" + dbTunnelSecretVar + "}", RemoteHost: "db.internal", RemotePort: 5432},
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-fallback", "project_path": root,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d body=%s; want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dbTunnelSecretVar) {
		t.Fatalf("unlock body = %s, want the unset variable named", rec.Body.String())
	}
	assertNoTunnelSecret(t, "unlock response", rec.Body.String())
	h.dbMu.Lock()
	entry, granted := h.dbGrants["s-fallback"]["staging"]
	h.dbMu.Unlock()
	if granted {
		t.Fatalf("tunnel resolved from the process env: %+v", entry.tunnel)
	}
}

// A variable the selected env file defines resolves from that file, and the
// process env value is ignored.
func TestDBUnlockTunnelEnvResolvesFromNamedEnvFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbTunnelSecretVar, dbTunnelSecretValue)
	body := "DATABASE_URL=postgres://u:p@db.internal:5432/db\n" + dbTunnelSecretVar + "=bastion.example\n"
	if err := os.WriteFile(filepath.Join(root, ".env.staging"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	saveDBConnectionForTest(t, config.DBConnection{
		Name: "staging", Driver: config.DBDriverPostgres,
		EnvTemplate: "${DATABASE_URL}", EnvFile: ".staging",
		Tunnel: &config.DBTunnel{Host: "${" + dbTunnelSecretVar + "}", RemoteHost: "db.internal", RemotePort: 5432},
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-file", "project_path": root,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	assertNoTunnelSecret(t, "unlock response", rec.Body.String())
	h.dbMu.Lock()
	entry := h.dbGrants["s-file"]["staging"]
	h.dbMu.Unlock()
	if !entry.hasTunnel || entry.tunnel.Host != "bastion.example" {
		t.Fatalf("grant tunnel = %+v (hasTunnel=%v), want host from the env file", entry.tunnel, entry.hasTunnel)
	}
	assertNoTunnelSecret(t, "grant tunnel", entry.tunnel.Host)
}

// The helper itself: no env file means nothing resolves and the caller is told
// why; a named file never reads the process environment.
func TestDBTunnelEnvLookupNeverReadsProcessEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dbTunnelSecretVar, dbTunnelSecretValue)

	lookup, envErr := dbTunnelEnvLookup(dir, "")
	if v, ok := lookup(dbTunnelSecretVar); ok {
		t.Fatalf("no env file: lookup = %q, true; want unset", v)
	}
	if err := envErr(); err == nil || err.Error() != dbTunnelNeedsEnvFileMsg {
		t.Fatalf("no env file: envErr = %v, want %q", err, dbTunnelNeedsEnvFileMsg)
	}

	if err := os.WriteFile(filepath.Join(dir, ".env.staging"), []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lookup, envErr = dbTunnelEnvLookup(dir, ".staging")
	if err := envErr(); err != nil {
		t.Fatalf("named file: envErr = %v", err)
	}
	if v, ok := lookup(dbTunnelSecretVar); ok {
		t.Fatalf("named file without the key: lookup = %q, true; want unset", v)
	}
}
