package dbbrowse

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// This file is the write side of the browser (phases P2–P4): parameterized row
// CRUD, a confirmed multi-statement executor, and server-built DDL. Everything
// here opens the file read-write, so every caller MUST have obtained the user's
// explicit confirmation first (see the safety model in the design spec). The
// package itself enforces no policy — the server layer owns the session-data
// guard and the ATTACH/DETACH block.

// RowIDColumn is the synthetic column name under which TablePageKeyed exposes
// the true rowid of a table that has no declared primary key.
const RowIDColumn = "_rowid_"

// ErrNoRowsAffected is returned when an UPDATE/DELETE key matched no row. It is
// the optimistic-concurrency signal: the row changed or was deleted since the
// grid loaded.
var ErrNoRowsAffected = errors.New("no row matched the key")

// ErrMultipleRowsAffected is returned when a key matched more than one row,
// which means the key did not uniquely identify a row.
var ErrMultipleRowsAffected = errors.New("key matched more than one row")

// ExecResult is the JSON-safe result of a mutating statement.
type ExecResult struct {
	RowsAffected int64 `json:"rows_affected"`
	LastInsertID int64 `json:"last_insert_id,omitempty"`
	ElapsedMS    int64 `json:"elapsed_ms"`
}

// openReadWrite opens path read-write with a busy timeout. Unlike the reader it
// does not set mode=ro.
func openReadWrite(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", escapePath(path))
	return sql.Open("sqlite", dsn)
}

