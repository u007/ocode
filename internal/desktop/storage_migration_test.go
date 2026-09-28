package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrationReq(method, token, body string) *http.Request {
	r := httptest.NewRequest(method, StorageMigrationPath, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestNewStorageMigrationPendingOnlyOnSavedPortWithoutMarker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", dir)
	marker := filepath.Join(dir, storageMigrationMarker)

	if m := newStorageMigration(5000, 5001, "tok"); m != nil {
		t.Fatal("drifted port must not migrate (old origin lives on the saved port)")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("drifted port must leave the marker unwritten, stat err = %v", err)
	}
	if m := newStorageMigration(5000, 5000, "tok"); m == nil {
		t.Fatal("saved port without marker must migrate")
	}

	if m := newStorageMigration(0, 5000, "tok"); m != nil {
		t.Fatal("first-ever launch has no old origin to migrate")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("first-ever launch must write the marker: %v", err)
	}
	if m := newStorageMigration(5000, 5000, "tok"); m != nil {
		t.Fatal("marker present must skip migration")
	}
}

func TestStorageMigrationOneShotHandoff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", dir)
	m := newStorageMigration(5000, 5000, "tok")
	if m == nil {
		t.Fatal("expected pending migration")
	}

	for _, tc := range []struct{ name, token string }{{"missing", ""}, {"wrong", "nope"}} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, migrationReq(http.MethodPost, tc.token, `{}`))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s token: status %d, want 401", tc.name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, migrationReq(http.MethodPost, "tok", `{"a":1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-string values: status %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, migrationReq(http.MethodGet, "tok", ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET before export: status %d, want 204", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, storageMigrationMarker)); !os.IsNotExist(err) {
		t.Fatal("an empty import must not write the marker (next launch retries)")
	}

	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, migrationReq(http.MethodPost, "tok", `{"ocode.ui.tabs.v1":"[1]"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("export: status %d, body %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, migrationReq(http.MethodGet, "tok", ""))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ocode.ui.tabs.v1":"[1]"}` {
		t.Fatalf("import: status %d, body %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, storageMigrationMarker)); err != nil {
		t.Fatalf("consuming the import must write the marker: %v", err)
	}

	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, migrationReq(http.MethodGet, "tok", ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second import: status %d, want 204 (one-shot)", rec.Code)
	}
}
