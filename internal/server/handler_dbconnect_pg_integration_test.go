//go:build pgintegration

package server

// HTTP-level checks against a real Postgres server, run by scripts/test-postgres.sh.
// Each test saves its connection through the real add and unlock handlers, so the
// encrypted envelope and the grant path are both exercised.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/dbconnect/dbtest"
)

const pgSurface = "s1"
const pgConnection = "local"
const pgPassword = "master-pw"

// pgHandler returns a handler whose "local" connection points at a fresh database
// and whose surface is unlocked.
func pgHandler(t *testing.T) *Handler {
	t.Helper()
	setHomeTree(t, t.TempDir())
	h := &Handler{}
	if rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/connections",
		map[string]string{"name": pgConnection, "url": dbtest.NewDatabase(t), "password": pgPassword}); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock",
		map[string]string{"surface": pgSurface, "password": pgPassword}); rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d %s", rec.Code, rec.Body.String())
	}
	return h
}

func pgQuery(t *testing.T, h *Handler, sqlText string, confirm bool) (int, map[string]any) {
	t.Helper()
	rec := dbPostJSON(t, h.HandleDBConnectQuery, "/api/dbconnect/query",
		map[string]any{"surface": pgSurface, "connection": pgConnection, "sql": sqlText, "confirm": confirm})
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("query body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, out
}

func pgCountRows(t *testing.T, h *Handler, table string) string {
	t.Helper()
	code, out := pgQuery(t, h, "SELECT count(*) FROM "+table, false)
	if code != http.StatusOK {
		t.Fatalf("count %s = %d %v", table, code, out)
	}
	return pgJSON(t, out["rows"])
}

func pgJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func pgRows(t *testing.T, h *Handler, params url.Values) (int, string) {
	t.Helper()
	params.Set("surface", pgSurface)
	params.Set("connection", pgConnection)
	rec := httptest.NewRecorder()
	h.HandleDBConnectRows(rec, httptest.NewRequest(http.MethodGet, "/api/dbconnect/rows?"+params.Encode(), nil))
	return rec.Code, rec.Body.String()
}

func pgRow(t *testing.T, h *Handler, body map[string]any) (int, string) {
	t.Helper()
	body["surface"] = pgSurface
	body["connection"] = pgConnection
	rec := dbPostJSON(t, h.HandleDBConnectRow, "/api/dbconnect/row", body)
	return rec.Code, rec.Body.String()
}

func TestPGQueryEscapesAreRefusedAndChangeNothing(t *testing.T) {
	h := pgHandler(t)
	if code, out := pgQuery(t, h, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)", true); code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	if code, out := pgQuery(t, h, "INSERT INTO k VALUES (1, true), (2, true)", true); code != http.StatusOK {
		t.Fatalf("insert = %d %v", code, out)
	}
	before := pgCountRows(t, h, "k")
	for _, sqlText := range []string{
		"COMMIT; DELETE FROM k",
		"END; DELETE FROM k",
		"ROLLBACK; DELETE FROM k",
		"SELECT 1; COMMIT; DELETE FROM k",
	} {
		for _, confirm := range []bool{false, true} {
			if code, out := pgQuery(t, h, sqlText, confirm); code != http.StatusBadRequest {
				t.Fatalf("%q confirm=%v = %d %v, want 400", sqlText, confirm, code, out)
			}
		}
	}
	if after := pgCountRows(t, h, "k"); after != before {
		t.Fatalf("rows changed by escapes: %s -> %s", before, after)
	}
}

func TestPGQueryWriteNeedsConfirmation(t *testing.T) {
	h := pgHandler(t)
	ddl := "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)"
	if code, _ := pgQuery(t, h, ddl, false); code != http.StatusConflict {
		t.Fatalf("unconfirmed DDL = %d, want 409", code)
	}
	if code, out := pgQuery(t, h, ddl, true); code != http.StatusOK {
		t.Fatalf("confirmed DDL = %d %v", code, out)
	}
	if code, _ := pgQuery(t, h, "INSERT INTO k VALUES (1, true)", false); code != http.StatusConflict {
		t.Fatalf("unconfirmed insert = %d, want 409", code)
	}
	if got := pgCountRows(t, h, "k"); got != "[[0]]" {
		t.Fatalf("rows after unconfirmed insert = %s, want [[0]]", got)
	}
	code, out := pgQuery(t, h, "INSERT INTO k VALUES (1, true)", true)
	if code != http.StatusOK || out["rows_affected"] != float64(1) {
		t.Fatalf("confirmed insert = %d %v", code, out)
	}
}