// withReadWrite opens a read-write connection, runs fn, and closes it.
func withReadWrite(ctx context.Context, path string, fn func(*sql.DB) error) error {
	db, err := openReadWrite(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}

// rowCountError reports that a statement affected an unexpected number of rows.
// The exactly-one contract is enforced inside a transaction (execExact), so a
// multi-row key can be rolled back rather than silently applied.
type rowCountError struct {
	got  int64
	want int64
}

func (e *rowCountError) Error() string {
	return fmt.Sprintf("statement affected %d rows, want %d", e.got, e.want)
}

// execExact runs one parameterized statement inside a transaction and commits
// only when it affected exactly want rows. Running inside a transaction is what
// makes the exactly-one contract real: in autocommit the rows would already be
// changed before the count could be checked, so a multi-row key could not be
// rolled back.
func execExact(ctx context.Context, path, statement string, args []any, want int64) (ExecResult, error) {
	start := time.Now()
	var out ExecResult
	err := withReadWrite(ctx, path, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
		res, err := tx.ExecContext(ctx, statement, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != want {
			return &rowCountError{got: n, want: want}
		}
		out.RowsAffected = n
		if id, err := res.LastInsertId(); err == nil {
			out.LastInsertID = id
		}
		return tx.Commit()
	})
	if err != nil {
		return ExecResult{}, err
	}
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

// Exec runs every statement in script inside ONE transaction, so a failure in a
// later statement rolls back the whole batch. Statements are split on top-level
// semicolons (see SplitStatements). RowsAffected is the sum across statements.
func Exec(ctx context.Context, path, script string) (ExecResult, error) {
	stmts := SplitStatements(script)
	if len(stmts) == 0 {
		return ExecResult{}, errors.New("no statement to execute")
	}
	start := time.Now()
	var out ExecResult
	err := withReadWrite(ctx, path, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
		for _, s := range stmts {
			res, err := tx.ExecContext(ctx, s)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err == nil {
				out.RowsAffected += n
			}
			if id, err := res.LastInsertId(); err == nil && id != 0 {
				out.LastInsertID = id
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return ExecResult{}, err
	}
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

// SplitStatements splits a SQL script on top-level semicolons, respecting
// single/double/backtick quotes, bracket identifiers and line/block comments.
// It is used both to run a confirmed batch and to decide whether any
// constituent statement mutates.
func SplitStatements(script string) []string {
	var (
		out          []string
		cur          strings.Builder
		words        []string
		word         strings.Builder
		begin, cases int
	)
	const (
		none = iota
		single
		double
		backtick
		bracket
	)
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		w := strings.ToUpper(word.String())
		word.Reset()
		words = append(words, w)
		switch w {
		case "BEGIN":
			if isTriggerHeader(words) {
				begin++
			}
		case "CASE":
			cases++
		case "END":
			if cases > 0 {
				cases--
			} else if begin > 0 {
				begin--
			}
		}
	}
	resetStmt := func() {
		words = words[:0]
		begin, cases = 0, 0
		word.Reset()
	}
	flushStmt := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
		resetStmt()
	}
	quote := none
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch quote {
		case single:
			cur.WriteByte(c)
			if c == '\'' {
				if i+1 < len(script) && script[i+1] == '\'' {
					cur.WriteByte(script[i+1])
					i++
				} else {
					quote = none
				}
			}
		case double:
			cur.WriteByte(c)
			if c == '"' {
				if i+1 < len(script) && script[i+1] == '"' {
					cur.WriteByte(script[i+1])
					i++
				} else {
					quote = none
				}
			}
		case backtick:
			cur.WriteByte(c)
			if c == '`' {
				quote = none
			}
		case bracket:
			cur.WriteByte(c)
			if c == ']' {
				quote = none
			}
		default:
			switch {
			case c == '\'':
				flushWord()
				quote = single
				cur.WriteByte(c)
			case c == '"':
				flushWord()
				quote = double
				cur.WriteByte(c)
			case c == '`':
				flushWord()
				quote = backtick
				cur.WriteByte(c)
			case c == '[':
				flushWord()
				quote = bracket
				cur.WriteByte(c)
			case c == '-' && i+1 < len(script) && script[i+1] == '-':
				flushWord()
				for i < len(script) && script[i] != '\n' {
					cur.WriteByte(script[i])
					i++
				}
				if i < len(script) {
					cur.WriteByte(script[i])
				}
			case c == '/' && i+1 < len(script) && script[i+1] == '*':
				flushWord()
				cur.WriteString("/*")
				i += 2
				for i+1 < len(script) && !(script[i] == '*' && script[i+1] == '/') {
					cur.WriteByte(script[i])
					i++
				}
				if i+1 < len(script) {
					cur.WriteString("*/")
					i++
				}
			case c == ';':
				flushWord()
				if begin > 0 {
					// Inside a CREATE TRIGGER body: the semicolon is part of the
					// statement, not a separator.
					cur.WriteByte(c)
					continue
				}
				flushStmt()
			case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_':
				word.WriteByte(c)
				cur.WriteByte(c)
			default:
				flushWord()
				cur.WriteByte(c)
			}
		}
	}
	flushStmt()
	return out
}

// isTriggerHeader reports whether the words seen so far begin a CREATE TRIGGER
// statement, so BEGIN/END inside it delimit a body rather than a transaction.
func isTriggerHeader(words []string) bool {
	if len(words) < 2 || words[0] != "CREATE" {
		return false
	}
	if words[1] == "TRIGGER" {
		return true
	}
	if (words[1] == "TEMP" || words[1] == "TEMPORARY") && len(words) >= 3 {
		return words[2] == "TRIGGER"
	}
	return false
}

// blockedWriteWords are statement-initial keywords that must never run on the
// write path: there is no authorizer hook, so ATTACH could reach a file outside
// the containment boundary and VACUUM INTO could write one.
var blockedWriteWords = []string{"ATTACH", "DETACH"}

// BlockedWriteStatement reports the offending keyword when script contains a
// statement that is refused outright on the write path (ATTACH, DETACH, or
// VACUUM ... INTO). The scan is deliberately conservative: it strips
// strings/comments and looks for the words anywhere, so a rare legitimate
// identifier named `attach` is refused too. Over-blocking a write is safe.
func BlockedWriteStatement(script string) (string, bool) {
	words := sqlWords(script)
	for _, w := range blockedWriteWords {
		for _, got := range words {
			if got == w {
				return w, true
			}
		}
	}
	hasVacuum, hasInto := false, false
	for _, got := range words {
		if got == "VACUUM" {
			hasVacuum = true
		}
		if got == "INTO" {
			hasInto = true
		}
	}
	if hasVacuum && hasInto {
		return "VACUUM INTO", true
	}
	return "", false
}

// WritePragma reports whether script assigns a PRAGMA that changes database
// behaviour or the schema. Some of these (journal_mode) can succeed on a
// read-only connection without error, so the readonly probe alone cannot gate
// them — the caller must require explicit confirmation.
func WritePragma(script string) bool {
	if !strings.Contains(script, "=") {
		return false
	}
	hasPragma := false
	for _, w := range sqlWords(script) {
		switch w {
		case "PRAGMA":
			hasPragma = true
		case "JOURNAL_MODE", "WRITABLE_SCHEMA":
			if hasPragma {
				return true
			}
		}
	}
	return false
}

// sqlWords returns the upper-cased bare words of script, ignoring the contents
// of quoted strings, identifiers and comments.
func sqlWords(script string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.ToUpper(cur.String()))
			cur.Reset()
		}
	}
	const (
		none = iota
		single
		double
		backtick
		bracket
	)
	quote := none
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch quote {
		case single:
			if c == '\'' {
				quote = none
			}
			continue
		case double:
			if c == '"' {
				quote = none
			}
			continue
		case backtick:
			if c == '`' {
				quote = none
			}
			continue
		case bracket:
			if c == ']' {
				quote = none
			}
			continue
		}
		switch {
		case c == '\'':
			flush()
			quote = single
		case c == '"':
			flush()
			quote = double
		case c == '`':
			flush()
			quote = backtick
		case c == '[':
			flush()
			quote = bracket
		case c == '-' && i+1 < len(script) && script[i+1] == '-':
			flush()
			for i < len(script) && script[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(script) && script[i+1] == '*':
			flush()
			i += 2
			for i+1 < len(script) && !(script[i] == '*' && script[i+1] == '/') {
				i++
			}
			i++
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_':
			cur.WriteByte(c)
		default:
			flush()
		}
	}
	flush()
	return words
}

