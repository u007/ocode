package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/u007/ocode/internal/dbbrowse"
	"github.com/u007/ocode/internal/paths"
)

// The sqlite_* tools give the model typed access to a SQLite file so it does
// not have to write a throwaway Python/sqlite3 script for every query. The
// engine, the read-only allowlist and the pre-write snapshot are
// internal/dbbrowse (the same ones the web SQLite browser uses); these tools
// only add path confinement and agent-friendly output.
//
// Why tools instead of scripts: a permission judge can classify
// {path, sql} directly, whereas a script's effect has to be inferred from its
// source (and a source over the read cap is refused as truncated).

const (
	sqliteToolTimeout = 30 * time.Second
	// sqliteToolDefaultRows / sqliteToolMaxRows bound a single result handed
	// to the model. dbbrowse allows 1000/10000, which is far more than a
	// context window should absorb in one call; the caller pages with LIMIT /
	// OFFSET (or the limit argument) instead.
	sqliteToolDefaultRows = 100
	sqliteToolMaxRows     = 1000
)

// sqlitePath resolves and probes a database path: confined to the allowed
// roots like every file tool, and required to be an existing SQLite file so a
// typo can never make a driver create a stray empty database.
func sqlitePath(ctx context.Context, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("path is required")
	}
	safe, err := confinedPath(ctx, raw)
	if err != nil {
		return "", err
	}
	ok, err := dbbrowse.Probe(safe)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("database file not found: %s", raw)
		}
		return "", fmt.Errorf("cannot read %s: %w", raw, err)
	}
	if !ok {
		return "", fmt.Errorf("%s is not a SQLite database", raw)
	}
	return safe, nil
}

// sqliteWriteGuard refuses a mutation of ocode's own data directory (session
// store, snapshot journal, usage ledger), resolving symlinks so a link cannot
// dodge the check. Mirrors Handler.dbWriteGuard in internal/server.
func sqliteWriteGuard(path string) error {
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		return fmt.Errorf("refusing sqlite write to %s: cannot resolve ocode's data directory: %w", path, err)
	}
	if dataDir == "" {
		return fmt.Errorf("refusing sqlite write to %s: ocode's data directory is empty", path)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		realPath = path
	}
	realDir, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		realDir = dataDir
	}
	for _, p := range [][2]string{{realPath, realDir}, {path, dataDir}} {
		rel, err := filepath.Rel(p[1], p[0])
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("refusing to write to ocode's own data directory")
		}
	}
	return nil
}

func sqliteJSON(v any) (string, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func sqliteResultJSON(rs dbbrowse.ResultSet) (string, error) {
	names := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		names[i] = c.Name
	}
	return sqliteJSON(map[string]any{
		"columns":   names,
		"rows":      rs.Rows,
		"row_count": rs.RowCount,
		"truncated": rs.Truncated,
	})
}

// ---- sqlite_schema -------------------------------------------------------

type SqliteSchemaTool struct{}

var _ ContextualTool = SqliteSchemaTool{}

func (SqliteSchemaTool) Name() string { return "sqlite_schema" }
func (SqliteSchemaTool) Description() string {
	return "List the tables of a SQLite database, or describe one table (columns, indexes, foreign keys, DDL)"
}
func (SqliteSchemaTool) Parallel() bool { return true }

func (SqliteSchemaTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "sqlite_schema",
		"description": "Inspect a SQLite database file (read-only). Without 'table' it lists every table and view with an " +
			"estimated row count; with 'table' it returns that table's columns, primary key, indexes, foreign keys and CREATE statement. " +
			"Use this before sqlite_query so column names are not guessed.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":  map[string]interface{}{"type": "string", "description": "Path to the SQLite database file"},
				"table": map[string]interface{}{"type": "string", "description": "Optional table or view name to describe"},
			},
			"required": []string{"path"},
		},
	}
}

