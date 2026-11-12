package server

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/dbconnect"
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

// dbRemoveVia sends one DELETE through the real route, so the {name} path value
// is populated the way the server populates it.
func dbRemoveVia(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/dbconnect/connections/{name}", h.HandleDBConnectRemove)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, target, nil))
	return rec
}

// A remote project's panel lists the global connections too, and it sends
// project_path on remove. The project has no .ocode/db.json at all, so the
// removal must reach the global config instead of answering 404.
func TestDBConnectRemoveWithProjectPathFallsThroughToGlobal(t *testing.T) {
	setHomeTree(t, t.TempDir())
	project := t.TempDir()
	h := dbEnvProjectHandler(t, project)

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	rec := dbRemoveVia(t, h, "/api/dbconnect/connections/prod?project_path="+url.QueryEscape(project))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DB.Connections) != 0 {
		t.Fatalf("global connections after remove = %+v, want none", cfg.DB.Connections)
	}
}

func TestDBConnectRemoveWithProjectPathNotFoundAnywhereIs404(t *testing.T) {
	setHomeTree(t, t.TempDir())
	project := t.TempDir()
	h := dbEnvProjectHandler(t, project)

	rec := dbRemoveVia(t, h, "/api/dbconnect/connections/nope?project_path="+url.QueryEscape(project))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("remove = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// The fall-through must not bypass root validation: a project_path outside the
// allowed roots is refused before any config is read or changed.
func TestDBConnectRemoveWithForeignProjectPathStillRefused(t *testing.T) {
	setHomeTree(t, t.TempDir())
	project := t.TempDir()
	h := dbEnvProjectHandler(t, project)

	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "prod", "url": dbTestURL, "password": "master-pw"}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	foreign := t.TempDir()
	rec := dbRemoveVia(t, h, "/api/dbconnect/connections/prod?project_path="+url.QueryEscape(foreign))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("remove with a foreign project = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DB.Connections) != 1 {
		t.Fatalf("a refused remove changed the global config: %+v", cfg.DB.Connections)
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
	h.dbSetGrant("s1", dbGrant{"prod": {url: dbTestURL}})
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
		h.dbSetGrant("s1", dbGrant{"prod": {url: dbTestURL}})
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
	h.dbSetGrant("s1", dbGrant{"prod": {url: dbTestURL}})
	assertClosed(t, pool, "replaced grant")

	pool = plant(h)
	h.dbDropSurface("s1")
	assertClosed(t, pool, "locked surface")
	if _, granted, _ := h.dbGrantDB(context.Background(), "s1", "prod"); granted {
		t.Fatal("locked surface still reports a grant")
	}
}

func TestDBConnectAddRejectsUnknownDriver(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "badconn", "url": "postgres://u:p@host/db", "password": "mp", "driver": "oracle"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported db driver") {
		t.Fatalf("body = %s, want unsupported db driver", rec.Body.String())
	}
}

func TestDBConnectAddAcceptsMySQLDriver(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "mysqlconn", "url": "mysql://u:p@host/db", "password": "mp", "driver": "mysql"})
	// sealing requires a real password-derived key; with an invalid/short
	// master password the handler may still 400 on seal, so only assert it
	// does not reject the driver itself.
	if rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "unsupported db driver") {
		t.Fatalf("driver rejected: %s", rec.Body.String())
	}
}

func TestRewriteURLForTunnelMySQL(t *testing.T) {
	cases := []struct {
		in   string
		port int
		want string
	}{
		{"mysql://u:p%40ss@host/db", 1234, "mysql://u:p%40ss@127.0.0.1:1234/db"},
		{"mysql://u:p@host:3306/db", 9999, "mysql://u:p@127.0.0.1:9999/db"},
	}
	for _, c := range cases {
		got, err := rewriteURLForTunnel(c.in, c.port)
		if err != nil {
			t.Fatalf("rewriteURLForTunnel(%q,%d): %v", c.in, c.port, err)
		}
		if got != c.want {
			t.Errorf("rewriteURLForTunnel(%q,%d) = %q, want %q", c.in, c.port, got, c.want)
		}
	}
}

