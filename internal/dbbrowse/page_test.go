package dbbrowse

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── PageOptions ────────────────────────────────────────────────────────────

func TestPageOptionsDefaults(t *testing.T) {
	o := PageOptions{}
	if o.limit() != DefaultLimit {
		t.Fatalf("limit = %d, want the package default %d", o.limit(), DefaultLimit)
	}
	if o.offset() != 0 {
		t.Fatalf("offset = %d, want 0", o.offset())
	}
	if o.pageSize() != DefaultLimit+1 {
		t.Fatalf("pageSize = %d, want limit+1 (the extra row that reveals truncation)", o.pageSize())
	}
}

func TestPageOptionsClamp(t *testing.T) {
	o := PageOptions{Limit: MaxLimit + 500, Offset: -7}
	if o.limit() != MaxLimit {
		t.Fatalf("limit = %d, want the MaxLimit clamp %d", o.limit(), MaxLimit)
	}
	if o.offset() != 0 {
		t.Fatalf("offset = %d, want a negative offset clamped to 0", o.offset())
	}
}

// ── Filter / sort assembly ─────────────────────────────────────────────────

// A filter is interpolated into the SELECT, so the assembled statement must
// still pass the read-only gate: that is what keeps a filter from smuggling a
// second statement or an ATTACH past containment.
func TestFilterMayNotSmuggleAnotherStatement(t *testing.T) {
	_, err := filterExpression("1=1; ATTACH '/etc/passwd' AS p")
	if err == nil {
		t.Fatal("expected a multi-statement filter to be rejected")
	}
	if !errors.Is(err, ErrStatementNotReadOnly) {
		t.Fatalf("err = %v, want ErrStatementNotReadOnly", err)
	}
}

// A subquery in a filter is legal read-only SQL and must be admitted: the
// containment story rests on ATTACH being impossible and query_only refusing
// writes, not on the filter being a bare literal comparison.
func TestFilterAllowsASubquery(t *testing.T) {
	clause, err := filterExpression("id IN (SELECT id FROM t WHERE n > 5)")
	if err != nil {
		t.Fatalf("a read-only subquery filter should be admitted: %v", err)
	}
	if !strings.Contains(clause, "SELECT id FROM t") {
		t.Fatalf("clause = %q, want the subquery preserved", clause)
	}
}

func TestFilterMayNotAttach(t *testing.T) {
	if _, err := filterExpression("1=1 AND (SELECT 1 FROM (SELECT 1) WHERE 1=ATTACH)"); !errors.Is(err, ErrStatementNotReadOnly) {
		t.Fatalf("err = %v, want ErrStatementNotReadOnly", err)
	}
	if _, err := filterExpression("1=1 AND (SELECT 1 FROM (SELECT 1) WHERE 1=ATTACH)"); !errors.Is(err, ErrStatementNotReadOnly) {
		t.Fatalf("err = %v, want ErrStatementNotReadOnly", err)
	}
	// A COMMENT mentioning ATTACH is inert: stripSQLNoise drops it before the
	// engine sees it, so the filter is an ordinary read and must be admitted.
	// Refusing it would be wrong — the comment can never attach anything.
	if _, err := filterExpression("1=1 -- ATTACH '/etc/passwd' AS p"); err != nil {
		t.Fatalf("a commented-out ATTACH is not executable and should be admitted: %v", err)
	}
}

func TestFilterMayNotWrite(t *testing.T) {
	_, err := filterExpression("1=1) ; DELETE FROM t --")
	if !errors.Is(err, ErrStatementNotReadOnly) {
		t.Fatalf("err = %v, want ErrStatementNotReadOnly", err)
	}
}

// An empty filter yields no WHERE clause at all, so the ordinary table page is
// byte-identical to what TablePageKeyed produced before this option existed.
func TestEmptyFilterAddsNoWhere(t *testing.T) {
	clause, err := filterExpression("   ")
	if err != nil {
		t.Fatalf("filterExpression: %v", err)
	}
	if clause != "" {
		t.Fatalf("clause = %q, want empty", clause)
	}
}

func TestFilterIsWrappedInParentheses(t *testing.T) {
	clause, err := filterExpression("a = 1 OR b = 2")
	if err != nil {
		t.Fatalf("filterExpression: %v", err)
	}
	if clause != "WHERE (a = 1 OR b = 2)" {
		t.Fatalf("clause = %q", clause)
	}
}

