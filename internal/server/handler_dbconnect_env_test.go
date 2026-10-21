package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/dbconnect"
)

// dbEnvHandler returns a Handler with a session registry, so a test can bind
// a session id to a project root.
func dbEnvHandler(t *testing.T) *Handler {
	t.Helper()
	h := &Handler{}
	h.sessions = NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	return h
}

// dbEnvFile writes a dotenv file into dir and returns dir.
func dbEnvFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return dir
}

// dbUnlockSession unlocks with a session id and returns the recorder.
func dbUnlockSession(t *testing.T, h *Handler, surface, password, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	return dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": surface, "password": password, "session_id": sessionID})
}

func TestDBConnectAddStoresTemplateInTheClear(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${DATABASE_URL}", "password": "master-pw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DB.Connections) != 1 {
		t.Fatalf("stored %d connections, want 1", len(cfg.DB.Connections))
	}
	c := cfg.DB.Connections[0]
	if c.EnvTemplate != "${DATABASE_URL}" {
		t.Errorf("EnvTemplate = %q, want ${DATABASE_URL}", c.EnvTemplate)
	}
	if c.EncryptedURL != "" {
		t.Errorf("EncryptedURL = %q, want empty for a template connection", c.EncryptedURL)
	}
}

func TestDBConnectAddRejectsURLThatIsNeitherSecretNorTemplate(t *testing.T) {
	// A URL with a "$" that is not a variable reference is still a literal
	// URL and is sealed, not treated as a template.
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Connections[0].EnvTemplate != "" {
		t.Errorf("EnvTemplate = %q, want empty for a literal URL", cfg.DB.Connections[0].EnvTemplate)
	}
	if cfg.DB.Connections[0].EncryptedURL == "" {
		t.Error("a literal URL must still be sealed")
	}
}

func TestDBConnectAddTreatsALiteralDollarInThePasswordAsALiteral(t *testing.T) {
	// A Postgres password may contain "$". If that made the URL a template,
	// the connection would stop working (or fail to seal) for a URL that has
	// nothing to do with the environment.
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	const url = "postgres://app:p$ssword@127.0.0.1:1/app"
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": url, "password": "master-pw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Connections[0].EnvTemplate != "" {
		t.Errorf("EnvTemplate = %q, want empty: a literal password is not a template", cfg.DB.Connections[0].EnvTemplate)
	}
	if cfg.DB.Connections[0].EncryptedURL == "" {
		t.Error("a literal url with a dollar in the password must still be sealed")
	}
}

func TestDBConnectAddSealsPasswordThatNamesAProcessEnvVar(t *testing.T) {
	// HOME is always set in the process environment. A classifier that
	// consulted it would call "pa$HOME" a template and save the password in
	// plaintext. It must be sealed like any other literal URL.
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	const url = "postgres://app:pa$HOME@db/prod"
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": url, "password": "master-pw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.DB.Connections[0]
	if c.EnvTemplate != "" {
		t.Fatalf("EnvTemplate = %q, want empty: the password is a literal, not a template", c.EnvTemplate)
	}
	if c.EncryptedURL == "" {
		t.Fatal("a literal url with a bare name in the password must be sealed")
	}
	opened, err := dbconnect.OpenURL(c.EncryptedURL, "master-pw")
	if err != nil {
		t.Fatal(err)
	}
	if opened != url {
		t.Fatalf("opened url = %q, want %q", opened, url)
	}
}

func TestDBConnectUnlockResolvesTemplateFromProjectEnvFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "DATABASE_URL=postgres://app:TOPSECRET-pw@127.0.0.1:1/app\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${DATABASE_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	// Bind the session to the project so the unlock can find its .env.
	h.sessions.Register("sess-1", project)

	rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	// The resolved URL is a credential: it must never reach the response.
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Fatalf("unlock response leaked the resolved url: %s", rec.Body.String())
	}
	// But the grant holds it, so a later request can use it.
	url, ok := h.dbGrantURL("s1", "env")
	if !ok {
		t.Fatal("template connection was not granted")
	}
	if url != "postgres://app:TOPSECRET-pw@127.0.0.1:1/app" {
		t.Fatalf("granted url = %q", url)
	}
}

func TestDBConnectUnlockTemplatePrefersProjectEnvOverProcessEnv(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=postgres://app:from-file@127.0.0.1:1/app\n")
	// A process value must WIN over the file.
	t.Setenv("OCODE_TEST_DB_URL", "postgres://app:from-process@127.0.0.1:1/app")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	if rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1"); rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	url, ok := h.dbGrantURL("s1", "env")
	if !ok {
		t.Fatal("template connection was not granted")
	}
	if !strings.Contains(url, "from-process") {
		t.Fatalf("granted url = %q, want the process value to win over the file", url)
	}
}

func TestDBConnectUnlockTemplatePrefersEnvLocalOverEnv(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=postgres://app:from-dotenv@127.0.0.1:1/app\n")
	dbEnvFile(t, project, ".env.local", "OCODE_TEST_DB_URL=postgres://app:from-dotenv-local@127.0.0.1:1/app\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	if rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1"); rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	url, _ := h.dbGrantURL("s1", "env")
	if !strings.Contains(url, "from-dotenv-local") {
		t.Fatalf("granted url = %q, want .env.local to win over .env", url)
	}
}

