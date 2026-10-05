// Package dbbrowse provides read access to a SQLite database file for the web
// SQLite browser: header probing, table listing, schema description and
// read-only queries. It is deliberately free of server dependencies so it can
// be unit-tested directly against temp databases.
//
// Every connection is opened read-only TWICE over: mode=ro on the main
// database plus query_only(1), which covers every attached database too, AND
// validateReadOnlyStatement rejects ATTACH/DETACH and any statement that is not
// a single read. mode=ro on its own is not containment — see openReadOnly.
// Mutating access lives behind an explicit read-write open and is gated by the
// caller (see the confirmation-escalation contract in the design spec).
package dbbrowse

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// DefaultLimit is the default page size for Query.
	DefaultLimit = 1000
	// MaxLimit caps a single Query result page.
	MaxLimit = 10000

	// blobPreviewBytes is how many leading bytes of a BLOB are rendered as hex.
	blobPreviewBytes = 64
	// blobDataMaxBytes caps the base64 payload returned for a BLOB cell.
	blobDataMaxBytes = 8192
)

// sqliteMagic is the 16-byte header of every SQLite 3 database file.
const sqliteMagic = "SQLite format 3\x00"

// Table is one entry in a database's table/view listing.
type Table struct {
	Name string `json:"name"`
	Type string `json:"type"` // "table" or "view"
	// Rows is the ANALYZE estimate from sqlite_stat1, or -1 when unknown.
	Rows int64 `json:"rows"`
}

// Column describes one column of a table.
type Column struct {
	Name      string  `json:"name"`
	DeclType  string  `json:"decl_type"`
	NotNull   bool    `json:"not_null"`
	Default   *string `json:"default,omitempty"`
	PK        int     `json:"pk"` // 1-based position in the primary key, 0 = not part of it
	Generated bool    `json:"generated"`
}

// Index describes one index on a table.
type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
	Origin  string   `json:"origin"` // "c" CREATE INDEX, "u" UNIQUE constraint, "pk" primary key
}

// ForeignKey describes one foreign-key relationship.
type ForeignKey struct {
	From  string `json:"from"`
	Table string `json:"table"`
	To    string `json:"to"`
}