// stubDBEnsureTunnel replaces the tunnel seam for one test and restores it.
func stubDBEnsureTunnel(t *testing.T, fn func(context.Context, string, *config.DBTunnel, string) (int, error)) {
	t.Helper()
	old := dbEnsureTunnel
	dbEnsureTunnel = fn
	t.Cleanup(func() { dbEnsureTunnel = old })
}

// TestRewriteURLForTunnelKeepsCredentialsByteExact: only host:port changes. The
// escaped and unescaped reserved characters in the userinfo, and the query, are
// copied as they were; a url.URL re-encode is what this guards against.
func TestRewriteURLForTunnelKeepsCredentialsByteExact(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"escaped at colon slash percent",
			"postgres://app:p%40ss%3Aw%2Frd%2521@db.internal:5432/app",
			"postgres://app:p%40ss%3Aw%2Frd%2521@127.0.0.1:9999/app"},
		{"escaped password decodes to the literal characters",
			"postgres://app:p%40ss%3Aw%2Frd%2521@db.internal:5432/app",
			"postgres://app:p%40ss%3Aw%2Frd%2521@127.0.0.1:9999/app"},
		{"plus and equals survive",
			"postgres://app:a%2Bb%3Dc@db.internal:5432/app?sslmode=disable",
			"postgres://app:a%2Bb%3Dc@127.0.0.1:9999/app?sslmode=disable"},
		{"no userinfo",
			"postgres://db.internal:5432/app?sslmode=require",
			"postgres://127.0.0.1:9999/app?sslmode=require"},
		{"mysql with query",
			"mysql://u:p%2Bq@db.internal:3306/app?parseTime=true",
			"mysql://u:p%2Bq@127.0.0.1:9999/app?parseTime=true"},
		{"unescaped at inside password resolves to the last at",
			"postgres://app:p@ss@db.internal:5432/app",
			"postgres://app:p@ss@127.0.0.1:9999/app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rewriteURLForTunnel(c.in, 9999)
			if err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			// The decoded password must survive too. url.URL.String would emit
			// "a+b=c" for "a%2Bb%3Dc", so the byte comparison above is what
			// catches a re-encode.
			inURL, _ := url.Parse(c.in)
			gotURL, _ := url.Parse(got)
			if inURL.User.String() != gotURL.User.String() {
				t.Fatalf("userinfo changed: %q -> %q", inURL.User.String(), gotURL.User.String())
			}
		})
	}
}

// TestRewriteURLForTunnelRefusesUnroutableURL: when the URL cannot be rewritten,
// the caller must get an error, never the original URL (that would connect past
// the tunnel). The error must not echo the URL, which carries the password.
func TestRewriteURLForTunnelRefusesUnroutableURL(t *testing.T) {
	const secret = "SECRETPW-x"
	for _, in := range []string{
		// Unescaped reserved characters in the password: the parser reads the
		// host as "ss:w", so the port is invalid.
		"postgres://app:p@ss:w/rd%21@db.internal:5432/app",
		"postgres://app:" + secret + "@db.internal:notaport/app",
		"db.internal:5432/app",
	} {
		got, err := rewriteURLForTunnel(in, 9999)
		if err == nil {
			t.Fatalf("rewriteURLForTunnel(%q) = %q, want an error", in, got)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "db.internal") {
			t.Fatalf("error leaks the url: %v", err)
		}
	}
}

