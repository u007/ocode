package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dbIDEFixture = `CREATE TABLE people(id INTEGER PRIMARY KEY, name TEXT, age INTEGER);
	INSERT INTO people VALUES (1,'ann',30),(2,'bob',40),(3,'cat',50);`

// dbTableBody issues GET /api/db/table with the given extra query params and
// decodes the response.
func dbTableBody(t *testing.T, h *Handler, dbPath string, params map[string]string) (int, struct {
	Result struct {
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
		Rows     [][]any `json:"rows"`
		RowCount int     `json:"row_count"`
	} `json:"result"`
	Total *int `json:"total"`
}) {
	t.Helper()
	q := url.Values{"path": {dbPath}}
	for k, v := range params {
		q.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?"+q.Encode(), nil))
	var out struct {
		Result struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows     [][]any `json:"rows"`
			RowCount int     `json:"row_count"`
		} `json:"result"`
		Total *int `json:"total"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestDBTableFilterSortAndTotal(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, body := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "filter": "age >= 40", "sort": "name", "dir": "desc",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Total == nil || *body.Total != 2 {
		t.Fatalf("total = %v, want 2", body.Total)
	}
	if len(body.Result.Rows) != 2 {
		t.Fatalf("rows = %#v, want 2", body.Result.Rows)
	}
	// desc: cat(50) then bob(40)
	if body.Result.Rows[0][1] != "cat" || body.Result.Rows[1][1] != "bob" {
		t.Fatalf("rows = %#v, want cat then bob", body.Result.Rows)
	}
}

// dir=desc must not be the only way to ask for descending: an unknown direction
// is an error rather than a silent ascending sort the user did not ask for.
func TestDBTableRejectsAnUnknownSortDirection(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, _ := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "sort": "name", "dir": "sideways",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for dir=sideways", code)
	}
}

func TestDBTableRejectsAnUnknownSortColumn(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, _ := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "sort": "salary",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a column the table lacks", code)
	}
}

// A filter is user SQL reaching a live query, so a write smuggled through it
// must be refused, not executed.
func TestDBTableRejectsAWriteInTheFilter(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, _ := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "filter": "1=1; DROP TABLE people",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
	// The table must still be there.
	code, body := dbTableBody(t, h, dbPath, map[string]string{"table": "people"})
	if code != http.StatusOK || body.Result.RowCount != 3 {
		t.Fatalf("the table did not survive: status=%d rows=%d", code, body.Result.RowCount)
	}
}

// An ATTACH in a filter would reach a file outside the containment boundary.
func TestDBTableRejectsAttachInTheFilter(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, _ := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "filter": "1=1 AND (SELECT 1 FROM (SELECT 1) WHERE 1=ATTACH)",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}

// Without an explicit filter the response is unchanged from before this feature:
// no total key, so a client that does not ask for one is unaffected.
func TestDBTableOmitsTotalWhenNotRequested(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, body := dbTableBody(t, h, dbPath, map[string]string{"table": "people"})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Total != nil {
		t.Fatalf("total = %v, want it absent when count was not requested", *body.Total)
	}
}

func TestDBTableCountMatchesAFilter(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, body := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "count": "1", "filter": "name <> 'bob'",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Total == nil || *body.Total != 2 {
		t.Fatalf("total = %v, want 2", body.Total)
	}
	// The page is still bounded, so a count never means "load everything".
	if body.Result.RowCount != 2 {
		t.Fatalf("row_count = %d, want 2", body.Result.RowCount)
	}
}

// ── Maintenance endpoint ───────────────────────────────────────────────────

func dbPost(t *testing.T, h *Handler, route string, payload any) (int, string) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleDBMaintenance(rec, httptest.NewRequest(http.MethodPost, route, strings.NewReader(string(b))))
	return rec.Code, rec.Body.String()
}

func TestDBMaintenanceVacuumAnalyzeAndIntegrity(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture+` DELETE FROM people WHERE id = 3;`)

	code, body := dbPost(t, h, "/api/db/maintenance", map[string]any{
		"project_root": tmpDir, "path": dbPath, "op": "analyze",
	})
	if code != http.StatusOK {
		t.Fatalf("analyze: status = %d body = %s", code, body)
	}

	code, body = dbPost(t, h, "/api/db/maintenance", map[string]any{
		"project_root": tmpDir, "path": dbPath, "op": "integrity_check",
	})
	if code != http.StatusOK {
		t.Fatalf("integrity_check: status = %d body = %s", code, body)
	}
	var res struct {
		Rows []string `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Rows) != 1 || !strings.EqualFold(res.Rows[0], "ok") {
		t.Fatalf("rows = %#v, want [ok]", res.Rows)
	}

	code, body = dbPost(t, h, "/api/db/maintenance", map[string]any{
		"project_root": tmpDir, "path": dbPath, "op": "vacuum",
	})
	if code != http.StatusOK {
		t.Fatalf("vacuum: status = %d body = %s", code, body)
	}
}