// RowValues maps a column name to a driver-ready value.
type RowValues map[string]any

// NormalizeValue converts a JSON-decoded value (from json.Decoder with
// UseNumber) into a value the SQLite driver accepts: null, bool, string,
// int64/float64 from json.Number, or []byte from a {"$blob":true,"data":...}
// object.
func NormalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string:
		return t, nil
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i, nil
		}
		if f, err := t.Float64(); err == nil {
			return f, nil
		}
		return nil, fmt.Errorf("invalid number %q", t.String())
	case float64:
		return t, nil
	case map[string]any:
		if b, ok := t["$blob"]; ok && isTrue(b) {
			data, _ := t["data"].(string)
			raw, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("invalid blob data: %w", err)
			}
			return raw, nil
		}
		return nil, errors.New("unsupported object value (only {$blob:true,data:<base64>} is accepted)")
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}

// NormalizeRow converts every value in a JSON-decoded row map.
func NormalizeRow(raw map[string]any) (RowValues, error) {
	out := make(RowValues, len(raw))
	for k, v := range raw {
		nv, err := NormalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", k, err)
		}
		out[k] = nv
	}
	return out, nil
}

func isTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return false
	}
}

func sortedKeys(m RowValues) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RowInsert inserts one row and requires exactly one row to be affected.
func RowInsert(ctx context.Context, path, table string, values RowValues) (ExecResult, error) {
	if strings.TrimSpace(table) == "" {
		return ExecResult{}, errors.New("table is required")
	}
	if len(values) == 0 {
		return ExecResult{}, errors.New("no values to insert")
	}
	cols := sortedKeys(values)
	placeholders := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, c := range cols {
		placeholders[i] = "?"
		args[i] = values[c]
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		QuoteIdent(table), joinQuoted(cols), strings.Join(placeholders, ", "))
	res, err := execExact(ctx, path, stmt, args, 1)
	if err != nil {
		var rc *rowCountError
		if errors.As(err, &rc) {
			return ExecResult{}, fmt.Errorf("insert affected %d rows", rc.got)
		}
		return ExecResult{}, err
	}
	return res, nil
}

// RowUpdate updates the row identified by key and requires exactly one row to
// be affected. NULL-safe `IS` comparisons mean a NULL key component still
// matches the row it came from.
func RowUpdate(ctx context.Context, path, table string, key, values RowValues) (ExecResult, error) {
	if strings.TrimSpace(table) == "" {
		return ExecResult{}, errors.New("table is required")
	}
	if len(values) == 0 {
		return ExecResult{}, errors.New("no values to update")
	}
	if len(key) == 0 {
		return ExecResult{}, errors.New("no key to identify the row")
	}
	setCols := sortedKeys(values)
	keyCols := sortedKeys(key)
	var sb strings.Builder
	sb.WriteString("UPDATE ")
	sb.WriteString(QuoteIdent(table))
	sb.WriteString(" SET ")
	args := make([]any, 0, len(setCols)+len(keyCols))
	for i, c := range setCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(QuoteIdent(c))
		sb.WriteString(" = ?")
		args = append(args, values[c])
	}
	sb.WriteString(" WHERE ")
	for i, c := range keyCols {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(QuoteIdent(c))
		appendKeyPredicate(&sb, &args, key[c])
	}
	return execRowChange(ctx, path, sb.String(), args)
}

