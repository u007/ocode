package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dbIDEInfo calls the SQLite info endpoint for a path and returns the
// recorder.
func dbIDEInfo(t *testing.T, h *Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleDBInfo(rec, httptest.NewRequest(http.MethodGet, "/api/db/info?"+query, nil))
	return rec
}

func TestDBIDEPathExpandsEnvironmentReference(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "app.db"), "CREATE TABLE t(id INTEGER);")

	// The variable names the file inside the project.
	t.Setenv("OCODE_TEST_DB_FILE", "app.db")

	rec := dbIDEInfo(t, h, "path="+urlEscape("${OCODE_TEST_DB_FILE}"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"is_sqlite":true`) {
		t.Fatalf("body = %s, want a sqlite database", rec.Body.String())
	}
}

func TestDBIDEPathExpansionUsesProjectEnvFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "app.db"), "CREATE TABLE t(id INTEGER);")
	// A .env in the project names the file.
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("OCODE_TEST_DB_FILE=app.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := dbIDEInfo(t, h, "path="+urlEscape("${OCODE_TEST_DB_FILE}")+"&project_root="+urlEscape(tmpDir))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDBIDEPathExpansionCannotEscapeTheAllowedRoots(t *testing.T) {
	// The security property: an expanded path is checked against the allowed
	// roots exactly like a literal one. Expansion must not become a way to
	// address a file outside the project.
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.db")
	seedDB(t, secret, "CREATE TABLE t(id INTEGER);")

	t.Setenv("OCODE_TEST_DB_FILE", secret)

	rec := dbIDEInfo(t, h, "path="+urlEscape("${OCODE_TEST_DB_FILE}"))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a refusal for a path outside the allowed roots (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "outside the allowed project roots") {
		t.Fatalf("body = %s, want the containment refusal", rec.Body.String())
	}
}

func TestDBIDEPathExpansionMissingVariableIsRefused(t *testing.T) {
	h, _ := newFilesHandler(t)

	rec := dbIDEInfo(t, h, "path="+urlEscape("${OCODE_TEST_ABSENT_DB_FILE}"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OCODE_TEST_ABSENT_DB_FILE") {
		t.Fatalf("body = %s, want the error to name the missing variable", rec.Body.String())
	}
}

func TestDBIDELiteralPathIsUnchanged(t *testing.T) {
	// A path with no reference must behave exactly as before.
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "app.db"), "CREATE TABLE t(id INTEGER);")

	rec := dbIDEInfo(t, h, "path=app.db")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDBIDEDollarInPathIsNotAReference(t *testing.T) {
	// A "$" that does not begin a variable name is literal text, not a
	// reference: the path must not be expanded or rejected.
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "we$ird.db"), "CREATE TABLE t(id INTEGER);")

	rec := dbIDEInfo(t, h, "path="+urlEscape("we$ird.db"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// urlEscape percent-encodes a path for a query string.
func urlEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteString("%")
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}
