package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// seedDB creates a SQLite file at path with the given setup SQL.
func seedDB(t *testing.T, path, setup string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if _, err := db.Exec(setup); err != nil {
		t.Fatalf("setup %s: %v", path, err)
	}
}

func TestHandleDBInfoListsTables(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "app.db"), `
		CREATE TABLE zebra(id INTEGER PRIMARY KEY);
		CREATE TABLE apple(a INTEGER);
		CREATE VIEW v AS SELECT * FROM apple;
	`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/db/info?path=app.db", nil)
	h.HandleDBInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		IsSQLite bool `json:"is_sqlite"`
		Tables   []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.IsSQLite {
		t.Fatal("expected is_sqlite=true")
	}
	var names []string
	for _, tb := range got.Tables {
		names = append(names, tb.Name)
	}
	if strings.Join(names, ",") != "apple,v,zebra" {
		t.Fatalf("tables = %v, want sorted apple,v,zebra", names)
	}
}

func TestHandleDBInfoNonSQLite(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.db"), []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.HandleDBInfo(rec, httptest.NewRequest(http.MethodGet, "/api/db/info?path=notes.db", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["is_sqlite"] != false {
		t.Fatalf("is_sqlite = %v, want false", got["is_sqlite"])
	}
}

func TestHandleDBInfoMissingFile(t *testing.T) {
	h, _ := newFilesHandler(t)
	rec := httptest.NewRecorder()
	h.HandleDBInfo(rec, httptest.NewRequest(http.MethodGet, "/api/db/info?path=nope.db", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleDBInfoRejectsOutsideAllowedRoots(t *testing.T) {
	h, _ := newFilesHandler(t)
	// A real database OUTSIDE the workDir must be refused even by absolute
	// path — the browser will gain write capability.
	outside := filepath.Join(t.TempDir(), "outside.db")
	seedDB(t, outside, `CREATE TABLE t(a)`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/db/info?path="+url.QueryEscape(outside), nil)
	h.HandleDBInfo(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestHandleDBInfoRejectsTraversal(t *testing.T) {
	h, _ := newFilesHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/db/info?path=../../etc/passwd", nil)
	h.HandleDBInfo(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleDBTablePaged(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "p.db"), `
		CREATE TABLE t(a INTEGER, b TEXT);
		INSERT INTO t(a,b) VALUES (1,'x'),(2,'y'),(3,'z'),(4,'w'),(5,'v');
	`)

	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path=p.db&table=t&limit=2&offset=0", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Schema struct {
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"schema"`
		Result struct {
			RowCount  int     `json:"row_count"`
			Truncated bool    `json:"truncated"`
			Rows      [][]any `json:"rows"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Schema.Name != "t" || len(got.Schema.Columns) != 2 {
		t.Fatalf("schema = %+v", got.Schema)
	}
	if got.Result.RowCount != 2 || !got.Result.Truncated {
		t.Fatalf("result rowCount=%d truncated=%v", got.Result.RowCount, got.Result.Truncated)
	}
}

// TestHandleDBTableEmptyListsAreArrays pins the /api/db/table wire contract at
// the HTTP boundary, where the browser actually sees it: an empty list is `[]`,
// never `null`. The SQLite preview dereferences these fields unguarded
// (`schema.foreign_keys.length`), so a null crashed the whole preview pane with
// WebKit's "null is not an object" instead of just hiding a section.
func TestHandleDBTableEmptyListsAreArrays(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// An INTEGER PRIMARY KEY is the rowid alias, so SQLite creates no index for
	// it and there is no foreign key: every list here is legitimately empty.
	seedDB(t, filepath.Join(tmpDir, "bare.db"), `CREATE TABLE t(a INTEGER PRIMARY KEY);`)

	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path=bare.db&table=t", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// RawMessage keeps the [] vs null distinction visible: unmarshalling into
	// []json.RawMessage would hide it, and a []struct would just be empty.
	var got struct {
		Schema map[string]json.RawMessage `json:"schema"`
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"indexes", "foreign_keys"} {
		if string(got.Schema[key]) != "[]" {
			t.Errorf("schema.%s = %s, want []", key, got.Schema[key])
		}
	}
	// columns is NOT empty here — the table has one — so it only has to be an
	// array rather than a null.
	if string(got.Schema["columns"]) == "null" {
		t.Errorf("schema.columns is null")
	}
	if string(got.Result["rows"]) != "[]" {
		t.Errorf("result.rows = %s, want []", got.Result["rows"])
	}
}

func TestHandleDBTableMissing(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "m.db"), `CREATE TABLE t(a)`)
	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path=m.db&table=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleDBQuerySelect(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "q.db"), `CREATE TABLE t(a INTEGER); INSERT INTO t(a) VALUES (1),(2);`)

	body := `{"path":"q.db","sql":"SELECT count(*) AS n FROM t"}`
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0][0].(float64) != 2 {
		t.Fatalf("rows = %#v", got.Rows)
	}
}

func TestHandleDBQueryWriteRejected(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "w.db"), `CREATE TABLE t(a INTEGER);`)

	body := `{"path":"w.db","sql":"INSERT INTO t(a) VALUES (1)"}`
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

// TestHandleDBQueryRejectsAttachEscape pins the endpoint boundary, not just the
// engine: the in-root ?path passes pathWithinAllowedRoots, so the ONLY thing
// standing between the caller and any SQLite file on the host is the statement
// gate inside dbbrowse. ATTACH is how mode=ro is defeated, so this is the test
// that fails if the engine gate is ever removed or bypassed.
func TestHandleDBQueryRejectsAttachEscape(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	inRoot := filepath.Join(tmpDir, "in.db")
	seedDB(t, inRoot, `CREATE TABLE t(a INTEGER);`)

	// The out-of-root victim stands in for any SQLite file the process can read,
	// ocode's own session transcripts most importantly.
	victim := filepath.Join(t.TempDir(), "victim.db")
	seedDB(t, victim, `CREATE TABLE secret(a TEXT); INSERT INTO secret VALUES('MY PRIVATE SESSION TITLE');`)

	for _, sqlText := range []string{
		fmt.Sprintf(`ATTACH 'file:%s' AS v; SELECT a FROM v.secret`, victim),
		fmt.Sprintf(`ATTACH 'file:%s' AS v; INSERT INTO v.secret VALUES('PWNED')`, victim),
		`ATTACH DATABASE ':memory:' AS mem`,
		`SELECT 1; ATTACH 'file:%tmp/v.db' AS v`,
	} {
		body, err := json.Marshal(map[string]any{"path": inRoot, "sql": sqlText})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rec := httptest.NewRecorder()
		h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(string(body))))
		if rec.Code == http.StatusOK {
			t.Errorf("%q: status 200; the statement gate did not reject it", sqlText)
		}
	}

	// And the victim must be byte-identical to how seedDB left it.
	db, err := sql.Open("sqlite", victim)
	if err != nil {
		t.Fatalf("reopen victim: %v", err)
	}
	defer db.Close()
	var got []string
	rows, err := db.Query(`SELECT a FROM secret`)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if len(got) != 1 || got[0] != "MY PRIVATE SESSION TITLE" {
		t.Fatalf("out-of-root file was read from or written to: %#v", got)
	}
}