// TableSchema is the full description of a table or view.
type TableSchema struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Columns     []Column     `json:"columns"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
	DDL         string       `json:"ddl"`
	// RowID reports whether UPDATE/DELETE can address rows by rowid.
	RowID bool `json:"rowid"`
}

// ResultColumn describes one output column of a query.
type ResultColumn struct {
	Name     string `json:"name"`
	DeclType string `json:"decl_type"`
}

// Blob is the JSON-safe representation of a BLOB cell.
type Blob struct {
	IsBlob    bool   `json:"$blob"`
	Bytes     int    `json:"bytes"`
	Preview   string `json:"preview"`             // hex of the leading bytes
	Data      string `json:"data,omitempty"`      // base64 of the leading bytes
	Truncated bool   `json:"truncated,omitempty"` // true when Data is a prefix
}

// ResultSet is the JSON-safe result of a read query.
type ResultSet struct {
	Columns   []ResultColumn `json:"columns"`
	Rows      [][]any        `json:"rows"`
	RowCount  int            `json:"row_count"`
	Truncated bool           `json:"truncated"`
	ElapsedMS int64          `json:"elapsed_ms"`
}

// Probe reports whether path is a regular file whose first 16 bytes are the
// SQLite magic. It never opens the file as a database, so a non-SQLite or
// encrypted file is rejected cheaply and without creating sidecar files.
func Probe(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, len(sqliteMagic))
	n, err := f.Read(buf)
	if err != nil && n < len(sqliteMagic) {
		// A file shorter than the magic is not a SQLite database; a read error
		// on a short file is reported as "not SQLite" rather than fatal.
		return false, nil
	}
	return string(buf[:n]) == sqliteMagic, nil
}

// escapePath makes a filesystem path safe to embed in a file: URI query.
func escapePath(path string) string {
	escaped := strings.ReplaceAll(path, "%", "%25")
	escaped = strings.ReplaceAll(escaped, "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	return escaped
}

// openReadOnly opens path read-only, with a busy timeout. It deliberately does
// NOT set _txlock=immediate or journal_mode: the former would take a write lock
// on BEGIN, and the latter would mutate the file — both wrong for a reader.
//
// mode=ro alone is NOT sufficient, and this is load-bearing. mode=ro applies to
// the MAIN database only, so `ATTACH DATABASE 'file:/elsewhere.db'` brings up a
// fully read-write sibling on the same connection. Two independent guards close
// that hole, and neither is sufficient alone:
//
//   - query_only(1) applies to EVERY attached database, so it is the
//     authoritative WRITE guard. Verified against modernc v1.57.0: without it an
//     INSERT through an attached database succeeds and lands on disk.
//   - validateReadOnlyStatement rejects ATTACH/DETACH and every non-read
//     statement, which is what closes the READ side — query_only does nothing
//     for reads, so an ATTACH+SELECT would otherwise read any SQLite file on
//     the host, ocode's own session transcripts included.
func openReadOnly(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)", escapePath(path))
	return sql.Open("sqlite", dsn)
}

// ErrStatementNotReadOnly is returned by Query when the caller's SQL is not a
// single read-only statement. It is a caller error, not a database error.
var ErrStatementNotReadOnly = errors.New("only a single read-only statement is permitted")

// ErrNoSuchTable is returned by DescribeTable when the named table or view does
// not exist, so callers can tell it apart from a failing database.
var ErrNoSuchTable = errors.New("no such table")

// readOnlyPragmas are the PRAGMA forms that only read. Every other pragma is
// refused: the writing set (journal_mode, user_version, journal_size_limit,
// wal_checkpoint, auto_vacuum, page_size, synchronous, cache_size=, …) either
// mutates the file or reconfigures the engine, and an allowlist of the ones we
// accept fails closed for the ones we did not think of.
var readOnlyPragmas = map[string]bool{
	"table_info": true, "table_xinfo": true, "table_list": true,
	"index_info": true, "index_list": true, "index_xinfo": true,
	"foreign_key_list": true, "foreign_key_check": true,
	"database_list": true, "collation_list": true, "compile_options": true,
	"function_list": true, "module_list": true, "pragma_list": true,
	"integrity_check": true, "quick_check": true, "schema_version": true,
	"data_version": true, "user_version": true, "application_id": true,
	"freelist_count": true, "page_count": true, "max_page_count": true,
	"encoding": true, "auto_vacuum": true, "journal_mode": true,
	"secure_delete": true, "legacy_file_format": true, "defer_foreign_keys": true,
}

// argumentPragmas are the allowlisted pragmas whose read form takes an
// argument, `PRAGMA table_info(t)`. Every other allowlisted pragma is a pure
// getter, so ANY argument or assignment on it (`journal_mode(WAL)`,
// `auto_vacuum=1`) is the setter form and is refused. No allowlisted pragma
// accepts `=`, even this set.
var argumentPragmas = map[string]bool{
	"table_info": true, "table_xinfo": true, "table_list": true,
	"index_info": true, "index_list": true, "index_xinfo": true,
	"foreign_key_list": true, "foreign_key_check": true,
	"integrity_check": true, "quick_check": true,
}

// sqlNoiseStripper removes comments and every quoting form from a statement so
// keyword scanning cannot be fooled by them — a `-- ATTACH` comment, an
// 'attach' string literal or a `"attach"` identifier must all read as noise.
// It walks left to right, copying safe bytes and replacing every comment and
// literal body with a space.
func stripSQLNoise(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	runes := []rune(q)
	for i := 0; i < len(runes); {
		c := runes[i]
		switch {
		case c == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i += 2
			if i > len(runes) {
				i = len(runes)
			}
			b.WriteByte(' ')
		case c == '\'' || c == '"' || c == '`':
			quote := c
			i++
			for i < len(runes) {
				if runes[i] == quote {
					// A doubled quote is an escaped quote, not a terminator.
					if i+1 < len(runes) && runes[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
		case c == '[':
			i++
			for i < len(runes) && runes[i] != ']' {
				i++
			}
			i++
			b.WriteByte(' ')
		default:
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}

// firstSQLKeyword returns the leading alphabetic token of an already-stripped
// statement, upper-cased, or "" when there is none.
func firstSQLKeyword(clean string) string {
	for i := 0; i < len(clean); i++ {
		c := clean[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			start := i
			for i < len(clean) {
				c = clean[i]
				if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
					i++
					continue
				}
				break
			}
			return strings.ToUpper(clean[start:i])
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '(' {
			return ""
		}
	}
	return ""
}

// sqlHasWord reports whether word appears in clean as a standalone token, so
// `attachments` does not match `attach`.
func sqlHasWord(clean, word string) bool {
	up := strings.ToUpper(clean)
	want := strings.ToUpper(word)
	for off := 0; off+len(want) <= len(up); {
		idx := strings.Index(up[off:], want)
		if idx < 0 {
			return false
		}
		at := off + idx
		off = at + len(want)
		if at > 0 && isSQLWordByte(up[at-1]) {
			continue
		}
		if off < len(up) && isSQLWordByte(up[off]) {
			continue
		}
		return true
	}
	return false
}

func isSQLWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// validateReadOnlyStatement rejects anything that is not exactly one read-only
// statement. It is deliberately an ALLOWLIST: a new SQLite statement type is
// refused until someone adds it here, rather than being admitted by default.
//
// The three ways out of the browser's path containment are all closed here:
// a second statement after a `;` (which is how ATTACH would reach an outside
// file), ATTACH/DETACH anywhere, and any leading keyword that is not a read.
// WITH is admitted because a CTE can only read once ATTACH is impossible, and
// query_only already refuses a `WITH ... INSERT`.
func validateReadOnlyStatement(query string) error {
	clean := strings.TrimSpace(stripSQLNoise(query))
	if clean == "" {
		return fmt.Errorf("%w: empty statement", ErrStatementNotReadOnly)
	}
	if strings.Contains(clean, ";") {
		return fmt.Errorf("%w: multiple statements are not permitted", ErrStatementNotReadOnly)
	}
	// Redundant with the leading-keyword check for well-formed SQL, because
	// SQLite only accepts ATTACH/DETACH as statements — but it is the check
	// that keeps the guarantee if a form ever reaches the engine that does not.
	if sqlHasWord(clean, "attach") || sqlHasWord(clean, "detach") {
		return fmt.Errorf("%w: ATTACH/DETACH is not permitted", ErrStatementNotReadOnly)
	}
	switch kw := firstSQLKeyword(clean); kw {
	case "SELECT", "WITH", "VALUES", "EXPLAIN":
		return nil
	case "PRAGMA":
		rest := strings.TrimSpace(clean[len("PRAGMA"):])
		// firstSQLKeyword upper-cases; the table keys are lower-case.
		name := strings.ToLower(firstSQLKeyword(rest))
		if name == "" {
			return fmt.Errorf("%w: PRAGMA without a name", ErrStatementNotReadOnly)
		}
		if !readOnlyPragmas[name] {
			return fmt.Errorf("%w: PRAGMA %s is not read-only", ErrStatementNotReadOnly, name)
		}
		// The name alone does not make a pragma read-only: journal_mode,
		// auto_vacuum, secure_delete and max_page_count all write when given a
		// value, and some succeed on a mode=ro connection.
		if strings.Contains(rest, "=") || (!argumentPragmas[name] && strings.Contains(rest, "(")) {
			return fmt.Errorf("%w: PRAGMA %s with a value is not read-only", ErrStatementNotReadOnly, name)
		}
		return nil
	case "":
		return fmt.Errorf("%w: unrecognised statement", ErrStatementNotReadOnly)
	default:
		return fmt.Errorf("%w: %s is not a read-only statement", ErrStatementNotReadOnly, kw)
	}
}

// withReadOnly opens a read-only connection, runs fn, and closes it.
func withReadOnly(ctx context.Context, path string, fn func(*sql.DB) error) error {
	db, err := openReadOnly(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}

// ListTables returns every user table and view, sorted by name, with an
// ANALYZE row estimate where sqlite_stat1 provides one (-1 otherwise).
func ListTables(ctx context.Context, path string) ([]Table, error) {
	var out []Table
	err := withReadOnly(ctx, path, func(db *sql.DB) error {
		rows, err := db.QueryContext(ctx,
			`SELECT name, type FROM sqlite_master
			 WHERE type IN ('table','view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
			 ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Table
			if err := rows.Scan(&t.Name, &t.Type); err != nil {
				return err
			}
			t.Rows = -1
			out = append(out, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Best-effort row estimates from sqlite_stat1 (populated by ANALYZE).
		estimates, err := tableRowEstimates(ctx, db)
		if err != nil {
			// Estimates are optional; a missing/older sqlite_stat1 must not
			// fail the listing.
			return nil //nolint:nilerr // estimates are advisory
		}
		for i := range out {
			if n, ok := estimates[out[i].Name]; ok {
				out[i].Rows = n
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// tableRowEstimates reads sqlite_stat1 for per-table row estimates. The stat
// column is a space-separated list whose first field is the row count for the
// table-level row (idx IS NULL).
func tableRowEstimates(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT tbl, stat FROM sqlite_stat1 WHERE idx IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var tbl string
		var stat sql.NullString
		if err := rows.Scan(&tbl, &stat); err != nil {
			return nil, err
		}
		if !stat.Valid {
			continue
		}
		fields := strings.Fields(stat.String)
		if len(fields) == 0 {
			continue
		}
		var n int64
		if _, err := fmt.Sscanf(fields[0], "%d", &n); err == nil {
			out[tbl] = n
		}
	}
	return out, rows.Err()
}

// DescribeTable returns columns, indexes, foreign keys and the original DDL
// for a table or view.
func DescribeTable(ctx context.Context, path, table string) (TableSchema, error) {
	var out TableSchema
	err := withReadOnly(ctx, path, func(db *sql.DB) error {
		if err := db.QueryRowContext(ctx,
			`SELECT name, type, COALESCE(sql,'') FROM sqlite_master
			 WHERE name = ? AND type IN ('table','view')`, table).
			Scan(&out.Name, &out.Type, &out.DDL); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("%w: %s", ErrNoSuchTable, table)
			}
			return err
		}

		cols, err := tableColumns(ctx, db, table)
		if err != nil {
			return err
		}
		out.Columns = cols
		out.RowID = out.Type == "table" && !strings.Contains(strings.ToUpper(out.DDL), "WITHOUT ROWID")

		idx, err := tableIndexes(ctx, db, table)
		if err != nil {
			return err
		}
		out.Indexes = idx

		fks, err := tableForeignKeys(ctx, db, table)
		if err != nil {
			return err
		}
		out.ForeignKeys = fks
		return nil
	})
	if err != nil {
		return TableSchema{}, err
	}
	return out, nil
}

// tableColumns reads PRAGMA table_xinfo, which (unlike table_info) reports
// generated columns via the hidden flag.
func tableColumns(ctx context.Context, db *sql.DB, table string) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var (
			c       Column
			notNull int
			dflt    sql.NullString
			hidden  int
		)
		if err := rows.Scan(&c.Name, &c.DeclType, &notNull, &dflt, &c.PK, &hidden); err != nil {
			return nil, err
		}
		c.NotNull = notNull != 0
		if dflt.Valid {
			v := dflt.String
			c.Default = &v
		}
		// hidden: 0 = normal, 1 = hidden (virtual table), 2/3 = generated.
		c.Generated = hidden == 2 || hidden == 3
		out = append(out, c)
	}
	return out, rows.Err()
}

// tableIndexes reads PRAGMA index_list and index_info.
func tableIndexes(ctx context.Context, db *sql.DB, table string) ([]Index, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, "unique", origin FROM pragma_index_list(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Index
	for rows.Next() {
		var (
			idx    Index
			unique int
		)
		if err := rows.Scan(&idx.Name, &unique, &idx.Origin); err != nil {
			return nil, err
		}
		idx.Unique = unique != 0
		cols, err := indexColumns(ctx, db, idx.Name)
		if err != nil {
			return nil, err
		}
		idx.Columns = cols
		out = append(out, idx)
	}
	return out, rows.Err()
}

func indexColumns(ctx context.Context, db *sql.DB, index string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name.String)
	}
	return out, rows.Err()
}

// tableForeignKeys reads PRAGMA foreign_key_list.
func tableForeignKeys(ctx context.Context, db *sql.DB, table string) ([]ForeignKey, error) {
	rows, err := db.QueryContext(ctx, `SELECT "table", "from", "to" FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForeignKey
	for rows.Next() {
		var (
			fk ForeignKey
			to sql.NullString
		)
		if err := rows.Scan(&fk.Table, &fk.From, &to); err != nil {
			return nil, err
		}
		fk.To = to.String
		out = append(out, fk)
	}
	return out, rows.Err()
}

// QuoteIdent quotes a SQL identifier (table/column name) so a name containing
// a double quote cannot break out of its context.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// clampLimit applies the default and maximum page sizes.
func clampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// Query runs ONE read-only SQL statement and returns a JSON-safe result page.
// A statement that is not read-only fails ErrStatementNotReadOnly before the
// engine sees it; a write that slips past the allowlist still fails with the
// engine's "attempt to write a readonly database" error, which the caller uses
// as the confirmation signal.
func Query(ctx context.Context, path, query string, limit int) (ResultSet, error) {
	return queryWithArgs(ctx, path, query, nil, clampLimit(limit))
}

// TablePage returns one page of rows from a table or view, addressing it by
// quoted identifier with bound LIMIT/OFFSET parameters. It fetches one extra
// row so Truncated reports whether more rows exist beyond the page.
func TablePage(ctx context.Context, path, table string, limit, offset int) (ResultSet, error) {
	limit = clampLimit(limit)
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf("SELECT * FROM %s LIMIT ? OFFSET ?", QuoteIdent(table))
	return queryWithArgs(ctx, path, q, []any{limit + 1, offset}, limit)
}

// queryWithArgs runs a read-only statement with optional bound args, capping
// the returned rows at cap.
func queryWithArgs(ctx context.Context, path, query string, args []any, cap int) (ResultSet, error) {
	// The read-side gate. query_only(1) stops writes on every attached database
	// but does nothing for reads, so an ATTACH + SELECT would otherwise read any
	// SQLite file the process can open, regardless of the caller's roots.
	if err := validateReadOnlyStatement(query); err != nil {
		return ResultSet{}, err
	}
	start := time.Now()
	var out ResultSet
	err := withReadOnly(ctx, path, func(db *sql.DB) error {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		names, err := rows.Columns()
		if err != nil {
			return err
		}
		types, err := rows.ColumnTypes()
		if err != nil {
			return err
		}
		out.Columns = make([]ResultColumn, len(names))
		for i := range names {
			rc := ResultColumn{Name: names[i]}
			if i < len(types) {
				rc.DeclType = types[i].DatabaseTypeName()
			}
			out.Columns[i] = rc
		}

		vals := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if len(out.Rows) >= cap {
				out.Truncated = true
				break
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			row := make([]any, len(vals))
			for i, v := range vals {
				row[i] = jsonValue(v)
			}
			out.Rows = append(out.Rows, row)
		}
		return rows.Err()
	})
	if err != nil {
		return ResultSet{}, err
	}
	out.RowCount = len(out.Rows)
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

// jsonValue converts a driver value into something json.Marshal renders
// losslessly: numbers, strings, booleans and nil pass through; []byte becomes a
// Blob; time.Time becomes RFC3339 text.
func jsonValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case int64, float64, bool, string:
		return t
	case []byte:
		return blobValue(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// blobValue renders a BLOB as hex preview plus a capped base64 payload.
func blobValue(b []byte) Blob {
	preview := b
	if len(preview) > blobPreviewBytes {
		preview = preview[:blobPreviewBytes]
	}
	data := b
	truncated := false
	if len(data) > blobDataMaxBytes {
		data = data[:blobDataMaxBytes]
		truncated = true
	}
	return Blob{
		IsBlob:    true,
		Bytes:     len(b),
		Preview:   hex.EncodeToString(preview),
		Data:      base64.StdEncoding.EncodeToString(data),
		Truncated: truncated,
	}
}