func (t SqliteSchemaTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (SqliteSchemaTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path  string `json:"path"`
		Table string `json:"table"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	path, err := sqlitePath(ctx, p.Path)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, sqliteToolTimeout)
	defer cancel()
	if strings.TrimSpace(p.Table) == "" {
		tables, err := dbbrowse.ListTables(ctx, path)
		if err != nil {
			return "", fmt.Errorf("list tables: %w", err)
		}
		return sqliteJSON(map[string]any{"tables": tables})
	}
	schema, err := dbbrowse.DescribeTable(ctx, path, p.Table)
	if err != nil {
		if errors.Is(err, dbbrowse.ErrNoSuchTable) {
			return "", fmt.Errorf("no such table or view: %s", p.Table)
		}
		return "", fmt.Errorf("describe %s: %w", p.Table, err)
	}
	return sqliteJSON(schema)
}

// ---- sqlite_query --------------------------------------------------------

type SqliteQueryTool struct{}

var _ ContextualTool = SqliteQueryTool{}

func (SqliteQueryTool) Name() string { return "sqlite_query" }
func (SqliteQueryTool) Description() string {
	return "Run ONE read-only SQL statement (SELECT / WITH / EXPLAIN / read PRAGMA) against a SQLite database"
}
func (SqliteQueryTool) Parallel() bool { return true }

func (SqliteQueryTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "sqlite_query",
		"description": "Run a single read-only SQL statement against a SQLite database file. The connection is opened read-only, " +
			"so nothing can be modified; ATTACH, multiple statements and writes are rejected. Use sqlite_exec to change data. " +
			fmt.Sprintf("Results are capped at %d rows (default %d); 'truncated' in the reply says more exist, so page with LIMIT/OFFSET. ", sqliteToolMaxRows, sqliteToolDefaultRows) +
			"BLOB cells come back as {\"$blob\":true,\"bytes\":N,\"preview\":hex}. Call sqlite_schema first if you do not know the columns.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":  map[string]interface{}{"type": "string", "description": "Path to the SQLite database file"},
				"sql":   map[string]interface{}{"type": "string", "description": "One read-only SQL statement"},
				"limit": map[string]interface{}{"type": "integer", "description": fmt.Sprintf("Max rows to return (default %d, max %d)", sqliteToolDefaultRows, sqliteToolMaxRows)},
			},
			"required": []string{"path", "sql"},
		},
	}
}

func (t SqliteQueryTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (SqliteQueryTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path  string `json:"path"`
		SQL   string `json:"sql"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.SQL) == "" {
		return "", errors.New("sql is required")
	}
	path, err := sqlitePath(ctx, p.Path)
	if err != nil {
		return "", err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = sqliteToolDefaultRows
	}
	if limit > sqliteToolMaxRows {
		limit = sqliteToolMaxRows
	}
	ctx, cancel := context.WithTimeout(ctx, sqliteToolTimeout)
	defer cancel()
	rs, err := dbbrowse.Query(ctx, path, p.SQL, limit)
	if err != nil {
		if errors.Is(err, dbbrowse.ErrStatementNotReadOnly) {
			return "", errors.New("sqlite_query runs one read-only statement (SELECT/WITH/EXPLAIN/read PRAGMA); use sqlite_exec to modify data")
		}
		return "", fmt.Errorf("query failed: %w", err)
	}
	return sqliteResultJSON(rs)
}

// ---- sqlite_exec ---------------------------------------------------------

type SqliteExecTool struct{}

var _ ContextualTool = SqliteExecTool{}

func (SqliteExecTool) Name() string { return "sqlite_exec" }
func (SqliteExecTool) Description() string {
	return "Run SQL that modifies a SQLite database (INSERT/UPDATE/DELETE/DDL) in one transaction, after a backup snapshot"
}
func (SqliteExecTool) Parallel() bool { return false }

func (SqliteExecTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "sqlite_exec",
		"description": "Modify a SQLite database file: INSERT, UPDATE, DELETE, CREATE/ALTER/DROP. One or more ';'-separated statements run in a " +
			"SINGLE transaction (any failure rolls back all of them). A snapshot is written next to the file as '<name>.bak' before anything changes. " +
			"Scope every UPDATE/DELETE with WHERE. ATTACH, DETACH, VACUUM INTO and PRAGMA writable_schema/journal_mode assignments are refused, as is " +
			"ocode's own data directory. Prefer sqlite_query for anything that only reads.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Path to the existing SQLite database file"},
				"sql":  map[string]interface{}{"type": "string", "description": "One or more SQL statements separated by ';'"},
			},
			"required": []string{"path", "sql"},
		},
	}
}

func (t SqliteExecTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (SqliteExecTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
		SQL  string `json:"sql"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.SQL) == "" {
		return "", errors.New("sql is required")
	}
	path, err := sqlitePath(ctx, p.Path)
	if err != nil {
		return "", err
	}
	if err := sqliteWriteGuard(path); err != nil {
		return "", err
	}
	if kw, blocked := dbbrowse.BlockedWriteStatement(p.SQL); blocked {
		return "", fmt.Errorf("%s is not allowed in sqlite_exec", kw)
	}
	if dbbrowse.WritePragma(p.SQL) {
		return "", errors.New("PRAGMA assignments that change journal mode or the schema are not allowed in sqlite_exec")
	}
	ctx, cancel := context.WithTimeout(ctx, sqliteToolTimeout)
	defer cancel()
	// Snapshot first, never after: it must not contain the change it protects
	// against. A failed backup aborts the write rather than proceeding unprotected.
	backup, err := dbbrowse.Backup(ctx, path)
	if err != nil {
		return "", fmt.Errorf("backup before write failed, nothing was changed: %w", err)
	}
	res, err := dbbrowse.Exec(ctx, path, p.SQL)
	if err != nil {
		return "", fmt.Errorf("exec failed, rolled back (backup at %s): %w", backup, err)
	}
	return sqliteJSON(map[string]any{
		"rows_affected":  res.RowsAffected,
		"last_insert_id": res.LastInsertID,
		"backup":         backup,
	})
}