func TestHandleDBQueryAllowsReadOnlySelect(t *testing.T) {
	// The gate must not over-block: an ordinary read through the same endpoint
	// still has to work, or the browser is useless.
	h, tmpDir := newFilesHandler(t)
	inRoot := filepath.Join(tmpDir, "in.db")
	seedDB(t, inRoot, `CREATE TABLE t(a INTEGER); INSERT INTO t(a) VALUES(7);`)

	body := `{"path":"in.db","sql":"SELECT a FROM t"}`
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
}

func TestHandleDBQueryMissingArgs(t *testing.T) {
	h, _ := newFilesHandler(t)
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(`{"path":"x.db"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestDBRoutesRegistered drives the REAL mux: a handler-only test cannot catch
// a missing or shadowed route, which presents as a 404 from the SPA fallback.
func TestDBRoutesRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.SetWorkDir(tmpDir)
	seedDB(t, filepath.Join(tmpDir, "route.db"), `CREATE TABLE t(a INTEGER);`)
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/db/info?path=route.db", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/db/info = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path=route.db&table=t", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/db/table = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/db/query",
		strings.NewReader(`{"path":"route.db","sql":"SELECT 1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/db/query = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/db/row",
		strings.NewReader(`{"path":"route.db","op":"insert","table":"t","values":{"a":1}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/db/row = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/db/schema",
		strings.NewReader(`{"path":"route.db","op":"create_table","table":"t2","columns":[{"name":"a","type":"INTEGER"}]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/db/schema = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleDBQueryRejectsOutsideAllowedRoots pins the sibling PARITY between
// the DB endpoints: the query endpoint must enforce the same unconditional
// allowed-roots check as info/table. Calling resolveProjectFilePath alone (which
// skips containment for an absolute path with no project_root) let the query
// endpoint accept a path the other two rejected.
func TestHandleDBQueryRejectsOutsideAllowedRoots(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := filepath.Join(t.TempDir(), "outside.db")
	seedDB(t, outside, `CREATE TABLE t(a)`)

	body, _ := json.Marshal(map[string]string{"path": outside, "sql": "SELECT 1"})
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(string(body))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestHandleDBQueryRejectsTraversal(t *testing.T) {
	h, _ := newFilesHandler(t)
	body, _ := json.Marshal(map[string]string{"path": "../../etc/passwd", "sql": "SELECT 1"})
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", strings.NewReader(string(body))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ── Row CRUD (P2) ───────────────────────────────────────────────────────────

func dbPostRow(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.HandleDBRow(rec, httptest.NewRequest(http.MethodPost, "/api/db/row", bytes.NewReader(raw)))
	return rec
}

func TestHandleDBRowInsertUpdateDelete(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "rows.db"), `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT)`)

	rec := dbPostRow(t, h, map[string]any{"path": "rows.db", "op": "insert", "table": "t",
		"values": map[string]any{"id": 1, "name": "alice"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("insert: %d %s", rec.Code, rec.Body.String())
	}

	rec = dbPostRow(t, h, map[string]any{"path": "rows.db", "op": "update", "table": "t",
		"key": map[string]any{"id": 1}, "values": map[string]any{"name": "ALICE"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// A key that matches nothing is a 409 (stale grid), not a silent no-op.
	rec = dbPostRow(t, h, map[string]any{"path": "rows.db", "op": "update", "table": "t",
		"key": map[string]any{"id": 99}, "values": map[string]any{"name": "x"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale update: %d %s, want 409", rec.Code, rec.Body.String())
	}

	rec = dbPostRow(t, h, map[string]any{"path": "rows.db", "op": "delete", "table": "t",
		"key": map[string]any{"id": 1}})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	db, err := sql.Open("sqlite", filepath.Join(tmpDir, "rows.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func TestHandleDBRowRejectsNonSQLite(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "not.db"), []byte("hello world not a database"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := dbPostRow(t, h, map[string]any{"path": "not.db", "op": "insert", "table": "t",
		"values": map[string]any{"a": 1}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-sqlite insert: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// A table with no declared primary key is edited by its _rowid_ key, which the
// table endpoint exposes as a leading column.
func TestHandleDBRowByRowID(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "rowid.db"), `CREATE TABLE t(x TEXT); INSERT INTO t VALUES ('a');`)

	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path=rowid.db&table=t", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("table: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Result struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Result.Columns) == 0 || got.Result.Columns[0].Name != "_rowid_" {
		t.Fatalf("first column = %#v, want _rowid_", got.Result.Columns)
	}
	rowid := got.Result.Rows[0][0]

	rec = dbPostRow(t, h, map[string]any{"path": "rowid.db", "op": "update", "table": "t",
		"key": map[string]any{"_rowid_": rowid}, "values": map[string]any{"x": "b"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("update by rowid: %d %s", rec.Code, rec.Body.String())
	}
}

// ── SQL writes (P3) ─────────────────────────────────────────────────────────

func dbPostQuery(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.HandleDBQuery(rec, httptest.NewRequest(http.MethodPost, "/api/db/query", bytes.NewReader(raw)))
	return rec
}

func TestHandleDBQueryConfirmEscalation(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "write.db"), `CREATE TABLE t(id INTEGER PRIMARY KEY); INSERT INTO t VALUES (1);`)

	// Without confirm a write is refused with 409 — the confirmation signal.
	rec := dbPostQuery(t, h, map[string]any{"path": "write.db", "sql": "UPDATE t SET id = 2"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("unconfirmed write: %d %s, want 409", rec.Code, rec.Body.String())
	}

	// With confirm the write runs and commits.
	rec = dbPostQuery(t, h, map[string]any{"path": "write.db", "sql": "UPDATE t SET id = 2", "confirm": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed write: %d %s", rec.Code, rec.Body.String())
	}

	db, _ := sql.Open("sqlite", filepath.Join(tmpDir, "write.db"))
	defer db.Close()
	var id int
	if err := db.QueryRow(`SELECT id FROM t`).Scan(&id); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if id != 2 {
		t.Fatalf("id = %d, want 2 (write did not commit)", id)
	}
}

// ATTACH is refused even with confirmation: there is no authorizer hook, so it
// is the one way the SQL editor could reach a file outside containment.
func TestHandleDBQueryConfirmBlocksAttach(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "attach.db"), `CREATE TABLE t(a)`)

	rec := dbPostQuery(t, h, map[string]any{
		"path": "attach.db", "sql": "ATTACH DATABASE '/etc/hosts' AS x", "confirm": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("attach: %d %s, want 400", rec.Code, rec.Body.String())
	}
	// Pin the EXPLICIT block, not an incidental engine refusal: Exec runs in a
	// transaction, which also rejects ATTACH, so a code-only check would pass
	// with the block removed.
	if !strings.Contains(rec.Body.String(), "not allowed") {
		t.Fatalf("ATTACH was refused by the engine, not the explicit block: %s", rec.Body.String())
	}
}

func TestHandleDBQueryConfirmRejectsNonSQLite(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "not.db"), []byte("hello world not a database"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := dbPostQuery(t, h, map[string]any{
		"path": "not.db", "sql": "CREATE TABLE t(a)", "confirm": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-sqlite write: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// ── Guided DDL (P4) ─────────────────────────────────────────────────────────

func dbPostSchema(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.HandleDBSchema(rec, httptest.NewRequest(http.MethodPost, "/api/db/schema", bytes.NewReader(raw)))
	return rec
}

func TestHandleDBSchemaLifecycle(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	seedDB(t, filepath.Join(tmpDir, "ddl.db"), `CREATE TABLE keep(a)`)

	rec := dbPostSchema(t, h, map[string]any{"path": "ddl.db", "op": "create_table", "table": "made",
		"columns": []map[string]any{{"name": "id", "type": "INTEGER", "pk": true}, {"name": "name", "type": "TEXT"}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("create table: %d %s", rec.Code, rec.Body.String())
	}

	rec = dbPostSchema(t, h, map[string]any{"path": "ddl.db", "op": "add_column", "table": "made",
		"column": map[string]any{"name": "age", "type": "INTEGER"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("add column: %d %s", rec.Code, rec.Body.String())
	}

	rec = dbPostSchema(t, h, map[string]any{"path": "ddl.db", "op": "create_index", "table": "made",
		"index": "idx_name", "index_columns": []string{"name"}, "unique": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("create index: %d %s", rec.Code, rec.Body.String())
	}

	// A type that tries to break out of the type position is rejected.
	rec = dbPostSchema(t, h, map[string]any{"path": "ddl.db", "op": "create_table", "table": "bad",
		"columns": []map[string]any{{"name": "a", "type": "TEXT); DROP TABLE keep; --"}}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("injected type: %d %s, want 400", rec.Code, rec.Body.String())
	}

	// The injected statement must not have run.
	db, _ := sql.Open("sqlite", filepath.Join(tmpDir, "ddl.db"))
	defer db.Close()
	var keep int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='keep'`).Scan(&keep); err != nil {
		t.Fatalf("read master: %v", err)
	}
	if keep != 1 {
		t.Fatal("keep table was dropped — the injected DDL ran")
	}

	rec = dbPostSchema(t, h, map[string]any{"path": "ddl.db", "op": "drop_table", "table": "made"})
	if rec.Code != http.StatusOK {
		t.Fatalf("drop table: %d %s", rec.Code, rec.Body.String())
	}
}

// A row write must never CREATE a database file. Without the guard, opening a
// missing path read-write would create it (and leave a stray empty DB behind).
func TestHandleDBRowDoesNotCreateMissingFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	rec := dbPostRow(t, h, map[string]any{"path": "new.db", "op": "insert", "table": "t",
		"values": map[string]any{"a": 1}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "new.db")); !os.IsNotExist(err) {
		t.Fatalf("a missing database file was created (stat err=%v)", err)
	}
}

// A symlink inside the project pointing at a database outside every allowed
// root must be refused on read AND write endpoints: containment is judged on
// the resolved target, never the lexical in-root name.
func TestDBRejectsSymlinkEscapingAllowedRoots(t *testing.T) {
	h, workDir := newFilesHandler(t)
	outside := filepath.Join(t.TempDir(), "outside.db")
	seedDB(t, outside, `CREATE TABLE t(a); INSERT INTO t VALUES (1)`)
	link := filepath.Join(workDir, "link.db")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rec := httptest.NewRecorder()
	h.HandleDBInfo(rec, httptest.NewRequest(http.MethodGet, "/api/db/info?path="+url.QueryEscape(link), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("info via symlink: status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if rec := dbPostRow(t, h, map[string]any{"path": link, "table": "t", "op": "insert", "values": map[string]any{"a": 2}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("row via symlink: status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if rec := dbPostSchema(t, h, map[string]any{"path": link, "sql": "DROP TABLE t"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("schema via symlink: status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