func TestDBConnectUnlockTemplateResolvesPerProject(t *testing.T) {
	// Two projects, two different databases, one saved connection.
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	projA := t.TempDir()
	projB := t.TempDir()
	dbEnvFile(t, projA, ".env", "OCODE_TEST_DB_URL=postgres://app:project-a@127.0.0.1:1/a\n")
	dbEnvFile(t, projB, ".env", "OCODE_TEST_DB_URL=postgres://app:project-b@127.0.0.1:1/b\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-a", projA)
	h.sessions.Register("sess-b", projB)

	if rec := dbUnlockSession(t, h, "sA", "master-pw", "sess-a"); rec.Code != http.StatusOK {
		t.Fatalf("unlock A = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := dbUnlockSession(t, h, "sB", "master-pw", "sess-b"); rec.Code != http.StatusOK {
		t.Fatalf("unlock B = %d body=%s", rec.Code, rec.Body.String())
	}
	urlA, _ := h.dbGrantURL("sA", "env")
	urlB, _ := h.dbGrantURL("sB", "env")
	if !strings.Contains(urlA, "project-a") {
		t.Errorf("surface A url = %q, want project-a", urlA)
	}
	if !strings.Contains(urlB, "project-b") {
		t.Errorf("surface B url = %q, want project-b", urlB)
	}
}

func TestDBConnectUnlockTemplateMissingVariableIsRefused(t *testing.T) {
	// An unresolved variable must fail loudly. A silent empty string would
	// produce a plausible-looking but wrong connection.
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_ABSENT_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OCODE_TEST_ABSENT_DB_URL") {
		t.Errorf("error %q does not name the missing variable", rec.Body.String())
	}
	if _, ok := h.dbGrantURL("s1", "env"); ok {
		t.Error("a failed template resolution left a grant behind")
	}
}

func TestDBConnectUnlockWrongPasswordIsRejectedBeforeAnyTemplateResolves(t *testing.T) {
	// The master password gates the whole set. A wrong password must be
	// rejected on the sealed connection, and must NOT be masked by a
	// template that happens to fail to resolve: the user needs to know the
	// password is wrong.
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=postgres://app:pw@127.0.0.1:1/app\n")

	// A sealed connection, so the password has something to be checked
	// against.
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add prod = %d body=%s", rec.Code, rec.Body.String())
	}
	// A template connection whose variable is NOT set.
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_ABSENT_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add env = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	rec := dbUnlockSession(t, h, "s1", "wrong-pw", "sess-1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unlock with a wrong password = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := h.dbGrantURL("s1", "prod"); ok {
		t.Error("a wrong password left a grant behind")
	}
}

func TestDBConnectUnlockTemplateResolvingToInvalidURLIsRefused(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=not-a-postgres-url\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := h.dbGrantURL("s1", "env"); ok {
		t.Error("a failed template resolution left a grant behind")
	}
}

func TestDBConnectUnlockTemplateErrorDoesNotLeakTheValue(t *testing.T) {
	// A resolved-but-invalid URL must not be echoed: the value can hold a
	// password, and ParseURL's own message is deliberately URL-free.
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=postgres://app:TOPSECRET-pw@[bad\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	rec := dbUnlockSession(t, h, "s1", "master-pw", "sess-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Fatalf("error response leaked the resolved value: %s", rec.Body.String())
	}
}

func TestDBConnectUnlockTemplateStillRequiresTheMasterPassword(t *testing.T) {
	// A template-only set has nothing to decrypt, but the password is still
	// required: the flow stays uniform and a guessed connection name is not
	// enough to open it.
	setHomeTree(t, t.TempDir())
	h := dbEnvHandler(t)
	project := t.TempDir()
	dbEnvFile(t, project, ".env", "OCODE_TEST_DB_URL=postgres://app:pw@127.0.0.1:1/app\n")

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${OCODE_TEST_DB_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.Register("sess-1", project)

	// An empty password is refused.
	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "", "session_id": "sess-1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock with no password = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := h.dbGrantURL("s1", "env"); ok {
		t.Error("an unlock without a password left a grant behind")
	}
}

func TestDBConnectListReportsEnvBackedConnection(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "env", "url": "${DATABASE_URL}", "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add prod = %d body=%s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	h.HandleDBConnectList(rec, httptest.NewRequest(http.MethodGet, "/api/dbconnect/connections?surface=s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Connections []struct {
			Name    string `json:"name"`
			FromEnv bool   `json:"from_env"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, c := range out.Connections {
		byName[c.Name] = c.FromEnv
	}
	if !byName["env"] {
		t.Error("the template connection is not reported as env-backed")
	}
	if byName["prod"] {
		t.Error("a literal connection is reported as env-backed")
	}
	// The template itself must not be returned: it names variables, but the
	// resolved URL is a credential.
	if strings.Contains(rec.Body.String(), "DATABASE_URL") {
		t.Fatalf("list response leaked the template: %s", rec.Body.String())
	}
}