// RowDelete deletes the row identified by key and requires exactly one row to
// be affected.
func RowDelete(ctx context.Context, path, table string, key RowValues) (ExecResult, error) {
	if strings.TrimSpace(table) == "" {
		return ExecResult{}, errors.New("table is required")
	}
	if len(key) == 0 {
		return ExecResult{}, errors.New("no key to identify the row")
	}
	keyCols := sortedKeys(key)
	var sb strings.Builder
	sb.WriteString("DELETE FROM ")
	sb.WriteString(QuoteIdent(table))
	sb.WriteString(" WHERE ")
	args := make([]any, 0, len(keyCols))
	for i, c := range keyCols {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(QuoteIdent(c))
		appendKeyPredicate(&sb, &args, key[c])
	}
	return execRowChange(ctx, path, sb.String(), args)
}

// execRowChange runs an UPDATE/DELETE and enforces exactly-one-row semantics so
// the caller can surface a 409 instead of silently changing many rows.
func execRowChange(ctx context.Context, path, stmt string, args []any) (ExecResult, error) {
	res, err := execExact(ctx, path, stmt, args, 1)
	if err != nil {
		var rc *rowCountError
		if errors.As(err, &rc) {
			if rc.got == 0 {
				return ExecResult{}, ErrNoRowsAffected
			}
			return ExecResult{}, ErrMultipleRowsAffected
		}
		return ExecResult{}, err
	}
	return res, nil
}

// appendKeyPredicate appends a NULL-safe, index-friendly comparison: `IS NULL`
// for a nil value, otherwise `= ?`. Using `IS ?` for every value is correct but
// can defeat index use, so a large table would scan on every edit.
func appendKeyPredicate(sb *strings.Builder, args *[]any, v any) {
	if v == nil {
		sb.WriteString(" IS NULL")
		return
	}
	sb.WriteString(" = ?")
	*args = append(*args, v)
}

// joinQuoted quotes and joins column names for an INSERT column list.
func joinQuoted(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = QuoteIdent(c)
	}
	return strings.Join(quoted, ", ")
}

// ── Guided DDL ──────────────────────────────────────────────────────────────

// ColumnDef is one column in a guided CREATE TABLE / ADD COLUMN. Type and
// Default are validated before they reach SQL.
type ColumnDef struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	NotNull bool    `json:"not_null"`
	PK      bool    `json:"pk"`
	Default *string `json:"default"`
}