// An unknown op must be refused: the handler maps op to SQL, so an unrecognised
// value must never fall through to something executable.
func TestDBMaintenanceRejectsAnUnknownOp(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, body := dbPost(t, h, "/api/db/maintenance", map[string]any{
		"project_root": tmpDir, "path": dbPath, "op": "DROP TABLE people",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", code, body)
	}
	code, _ = dbTableBody(t, h, dbPath, map[string]string{"table": "people"})
	if code != http.StatusOK {
		t.Fatal("the table did not survive the rejected op")
	}
}

// VACUUM rewrites the file, so it is a mutation and must pass the write guard.
func TestDBMaintenanceRefusesANonSQLiteFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	txt := filepath.Join(tmpDir, "notes.db")
	if err := os.WriteFile(txt, []byte("not a database"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, body := dbPost(t, h, "/api/db/maintenance", map[string]any{
		"project_root": tmpDir, "path": txt, "op": "vacuum",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", code, body)
	}
}

// ── Backup endpoint ────────────────────────────────────────────────────────

func TestDBRowBacksUpBeforeMutating(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	payload, err := json.Marshal(map[string]any{
		"project_root": tmpDir, "path": dbPath, "table": "people",
		"op": "delete", "key": map[string]any{"id": 2},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleDBRow(rec, httptest.NewRequest(http.MethodPost, "/api/db/row", strings.NewReader(string(payload))))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body = %s", rec.Code, rec.Body.String())
	}

	bak := dbPath + ".bak"
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("no backup was taken: %v", err)
	}
	// The backup is a real database that still has the deleted row, which is the
	// whole point: it is the way back.
	if n, err := readDBRowCount(t, bak); err != nil || n != 3 {
		t.Fatalf("backup holds %d rows (err %v), want the 3 rows from before the delete", n, err)
	}
}

// A refused mutation changes nothing, so the snapshot the handler took before
// attempting it must be a faithful copy of the current file — never a stale one
// from an earlier write, and never evidence that the delete happened.
func TestDBRowBackupOnRefusalMatchesTheUnchangedFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	// A key that matches no row: the delete is refused with 409.
	payload, _ := json.Marshal(map[string]any{
		"project_root": tmpDir, "path": dbPath, "table": "people",
		"op": "delete", "key": map[string]any{"id": 999},
	})
	rec := httptest.NewRecorder()
	h.HandleDBRow(rec, httptest.NewRequest(http.MethodPost, "/api/db/row", strings.NewReader(string(payload))))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s, want 409", rec.Code, rec.Body.String())
	}

	bak, err := readDBRowCount(t, dbPath+".bak")
	if err != nil {
		t.Fatalf("no backup was taken: %v", err)
	}
	if bak != 3 {
		t.Fatalf("the snapshot holds %d rows, want the 3 rows the database still has", bak)
	}
	live, err := readDBRowCount(t, dbPath)
	if err != nil {
		t.Fatalf("live db: %v", err)
	}
	if live != 3 {
		t.Fatalf("live row count = %d, want 3 (the refused delete must change nothing)", live)
	}
}

// readDBRowCount opens a database read-only and counts its people rows, so the
// assertion is about DATA and not about file bytes: a snapshot taken with a
// plain file copy can differ byte-wise while holding exactly the same rows.
func readDBRowCount(t *testing.T, path string) (int, error) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM people`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// The maintenance endpoint is reached through the real mux: a handler-only
// test cannot catch a missing route, which is the failure mode a new endpoint
// actually has.
func TestDBMaintenanceRouteRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.SetWorkDir(tmpDir)
	seedDB(t, filepath.Join(tmpDir, "route.db"), dbIDEFixture)
	h := srv.serveHandler()

	body, _ := json.Marshal(map[string]any{"path": "route.db", "op": "integrity_check"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/db/maintenance", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/db/maintenance = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
}

// A row save that carries a blob is base64, so the body is bounded rather than
// buffered whole. The cap must be above the blob limit (base64 inflates 4/3) or
// a legal 64 MB upload would be refused here after passing the blob endpoint.
func TestDBRowRejectsAnOversizeBody(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	// A VALID JSON prefix followed by more than the cap: the decoder only stops
	// at the limit, so a 413 here can only come from the cap. A body of raw
	// garbage would fail to parse long before the limit and pass for the wrong
	// reason.
	body := io.MultiReader(
		strings.NewReader(`{"path":"`),
		io.LimitReader(neverEndingReader{}, dbRowMaxBodyBytes+1),
	)
	rec := httptest.NewRecorder()
	h.HandleDBRow(rec, httptest.NewRequest(http.MethodPost, "/api/db/row", body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body = %s, want 413 (the body cap is exceeded)", rec.Code, rec.Body.String())
	}
}

// The table response carries exact string row keys so the browser never has to
// rebuild a key from JSON numbers, which round past 2^53 and would silently
// address the neighbouring row.
func TestDBTableSendsExactRowKeys(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO t VALUES (9007199254740993,'target');`)

	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path="+url.QueryEscape(dbPath)+"&table=t", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RowKeys []map[string]*string `json:"row_keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.RowKeys) != 1 {
		t.Fatalf("got %d row keys, want 1", len(body.RowKeys))
	}
	if got := body.RowKeys[0]["id"]; got == nil || *got != "9007199254740993" {
		t.Fatalf("row_keys[0].id = %v, want the exact string 9007199254740993", got)
	}
}

