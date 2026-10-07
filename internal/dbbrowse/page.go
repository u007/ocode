package dbbrowse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// This file is the IDE surface of the browser: server-side filtering, sorting
// and counting over a table page, plus the maintenance operations (ANALYZE,
// VACUUM, integrity_check) and the pre-write backup that makes a mutation
// recoverable.
//
// The security model is unchanged from the reader. A filter is user SQL, so it
// is validated with the SAME allowlist as any statement (validateReadOnlyStatement)
// and the connection is still mode=ro + query_only(1). A sort column is the
// one identifier-shaped value, so it is admitted only when the table really has
// that column and is then quoted.

// PageOptions describes one page request: which window of which filtered,
// sorted row set.
type PageOptions struct {
	// Limit is the page size; 0 means DefaultLimit and anything above MaxLimit
	// is clamped.
	Limit int
	// Offset is the first row to return; a negative value is clamped to 0.
	Offset int
	// Filter is a SQL boolean expression appended as WHERE (filter), without the
	// WHERE keyword. An empty filter adds no clause.
	Filter string
	// SortBy is a column of the table (or RowIDColumn) to order by. Empty means
	// no ORDER BY at all.
	SortBy string
	// SortDesc flips the direction; it is ignored when SortBy is empty.
	SortDesc bool
	// IncludeRowID adds the synthetic leading rowid column used to address a row
	// in a table with no declared primary key.
	IncludeRowID bool
	// WithCount makes TablePageFiltered also run COUNT(*) for the filter. It is a
	// full scan on a large table, so it is opt-in: without it the returned total
	// is 0 and no count query runs.
	WithCount bool
}

// limit returns the clamped page size.
func (o PageOptions) limit() int { return clampLimit(o.Limit) }

// offset returns the non-negative offset.
func (o PageOptions) offset() int {
	if o.Offset < 0 {
		return 0
	}
	return o.Offset
}

// pageSize is what the SELECT binds as LIMIT: one more than the page, so the
// extra row is what sets Truncated without a second count query.
func (o PageOptions) pageSize() int { return o.limit() + 1 }

// ErrBadPageInput marks a page request the caller got wrong (unknown sort column).
var ErrBadPageInput = errors.New("invalid page request")

// IsBadPageInput reports whether err from a page query is the caller's input —
// an unknown sort column, a non-read-only filter, or SQLite's generic
// SQLITE_ERROR (syntax error, unknown column, type mismatch in the filter). A
// busy/locked/corrupt/full/I-O/interrupted database or a cancelled context is a
// server fault and returns false.
func IsBadPageInput(err error) bool {
	if errors.Is(err, ErrBadPageInput) || errors.Is(err, ErrStatementNotReadOnly) {
		return true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()&0xff == sqlite3.SQLITE_ERROR
	}
	return false
}

// filterExpression validates a user filter and renders its SQL clause.
//
// The filter is NOT free-form trust: it is checked by running the statement it
// would appear in — `SELECT 1 WHERE (<filter>)` — through the same read-only
// allowlist every other statement passes. That rejects a second statement after
// a `;`, ATTACH/DETACH anywhere (including inside a comment or string, which
// stripSQLNoise neutralises), and anything whose leading keyword is not a read.
//
// A read-only SUBQUERY is deliberately allowed: `id IN (SELECT …)` is ordinary
// filtering, and containment does not depend on it being refused — ATTACH is
// impossible and query_only(1) refuses writes.
func filterExpression(filter string) (string, error) {
	f := strings.TrimSpace(filter)
	if f == "" {
		return "", nil
	}
	// Guard the filter's own bytes before wrapping: a `;` anywhere would make
	// `SELECT 1 WHERE (a; b)` a two-statement string, and validateReadOnlyStatement
	// checks the WRAPPED form's `;` — which the wrapping parentheses do not hide.
	if err := validateReadOnlyStatement("SELECT 1 WHERE (" + f + ")"); err != nil {
		return "", err
	}
	return "WHERE (" + f + ")", nil
}

// sortClause validates that column names a real column of the table and renders
// its ORDER BY. Quoting alone would keep the SQL well-formed while still letting
// ORDER BY reference a column from elsewhere or a bare expression, so the name
// must be one the table actually has.
//
// RowIDColumn is admitted without being in the column list: it is the synthetic
// name the grid itself projects for a rowid table.
func sortClause(column string, desc bool, columns []string) (string, error) {
	c := strings.TrimSpace(column)
	if c == "" {
		return "", nil
	}
	if c != RowIDColumn && !slices.Contains(columns, c) {
		return "", fmt.Errorf("%w: no such column: %s", ErrBadPageInput, c)
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return "ORDER BY " + QuoteIdent(c) + " " + dir, nil
}

// tableColumnNames returns the declared column names of a table or view.
func tableColumnNames(ctx context.Context, path, table string) ([]string, error) {
	schema, err := DescribeTable(ctx, path, table)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(schema.Columns))
	for _, c := range schema.Columns {
		out = append(out, c.Name)
	}
	return out, nil
}

