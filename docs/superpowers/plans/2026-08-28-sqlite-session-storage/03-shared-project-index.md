# Task 3: Shared per-project index

**Files:**
- Modify: `internal/session/sqlitestore.go`
- Test: `internal/session/sqlitestore_test.go`

**Interfaces:**
- Consumes: `openIndexDB(dir string) (*sql.DB, error)`, `indexDBPath(dir string) string` (Task 1); `ocodeMeta{ID, Title, CreatedAt, UpdatedAt, CloneOf}` (existing, `session.go:657`).
- Produces: `upsertIndexRow(dir string, meta ocodeMeta) error`, `deleteIndexRow(dir, id string) error`, `queryIndexMetas(dir string) ([]ocodeMeta, error)`, `mergeMetas(legacy, indexed []ocodeMeta) []ocodeMeta`.

- [x] **Step 1: Write the failing test**

```go
// internal/session/sqlitestore_test.go — add to existing file

func TestUpsertAndQueryIndexRow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_idx1", Title: "first", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsertIndexRow: %v", err)
	}

	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "ses_idx1" || metas[0].Title != "first" {
		t.Fatalf("expected 1 indexed row, got %+v", metas)
	}

	// Upsert again with a new title — must update, not duplicate.
	later := now.Add(time.Hour)
	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_idx1", Title: "renamed", CreatedAt: now, UpdatedAt: later}); err != nil {
		t.Fatalf("upsertIndexRow (update): %v", err)
	}
	metas, err = queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 1 || metas[0].Title != "renamed" || !metas[0].UpdatedAt.Equal(later) {
		t.Fatalf("expected upsert to update in place, got %+v", metas)
	}
}

func TestQueryIndexMetasOnMissingIndexReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // no index.sqlite ever written here
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected no rows, got %+v", metas)
	}
}

func TestDeleteIndexRow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_del1", Title: "x", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsertIndexRow: %v", err)
	}
	if err := deleteIndexRow(dir, "ses_del1"); err != nil {
		t.Fatalf("deleteIndexRow: %v", err)
	}
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected row removed, got %+v", metas)
	}
}

func TestDeleteIndexRowOnMissingIndexIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := deleteIndexRow(dir, "ses_never_existed"); err != nil {
		t.Fatalf("deleteIndexRow on missing index.sqlite should be a no-op, got: %v", err)
	}
}

func TestMergeMetasIndexShadowsSameIDLegacyEntry(t *testing.T) {
	now := time.Now()
	legacy := []ocodeMeta{
		{ID: "ses_a", Title: "legacy a", UpdatedAt: now},
		{ID: "ses_b", Title: "legacy b (stale orphan)", UpdatedAt: now},
	}
	indexed := []ocodeMeta{
		{ID: "ses_b", Title: "migrated b", UpdatedAt: now.Add(time.Minute)},
	}

	merged := mergeMetas(legacy, indexed)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged entries, got %d: %+v", len(merged), merged)
	}
	var gotB ocodeMeta
	found := false
	for _, m := range merged {
		if m.ID == "ses_b" {
			gotB = m
			found = true
		}
	}
	if !found || gotB.Title != "migrated b" {
		t.Fatalf("expected indexed entry to shadow legacy entry for ses_b, got %+v", merged)
	}
}

func TestMergeMetasNoIndexReturnsLegacyUnchanged(t *testing.T) {
	legacy := []ocodeMeta{{ID: "ses_a", Title: "a"}}
	merged := mergeMetas(legacy, nil)
	if len(merged) != 1 || merged[0].ID != "ses_a" {
		t.Fatalf("expected legacy passed through unchanged, got %+v", merged)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestUpsertAndQueryIndexRow|TestQueryIndexMetasOnMissingIndexReturnsEmpty|TestDeleteIndexRow|TestMergeMetas' -v`
Expected: FAIL — `upsertIndexRow`, `deleteIndexRow`, `queryIndexMetas`, `mergeMetas` undefined.

- [x] **Step 3: Implement the shared index**

Add to `internal/session/sqlitestore.go` (add `"os"` to the existing
import block):

```go
// upsertIndexRow writes/updates a migrated session's row in the project's
// shared index.sqlite so listing (queryIndexMetas) can serve migrated
// sessions from one indexed query instead of opening every session file.
func upsertIndexRow(dir string, meta ocodeMeta) error {
	db, err := openIndexDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO sessions (id, title, created_at, updated_at, clone_of) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET title=excluded.title, updated_at=excluded.updated_at, clone_of=excluded.clone_of`,
		meta.ID, meta.Title, meta.CreatedAt, meta.UpdatedAt, meta.CloneOf,
	)
	return err
}

// deleteIndexRow removes a session's row from the project's index.sqlite.
// A no-op (not an error) if the index file doesn't exist yet — Delete
// (Task 7) calls this unconditionally regardless of the session's
// on-disk format, including for projects that have never had a migrated
// session.
func deleteIndexRow(dir, id string) error {
	path := indexDBPath(dir)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	db, err := openIndexDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// queryIndexMetas returns metadata for every migrated (.sqlite-format)
// session in dir, from the shared index — no per-session file opens.
// Returns an empty slice (not an error) if index.sqlite doesn't exist yet
// (no session in this project has migrated).
func queryIndexMetas(dir string) ([]ocodeMeta, error) {
	path := indexDBPath(dir)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := openIndexDB(dir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, title, created_at, updated_at, clone_of FROM sessions`)
	if err != nil {
		return nil, fmt.Errorf("session: query index: %w", err)
	}
	defer rows.Close()

	var metas []ocodeMeta
	for rows.Next() {
		var m ocodeMeta
		if err := rows.Scan(&m.ID, &m.Title, &m.CreatedAt, &m.UpdatedAt, &m.CloneOf); err != nil {
			return nil, fmt.Errorf("session: scan index row: %w", err)
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

// mergeMetas combines legacy-format metadata (from a directory scan) with
// the migrated-session index, letting an index row shadow a same-ID
// legacy entry. This is belt-and-suspenders for the narrow crash window
// in migrateToSqlite (Task 4) where a migrated session's old file could
// momentarily still be on disk — without this, that session would appear
// twice in a listing until the orphan is cleaned up on next Load.
func mergeMetas(legacy, indexed []ocodeMeta) []ocodeMeta {
	if len(indexed) == 0 {
		return legacy
	}
	migrated := make(map[string]struct{}, len(indexed))
	for _, m := range indexed {
		migrated[m.ID] = struct{}{}
	}
	merged := make([]ocodeMeta, 0, len(legacy)+len(indexed))
	for _, m := range legacy {
		if _, ok := migrated[m.ID]; !ok {
			merged = append(merged, m)
		}
	}
	return append(merged, indexed...)
}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestUpsertAndQueryIndexRow|TestQueryIndexMetasOnMissingIndexReturnsEmpty|TestDeleteIndexRow|TestMergeMetas' -v`
Expected: PASS (all 6 tests).

- [x] **Step 5: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures.

- [x] **Step 6: Commit**

```bash
git add internal/session/sqlitestore.go internal/session/sqlitestore_test.go
git commit -m "feat: shared per-project sqlite index for migrated sessions"
```