// A view has no addressable row, so the field is absent rather than a list of
// empty objects the client would have to interpret.
func TestDBTableOmitsRowKeysForAView(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO t VALUES (1,'x'); CREATE VIEW v AS SELECT * FROM t;`)

	rec := httptest.NewRecorder()
	h.HandleDBTable(rec, httptest.NewRequest(http.MethodGet, "/api/db/table?path="+url.QueryEscape(dbPath)+"&table=v", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "row_keys") {
		t.Fatalf("a view should carry no row_keys, got %s", rec.Body.String())
	}
}

// An exact key from row_keys must actually address its row through the row
// endpoint — the end-to-end proof that the string round trip works.
func TestDBRowAcceptsAnExactStringKey(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO t VALUES (9007199254740993,'target'), (9007199254740992,'neighbour');`)

	payload, _ := json.Marshal(map[string]any{
		"project_root": tmpDir, "path": dbPath, "table": "t", "op": "update",
		// The key as the browser would send it back: an exact string.
		"key":    map[string]any{"id": "9007199254740993"},
		"values": map[string]any{"v": "renamed"},
	})
	rec := httptest.NewRecorder()
	h.HandleDBRow(rec, httptest.NewRequest(http.MethodPost, "/api/db/row", strings.NewReader(string(payload))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	// The 2^53+1 row changed; its neighbour did not.
	code, body := dbTableBody(t, h, dbPath, map[string]string{"table": "t", "sort": "id"})
	if code != http.StatusOK || len(body.Result.Rows) != 2 {
		t.Fatalf("read back: code=%d rows=%d", code, len(body.Result.Rows))
	}
	if body.Result.Rows[0][1] != "neighbour" {
		t.Fatalf("the neighbour was modified: %#v", body.Result.Rows[0])
	}
	if body.Result.Rows[1][1] != "renamed" {
		t.Fatalf("the targeted row was not updated: %#v", body.Result.Rows[1])
	}
}

// A filter naming a column the table lacks is the caller's input (400); the
// server-fault branch (500 + log) is reserved for errors that are not.
func TestDBTableRejectsAnUnknownFilterColumn(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	dbPath := filepath.Join(tmpDir, "app.db")
	seedDB(t, dbPath, dbIDEFixture)

	code, _ := dbTableBody(t, h, dbPath, map[string]string{
		"table": "people", "filter": "salary > 1",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a filter naming a missing column", code)
	}
}