// CountRows returns the number of rows in a table matching an optional filter.
// It is used for the table list's real counts and for the grid's total, where an
// ANALYZE estimate would be wrong after a write.
func CountRows(ctx context.Context, path, table, filter string) (int64, error) {
	// Resolve the table first so an unknown name is ErrNoSuchTable rather than a
	// COUNT that silently answers 0 for a typo'd table.
	if _, err := tableColumnNames(ctx, path, table); err != nil {
		return 0, err
	}
	return countRows(ctx, path, table, filter)
}

// countRows is CountRows for a table the caller has already resolved, so it
// does not describe the schema a second time.
func countRows(ctx context.Context, path, table, filter string) (int64, error) {
	clause, err := filterExpression(filter)
	if err != nil {
		return 0, err
	}
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", QuoteIdent(table), clause)
	if err := validateReadOnlyStatement(q); err != nil {
		return 0, err
	}
	var n int64
	err = withReadOnly(ctx, path, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, q).Scan(&n)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// TablePageFiltered returns one page of a table or view together with the total
// number of rows the filter matches when opts.WithCount is set, so the grid can
// show "showing 1–100 of 4,231" without a second request. The total is 0 when
// WithCount is unset.
//
// The filter is user SQL and goes through the read-only gate; the sort column
// must exist on the table. Everything else (identifier quoting, LIMIT/OFFSET as
// bound parameters, mode=ro + query_only) is the same contract TablePageKeyed
// already had, so a filtered page cannot reach further than an unfiltered one.
func TablePageFiltered(ctx context.Context, path, table string, opts PageOptions) (ResultSet, int64, error) {
	columns, err := tableColumnNames(ctx, path, table)
	if err != nil {
		return ResultSet{}, 0, err
	}
	where, err := filterExpression(opts.Filter)
	if err != nil {
		return ResultSet{}, 0, err
	}
	order, err := sortClause(opts.SortBy, opts.SortDesc, columns)
	if err != nil {
		return ResultSet{}, 0, err
	}
	sel := "*"
	if opts.IncludeRowID {
		sel = QuoteIdent(RowIDColumn) + " AS " + QuoteIdent(RowIDColumn) + ", *"
	}
	q := fmt.Sprintf("SELECT %s FROM %s %s %s LIMIT ? OFFSET ?", sel, QuoteIdent(table), where, order)
	if err := validateReadOnlyStatement(q); err != nil {
		return ResultSet{}, 0, err
	}
	page, err := queryWithArgs(ctx, path, q, []any{opts.pageSize(), opts.offset()}, opts.limit())
	if err != nil {
		return ResultSet{}, 0, err
	}
	if !opts.WithCount {
		return page, 0, nil
	}
	total, err := countRows(ctx, path, table, opts.Filter)
	if err != nil {
		return ResultSet{}, 0, err
	}
	return page, total, nil
}

// ── Maintenance ────────────────────────────────────────────────────────────

// Analyze runs ANALYZE, which populates sqlite_stat1 so the table list shows
// real row counts instead of -1. It is a write, so it runs read-write.
func Analyze(ctx context.Context, path string) (ExecResult, error) {
	return execMaintenance(ctx, path, "ANALYZE")
}

// Vacuum rewrites the database file to reclaim free pages. It cannot run inside
// a transaction, so it is issued directly rather than through execExact.
func Vacuum(ctx context.Context, path string) (ExecResult, error) {
	return execMaintenance(ctx, path, "VACUUM")
}

// execMaintenance runs one of the server's own maintenance statements. The SQL
// is a package constant, never user input.
func execMaintenance(ctx context.Context, path, stmt string) (ExecResult, error) {
	if kw, blocked := BlockedWriteStatement(stmt); blocked {
		return ExecResult{}, fmt.Errorf("%s is not an allowed maintenance statement", kw)
	}
	start := time.Now()
	var out ExecResult
	err := withReadWrite(ctx, path, func(db *sql.DB) error {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
		out = ExecResult{ElapsedMS: time.Since(start).Milliseconds()}
		return nil
	})
	if err != nil {
		return ExecResult{}, err
	}
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns its report lines. A
// healthy database answers exactly ["ok"]; a damaged one answers one line per
// problem, which is what the caller shows the user. It reads on a read-only
// connection because the pragma takes no value.
func IntegrityCheck(ctx context.Context, path string) ([]string, error) {
	return pragmaLines(ctx, path, "PRAGMA integrity_check")
}

// pragmaLines runs a value-less read-only PRAGMA and returns its rows.
func pragmaLines(ctx context.Context, path, pragma string) ([]string, error) {
	if err := validateReadOnlyStatement(pragma); err != nil {
		return nil, err
	}
	out := []string{}
	err := withReadOnly(ctx, path, func(db *sql.DB) error {
		rows, err := db.QueryContext(ctx, pragma)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v any
			if err := rows.Scan(&v); err != nil {
				return err
			}
			out = append(out, fmt.Sprint(v))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Backup ─────────────────────────────────────────────────────────────────

// BackupSuffix is appended to the backup file name.
const BackupSuffix = ".bak"

// Backup copies the database file to a sibling path and returns it. Every
// mutation the browser performs is preceded by one of these, so a wrong delete
// or a bad UPDATE has a way back that does not depend on the user having their
// own version control on the file.
//
// The copy is a sibling (`<name>.db.bak`) rather than a file inside ocode's data
// directory: it sits next to the database, which is where the user looking for
// a way to undo this will look, and it keeps dbWriteGuard's "never write into
// the data dir" rule intact for the backup itself.
//
// SQLite's write-ahead log means a plain file copy can miss rows still sitting
// in the -wal sidecar, so the backup is taken through VACUUM INTO, which
// produces a consistent, fully-checkpointed snapshot as a single file. That also
// means the destination must not already exist, so it is written to a temp
// sibling and renamed into place. The previous `<name>.db.bak` is rotated to
// `<name>.db.bak.<unixnano>` (newest 5 kept) instead of being overwritten.
func Backup(ctx context.Context, path string) (string, error) {
	// Probe before the copy: sql.Open is lazy and VACUUM INTO happily snapshots
	// an empty database it just created, so without this a typo'd path would
	// report a successful backup of nothing.
	ok, err := Probe(path)
	if err != nil || !ok {
		return "", fmt.Errorf("cannot back up %s: not a readable SQLite database", filepath.Base(path))
	}
	dest := BackupPath(path)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("backup directory: %w", err)
	}
	// Snapshot to a temp sibling first: the previous backup is only displaced once
	// a new one exists, so a failed VACUUM INTO (disk full, timeout) never costs
	// the user their only way back.
	tmp := dest + backupTmpSuffix
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("clear stale backup temp: %w", err)
	}
	// VACUUM INTO takes the destination as a bound value, so a database path
	// containing a quote cannot break the statement.
	err = withReadWrite(ctx, path, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, "VACUUM INTO ?", tmp)
		return err
	})
	if err != nil {
		return "", err
	}
	// Rotate rather than replace: a second edit must not overwrite the snapshot
	// that holds the state from before the first (possibly mistaken) one.
	if _, err := os.Stat(dest); err == nil {
		rotated := fmt.Sprintf("%s.%d", dest, time.Now().UnixNano())
		if err := os.Rename(dest, rotated); err != nil {
			return "", fmt.Errorf("rotate previous backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat previous backup: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("install backup: %w", err)
	}
	if err := pruneRotatedBackups(dest); err != nil {
		log.Printf("db browser: prune old backups for %q: %v", path, err)
	}
	return dest, nil
}

// backupTmpSuffix names the in-progress snapshot; it is not purely numeric, so
// pruneRotatedBackups never mistakes it for a rotated backup.
const backupTmpSuffix = ".tmp"

// rotatedBackupsKept is how many displaced snapshots (`<name>.db.bak.<unixnano>`)
// are retained beside the latest one.
const rotatedBackupsKept = 5

// pruneRotatedBackups deletes all but the newest rotatedBackupsKept rotated
// snapshots of dest.
func pruneRotatedBackups(dest string) error {
	matches, err := filepath.Glob(dest + ".*")
	if err != nil {
		return err
	}
	var rotated []string
	for _, m := range matches {
		if _, err := strconv.ParseInt(strings.TrimPrefix(m, dest+"."), 10, 64); err == nil {
			rotated = append(rotated, m)
		}
	}
	// Same-width nanosecond stamps, so lexical order is chronological.
	slices.Sort(rotated)
	for len(rotated) > rotatedBackupsKept {
		if err := os.Remove(rotated[0]); err != nil {
			return err
		}
		rotated = rotated[1:]
	}
	return nil
}

// BackupPath is where Backup writes for a given database file.
func BackupPath(path string) string { return path + BackupSuffix }