// TestDBConnectAddExplicitDriverWinsForEnvTemplate: "${DATABASE_URL}" does not
// parse, so the scheme guess can never see mysql. The explicit driver must still
// be stored, and a template with no driver stays postgres.
func TestDBConnectAddExplicitDriverWinsForEnvTemplate(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "envmysql", "url": "${DATABASE_URL}", "driver": "mysql"}); rec.Code != http.StatusCreated {
		t.Fatalf("add mysql template = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": "envdefault", "url": "${DATABASE_URL}"}); rec.Code != http.StatusCreated {
		t.Fatalf("add default template = %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"envmysql": config.DBDriverMySQL, "envdefault": config.DBDriverPostgres}
	for _, c := range cfg.DB.Connections {
		if w, ok := want[c.Name]; ok && c.Driver != w {
			t.Errorf("%s: driver = %q, want %q", c.Name, c.Driver, w)
		}
		delete(want, c.Name)
	}
	if len(want) != 0 {
		t.Fatalf("connections not saved: %v", want)
	}
}

// TestDBConnectUnlockDoesNotStartTunnels: unlock runs no ssh. A tunnel starts
// only when its connection is first opened, and then once per connection.
func TestDBConnectUnlockDoesNotStartTunnels(t *testing.T) {
	setHomeTree(t, t.TempDir())
	calls := 0
	stubDBEnsureTunnel(t, func(context.Context, string, *config.DBTunnel, string) (int, error) {
		calls++
		return 0, errors.New("ssh must not run here")
	})
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections", map[string]any{
		"name": "prod", "url": dbTestURL, "password": "master-pw",
		"tunnel": map[string]any{"host": "bastion", "remote_host": "db.internal", "remote_port": 5432,
			"auth_method": "password", "password": "tunnel-pw"},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}

	if rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "wrong-pw"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unlock wrong password = %d, want 401", rec.Code)
	}
	if calls != 0 {
		t.Fatalf("unlock with a wrong password started %d tunnels", calls)
	}

	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "master-pw"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	if calls != 0 {
		t.Fatalf("unlock started %d tunnels, want 0", calls)
	}
	if names := dbListNames(t, h, "s1"); !names["prod"] {
		t.Fatal("unlock with a tunnel did not grant the connection")
	}

	// The first use starts the tunnel. Its failure fails that request only.
	tables := httptest.NewRecorder()
	h.HandleDBConnectTables(tables, httptest.NewRequest(http.MethodGet,
		"/api/dbconnect/tables?surface=s1&connection=prod", nil))
	if tables.Code != http.StatusBadGateway {
		t.Fatalf("tables with a failing tunnel = %d, want 502", tables.Code)
	}
	if calls != 1 {
		t.Fatalf("first use started %d tunnels, want 1", calls)
	}
}

// TestDBConnectTunnelPortIsUsedOnOpen: the first open dials the port the tunnel
// manager returned. The database's own address is 127.0.0.1:1, so the listener
// accepting a connection proves the URL was rewritten onto the tunnel port.
func TestDBConnectTunnelPortIsUsedOnOpen(t *testing.T) {
	setHomeTree(t, t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- struct{}{}
		conn.Close()
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	var seen *config.DBTunnel
	stubDBEnsureTunnel(t, func(_ context.Context, _ string, tun *config.DBTunnel, _ string) (int, error) {
		seen = tun
		return port, nil
	})
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections", map[string]any{
		"name": "prod", "url": dbTestURL, "password": "master-pw",
		"tunnel": map[string]any{"host": "bastion", "remote_host": "db.internal", "remote_port": 5432},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": "s1", "password": "master-pw"}); rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}
	h.dbMu.Lock()
	e := h.dbGrants["s1"]["prod"]
	h.dbMu.Unlock()
	if !e.hasTunnel || e.url != dbTestURL {
		t.Fatalf("grant = %+v, want the tunnel and the unrewritten url", e)
	}

	// The open itself fails once the listener hangs up; what matters is that it
	// dialed the tunnel port.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := h.dbGrantDB(ctx, "s1", "prod"); err == nil {
		t.Fatal("open against a listener that hangs up succeeded")
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("open did not dial the tunnel port")
	}
	if seen == nil || seen.RemoteHost != "db.internal" || seen.RemotePort != 5432 {
		t.Fatalf("tunnel descriptor seen by the manager = %+v", seen)
	}
}

// dbNoConnector is a driver connector for a pool that is never used: it lets a
// test cache a real *sql.DB without a database behind it.
type dbNoConnector struct{}

func (dbNoConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("no database in this test")
}

