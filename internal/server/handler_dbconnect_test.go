package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

const dbTestURL = "postgres://app:TOPSECRET-pw@127.0.0.1:1/app?sslmode=disable"

func dbPostJSON(t *testing.T, h http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec
}

func dbListNames(t *testing.T, h *Handler, surface string) map[string]bool {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleDBConnectList(rec, httptest.NewRequest(http.MethodGet, "/api/dbconnect/connections?surface="+surface, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Connections []struct {
			Name     string `json:"name"`
			Unlocked bool   `json:"unlocked"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	names := map[string]bool{}
	for _, c := range out.Connections {
		names[c.Name] = c.Unlocked
	}
	return names
}

func TestDBConnectAddListRemoveKeepsURLOutOfResponses(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Fatalf("add response leaked the password: %s", rec.Body.String())
	}

	names := dbListNames(t, h, "s1")
	if _, ok := names["prod"]; !ok || names["prod"] {
		t.Fatalf("list = %v, want prod present and locked", names)
	}

	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DB.Connections) != 1 || strings.Contains(cfg.DB.Connections[0].EncryptedURL, "TOPSECRET") {
		t.Fatalf("stored connection = %+v; want one sealed entry with no plaintext", cfg.DB.Connections)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/dbconnect/connections/{name}", h.HandleDBConnectRemove)
	del := httptest.NewRecorder()
	mux.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/dbconnect/connections/prod", nil))
	if del.Code != http.StatusOK {
		t.Fatalf("remove = %d body=%s", del.Code, del.Body.String())
	}
	if names := dbListNames(t, h, "s1"); len(names) != 0 {
		t.Fatalf("after remove list = %v, want empty", names)
	}
}

func TestDBConnectAddRejectsBadURLWithoutEchoingIt(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}

	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "bad", "url": "postgres://app:TOPSECRET-pw@[bad", "password": "master-pw"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add bad url = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Fatalf("bad-url response echoed the password: %s", rec.Body.String())
	}
}

func TestDBConnectAddRequiresSameMasterPassword(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "pw-a"}); rec.Code != http.StatusCreated {
		t.Fatalf("add prod = %d body=%s", rec.Code, rec.Body.String())
	}

	mismatch := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "stage", "url": dbTestURL, "password": "pw-b"})
	if mismatch.Code != http.StatusUnauthorized {
		t.Fatalf("add stage with other password = %d, want 401 (body=%s)", mismatch.Code, mismatch.Body.String())
	}

	if names := dbListNames(t, h, "s1"); len(names) != 1 {
		t.Fatalf("after rejected add list = %v, want only prod", names)
	}
	unlock := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "pw-a"})
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock with original password = %d body=%s", unlock.Code, unlock.Body.String())
	}
}

func TestDBConnectUnlockLockAndLockedAccessRefused(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	wrong := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "wrong-pw"})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("unlock wrong password = %d, want 401 (body=%s)", wrong.Code, wrong.Body.String())
	}
	if names := dbListNames(t, h, "s1"); names["prod"] {
		t.Fatal("wrong password left a grant behind")
	}

	ok := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "master-pw"})
	if ok.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", ok.Code, ok.Body.String())
	}
	if names := dbListNames(t, h, "s1"); !names["prod"] {
		t.Fatal("unlocked connection not reported as unlocked for its surface")
	}
	if names := dbListNames(t, h, "s2"); names["prod"] {
		t.Fatal("grant leaked to another surface")
	}

	lock := dbPostJSON(t, h.HandleDBConnectLock, "/api/dbconnect/lock", map[string]string{"surface": "s1"})
	if lock.Code != http.StatusOK {
		t.Fatalf("lock = %d body=%s", lock.Code, lock.Body.String())
	}
	if names := dbListNames(t, h, "s1"); names["prod"] {
		t.Fatal("lock did not drop the grant")
	}

	tables := httptest.NewRecorder()
	h.HandleDBConnectTables(tables, httptest.NewRequest(http.MethodGet,
		"/api/dbconnect/tables?surface=s1&connection=prod", nil))
	if tables.Code != http.StatusForbidden {
		t.Fatalf("tables while locked = %d, want 403", tables.Code)
	}
	query := dbPostJSON(t, h.HandleDBConnectQuery, "/api/dbconnect/query",
		map[string]string{"surface": "s1", "connection": "prod", "sql": "SELECT 1"})
	if query.Code != http.StatusForbidden {
		t.Fatalf("query while locked = %d, want 403", query.Code)
	}
}

// TestDBConnectQueryAsksBeforeServerSideEffect: a statement PostgreSQL runs in a
// READ ONLY transaction but that still acts on the server gets the confirmation
// 409 unconfirmed. The grant points at a closed port, so an attempt to run the
// statement would fail with 502 instead of 409.
func TestDBConnectQueryAsksBeforeServerSideEffect(t *testing.T) {
	h := &Handler{}
	h.dbSetGrant("s1", dbGrant{"prod": dbTestURL})
	for _, sql := range []string{
		"SELECT pg_terminate_backend(1)",
		"SELECT pg_cancel_backend(1)",
		"NOTIFY chan",
		"select pg_try_advisory_lock(7)",
		"SELECT pg_advisory_lock(7)",
		"SELECT pg_sleep(60)",
		"SET ROLE postgres",
		"SET SESSION AUTHORIZATION postgres",
		"SELECT set_config('role', 'postgres', false)",
	} {
		rec := dbPostJSON(t, h.HandleDBConnectQuery, "/api/dbconnect/query",
			map[string]any{"surface": "s1", "connection": "prod", "sql": sql})
		if rec.Code != http.StatusConflict {
			t.Errorf("%q: status = %d, want 409 (body=%s)", sql, rec.Code, rec.Body.String())
		}
	}
}

// TestDBGrantPoolLifecycle: a granted connection keeps one pooled handle, and
// dropping the connection, replacing the grant or locking the surface closes it.
// The handle is planted so the test needs no server; a closed pool reports
// "database is closed" instead of a connection error.
func TestDBGrantPoolLifecycle(t *testing.T) {
	plant := func(h *Handler) *sql.DB {
		t.Helper()
		pool, err := sql.Open("pgx", dbTestURL)
		if err != nil {
			t.Fatal(err)
		}
		h.dbSetGrant("s1", dbGrant{"prod": dbTestURL})
		h.dbPools = map[string]map[string]*sql.DB{"s1": {"prod": pool}}
		return pool
	}
	assertClosed := func(t *testing.T, pool *sql.DB, why string) {
		t.Helper()
		err := pool.PingContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "database is closed") {
			t.Fatalf("%s: pool still open (ping err = %v)", why, err)
		}
	}

	h := &Handler{}
	pool := plant(h)
	got, granted, err := h.dbGrantDB(context.Background(), "s1", "prod")
	if err != nil || !granted || got != pool {
		t.Fatalf("cached handle not reused: same=%v granted=%v err=%v", got == pool, granted, err)
	}

	h.dbDropConnection("prod")
	assertClosed(t, pool, "removed connection")

	pool = plant(h)
	h.dbSetGrant("s1", dbGrant{"prod": dbTestURL})
	assertClosed(t, pool, "replaced grant")

	pool = plant(h)
	h.dbDropSurface("s1")
	assertClosed(t, pool, "locked surface")
	if _, granted, _ := h.dbGrantDB(context.Background(), "s1", "prod"); granted {
		t.Fatal("locked surface still reports a grant")
	}
}
