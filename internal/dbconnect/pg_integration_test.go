//go:build pgintegration

package dbconnect

// Live checks against a real Postgres server. Run with scripts/test-postgres.sh,
// or with OCODE_TEST_POSTGRES_URL set and `go test -tags pgintegration -run TestPG`.
// Every test gets its own database, so tests do not see each other's tables.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/dbconnect/dbtest"
)

func pgOpen(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), dbtest.NewDatabase(t))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func pgExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
}

func pgCount(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func pgJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPGReadOnlyRefusesWrites(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)", "INSERT INTO k VALUES (1, true)")

	_, err := QueryReadOnly(context.Background(), db, "DELETE FROM k")
	if !IsReadOnlyViolation(err) {
		t.Fatalf("direct DELETE error = %v, want read-only violation (25006)", err)
	}
	if n := pgCount(t, db, "k"); n != 1 {
		t.Fatalf("rows after refused DELETE = %d, want 1", n)
	}
}

// Each escape puts a commit or a second statement in front of the DELETE. The
// read-only path and the confirmed path both refuse it, and no row changes.
func TestPGMultiStatementEscapesAreRefused(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)",
		"INSERT INTO k VALUES (1, true), (2, true)")
	ctx := context.Background()
	for _, sqlText := range []string{
		"COMMIT; DELETE FROM k",
		"END; DELETE FROM k",
		"ROLLBACK; DELETE FROM k",
		"SELECT 1; COMMIT; DELETE FROM k",
		"SELECT 1; SELECT 2",
	} {
		if _, err := QueryReadOnly(ctx, db, sqlText); err == nil {
			t.Errorf("read-only %q succeeded", sqlText)
		}
		if _, err := ExecWrite(ctx, db, sqlText); err == nil {
			t.Errorf("confirmed %q succeeded", sqlText)
		}
	}
	if n := pgCount(t, db, "k"); n != 2 {
		t.Fatalf("rows after escapes = %d, want 2", n)
	}
}

// Describing a statement must not run it. DROP TABLE is the proof: if the
// describe step executed it, the table would be gone.
func TestPGDescribeExecutesNothing(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE k(id bigint PRIMARY KEY)")
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	}()
	for _, s := range []string{"DROP TABLE k", "CREATE TABLE other(id int)", "DELETE FROM k"} {
		width, err := describeWidth(context.Background(), conn, s)
		if err != nil {
			t.Fatalf("describe %q: %v", s, err)
		}
		if width != 0 {
			t.Fatalf("describe %q width = %d, want 0", s, width)
		}
	}
	var exists bool
	if err := db.QueryRow("SELECT to_regclass('k') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("DROP TABLE k ran during describe")
	}
}

func TestPGVacuumIsRefusedInTransaction(t *testing.T) {
	db := pgOpen(t)
	if _, err := ExecWrite(context.Background(), db, "VACUUM"); err == nil {
		t.Fatal("VACUUM inside the confirmed transaction succeeded")
	}
}

func TestPGConfirmedDDLAndDMLRowCounts(t *testing.T) {
	db := pgOpen(t)
	ctx := context.Background()
	res, err := ExecWrite(ctx, db, "CREATE TABLE k(id int PRIMARY KEY, flag bool)")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if *res.RowsAffected != 0 || len(res.Columns) != 0 {
		t.Fatalf("create result = %+v", res)
	}
	pgExec(t, db, "INSERT INTO k SELECT g, true FROM generate_series(1, 5) g")
	res, err = ExecWrite(ctx, db, "UPDATE k SET flag = false WHERE id <= 3")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if *res.RowsAffected != 3 {
		t.Fatalf("update rows_affected = %d, want 3", *res.RowsAffected)
	}
}

