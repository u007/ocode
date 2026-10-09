package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/paths"
	_ "modernc.org/sqlite"
)

func newSqliteFixture(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE positions (id INTEGER PRIMARY KEY, title TEXT NOT NULL, status TEXT);
		INSERT INTO positions (title, status) VALUES ('a','new'),('b','applied'),('c','applied');`); err != nil {
		t.Fatal(err)
	}
	return WithWorkDir(context.Background(), dir), path
}

func runTool(t *testing.T, tl ContextualTool, ctx context.Context, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tl.ExecuteCtx(ctx, raw)
}

func TestSqliteSchemaListsAndDescribes(t *testing.T) {
	ctx, path := newSqliteFixture(t)
	out, err := runTool(t, SqliteSchemaTool{}, ctx, map[string]any{"path": path})
	if err != nil || !strings.Contains(out, `"positions"`) {
		t.Fatalf("list: %v %s", err, out)
	}
	out, err = runTool(t, SqliteSchemaTool{}, ctx, map[string]any{"path": path, "table": "positions"})
	if err != nil || !strings.Contains(out, `"status"`) || !strings.Contains(out, "CREATE TABLE") {
		t.Fatalf("describe: %v %s", err, out)
	}
	if _, err := runTool(t, SqliteSchemaTool{}, ctx, map[string]any{"path": path, "table": "nope"}); err == nil {
		t.Fatal("missing table should error")
	}
}

func TestSqliteQueryReadOnly(t *testing.T) {
	ctx, path := newSqliteFixture(t)
	out, err := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": "SELECT id FROM positions WHERE status='applied' ORDER BY id"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Columns  []string `json:"columns"`
		Rows     [][]any  `json:"rows"`
		RowCount int      `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.RowCount != 2 || got.Columns[0] != "id" {
		t.Fatalf("got %+v err=%v out=%s", got, err, out)
	}
	out, _ = runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": "SELECT * FROM positions", "limit": 1})
	if !strings.Contains(out, `"truncated":true`) {
		t.Fatalf("limit not applied: %s", out)
	}
	for _, q := range []string{
		"DELETE FROM positions",
		"SELECT 1; DELETE FROM positions",
		"ATTACH DATABASE '/etc/x' AS x",
		"PRAGMA journal_mode=DELETE",
	} {
		if _, err := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": q}); err == nil {
			t.Errorf("sqlite_query accepted %q", q)
		}
	}
	if out, _ := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": "SELECT count(*) FROM positions"}); !strings.Contains(out, "3") {
		t.Errorf("rows were modified or count wrong: %s", out)
	}
}

func TestSqliteExecWritesWithBackup(t *testing.T) {
	ctx, path := newSqliteFixture(t)
	out, err := runTool(t, SqliteExecTool{}, ctx, map[string]any{"path": path, "sql": "UPDATE positions SET status='rejected' WHERE id=1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"rows_affected":1`) {
		t.Fatalf("out=%s", out)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	q, _ := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": "SELECT status FROM positions WHERE id=1"})
	if !strings.Contains(q, "rejected") {
		t.Fatalf("update not applied: %s", q)
	}
}

func TestSqliteExecRollsBackBatchAndRefusesDangerous(t *testing.T) {
	ctx, path := newSqliteFixture(t)
	// second statement fails (NOT NULL) -> first must roll back
	if _, err := runTool(t, SqliteExecTool{}, ctx, map[string]any{"path": path, "sql": "UPDATE positions SET status='x'; INSERT INTO positions (title) VALUES (NULL)"}); err == nil {
		t.Fatal("expected failure")
	}
	q, _ := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": path, "sql": "SELECT count(*) FROM positions WHERE status='x'"})
	if !strings.Contains(q, "0") {
		t.Fatalf("batch not rolled back: %s", q)
	}
	for _, s := range []string{
		"ATTACH DATABASE '/tmp/other.db' AS o",
		"VACUUM INTO '/tmp/copy.db'",
		"PRAGMA writable_schema=1",
		"PRAGMA journal_mode=WAL",
	} {
		if _, err := runTool(t, SqliteExecTool{}, ctx, map[string]any{"path": path, "sql": s}); err == nil {
			t.Errorf("sqlite_exec accepted %q", s)
		}
	}
}

func TestSqlitePathValidation(t *testing.T) {
	ctx, path := newSqliteFixture(t)
	dir := filepath.Dir(path)
	notDB := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(notDB, []byte("hello world, definitely not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.db")
	for _, p := range []string{notDB, missing, ""} {
		if _, err := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": p, "sql": "SELECT 1"}); err == nil {
			t.Errorf("accepted path %q", p)
		}
		if _, err := runTool(t, SqliteExecTool{}, ctx, map[string]any{"path": p, "sql": "SELECT 1"}); err == nil {
			t.Errorf("exec accepted path %q", p)
		}
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a missing path must not create a database")
	}
	if _, err := runTool(t, SqliteQueryTool{}, ctx, map[string]any{"path": "/etc/hosts", "sql": "SELECT 1"}); err == nil {
		t.Error("path outside the workdir accepted")
	}
}

func TestSqliteWriteGuardRefusesDataDir(t *testing.T) {
	dataDir, err := paths.GlobalDataDir()
	if err != nil || dataDir == "" {
		t.Skip("no global data dir")
	}
	if err := sqliteWriteGuard(filepath.Join(dataDir, "opencode.db")); err == nil {
		t.Error("write inside the data dir must be refused")
	}
	if err := sqliteWriteGuard(filepath.Join(t.TempDir(), "x.db")); err != nil {
		t.Errorf("unrelated path refused: %v", err)
	}
}