// The same typed row must serialize the same way from RETURNING and from SELECT,
// and a bigint over 2^53 must keep its digits.
func TestPGQueryReturningMatchesSelect(t *testing.T) {
	h := pgHandler(t)
	if code, out := pgQuery(t, h, `CREATE TABLE typed(id bigint primary key, u uuid, n numeric(10,2),
		ts timestamptz, b bytea, j jsonb, flag bool)`, true); code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	insert := `INSERT INTO typed VALUES (9007199254740993, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', 12.34,
		'2026-10-09 12:00:00+00', '\xdeadbeef', '{"a":1,"b":[true,null]}', true) RETURNING *`
	code, ret := pgQuery(t, h, insert, true)
	if code != http.StatusOK {
		t.Fatalf("insert returning = %d %v", code, ret)
	}
	code, sel := pgQuery(t, h, "SELECT * FROM typed WHERE id = 9007199254740993", false)
	if code != http.StatusOK {
		t.Fatalf("select = %d %v", code, sel)
	}
	if got, want := pgJSON(t, ret["rows"]), pgJSON(t, sel["rows"]); got != want {
		t.Fatalf("RETURNING rows differ from SELECT:\n ret=%s\n sel=%s", got, want)
	}
	if ret["rows_affected"] != float64(1) {
		t.Fatalf("rows_affected = %v, want 1", ret["rows_affected"])
	}
	if !strings.Contains(pgJSON(t, ret["rows"]), `"9007199254740993"`) {
		t.Fatalf("bigint not exact: %s", pgJSON(t, ret["rows"]))
	}

	if code, out := pgQuery(t, h, "CREATE TABLE many(g int)", true); code != http.StatusOK {
		t.Fatalf("create many = %d %v", code, out)
	}
	code, many := pgQuery(t, h, "INSERT INTO many SELECT g FROM generate_series(1,1005) g RETURNING g", true)
	if code != http.StatusOK || many["rows_affected"] != float64(1005) || many["truncated"] != true {
		t.Fatalf("truncation = %d %v", code, many)
	}
	if got := len(many["rows"].([]any)); got != 1000 {
		t.Fatalf("returned rows = %d, want 1000", got)
	}
}

func TestPGRowsBrowseFilterAndSort(t *testing.T) {
	h := pgHandler(t)
	if code, out := pgQuery(t, h, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)", true); code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	if code, out := pgQuery(t, h, "INSERT INTO k VALUES (9007199254740992, true), (9007199254740993, false)", true); code != http.StatusOK {
		t.Fatalf("insert = %d %v", code, out)
	}

	code, body := pgRows(t, h, url.Values{"table": {"k"}, "limit": {"1"}, "offset": {"0"}})
	if code != http.StatusOK || !strings.Contains(body, `"has_more":true`) || !strings.Contains(body, `"9007199254740992"`) {
		t.Fatalf("page 1 = %d %s", code, body)
	}
	code, body = pgRows(t, h, url.Values{"table": {"k"}, "sort": {"id"}, "dir": {"desc"}, "limit": {"10"}})
	if code != http.StatusOK || strings.Index(body, "9007199254740993") > strings.Index(body, "9007199254740992") {
		t.Fatalf("desc sort = %d %s", code, body)
	}
	if code, body = pgRows(t, h, url.Values{"table": {"k"}, "sort": {"id; DROP TABLE k"}, "limit": {"10"}}); code != http.StatusBadRequest {
		t.Fatalf("sort injection = %d %s, want 400", code, body)
	}
	if code, _ = pgRows(t, h, url.Values{"table": {"nope"}, "limit": {"10"}}); code != http.StatusNotFound {
		t.Fatalf("missing table = %d, want 404", code)
	}

	before := pgCountRows(t, h, "k")
	for _, f := range []string{"true) --", "1=1); DELETE FROM k", "1=1 /*", "nextval('sq') > 0"} {
		if code, body = pgRows(t, h, url.Values{"table": {"k"}, "limit": {"10"}, "filter": {f}}); code != http.StatusBadRequest {
			t.Fatalf("filter %q = %d %s, want 400", f, code, body)
		}
	}
	if after := pgCountRows(t, h, "k"); after != before {
		t.Fatalf("rows changed by filters: %s -> %s", before, after)
	}
	if code, body = pgRows(t, h, url.Values{"table": {"k"}, "limit": {"10"}, "filter": {"flag"}}); code != http.StatusOK || !strings.Contains(body, `"rows":[[`) {
		t.Fatalf("valid filter = %d %s", code, body)
	}
}