func TestPGReturningMatchesSelect(t *testing.T) {
	db := pgOpen(t)
	ctx := context.Background()
	pgExec(t, db, `CREATE TABLE typed(id bigint PRIMARY KEY, u uuid, n numeric(10,2),
		ts timestamptz, b bytea, j jsonb, flag bool)`)

	ret, err := ExecWrite(ctx, db, `INSERT INTO typed VALUES (9007199254740993,
		'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', 12.34, '2026-10-09 12:00:00+00',
		'\xdeadbeef', '{"a":1,"b":[true,null]}', true) RETURNING *`)
	if err != nil {
		t.Fatalf("insert returning: %v", err)
	}
	sel, err := QueryReadOnly(ctx, db, "SELECT * FROM typed WHERE id = 9007199254740993")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got, want := pgJSON(t, ret.Rows), pgJSON(t, sel.Rows); got != want {
		t.Fatalf("RETURNING rows differ from SELECT:\n ret=%s\n sel=%s", got, want)
	}
	if *ret.RowsAffected != 1 {
		t.Fatalf("rows_affected = %d, want 1", *ret.RowsAffected)
	}
	if !strings.Contains(pgJSON(t, ret.Rows), `"9007199254740993"`) {
		t.Fatalf("bigint not exact: %s", pgJSON(t, ret.Rows))
	}
}

func TestPGByteaRendersAsHex(t *testing.T) {
	db := pgOpen(t)
	res, err := QueryReadOnly(context.Background(), db, `SELECT '\xdeadbeef'::bytea AS b`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Rows[0][0]; got != `\xdeadbeef` {
		t.Fatalf("bytea = %v, want \\xdeadbeef", got)
	}
}

func TestPGReturningTruncatesButCountsEveryRow(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE many(g int)")
	res, err := ExecWrite(context.Background(), db,
		"INSERT INTO many SELECT g FROM generate_series(1, 1005) g RETURNING g")
	if err != nil {
		t.Fatal(err)
	}
	if *res.RowsAffected != 1005 || !res.Truncated || len(res.Rows) != maxQueryRows {
		t.Fatalf("rows_affected=%d truncated=%v rows=%d, want 1005/true/%d",
			*res.RowsAffected, res.Truncated, len(res.Rows), maxQueryRows)
	}
	if n := pgCount(t, db, "many"); n != 1005 {
		t.Fatalf("committed rows = %d, want 1005", n)
	}
}

// Postgres reports the top-level command tag. For WITH … DELETE … SELECT that
// tag is SELECT, so rows_affected is the number of rows the SELECT returned,
// and the deleted rows are not counted. This test pins that behaviour.
func TestPGDataModifyingCTEReportsReturnedRows(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE k(id int PRIMARY KEY)", "INSERT INTO k SELECT g FROM generate_series(1, 4) g")
	res, err := ExecWrite(context.Background(), db,
		"WITH d AS (DELETE FROM k WHERE id <= 2 RETURNING id) SELECT count(*) FROM d")
	if err != nil {
		t.Fatalf("cte delete: %v", err)
	}
	if *res.RowsAffected != 1 {
		t.Fatalf("rows_affected = %d, want 1 (the one row the SELECT returned)", *res.RowsAffected)
	}
	if n := pgCount(t, db, "k"); n != 2 {
		t.Fatalf("rows left = %d, want 2 (two were deleted)", n)
	}
}

func TestPGReadOnlyFilterCannotWrite(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE k(id int PRIMARY KEY)", "INSERT INTO k VALUES (1)",
		"CREATE SEQUENCE sq")
	cols, err := TableColumns(context.Background(), db, "k")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"nextval('sq') > 0", "true) --", "1=1); DELETE FROM k", "1=1 /*"} {
		q, err := BuildBrowseSQL("k", cols, "", false, f, 10, 0)
		if err != nil {
			t.Fatalf("build filter %q: %v", f, err)
		}
		if _, err := QueryReadOnly(context.Background(), db, q); err == nil {
			t.Errorf("filter %q ran", f)
		}
	}
	if n := pgCount(t, db, "k"); n != 1 {
		t.Fatalf("rows after filters = %d, want 1", n)
	}
}