func (dbNoConnector) Driver() driver.Driver { return nil }

// TestDBGrantPoolFollowsTunnelPort: a pool opened on one tunnel port is reused
// only while the tunnel reports that port. A restarted tunnel on another port
// must not be served by the old pool, which dials a dead port.
func TestDBGrantPoolFollowsTunnelPort(t *testing.T) {
	h := &Handler{}
	tun := config.DBTunnel{Host: "bastion", RemoteHost: "db.internal", RemotePort: 5432}
	h.dbSetGrant("s1", dbGrant{"prod": {url: dbTestURL, tunnel: tun, hasTunnel: true, poolPort: 1}})
	cached := sql.OpenDB(dbNoConnector{})
	defer cached.Close()
	h.dbMu.Lock()
	h.dbPools = map[string]map[string]*sql.DB{"s1": {"prod": cached}}
	h.dbMu.Unlock()

	port := 1
	stubDBEnsureTunnel(t, func(context.Context, string, *config.DBTunnel, string) (int, error) {
		return port, nil
	})
	got, granted, err := h.dbGrantDB(context.Background(), "s1", "prod")
	if err != nil || !granted || got != cached {
		t.Fatalf("same port: got %p granted %v err %v; want the cached pool", got, granted, err)
	}

	// Nothing listens on port 2, so a fresh open must fail. It must not hand back
	// the pool that was opened on port 1.
	port = 2
	got, _, err = h.dbGrantDB(context.Background(), "s1", "prod")
	if err == nil || got == cached {
		t.Fatalf("new port: got %p err %v; want a fresh open attempt, not the port-1 pool", got, err)
	}
}

// TestDBUnlockTunnelPassword: a stored tunnel password is opened with the master
// password and never replaced by the client's value. Only a connection with no
// stored password takes the client's value.
func TestDBUnlockTunnelPassword(t *testing.T) {
	sealed, err := dbconnect.SealPassword("tunnel-pw", "master-pw")
	if err != nil {
		t.Fatal(err)
	}
	stored := config.DBConnection{Name: "prod", Tunnel: &config.DBTunnel{Host: "bastion", AuthMethod: "password", Password: sealed}}
	if got, err := dbUnlockTunnelPassword(stored, "client-pw", "master-pw"); err != nil || got != "tunnel-pw" {
		t.Fatalf("stored, right master = %q, %v; want tunnel-pw", got, err)
	}
	if got, err := dbUnlockTunnelPassword(stored, "client-pw", "wrong-pw"); err == nil || got != "" {
		t.Fatalf("stored, wrong master = %q, %v; want an error and no password", got, err)
	}
	if got, err := dbUnlockTunnelPassword(stored, "client-pw", ""); err == nil || got != "" {
		t.Fatalf("stored, no master = %q, %v; want an error and no password", got, err)
	}
	prompted := config.DBConnection{Name: "prompted", Tunnel: &config.DBTunnel{Host: "bastion", AuthMethod: "password"}}
	if got, err := dbUnlockTunnelPassword(prompted, "client-pw", ""); err != nil || got != "client-pw" {
		t.Fatalf("no stored password = %q, %v; want the client value", got, err)
	}
}

// TestDBConnectAddTunnelPasswordNeedsAndChecksMasterPassword: a stored tunnel
// password needs the master password, and it must match the saved set.
func TestDBConnectAddTunnelPasswordNeedsAndChecksMasterPassword(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	tunnel := map[string]any{"host": "bastion", "remote_host": "db.internal", "remote_port": 5432,
		"auth_method": "password", "password": "tunnel-pw"}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections", map[string]any{
		"name": "envnopw", "url": "${DATABASE_URL}", "tunnel": tunnel,
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("tunnel password without master = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections", map[string]any{
		"name": "envt", "url": "${DATABASE_URL}", "password": "master-pw", "tunnel": tunnel,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("add envt = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections", map[string]any{
		"name": "envx", "url": "${DATABASE_URL}", "password": "other-pw", "tunnel": tunnel,
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second master = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
}