// The sort column reaches SQL as text, so it is only admitted when the table
// really has that column (or the synthetic rowid column). Quoting alone would
// make it safe but would still let `ORDER BY` name a column from another table
// or an expression.
func TestSortColumnMustExistOnTheTable(t *testing.T) {
	cols := []string{"id", "name"}
	if _, err := sortClause("name", false, cols); err != nil {
		t.Fatalf("an existing column should sort: %v", err)
	}
	if _, err := sortClause("other", false, cols); err == nil {
		t.Fatal("expected a column the table does not have to be rejected")
	}
	if _, err := sortClause(`name" ; DROP TABLE t --`, false, cols); err == nil {
		t.Fatal("expected an injection-shaped column name to be rejected")
	}
}

func TestSortClauseDirection(t *testing.T) {
	cols := []string{"name"}
	clause, err := sortClause("name", false, cols)
	if err != nil || clause != `ORDER BY "name" ASC` {
		t.Fatalf("asc clause = %q err = %v", clause, err)
	}
	clause, err = sortClause("name", true, cols)
	if err != nil || clause != `ORDER BY "name" DESC` {
		t.Fatalf("desc clause = %q err = %v", clause, err)
	}
	// The rowid column is addressable even though it is not in the column list.
	if _, err := sortClause(RowIDColumn, false, cols); err != nil {
		t.Fatalf("rowid sort: %v", err)
	}
	// No sort requested means no clause.
	clause, err = sortClause("", true, cols)
	if err != nil || clause != "" {
		t.Fatalf("empty sort = %q err = %v", clause, err)
	}
}

// ── TablePageFiltered ──────────────────────────────────────────────────────

const filterFixture = `CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, n INTEGER);
	INSERT INTO t VALUES (1,'alpha',10),(2,'beta',20),(3,'gamma',30),(4,'delta',40);`

func TestTablePageFilteredReturnsPageAndTotal(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)

	page, total, err := TablePageFiltered(ctx, path, "t", PageOptions{Limit: 2, SortBy: "name", WithCount: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4 (every row, not just the page)", total)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(page.Rows))
	}
	if page.Rows[0][1] != "alpha" || page.Rows[1][1] != "beta" {
		t.Fatalf("rows = %#v, want alpha then beta", page.Rows)
	}
	if !page.Truncated {
		t.Fatal("truncated = false, want true (4 rows behind a 2-row page)")
	}
}

