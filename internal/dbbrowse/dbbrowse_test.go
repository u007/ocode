package dbbrowse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// makeDB creates a fresh SQLite file at dir/name and runs setup against it.
func makeDB(t *testing.T, name string, setup string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if setup != "" {
		if _, err := db.Exec(setup); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return path
}

func TestProbe(t *testing.T) {
	dir := t.TempDir()

	t.Run("sqlite file", func(t *testing.T) {
		p := makeDB(t, "a.db", `CREATE TABLE t(x)`)
		ok, err := Probe(p)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if !ok {
			t.Fatal("expected a SQLite file to probe true")
		}
	})

	t.Run("text file", func(t *testing.T) {
		p := filepath.Join(dir, "notdb.db")
		if err := os.WriteFile(p, []byte("this is not a database, just text\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ok, err := Probe(p)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if ok {
			t.Fatal("expected a text file to probe false")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		p := filepath.Join(dir, "empty.db")
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		ok, err := Probe(p)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if ok {
			t.Fatal("expected an empty file to probe false")
		}
	})

	t.Run("directory", func(t *testing.T) {
		ok, err := Probe(dir)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if ok {
			t.Fatal("expected a directory to probe false")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if _, err := Probe(filepath.Join(dir, "nope.db")); err == nil {
			t.Fatal("expected an error for a missing file")
		}
	})
}

func TestListTables(t *testing.T) {
	p := makeDB(t, "t.db", `
		CREATE TABLE zebra(id INTEGER PRIMARY KEY, v TEXT);
		CREATE TABLE apple(a INTEGER);
		CREATE VIEW fruit_view AS SELECT * FROM apple;
		CREATE TABLE _private(x);
	`)

	tables, err := ListTables(context.Background(), p)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	var names []string
	for _, tb := range tables {
		names = append(names, tb.Name)
	}
	// Sorted by name; sqlite_ internal tables excluded.
	want := []string{"_private", "apple", "fruit_view", "zebra"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, tb := range tables {
		if tb.Rows != -1 {
			t.Errorf("%s: rows = %d, want -1 without ANALYZE", tb.Name, tb.Rows)
		}
	}
	if tables[2].Type != "view" {
		t.Errorf("fruit_view type = %q, want view", tables[2].Type)
	}
}

func TestListTablesRowEstimates(t *testing.T) {
	p := makeDB(t, "e.db", `
		CREATE TABLE t(a INTEGER);
		INSERT INTO t(a) VALUES (1),(2),(3),(4),(5);
		ANALYZE;
	`)
	tables, err := ListTables(context.Background(), p)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	if tables[0].Rows <= 0 {
		t.Fatalf("expected a positive ANALYZE estimate, got %d", tables[0].Rows)
	}
}

func TestDescribeTable(t *testing.T) {
	p := makeDB(t, "d.db", `
		CREATE TABLE parent(id INTEGER PRIMARY KEY);
		CREATE TABLE child(
			id INTEGER PRIMARY KEY,
			parent_id INTEGER NOT NULL REFERENCES parent(id),
			name TEXT DEFAULT 'anon',
			qty INTEGER,
			computed INTEGER GENERATED ALWAYS AS (qty * 2) VIRTUAL
		);
		CREATE UNIQUE INDEX idx_child_name ON child(name);
	`)

	s, err := DescribeTable(context.Background(), p, "child")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if s.Type != "table" || !s.RowID {
		t.Fatalf("type=%q rowid=%v, want table/true", s.Type, s.RowID)
	}
	if !strings.Contains(s.DDL, "CREATE TABLE child") {
		t.Errorf("DDL missing: %q", s.DDL)
	}

	byName := map[string]Column{}
	for _, c := range s.Columns {
		byName[c.Name] = c
	}
	if byName["id"].PK != 1 {
		t.Errorf("id pk = %d, want 1", byName["id"].PK)
	}
	if !byName["parent_id"].NotNull {
		t.Error("parent_id should be NOT NULL")
	}
	if byName["name"].Default == nil || *byName["name"].Default != "'anon'" {
		t.Errorf("name default = %v, want 'anon'", byName["name"].Default)
	}
	if !byName["computed"].Generated {
		t.Error("computed should be flagged generated")
	}
	if byName["qty"].Generated {
		t.Error("qty should not be generated")
	}

	var foundUnique bool
	for _, ix := range s.Indexes {
		if ix.Name == "idx_child_name" {
			foundUnique = ix.Unique && len(ix.Columns) == 1 && ix.Columns[0] == "name"
		}
	}
	if !foundUnique {
		t.Errorf("expected unique index on name, got %+v", s.Indexes)
	}

	if len(s.ForeignKeys) != 1 || s.ForeignKeys[0].Table != "parent" ||
		s.ForeignKeys[0].From != "parent_id" || s.ForeignKeys[0].To != "id" {
		t.Errorf("foreign keys = %+v", s.ForeignKeys)
	}
}

func TestDescribeTableWithoutRowIDAndView(t *testing.T) {
	p := makeDB(t, "w.db", `
		CREATE TABLE wr(a TEXT PRIMARY KEY, b TEXT) WITHOUT ROWID;
		CREATE VIEW v AS SELECT a FROM wr;
	`)
	ctx := context.Background()

	wr, err := DescribeTable(ctx, p, "wr")
	if err != nil {
		t.Fatalf("DescribeTable wr: %v", err)
	}
	if wr.RowID {
		t.Error("WITHOUT ROWID table should report rowid=false")
	}

	v, err := DescribeTable(ctx, p, "v")
	if err != nil {
		t.Fatalf("DescribeTable v: %v", err)
	}
	if v.Type != "view" || v.RowID {
		t.Errorf("view type=%q rowid=%v, want view/false", v.Type, v.RowID)
	}

	if _, err := DescribeTable(ctx, p, "missing"); err == nil {
		t.Fatal("expected an error for a missing table")
	}
}

func TestQueryValues(t *testing.T) {
	p := makeDB(t, "q.db", `
		CREATE TABLE t(id INTEGER PRIMARY KEY, r REAL, s TEXT, b BLOB);
		INSERT INTO t(id, r, s, b) VALUES (1, 1.5, 'hi', x'89504E47');
		INSERT INTO t(id, r, s, b) VALUES (2, NULL, NULL, NULL);
	`)

	rs, err := Query(context.Background(), p, `SELECT id, r, s, b FROM t ORDER BY id`, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rs.Columns) != 4 || rs.Columns[0].Name != "id" {
		t.Fatalf("columns = %+v", rs.Columns)
	}
	if rs.RowCount != 2 || rs.Truncated {
		t.Fatalf("rowCount=%d truncated=%v, want 2/false", rs.RowCount, rs.Truncated)
	}
	if rs.Rows[0][0].(int64) != 1 {
		t.Errorf("id[0] = %#v", rs.Rows[0][0])
	}
	if rs.Rows[0][1].(float64) != 1.5 {
		t.Errorf("r[0] = %#v", rs.Rows[0][1])
	}
	if rs.Rows[0][2].(string) != "hi" {
		t.Errorf("s[0] = %#v", rs.Rows[0][2])
	}
	blob, ok := rs.Rows[0][3].(Blob)
	if !ok || !blob.IsBlob || blob.Bytes != 4 || blob.Preview != "89504e47" {
		t.Errorf("blob[0] = %#v", rs.Rows[0][3])
	}
	if rs.Rows[1][0].(int64) != 2 || rs.Rows[1][1] != nil || rs.Rows[1][2] != nil || rs.Rows[1][3] != nil {
		t.Errorf("row[1] = %#v", rs.Rows[1])
	}
}

func TestQueryTruncation(t *testing.T) {
	p := makeDB(t, "tr.db", `
		CREATE TABLE t(a INTEGER);
		INSERT INTO t(a) VALUES (1),(2),(3),(4),(5);
	`)
	rs, err := Query(context.Background(), p, `SELECT a FROM t ORDER BY a`, 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if rs.RowCount != 3 || !rs.Truncated {
		t.Fatalf("rowCount=%d truncated=%v, want 3/true", rs.RowCount, rs.Truncated)
	}
}

func TestQueryRejectsWrites(t *testing.T) {
	p := makeDB(t, "ro.db", `CREATE TABLE t(a INTEGER);`)
	ctx := context.Background()
	for _, sqlText := range []string{
		`INSERT INTO t(a) VALUES (1)`,
		`CREATE TABLE x(a)`,
		`DROP TABLE t`,
	} {
		// The allowlist now rejects these before the engine sees them, so the
		// sentinel is the assertion. (It used to be a substring match on the
		// engine's "attempt to write a readonly database", which was the only
		// guard then; errors.Is is stricter, not looser.)
		if _, err := Query(ctx, p, sqlText, 0); err == nil {
			t.Errorf("%q: expected a readonly error", sqlText)
		} else if !errors.Is(err, ErrStatementNotReadOnly) {
			t.Errorf("%q: error = %v, want ErrStatementNotReadOnly", sqlText, err)
		}
	}

	// The database must be unchanged after the rejected writes.
	rs, err := Query(ctx, p, `SELECT count(*) AS n FROM t`, 0)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if rs.Rows[0][0].(int64) != 0 {
		t.Fatalf("table changed after rejected writes: %#v", rs.Rows[0])
	}
}

func TestTablePage(t *testing.T) {
	p := makeDB(t, "p.db", `
		CREATE TABLE t(a INTEGER, b TEXT);
		INSERT INTO t(a,b) VALUES (1,'x'),(2,'y'),(3,'z'),(4,'w'),(5,'v');
	`)
	ctx := context.Background()

	first, err := TablePage(ctx, p, "t", 2, 0)
	if err != nil {
		t.Fatalf("TablePage: %v", err)
	}
	if first.RowCount != 2 || !first.Truncated {
		t.Fatalf("first page rowCount=%d truncated=%v, want 2/true", first.RowCount, first.Truncated)
	}
	if first.Rows[0][0].(int64) != 1 {
		t.Errorf("first row = %#v", first.Rows[0])
	}

	last, err := TablePage(ctx, p, "t", 2, 4)
	if err != nil {
		t.Fatalf("TablePage offset: %v", err)
	}
	if last.RowCount != 1 || last.Truncated {
		t.Fatalf("last page rowCount=%d truncated=%v, want 1/false", last.RowCount, last.Truncated)
	}
}

func TestQuoteIdentResistsInjection(t *testing.T) {
	// A table whose name contains a double quote must round-trip, and a name
	// crafted to break out of quoting must be treated as a (missing) literal
	// identifier rather than executed.
	p := makeDB(t, "q2.db", `CREATE TABLE "we""ird"(a INTEGER); INSERT INTO "we""ird"(a) VALUES (7);`)
	rs, err := TablePage(context.Background(), p, `we"ird`, 10, 0)
	if err != nil {
		t.Fatalf("TablePage quoted name: %v", err)
	}
	if rs.RowCount != 1 || rs.Rows[0][0].(int64) != 7 {
		t.Fatalf("rows = %#v", rs.Rows)
	}

	if _, err := TablePage(context.Background(), p, `t"; DROP TABLE x; --`, 10, 0); err == nil {
		t.Fatal("expected an error for a non-existent injection-shaped table name")
	}
}

// victimDB creates a SQLite file in its own directory, outside mainDB's
// directory, and returns its path. It stands in for any SQLite file on the
// host that the browser must never reach — ocode's own session transcripts
// included.
func victimDB(t *testing.T, setup string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "victim.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open victim: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(setup); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	return path
}

func TestQueryRejectsAttachWriteToOutsideFile(t *testing.T) {
	// mode=ro constrains only the MAIN database, so ATTACH brings up a
	// read-write sibling and a write through it lands on disk. This is the
	// regression that made the browser an arbitrary-write primitive, so the
	// assertion is on the FILE, not just on the returned error.
	main0 := makeDB(t, "m.db", `CREATE TABLE t(a INTEGER);`)
	victim := victimDB(t, `CREATE TABLE secret(a TEXT); INSERT INTO secret VALUES('original');`)

	_, err := Query(context.Background(), main0,
		fmt.Sprintf(`ATTACH 'file:%s' AS v; INSERT INTO v.secret VALUES('PWNED')`, victim), 0)
	if err == nil {
		t.Fatal("ATTACH + INSERT into an outside file succeeded; want a rejection")
	}

	db, err := sql.Open("sqlite", victim)
	if err != nil {
		t.Fatalf("reopen victim: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT a FROM secret`)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, s)
	}
	if len(got) != 1 || got[0] != "original" {
		t.Fatalf("outside file was modified: %#v", got)
	}
}

func TestQueryRejectsAttachReadOfOutsideFile(t *testing.T) {
	// The write half is closed by query_only; the READ half is not, so it needs
	// its own statement-level gate. Without it the browser reads any SQLite
	// file on the host regardless of the allowed roots.
	main0 := makeDB(t, "m.db", `CREATE TABLE t(a INTEGER);`)
	victim := victimDB(t, `CREATE TABLE secret(a TEXT); INSERT INTO secret VALUES('MY PRIVATE SESSION TITLE');`)

	rs, err := Query(context.Background(), main0,
		fmt.Sprintf(`ATTACH 'file:%s' AS v; SELECT a FROM v.secret`, victim), 0)
	if err == nil {
		t.Fatalf("ATTACH + SELECT from an outside file succeeded: %#v", rs.Rows)
	}
}

func TestQueryRejectsAttachCreateOutsideFile(t *testing.T) {
	main0 := makeDB(t, "m.db", `CREATE TABLE t(a INTEGER);`)
	created := filepath.Join(t.TempDir(), "CREATED_BY_ATTACH.db")

	if _, err := Query(context.Background(), main0,
		fmt.Sprintf(`ATTACH 'file:%s' AS w; CREATE TABLE w.t(x)`, created), 0); err == nil {
		t.Fatal("ATTACH + CREATE at an outside path succeeded; want a rejection")
	}
	if _, err := os.Stat(created); err == nil {
		t.Fatal("a new file was created outside the allowed roots")
	}
}

// TestOpenReadOnlyIsWriteProofOnEveryAttachedDatabase pins the SECOND guard.
// The statement allowlist rejects ATTACH before the engine sees it, so
// TestQueryRejectsAttachWriteToOutsideFile cannot tell whether query_only(1)
// works. This calls withReadOnly directly — no allowlist in the path — so
// removing the allowlist cannot silently remove write protection too. The two
// guards fail independently and neither is sufficient alone.
func TestOpenReadOnlyIsWriteProofOnEveryAttachedDatabase(t *testing.T) {
	main0 := makeDB(t, "m.db", `CREATE TABLE t(a INTEGER);`)
	victim := victimDB(t, `CREATE TABLE secret(a TEXT); INSERT INTO secret VALUES('original');`)

	ctx := context.Background()
	err := withReadOnly(ctx, main0, func(db *sql.DB) error {
		// ATTACH itself is allowed by the engine; the point is what happens next.
		if _, err := db.Exec(fmt.Sprintf(`ATTACH 'file:%s' AS v`, victim)); err != nil {
			return fmt.Errorf("attach: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO v.secret VALUES('PWNED')`); err == nil {
			return errors.New("query_only did not block a write through an attached database")
		}
		// A write to the MAIN database must also be blocked.
		if _, err := db.Exec(`INSERT INTO t(a) VALUES(1)`); err == nil {
			return errors.New("query_only did not block a write to the main database")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withReadOnly: %v", err)
	}

	db, err := sql.Open("sqlite", victim)
	if err != nil {
		t.Fatalf("reopen victim: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM secret WHERE a = 'PWNED'`).Scan(&n); err != nil {
		t.Fatalf("check victim: %v", err)
	}
	if n != 0 {
		t.Fatal("the outside file was written despite query_only")
	}
}

func TestQueryRejectsNonReadStatements(t *testing.T) {
	// The allowlist is the real gate; query_only only covers writes. Each case
	// here must be refused on its own merits, so a case that slips through is a
	// hole rather than a coincidence.
	p := makeDB(t, "allow.db", `CREATE TABLE t(a INTEGER);`)
	ctx := context.Background()
	for _, sqlText := range []string{
		`ATTACH DATABASE ':memory:' AS mem`,
		`DETACH DATABASE main`,
		`PRAGMA journal_mode = WAL`,
		`VACUUM`,
		`REINDEX`,
		`INSERT INTO t(a) SELECT 1; INSERT INTO t(a) SELECT 2`,
	} {
		if _, err := Query(ctx, p, sqlText, 0); err == nil {
			t.Errorf("%q: accepted; want a rejection", sqlText)
		}
	}
}

func TestQueryAllowsReadOnlyStatements(t *testing.T) {
	// The allowlist must not over-block the reads the browser exists for.
	p := makeDB(t, "ok.db", `
		CREATE TABLE t(a INTEGER);
		INSERT INTO t(a) VALUES(1),(2);
		CREATE VIEW v AS SELECT a FROM t WHERE a > 1;
	`)
	ctx := context.Background()
	for _, sqlText := range []string{
		`SELECT a FROM t`,
		`WITH x AS (SELECT a FROM t) SELECT * FROM x`,
		`EXPLAIN SELECT a FROM t`,
		`EXPLAIN QUERY PLAN SELECT a FROM t`,
		`SELECT * FROM v`,
		`PRAGMA table_info(t)`,
	} {
		if _, err := Query(ctx, p, sqlText, 0); err != nil {
			t.Errorf("%q: rejected a read-only statement: %v", sqlText, err)
		}
	}
}

func TestQueryRejectsPragmaSetters(t *testing.T) {
	// An allowlisted pragma NAME is not enough: the same names write when given
	// a value, and journal_mode/auto_vacuum can succeed on a read-only handle.
	p := makeDB(t, "pragma.db", `CREATE TABLE t(a INTEGER);`)
	ctx := context.Background()
	for _, sqlText := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA auto_vacuum=1`,
		`PRAGMA secure_delete = 1`,
		`PRAGMA max_page_count=10`,
		`PRAGMA user_version(5)`,
		`PRAGMA table_info = t`,
	} {
		if err := validateReadOnlyStatement(sqlText); err == nil {
			t.Errorf("%q: accepted; want a rejection", sqlText)
		}
	}
	for _, sqlText := range []string{`PRAGMA journal_mode`, `PRAGMA user_version`, `PRAGMA table_info(t)`} {
		if _, err := Query(ctx, p, sqlText, 0); err != nil {
			t.Errorf("%q: rejected a read form: %v", sqlText, err)
		}
	}
}