func TestPGBrowsePageSortAndPaging(t *testing.T) {
	db := pgOpen(t)
	ctx := context.Background()
	pgExec(t, db, "CREATE TABLE k(id bigint PRIMARY KEY, amount numeric(10,2))",
		"INSERT INTO k VALUES (9007199254740992, 1.00), (9007199254740993, 3.00), (9007199254740994, 2.00)")

	page, err := BrowsePage(ctx, db, "k", "", false, "", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Rows) != 2 || page.Rows[0][0] != "9007199254740992" {
		t.Fatalf("first page = %+v", page)
	}
	page, err = BrowsePage(ctx, db, "k", "amount", true, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Rows[0][1] != "3.00" || page.Rows[2][1] != "1.00" {
		t.Fatalf("sorted desc = %v", page.Rows)
	}
	if _, err := BrowsePage(ctx, db, "k", "amount; DROP TABLE k", false, "", 10, 0); err == nil {
		t.Fatal("sort on a non-column succeeded")
	}
	if n := pgCount(t, db, "k"); n != 3 {
		t.Fatalf("rows after sort injection = %d, want 3", n)
	}
}

func TestPGListTablesPagesSorted(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE a(x int)", "CREATE TABLE b(x int)", "CREATE TABLE c(x int)")
	page, err := ListTables(context.Background(), db, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || pgJSON(t, page.Tables) != `["a","b"]` {
		t.Fatalf("first page = %+v", page)
	}
	page, err = ListTables(context.Background(), db, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore || pgJSON(t, page.Tables) != `["c"]` {
		t.Fatalf("second page = %+v", page)
	}
}

func TestPGTableColumnsPrimaryKeyFollowsKeyOrder(t *testing.T) {
	db := pgOpen(t)
	pgExec(t, db, "CREATE TABLE pair(a int, b int, c text, PRIMARY KEY (b, a))")
	cols, err := TableColumns(context.Background(), db, "pair")
	if err != nil {
		t.Fatal(err)
	}
	if got := pgJSON(t, PrimaryKey(cols)); got != `["b","a"]` {
		t.Fatalf("primary key = %s, want [\"b\",\"a\"] (key order, not column order)", got)
	}
	if _, err := BrowsePage(context.Background(), db, "missing", "", false, "", 10, 0); !errors.Is(err, ErrNoSuchTable) {
		t.Fatalf("missing table error = %v, want ErrNoSuchTable", err)
	}
}

func TestPGKeyedWritesAreExact(t *testing.T) {
	db := pgOpen(t)
	ctx := context.Background()
	pgExec(t, db, "CREATE TABLE k(id bigint PRIMARY KEY, flag bool)",
		"INSERT INTO k VALUES (9007199254740992, false), (9007199254740993, false)")

	n, err := ApplyRowChange(ctx, db, "update", "k",
		map[string]any{"id": "9007199254740993"}, map[string]any{"flag": "true"})
	if err != nil || n != 1 {
		t.Fatalf("update = %d, %v", n, err)
	}
	var flags []bool
	rows, err := db.Query("SELECT flag FROM k ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var f bool
		if err := rows.Scan(&f); err != nil {
			t.Fatal(err)
		}
		flags = append(flags, f)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 || flags[0] || !flags[1] {
		t.Fatalf("flags = %v, want [false true]: the neighbour key was touched", flags)
	}

	if _, err := ApplyRowChange(ctx, db, "delete", "k", map[string]any{"id": "1"}, map[string]any{}); !errors.Is(err, ErrNoRowMatched) {
		t.Fatalf("missing-key delete = %v, want ErrNoRowMatched", err)
	}
	if _, err := ApplyRowChange(ctx, db, "delete", "k", map[string]any{"flag": "false"}, map[string]any{}); err == nil {
		t.Fatal("delete keyed by a non-primary-key column succeeded")
	}
	if n := pgCount(t, db, "k"); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestPGKeylessTableIsInsertOnly(t *testing.T) {
	db := pgOpen(t)
	ctx := context.Background()
	pgExec(t, db, "CREATE TABLE nopk(a int, b text)")
	if _, err := ApplyRowChange(ctx, db, "insert", "nopk", map[string]any{}, map[string]any{"a": "2", "b": "y"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := ApplyRowChange(ctx, db, "delete", "nopk", map[string]any{"a": "2"}, map[string]any{}); err == nil {
		t.Fatal("keyed delete on a table without a primary key succeeded")
	}
	if n := pgCount(t, db, "nopk"); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}