func TestPGRowWritesUseExactKeys(t *testing.T) {
	h := pgHandler(t)
	if code, out := pgQuery(t, h, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)", true); code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	if code, out := pgQuery(t, h, "INSERT INTO k VALUES (9007199254740992, false), (9007199254740993, false)", true); code != http.StatusOK {
		t.Fatalf("insert = %d %v", code, out)
	}

	// The key arrives as a JSON number and must reach Postgres unrounded.
	raw := `{"surface":"s1","connection":"local","op":"update","table":"k","key":{"id":9007199254740993},"values":{"flag":true}}`
	rec := httptest.NewRecorder()
	h.HandleDBConnectRow(rec, httptest.NewRequest(http.MethodPost, "/api/dbconnect/row", bytes.NewReader([]byte(raw))))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rows_affected":1`) {
		t.Fatalf("exact update = %d %s", rec.Code, rec.Body.String())
	}
	code, out := pgQuery(t, h, "SELECT id, flag FROM k ORDER BY id", false)
	if code != http.StatusOK || pgJSON(t, out["rows"]) != `[["9007199254740992",false],["9007199254740993",true]]` {
		t.Fatalf("after update = %d %v: the neighbour row changed or the key was rounded", code, out)
	}

	if code, body := pgRow(t, h, map[string]any{"op": "update", "table": "k",
		"key": map[string]any{"id": json.Number("1")}, "values": map[string]any{"flag": false}}); code != http.StatusConflict {
		t.Fatalf("missing-row update = %d %s, want 409", code, body)
	}
	if code, body := pgRow(t, h, map[string]any{"op": "delete", "table": "k",
		"key": map[string]any{"id": json.Number("1")}}); code != http.StatusConflict {
		t.Fatalf("missing-row delete = %d %s, want 409", code, body)
	}
	if code, body := pgRow(t, h, map[string]any{"op": "insert", "table": "k",
		"values": map[string]any{"id": json.Number("9007199254740994"), "flag": true}}); code != http.StatusOK {
		t.Fatalf("typed insert = %d %s", code, body)
	}
	if code, body := pgRow(t, h, map[string]any{"op": "delete", "table": "k",
		"key": map[string]any{"id": json.Number("9007199254740994")}}); code != http.StatusOK || !strings.Contains(body, `"rows_affected":1`) {
		t.Fatalf("exact delete = %d %s", code, body)
	}
}

func TestPGRowWritesRefuseTableWithoutPrimaryKey(t *testing.T) {
	h := pgHandler(t)
	if code, out := pgQuery(t, h, "CREATE TABLE nopk(a int, b text)", true); code != http.StatusOK {
		t.Fatalf("create = %d %v", code, out)
	}
	if code, body := pgRow(t, h, map[string]any{"op": "delete", "table": "nopk",
		"key": map[string]any{"a": json.Number("1")}}); code != http.StatusBadRequest {
		t.Fatalf("keyed delete on no-PK = %d %s, want 400", code, body)
	}
	if code, body := pgRow(t, h, map[string]any{"op": "insert", "table": "nopk",
		"values": map[string]any{"a": json.Number("2"), "b": "y"}}); code != http.StatusOK {
		t.Fatalf("insert on no-PK = %d %s", code, body)
	}
}
