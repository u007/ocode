package dbbrowse

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ── SplitStatements ─────────────────────────────────────────────────────────

func TestSplitStatementsBasics(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "SELECT 1", []string{"SELECT 1"}},
		{"two", "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"trailing", "SELECT 1;", []string{"SELECT 1"}},
		{"empty", "   ", nil},
		{"semicolon in string", "INSERT INTO t VALUES ('a;b')", []string{"INSERT INTO t VALUES ('a;b')"}},
		{"semicolon in ident", `SELECT "a;b" FROM t`, []string{`SELECT "a;b" FROM t`}},
		{"line comment", "SELECT 1 -- ; not a split\n; SELECT 2", []string{"SELECT 1 -- ; not a split", "SELECT 2"}},
		{"block comment", "SELECT /* ; */ 1; SELECT 2", []string{"SELECT /* ; */ 1", "SELECT 2"}},
		{"escaped quote", "SELECT 'it''s;ok'", []string{"SELECT 'it''s;ok'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitStatements(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SplitStatements(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// A CREATE TRIGGER body contains top-level semicolons and a CASE ... END; the
// splitter must keep the whole trigger as ONE statement, or Exec would run the
// fragments and fail.
func TestSplitStatementsKeepsTriggerBody(t *testing.T) {
	script := `CREATE TRIGGER tr AFTER INSERT ON t BEGIN
		UPDATE t SET a = CASE WHEN a > 0 THEN 1 ELSE 0 END;
		UPDATE t SET b = 2;
	END;
	SELECT 1`
	got := SplitStatements(script)
	if len(got) != 2 {
		t.Fatalf("got %d statements, want 2: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "CASE") || !strings.Contains(got[0], "UPDATE t SET b = 2") {
		t.Fatalf("trigger body was split: %q", got[0])
	}
	if got[1] != "SELECT 1" {
		t.Fatalf("second statement = %q, want SELECT 1", got[1])
	}
}

func TestSplitStatementsTriggerExecutes(t *testing.T) {
	path := makeDB(t, "trig.db", `CREATE TABLE t(a INTEGER, b INTEGER);
		CREATE TRIGGER tr AFTER INSERT ON t BEGIN
			UPDATE t SET b = CASE WHEN a > 0 THEN 10 ELSE 20 END;
		END;`)
	// The trigger fires on insert; if the splitter had broken the body the
	// CREATE TRIGGER would have failed at setup. Insert and check the effect.
	if _, err := RowInsert(context.Background(), path, "t", RowValues{"a": int64(5)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rs, err := Query(context.Background(), path, "SELECT a, b FROM t", 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rs.Rows))
	}
	if rs.Rows[0][1] != int64(10) {
		t.Fatalf("trigger did not fire correctly: b = %v, want 10", rs.Rows[0][1])
	}
}

// ── BlockedWriteStatement ───────────────────────────────────────────────────

func TestBlockedWriteStatement(t *testing.T) {
	blocked := []string{
		"ATTACH DATABASE 'x' AS y",
		"attach 'x' as y",
		"DETACH DATABASE y",
		"VACUUM INTO 'out.db'",
		"VACUUM main INTO 'out.db'",
	}
	for _, s := range blocked {
		if _, ok := BlockedWriteStatement(s); !ok {
			t.Errorf("BlockedWriteStatement(%q) = false, want true", s)
		}
	}
	allowed := []string{
		"INSERT INTO t VALUES (1)",
		"INSERT INTO t VALUES ('attach')", // inside a string literal
		"UPDATE t SET a = 1 WHERE b = 2",
		"DELETE FROM t",
		"CREATE TABLE t(a INTEGER)",
	}
	for _, s := range allowed {
		if kw, ok := BlockedWriteStatement(s); ok {
			t.Errorf("BlockedWriteStatement(%q) = %q, want false", s, kw)
		}
	}
}

// ── NormalizeValue ──────────────────────────────────────────────────────────

func TestNormalizeValue(t *testing.T) {
	i, err := NormalizeValue(json.Number("42"))
	if err != nil || i != int64(42) {
		t.Fatalf("int: got %v (%T) err=%v", i, i, err)
	}
	f, err := NormalizeValue(json.Number("4.5"))
	if err != nil || f != 4.5 {
		t.Fatalf("float: got %v err=%v", f, err)
	}
	if _, err := NormalizeValue(json.Number("notanumber")); err == nil {
		t.Fatal("expected error for non-numeric json.Number")
	}
	blob, err := NormalizeValue(map[string]any{"$blob": true, "data": "aGVsbG8="})
	if err != nil || string(blob.([]byte)) != "hello" {
		t.Fatalf("blob: got %v err=%v", blob, err)
	}
	if _, err := NormalizeValue(map[string]any{"nope": 1}); err == nil {
		t.Fatal("expected error for unsupported object")
	}
	if v, err := NormalizeValue(nil); err != nil || v != nil {
		t.Fatalf("nil: got %v err=%v", v, err)
	}
}

// ── Row CRUD ────────────────────────────────────────────────────────────────

func TestRowInsertUpdateDelete(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "crud.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, score REAL)`)

	res, err := RowInsert(ctx, path, "t", RowValues{"id": int64(1), "name": "alice", "score": 9.5})
	if err != nil || res.RowsAffected != 1 {
		t.Fatalf("insert: res=%+v err=%v", res, err)
	}
	if res.LastInsertID != 1 {
		t.Fatalf("last insert id = %d, want 1", res.LastInsertID)
	}
	if _, err := RowInsert(ctx, path, "t", RowValues{"id": int64(2), "name": "bob"}); err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	if _, err := RowUpdate(ctx, path, "t", RowValues{"id": int64(1)}, RowValues{"name": "ALICE"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := RowDelete(ctx, path, "t", RowValues{"id": int64(2)}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rs, err := Query(ctx, path, "SELECT id, name FROM t ORDER BY id", 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	want := [][]any{{int64(1), "ALICE"}}
	if !reflect.DeepEqual(rs.Rows, want) {
		t.Fatalf("rows = %#v, want %#v", rs.Rows, want)
	}
}

// A key that matches more than one row must be REJECTED AND ROLLED BACK. In
// autocommit both rows would already have been changed before the count was
// checked; the transaction in execExact is what makes this contract real.
func TestRowUpdateMultiRowKeyRollsBack(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "multi.db", `CREATE TABLE t(a INTEGER);
		INSERT INTO t VALUES (1), (1);`)

	_, err := RowUpdate(ctx, path, "t", RowValues{"a": int64(1)}, RowValues{"a": int64(2)})
	if !errors.Is(err, ErrMultipleRowsAffected) {
		t.Fatalf("err = %v, want ErrMultipleRowsAffected", err)
	}
	rs, err := Query(ctx, path, "SELECT a FROM t ORDER BY a", 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, row := range rs.Rows {
		if row[0] != int64(1) {
			t.Fatalf("row was modified despite rollback: %#v", rs.Rows)
		}
	}
}

func TestRowDeleteNoMatch(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "nomatch.db", `CREATE TABLE t(id INTEGER PRIMARY KEY);
		INSERT INTO t VALUES (1);`)
	if _, err := RowDelete(ctx, path, "t", RowValues{"id": int64(99)}); !errors.Is(err, ErrNoRowsAffected) {
		t.Fatalf("err = %v, want ErrNoRowsAffected", err)
	}
}

// A NULL key component must still match the row it came from (NULL-safe WHERE).
func TestRowUpdateNullKey(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "nullkey.db", `CREATE TABLE t(a TEXT, b TEXT);
		INSERT INTO t VALUES (NULL, 'x');`)
	if _, err := RowUpdate(ctx, path, "t", RowValues{"a": nil, "b": "x"}, RowValues{"b": "y"}); err != nil {
		t.Fatalf("update with NULL key: %v", err)
	}
	rs, _ := Query(ctx, path, "SELECT b FROM t", 10)
	if len(rs.Rows) != 1 || rs.Rows[0][0] != "y" {
		t.Fatalf("rows = %#v, want b=y", rs.Rows)
	}
}

// A table with no declared primary key is addressed by the true rowid exposed
// as the synthetic _rowid_ column.
func TestRowCRUDByRowID(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "rowid.db", `CREATE TABLE t(x TEXT);
		INSERT INTO t VALUES ('a'), ('b');`)

	page, err := TablePageKeyed(ctx, path, "t", 10, 0, true)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if len(page.Columns) == 0 || page.Columns[0].Name != RowIDColumn {
		t.Fatalf("first column = %#v, want %q", page.Columns, RowIDColumn)
	}
	rowid := page.Rows[1][0]

	if _, err := RowUpdate(ctx, path, "t", RowValues{RowIDColumn: rowid}, RowValues{"x": "B"}); err != nil {
		t.Fatalf("update by rowid: %v", err)
	}
	if _, err := RowDelete(ctx, path, "t", RowValues{RowIDColumn: page.Rows[0][0]}); err != nil {
		t.Fatalf("delete by rowid: %v", err)
	}
	rs, _ := Query(ctx, path, "SELECT x FROM t", 10)
	if len(rs.Rows) != 1 || rs.Rows[0][0] != "B" {
		t.Fatalf("rows = %#v, want one row B", rs.Rows)
	}
}

func TestBlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "blob.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, data BLOB)`)
	raw, _ := NormalizeValue(map[string]any{"$blob": true, "data": "aGVsbG8="})
	if _, err := RowInsert(ctx, path, "t", RowValues{"id": int64(1), "data": raw}); err != nil {
		t.Fatalf("insert blob: %v", err)
	}
	rs, err := Query(ctx, path, "SELECT data FROM t", 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	got, ok := rs.Rows[0][0].(Blob)
	if !ok || got.Bytes != 5 || got.Preview != "68656c6c6f" {
		t.Fatalf("blob = %#v, want 5 bytes hex 68656c6c6f", rs.Rows[0][0])
	}
}

// ── Exec ────────────────────────────────────────────────────────────────────

func TestExecMultiStatementAtomic(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "exec.db", `CREATE TABLE t(a INTEGER)`)
	_, err := Exec(ctx, path, `INSERT INTO t VALUES (1);
		INSERT INTO t VALUES (2);
		INSERT INTO does_not_exist VALUES (3);`)
	if err == nil {
		t.Fatal("expected the batch to fail")
	}
	// The two successful inserts must have been rolled back with the failure.
	rs, _ := Query(ctx, path, "SELECT COUNT(*) FROM t", 10)
	if rs.Rows[0][0] != int64(0) {
		t.Fatalf("rows = %v, want 0 (batch not atomic)", rs.Rows[0][0])
	}
}

func TestExecSumsRowsAffected(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "execsum.db", `CREATE TABLE t(a INTEGER)`)
	res, err := Exec(ctx, path, `INSERT INTO t VALUES (1); INSERT INTO t VALUES (2);`)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.RowsAffected != 2 {
		t.Fatalf("rows affected = %d, want 2", res.RowsAffected)
	}
}

// ── DDL builders ────────────────────────────────────────────────────────────

func TestBuildCreateTable(t *testing.T) {
	got, err := BuildCreateTable("t", []ColumnDef{
		{Name: "id", Type: "INTEGER", PK: true},
		{Name: "name", Type: "TEXT", NotNull: true},
		{Name: "score", Type: "REAL", Default: strptr("0")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `CREATE TABLE "t" ("id" INTEGER PRIMARY KEY, "name" TEXT NOT NULL, "score" REAL DEFAULT 0)`
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestBuildCreateTableCompositePK(t *testing.T) {
	got, err := BuildCreateTable("t", []ColumnDef{
		{Name: "a", Type: "INTEGER", PK: true},
		{Name: "b", Type: "INTEGER", PK: true},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, `PRIMARY KEY ("a", "b")`) {
		t.Fatalf("composite PK missing: %q", got)
	}
}

func TestBuildCreateTableRejectsTypeInjection(t *testing.T) {
	bad := []string{
		"INTEGER PRIMARY KEY",     // a space would smuggle a constraint
		"TEXT NOT NULL",           // ditto
		"TEXT); DROP TABLE x; --", // breakout attempt
	}
	for _, typ := range bad {
		if _, err := BuildCreateTable("t", []ColumnDef{{Name: "a", Type: typ}}); err == nil {
			t.Errorf("type %q was accepted, want rejection", typ)
		}
	}
}

func TestBuildAddColumn(t *testing.T) {
	got, err := BuildAddColumn("t", ColumnDef{Name: "age", Type: "INTEGER"})
	if err != nil || got != `ALTER TABLE "t" ADD COLUMN "age" INTEGER` {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := BuildAddColumn("t", ColumnDef{Name: "age", Type: "INTEGER", NotNull: true}); err == nil {
		t.Fatal("NOT NULL without default should be rejected")
	}
	if _, err := BuildAddColumn("t", ColumnDef{Name: "id", Type: "INTEGER", PK: true}); err == nil {
		t.Fatal("adding a PK column should be rejected")
	}
}

func TestBuildIndexAndDrop(t *testing.T) {
	got, err := BuildCreateIndex("t", "idx", []string{"a", "b"}, true)
	if err != nil || got != `CREATE UNIQUE INDEX "idx" ON "t" ("a", "b")` {
		t.Fatalf("create index = %q err=%v", got, err)
	}
	if got, _ := BuildDropIndex("idx"); got != `DROP INDEX "idx"` {
		t.Fatalf("drop index = %q", got)
	}
	if got, _ := BuildDropTable("t"); got != `DROP TABLE "t"` {
		t.Fatalf("drop table = %q", got)
	}
}

func TestRenderDefaultSafety(t *testing.T) {
	cases := map[string]string{
		"CURRENT_TIMESTAMP": "CURRENT_TIMESTAMP",
		"5":                 "5",
		"-1.5e3":            "-1.5e3",
		"hello":             "'hello'",
		"it's":              "'it''s'",
		"NaN":               "'NaN'", // must NOT be emitted raw
		"0x10":              "'0x10'",
	}
	for in, want := range cases {
		got, err := renderDefault(in)
		if err != nil {
			t.Fatalf("renderDefault(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("renderDefault(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── KeyColumns ──────────────────────────────────────────────────────────────

func TestKeyColumns(t *testing.T) {
	ctx := context.Background()

	pk := makeDB(t, "pk.db", `CREATE TABLE t(a INTEGER, b INTEGER, PRIMARY KEY(a, b))`)
	s, err := DescribeTable(ctx, pk, "t")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if cols, ok := s.KeyColumns(); !ok || !reflect.DeepEqual(cols, []string{"a", "b"}) {
		t.Fatalf("pk key = %v ok=%v", cols, ok)
	}

	noPK := makeDB(t, "nopk.db", `CREATE TABLE t(x TEXT)`)
	s, _ = DescribeTable(ctx, noPK, "t")
	if cols, ok := s.KeyColumns(); !ok || !reflect.DeepEqual(cols, []string{RowIDColumn}) {
		t.Fatalf("rowid key = %v ok=%v", cols, ok)
	}

	view := makeDB(t, "view.db", `CREATE TABLE t(x TEXT); CREATE VIEW v AS SELECT * FROM t`)
	s, _ = DescribeTable(ctx, view, "v")
	if _, ok := s.KeyColumns(); ok {
		t.Fatal("a view should have no editable key")
	}
}

func strptr(s string) *string { return &s }