// typePattern accepts a single-word SQLite type name with an optional size,
// e.g. TEXT, INTEGER, VARCHAR(255), DECIMAL(10,2). It deliberately rejects a
// space (so `INTEGER PRIMARY KEY` cannot smuggle in a constraint that bypasses
// the ColumnDef flags) and anything that could break out of the type position.
var typePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\(\s*[0-9]+\s*(,\s*[0-9]+\s*)?\))?$`)

// numericLiteral matches a plain SQL numeric literal. strconv.ParseFloat is NOT
// used because it accepts NaN, Inf and hex floats, which would be emitted raw
// into a DEFAULT clause.
var numericLiteral = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func renderColumnDef(c ColumnDef, inlinePK bool) (string, error) {
	if strings.TrimSpace(c.Name) == "" {
		return "", errors.New("column name is required")
	}
	var sb strings.Builder
	sb.WriteString(QuoteIdent(c.Name))
	if t := strings.TrimSpace(c.Type); t != "" {
		if !typePattern.MatchString(t) {
			return "", fmt.Errorf("invalid column type %q", c.Type)
		}
		sb.WriteString(" ")
		sb.WriteString(t)
	}
	if c.PK && inlinePK {
		sb.WriteString(" PRIMARY KEY")
	}
	if c.NotNull {
		sb.WriteString(" NOT NULL")
	}
	if c.Default != nil {
		d, err := renderDefault(*c.Default)
		if err != nil {
			return "", err
		}
		sb.WriteString(" DEFAULT ")
		sb.WriteString(d)
	}
	return sb.String(), nil
}

// renderDefault turns a user-supplied default into a SQL literal. Only an
// allowlist of keyword defaults, a numeric literal, or a quoted string are
// accepted — never a raw expression, which would be an injection point.
func renderDefault(v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", errors.New("empty default")
	}
	switch strings.ToUpper(s) {
	case "NULL", "CURRENT_TIMESTAMP", "CURRENT_DATE", "CURRENT_TIME", "TRUE", "FALSE":
		return strings.ToUpper(s), nil
	}
	if numericLiteral.MatchString(s) {
		return s, nil
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

// BuildCreateTable renders a CREATE TABLE statement from validated definitions.
// A single primary-key column is declared inline (so INTEGER stays a rowid
// alias); a composite primary key becomes a table constraint.
func BuildCreateTable(table string, cols []ColumnDef) (string, error) {
	if strings.TrimSpace(table) == "" {
		return "", errors.New("table name is required")
	}
	if len(cols) == 0 {
		return "", errors.New("at least one column is required")
	}
	seen := map[string]bool{}
	var pkCols []string
	for _, c := range cols {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return "", errors.New("column name is required")
		}
		if seen[strings.ToLower(name)] {
			return "", fmt.Errorf("duplicate column %q", name)
		}
		seen[strings.ToLower(name)] = true
		if c.PK {
			pkCols = append(pkCols, name)
		}
	}
	inlinePK := len(pkCols) == 1
	parts := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		def, err := renderColumnDef(c, inlinePK)
		if err != nil {
			return "", err
		}
		parts = append(parts, def)
	}
	if len(pkCols) > 1 {
		quoted := make([]string, len(pkCols))
		for i, c := range pkCols {
			quoted[i] = QuoteIdent(c)
		}
		parts = append(parts, "PRIMARY KEY ("+strings.Join(quoted, ", ")+")")
	}
	return fmt.Sprintf("CREATE TABLE %s (%s)", QuoteIdent(table), strings.Join(parts, ", ")), nil
}

// BuildAddColumn renders an ALTER TABLE ADD COLUMN. A new NOT NULL column must
// carry a default, which SQLite requires for existing rows.
func BuildAddColumn(table string, c ColumnDef) (string, error) {
	if strings.TrimSpace(table) == "" {
		return "", errors.New("table name is required")
	}
	if c.PK {
		return "", errors.New("SQLite cannot add a primary-key column")
	}
	if c.NotNull && c.Default == nil {
		return "", errors.New("a new NOT NULL column needs a default")
	}
	def, err := renderColumnDef(c, false)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", QuoteIdent(table), def), nil
}

// BuildDropTable renders a DROP TABLE.
func BuildDropTable(table string) (string, error) {
	if strings.TrimSpace(table) == "" {
		return "", errors.New("table name is required")
	}
	return "DROP TABLE " + QuoteIdent(table), nil
}

// BuildCreateIndex renders a CREATE [UNIQUE] INDEX.
func BuildCreateIndex(table, index string, columns []string, unique bool) (string, error) {
	if strings.TrimSpace(table) == "" || strings.TrimSpace(index) == "" {
		return "", errors.New("table and index names are required")
	}
	if len(columns) == 0 {
		return "", errors.New("at least one index column is required")
	}
	uq := ""
	if unique {
		uq = "UNIQUE "
	}
	cols := make([]string, len(columns))
	for i, c := range columns {
		if strings.TrimSpace(c) == "" {
			return "", errors.New("empty index column")
		}
		cols[i] = QuoteIdent(c)
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		uq, QuoteIdent(index), QuoteIdent(table), strings.Join(cols, ", ")), nil
}

// BuildDropIndex renders a DROP INDEX.
func BuildDropIndex(index string) (string, error) {
	if strings.TrimSpace(index) == "" {
		return "", errors.New("index name is required")
	}
	return "DROP INDEX " + QuoteIdent(index), nil
}

// KeyColumns returns the columns that uniquely identify a row: the declared
// primary key, or [RowIDColumn] for a rowid table with no primary key. ok is
// false for a view, or a WITHOUT ROWID table with no primary key, where row
// editing is not possible.
func (s TableSchema) KeyColumns() ([]string, bool) {
	var pk []string
	for _, c := range s.Columns {
		if c.PK > 0 {
			pk = append(pk, c.Name)
		}
	}
	if len(pk) > 0 {
		sort.SliceStable(pk, func(i, j int) bool { return pk[i] < pk[j] })
		return pk, true
	}
	if s.RowID {
		return []string{RowIDColumn}, true
	}
	return nil, false
}

// TablePageKeyed is TablePage with an explicit row-identity column: when
// includeRowID is true the result gains a leading RowIDColumn holding the true
// rowid, used to address a row in a table with no declared primary key.
func TablePageKeyed(ctx context.Context, path, table string, limit, offset int, includeRowID bool) (ResultSet, error) {
	limit = clampLimit(limit)
	if offset < 0 {
		offset = 0
	}
	sel := "*"
	if includeRowID {
		sel = QuoteIdent(RowIDColumn) + " AS " + QuoteIdent(RowIDColumn) + ", *"
	}
	q := fmt.Sprintf("SELECT %s FROM %s LIMIT ? OFFSET ?", sel, QuoteIdent(table))
	return queryWithArgs(ctx, path, q, []any{limit + 1, offset}, limit)
}