func TestTablePageFilteredSortsDescending(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	page, _, err := TablePageFiltered(ctx, path, "t", PageOptions{Limit: 2, SortBy: "n", SortDesc: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if page.Rows[0][2] != int64(40) || page.Rows[1][2] != int64(30) {
		t.Fatalf("rows = %#v, want n=40 then 30", page.Rows)
	}
}

func TestTablePageFilteredCountsOnlyMatchingRows(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	page, total, err := TablePageFiltered(ctx, path, "t", PageOptions{
		Limit: 100, Filter: "n >= 30", WithCount: true,
	})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if page.Truncated {
		t.Fatal("truncated = true, want false (2 matching rows fit in the page)")
	}
}

func TestTablePageFilteredEmptyResultStillCounts(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	page, total, err := TablePageFiltered(ctx, path, "t", PageOptions{Filter: "n > 1000", WithCount: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("rows = %#v, want none", page.Rows)
	}
}

func TestTablePageFilteredPaginatesWithinAFilter(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	first, total, err := TablePageFiltered(ctx, path, "t", PageOptions{
		Limit: 2, Offset: 0, Filter: "n > 5", SortBy: "n", WithCount: true,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	second, _, err := TablePageFiltered(ctx, path, "t", PageOptions{
		Limit: 2, Offset: 2, Filter: "n > 5", SortBy: "n",
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if first.Rows[0][2] != int64(10) || first.Rows[1][2] != int64(20) {
		t.Fatalf("page 1 = %#v, want n=10,20", first.Rows)
	}
	if second.Rows[0][2] != int64(30) || second.Rows[1][2] != int64(40) {
		t.Fatalf("page 2 = %#v, want n=30,40", second.Rows)
	}
}

// A table with no declared primary key still needs the synthetic rowid column
// for row edits, and it must survive the sort/filter path.
func TestTablePageFilteredExposesRowIDColumn(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", `CREATE TABLE t(x TEXT); INSERT INTO t VALUES ('a'),('b');`)
	page, total, err := TablePageFiltered(ctx, path, "t", PageOptions{IncludeRowID: true, WithCount: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(page.Columns) == 0 || page.Columns[0].Name != RowIDColumn {
		t.Fatalf("columns = %#v, want the rowid column first", page.Columns)
	}
}

// A view has no rowid, so the synthetic column must not be requested for one.
func TestTablePageFilteredRejectsUnknownTable(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	if _, _, err := TablePageFiltered(ctx, path, "nope", PageOptions{}); !errors.Is(err, ErrNoSuchTable) {
		t.Fatalf("err = %v, want ErrNoSuchTable", err)
	}
}

// A filter that reaches the engine as an expression error must surface as an
// error, never as an empty page that reads like "no matches".
func TestTablePageFilteredSurfacesAFilterError(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	_, _, err := TablePageFiltered(ctx, path, "t", PageOptions{Filter: "no_such_column = 1"})
	if err == nil {
		t.Fatal("expected an unknown column in the filter to error")
	}
	if !strings.Contains(err.Error(), "no_such_column") {
		t.Fatalf("err = %v, want the engine's message naming the column", err)
	}
}

// A view is filtered and sorted like a table: SQLite reads it fine and the
// column allowlist comes from the same PRAGMA.
func TestTablePageFilteredWorksOnAView(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture+` CREATE VIEW big AS SELECT * FROM t WHERE n > 15;`)
	page, total, err := TablePageFiltered(ctx, path, "big", PageOptions{Limit: 10, WithCount: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 3 || len(page.Rows) != 3 {
		t.Fatalf("total = %d rows = %d, want 3", total, len(page.Rows))
	}
}

// ── Maintenance ────────────────────────────────────────────────────────────

func TestVacuumAndAnalyze(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "m.db", filterFixture+` DELETE FROM t WHERE id > 2;`)
	for _, op := range []struct {
		name string
		fn   func(context.Context, string) (ExecResult, error)
	}{
		{"analyze", Analyze},
		{"vacuum", Vacuum},
	} {
		t.Run(op.name, func(t *testing.T) {
			res, err := op.fn(ctx, path)
			if err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if res.ElapsedMS < 0 {
				t.Fatalf("%s: elapsed = %d", op.name, res.ElapsedMS)
			}
		})
	}
	// ANALYZE is what makes the table list show real counts.
	tables, err := ListTables(ctx, path)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if tables[0].Rows != 2 {
		t.Fatalf("rows estimate = %d, want 2 after ANALYZE", tables[0].Rows)
	}
}

func TestIntegrityCheckReports(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "m.db", filterFixture)
	rows, err := IntegrityCheck(ctx, path)
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if len(rows) != 1 || !strings.EqualFold(rows[0], "ok") {
		t.Fatalf("rows = %#v, want [ok]", rows)
	}
}

// writeAt overwrites bytes in place at off, which is how the corruption test
// damages a page without changing the file length.
func writeAt(path string, off int64, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(b, off)
	return err
}

// A corrupt file must report the damage rather than panic or claim "ok".
func TestIntegrityCheckReportsCorruption(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "m.db", filterFixture)
	// Overwrite page bytes past the header so the page structure is broken.
	corrupt := make([]byte, 4096)
	if err := writeAt(path, 1024, corrupt); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	// A corrupt file must surface as damage. The engine may refuse to open it
	// at all, which is also a correct answer — so both outcomes are accepted
	// here and the assertion is on the one thing that is never acceptable:
	// reporting a healthy database.
	rows, err := IntegrityCheck(ctx, path)
	if err != nil {
		if !strings.Contains(err.Error(), "malformed") && !strings.Contains(err.Error(), "corrupt") {
			t.Fatalf("err = %v, want the engine's corruption error", err)
		}
		return
	}
	if len(rows) == 1 && strings.EqualFold(rows[0], "ok") {
		t.Fatalf("a corrupted database reported ok: rows = %#v", rows)
	}
}

// ── Backup ─────────────────────────────────────────────────────────────────

// The backup must be a byte-for-byte usable copy taken BEFORE a mutation, and
// it must not live inside the data directory the write guard protects.
func TestBackupCopiesTheFile(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "b.db", filterFixture)
	dest, err := Backup(ctx, path)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if strings.TrimSpace(dest) == "" {
		t.Fatal("Backup returned no destination")
	}
	if dest != BackupPath(path) {
		t.Fatalf("dest = %q, want the sibling backup path %q", dest, BackupPath(path))
	}
	// The copy is a real database with the same rows.
	rows, err := CountRows(ctx, dest, "t", "")
	if err != nil {
		t.Fatalf("CountRows on the backup: %v", err)
	}
	if rows != 4 {
		t.Fatalf("backup rows = %d, want 4", rows)
	}
}

func TestBackupRefusesAMissingFile(t *testing.T) {
	ctx := context.Background()
	if _, err := Backup(ctx, t.TempDir()+"/absent.db"); err == nil {
		t.Fatal("expected backing up a missing file to fail")
	}
}

func TestCountRows(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "c.db", filterFixture)
	if n, err := CountRows(ctx, path, "t", ""); err != nil || n != 4 {
		t.Fatalf("all = %d err = %v, want 4", n, err)
	}
	if n, err := CountRows(ctx, path, "t", "name = 'beta'"); err != nil || n != 1 {
		t.Fatalf("filtered = %d err = %v, want 1", n, err)
	}
	if _, err := CountRows(ctx, path, "absent", ""); !errors.Is(err, ErrNoSuchTable) {
		t.Fatalf("err = %v, want ErrNoSuchTable", err)
	}
}

func TestTablePageFilteredSkipsCountUnlessAsked(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	_, total, err := TablePageFiltered(ctx, path, "t", PageOptions{Limit: 2})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d without WithCount, want 0 (no count query)", total)
	}
}

func TestIsBadPageInput(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "f.db", filterFixture)
	_, _, err := TablePageFiltered(ctx, path, "t", PageOptions{Filter: "no_such_column = 1"})
	if !IsBadPageInput(err) {
		t.Fatalf("unknown filter column: IsBadPageInput(%v) = false, want true", err)
	}
	_, _, err = TablePageFiltered(ctx, path, "t", PageOptions{SortBy: "nope"})
	if !IsBadPageInput(err) {
		t.Fatalf("unknown sort column: IsBadPageInput(%v) = false, want true", err)
	}
	_, _, err = TablePageFiltered(ctx, path, "t", PageOptions{Filter: "1; DROP TABLE t"})
	if !IsBadPageInput(err) {
		t.Fatalf("multi-statement filter: IsBadPageInput(%v) = false, want true", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = TablePageFiltered(cancelled, path, "t", PageOptions{})
	if err == nil || IsBadPageInput(err) {
		t.Fatalf("cancelled context: err=%v IsBadPageInput=%v, want a non-input error", err, IsBadPageInput(err))
	}
}

// A second backup must not destroy the snapshot taken before the first edit.
func TestBackupRotatesInsteadOfOverwriting(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "b.db", filterFixture)
	if _, err := Backup(ctx, path); err != nil {
		t.Fatalf("first Backup: %v", err)
	}
	if err := withReadWrite(ctx, path, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, "DELETE FROM t")
		return err
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if _, err := Backup(ctx, path); err != nil {
		t.Fatalf("second Backup: %v", err)
	}
	latest, err := CountRows(ctx, BackupPath(path), "t", "")
	if err != nil || latest != 0 {
		t.Fatalf("latest backup rows = %d, %v; want 0", latest, err)
	}
	rotated, err := filepath.Glob(BackupPath(path) + ".*")
	if err != nil || len(rotated) != 1 {
		t.Fatalf("rotated backups = %v, %v; want exactly one", rotated, err)
	}
	pre, err := CountRows(ctx, rotated[0], "t", "")
	if err != nil || pre != 4 {
		t.Fatalf("pre-mutation snapshot rows = %d, %v; want 4", pre, err)
	}
}

func TestBackupPrunesOldRotations(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "b.db", filterFixture)
	for i := 0; i < rotatedBackupsKept+3; i++ {
		if _, err := Backup(ctx, path); err != nil {
			t.Fatalf("Backup %d: %v", i, err)
		}
	}
	rotated, err := filepath.Glob(BackupPath(path) + ".*")
	if err != nil || len(rotated) != rotatedBackupsKept {
		t.Fatalf("rotated backups = %d (%v), want %d", len(rotated), err, rotatedBackupsKept)
	}
}

// A failed snapshot must leave the previous backup intact.
func TestBackupFailureKeepsPreviousBackup(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "b.db", filterFixture)
	if _, err := Backup(ctx, path); err != nil {
		t.Fatalf("first Backup: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Backup(cancelled, path); err == nil {
		t.Fatal("expected Backup with a cancelled context to fail")
	}
	n, err := CountRows(ctx, BackupPath(path), "t", "")
	if err != nil || n != 4 {
		t.Fatalf("previous backup rows = %d, %v; want 4 (untouched)", n, err)
	}
}
