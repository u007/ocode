# Task 4: Write-path dispatch and migrate-on-write

**Files:**
- Modify: `internal/session/session.go:163-176` (`saveToDir`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `writeSqliteSessionFull`, `appendSqliteSession`, `readSqliteSession` (Task 2); `upsertIndexRow` (Task 3); `sqliteSessionPath` (Task 1); `getOjsonlWriteState`, `ojsonlSessionPath`, `clearOjsonlWriteState` (existing, `internal/session/ojsonl.go`).
- Produces: `migrateToSqlite(dir, id, title string, messages []agent.Message, metadata map[string]any, wasJSON bool) error`; modified `saveToDir` with the same signature/behavior contract as today (`Save`/`SaveForDir` are unchanged callers).

- [x] **Step 1: Write the failing tests**

```go
// internal/session/session_test.go — add to existing file

func TestSaveToDirNewSessionCreatesSqlite(t *testing.T) {
	dir := t.TempDir()

	if err := saveToDir(dir, "ses_new1", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("saveToDir: %v", err)
	}

	if !fileExists(sqliteSessionPath(dir, "ses_new1")) {
		t.Fatalf("expected .sqlite file for a brand-new session")
	}
	if fileExists(ojsonlSessionPath(dir, "ses_new1")) {
		t.Fatalf("did not expect an .ojsonl file for a brand-new session")
	}
}

func TestSaveToDirExistingSqliteSessionAppends(t *testing.T) {
	dir := t.TempDir()
	id := "ses_appendflow1"

	if err := saveToDir(dir, id, "", []agent.Message{{Role: "user", Content: "one"}}, nil); err != nil {
		t.Fatalf("saveToDir (create): %v", err)
	}
	if err := saveToDir(dir, id, "", []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
	}, nil); err != nil {
		t.Fatalf("saveToDir (append): %v", err)
	}

	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("expected 2 messages after append, got %d", len(s.Messages))
	}
}

func TestSaveToDirMigratesExistingOjsonlSessionOnWrite(t *testing.T) {
	dir := t.TempDir()
	id := "ses_migrateojsonl1"

	// Simulate an existing .ojsonl session (created via the legacy path).
	if err := saveOjsonl(dir, id, "original title", []agent.Message{{Role: "user", Content: "first"}}, nil); err != nil {
		t.Fatalf("saveOjsonl (seed legacy session): %v", err)
	}
	if !fileExists(ojsonlSessionPath(dir, id)) {
		t.Fatalf("test setup: expected .ojsonl file to exist")
	}

	// This session is "loaded into chat" and written to again.
	if err := saveToDir(dir, id, "", []agent.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}, nil); err != nil {
		t.Fatalf("saveToDir (migrate): %v", err)
	}

	if fileExists(ojsonlSessionPath(dir, id)) {
		t.Fatalf("expected .ojsonl file to be removed after migration")
	}
	if !fileExists(sqliteSessionPath(dir, id)) {
		t.Fatalf("expected .sqlite file to exist after migration")
	}

	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if s.Title != "original title" {
		t.Fatalf("expected title preserved from .ojsonl, got %q", s.Title)
	}
	if len(s.Messages) != 2 || s.Messages[1].Content != "second" {
		t.Fatalf("expected 2 messages after migration, got %+v", s.Messages)
	}

	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != id {
		t.Fatalf("expected migrated session indexed, got %+v", metas)
	}
}

func TestSaveToDirMigratesExistingJSONSessionOnWrite(t *testing.T) {
	dir := t.TempDir()
	id := "ses_migratejson1"
	jsonPath := filepath.Join(dir, id+".json")

	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seed := Session{
		ID:        id,
		Title:     "legacy json title",
		Messages:  []agent.Message{{Role: "user", Content: "hi"}},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		t.Fatalf("write seed .json: %v", err)
	}

	if err := saveToDir(dir, id, "", []agent.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "reply"},
	}, nil); err != nil {
		t.Fatalf("saveToDir (migrate): %v", err)
	}

	if fileExists(jsonPath) {
		t.Fatalf("expected .json file to be removed after migration")
	}
	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if s.Title != "legacy json title" {
		t.Fatalf("expected title preserved from .json, got %q", s.Title)
	}
	if !s.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at preserved from .json, got %v want %v", s.CreatedAt, createdAt)
	}
}

func TestSaveToDirMigrationLeavesOriginalOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	id := "ses_migratefail1"

	if err := saveOjsonl(dir, id, "keep me", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("saveOjsonl (seed legacy session): %v", err)
	}

	// Make the directory read-only so the new .sqlite file can't be
	// created — simulates a write failure mid-migration.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck

	err := saveToDir(dir, id, "", []agent.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "reply"},
	}, nil)
	if err == nil {
		t.Fatalf("expected saveToDir to fail when the .sqlite write fails")
	}

	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	if !fileExists(ojsonlSessionPath(dir, id)) {
		t.Fatalf("expected original .ojsonl file to survive a failed migration")
	}
	s, err := Load(id) // best-effort: not scoped to dir, just check no panic; real Load coverage is Task 5
	_ = s
	_ = err
}
```

Add `"os"`, `"path/filepath"`, `"time"`, `"encoding/json"` to
`session_test.go`'s imports if not already present (check the existing
import block first — `encoding/json` and `os` are already imported per
the file read during planning).

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestSaveToDirNewSessionCreatesSqlite|TestSaveToDirExistingSqliteSessionAppends|TestSaveToDirMigratesExistingOjsonlSessionOnWrite|TestSaveToDirMigratesExistingJSONSessionOnWrite|TestSaveToDirMigrationLeavesOriginalOnWriteFailure' -v`
Expected: FAIL — new tests fail because `saveToDir` still dispatches only
between `.json` and `.ojsonl` (no `.sqlite` case), so no `.sqlite` file is
ever created and migration never happens.

- [x] **Step 3: Implement `migrateToSqlite` and the new `saveToDir` dispatch**

Replace `saveToDir` in `internal/session/session.go` (currently
`session.go:163-176`):

```go
// saveToDir is the shared save core: it resolves the session's storage
// dir and dispatches on which format already exists for id. New sessions
// are always created directly as .sqlite. An existing .json or .ojsonl
// session is migrated to .sqlite — and its old file deleted — the first
// time it is written to again after being loaded; sessions that are only
// ever read are left in their original format. See
// docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md.
func saveToDir(dir, id string, title string, messages []agent.Message, metadata map[string]any) error {
	if id == "" {
		id = NewSessionID()
	}

	if fileExists(sqliteSessionPath(dir, id)) {
		if err := appendSqliteSession(dir, id, title, messages, metadata); err != nil {
			return err
		}
		return refreshIndexRow(dir, id)
	}

	jsonPath := filepath.Join(dir, id+".json")
	ojsonlPath := ojsonlSessionPath(dir, id)
	wasJSON := fileExists(jsonPath)
	if wasJSON || fileExists(ojsonlPath) {
		return migrateToSqlite(dir, id, title, messages, metadata, wasJSON)
	}

	// Brand-new session: create directly in sqlite.
	now := time.Now()
	s := Session{
		ID:             id,
		Title:          title,
		TitleGenerated: title != "",
		Messages:       messages,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       metadata,
	}
	if err := writeSqliteSessionFull(dir, s); err != nil {
		return err
	}
	return refreshIndexRow(dir, id)
}

// migrateToSqlite converts an existing .json or .ojsonl session to
// .sqlite on save, preserving created_at (and title/title_generated when
// this save doesn't set an explicit new title) from the old file. It
// writes the new file and reads it back to confirm it round-trips before
// deleting the original — a transcript is never lost to a bad migration.
// wasJSON selects which legacy format id is currently stored in.
func migrateToSqlite(dir, id, title string, messages []agent.Message, metadata map[string]any, wasJSON bool) error {
	now := time.Now()
	s := Session{
		ID:             id,
		Title:          title,
		TitleGenerated: title != "",
		Messages:       messages,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       metadata,
	}

	if wasJSON {
		jsonPath := filepath.Join(dir, id+".json")
		if data, err := os.ReadFile(jsonPath); err == nil {
			var old Session
			if err := json.Unmarshal(data, &old); err == nil {
				if !old.CreatedAt.IsZero() {
					s.CreatedAt = old.CreatedAt
				}
				if s.Title == "" {
					s.Title = old.Title
					s.TitleGenerated = old.TitleGenerated
				}
			}
		}
	} else {
		ojsonlPath := ojsonlSessionPath(dir, id)
		if state, existed, err := getOjsonlWriteState(ojsonlPath); err == nil && existed {
			if !state.createdAt.IsZero() {
				s.CreatedAt = state.createdAt
			}
			if s.Title == "" {
				s.Title = state.title
				s.TitleGenerated = state.titleGenerated
			}
		}
	}

	if err := writeSqliteSessionFull(dir, s); err != nil {
		return fmt.Errorf("session: migrate %s to sqlite: %w", id, err)
	}

	// Verify the new file round-trips before touching the original.
	if _, err := readSqliteSession(sqliteSessionPath(dir, id)); err != nil {
		return fmt.Errorf("session: migrated sqlite file for %s failed verification, original left in place: %w", id, err)
	}

	if err := refreshIndexRow(dir, id); err != nil {
		log.Printf("session: index upsert for migrated %s failed (non-fatal): %v", id, err)
	}

	if wasJSON {
		os.Remove(filepath.Join(dir, id+".json")) //nolint:errcheck
	} else {
		ojsonlPath := ojsonlSessionPath(dir, id)
		os.Remove(ojsonlPath) //nolint:errcheck
		clearOjsonlWriteState(ojsonlPath)
	}
	return nil
}

// refreshIndexRow reads a just-written .sqlite session's meta straight
// back out and upserts it into the project's shared index — used after
// every sqlite write so the index never drifts from the file it mirrors.
func refreshIndexRow(dir, id string) error {
	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		return fmt.Errorf("session: read back %s for index refresh: %w", id, err)
	}
	cloneOf := ""
	if s.Metadata != nil {
		if v, ok := s.Metadata["claude_original_session_id"].(string); ok {
			cloneOf = v
		}
	}
	return upsertIndexRow(dir, ocodeMeta{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		CloneOf:   cloneOf,
	})
}
```

`session.go` already imports `encoding/json`, `fmt`, `log`, `os`,
`path/filepath`, and `time` (used by the existing `.json`/`.ojsonl` code
paths) — no new imports needed in `session.go` itself.

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestSaveToDirNewSessionCreatesSqlite|TestSaveToDirExistingSqliteSessionAppends|TestSaveToDirMigratesExistingOjsonlSessionOnWrite|TestSaveToDirMigratesExistingJSONSessionOnWrite|TestSaveToDirMigrationLeavesOriginalOnWriteFailure' -v`
Expected: PASS (all 5 tests).

- [x] **Step 5: Update two pre-existing tests whose asserted behavior this task deliberately changes**

`TestSaveNewSessionWritesOjsonl` (`session_test.go:553`) asserts a
brand-new session lands in `.ojsonl` — true before this task, false after
(new sessions now go straight to `.sqlite`, confirmed with the user in
this plan's Global Constraints). Replace it:

```go
func TestSaveNewSessionWritesSqlite(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_sqlite-new", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir, _ := GetStorageDir()
	if _, err := os.Stat(filepath.Join(dir, "ses_sqlite-new.sqlite")); err != nil {
		t.Fatalf("expected .sqlite file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ses_sqlite-new.ojsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected no .ojsonl file for a new session, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ses_sqlite-new.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .json file for a new session, got err=%v", err)
	}
}
```

`TestSaveExistingJSONSessionStaysJSON` (`session_test.go:572`) asserts an
existing `.json` session is saved back as `.json` forever — that was the
`.ojsonl` plan's deliberate constraint (`docs/superpowers/plans/2026-07-21-session-storage-ojsonl.md`:
"Existing `.json` sessions are never converted"), which this task
explicitly and intentionally supersedes: an existing `.json` (or
`.ojsonl`) session now migrates to `.sqlite` on its next write. Replace
it:

```go
func TestSaveExistingJSONSessionMigratesToSqlite(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	dir, _ := GetStorageDir()
	jsonPath := filepath.Join(dir, "ses_legacy-json.json")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := Session{ID: "ses_legacy-json", Title: "old", Messages: []agent.Message{{Role: "user", Content: "hi"}}}
	seedBytes, _ := json.Marshal(seed)
	if err := os.WriteFile(jsonPath, seedBytes, 0644); err != nil {
		t.Fatalf("seed json file: %v", err)
	}

	if err := Save("ses_legacy-json", "", []agent.Message{
		{Role: "user", Content: "hi"}, {Role: "assistant", Content: "there"},
	}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("expected .json file removed after migration, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ses_legacy-json.sqlite")); err != nil {
		t.Fatalf("expected .sqlite file to exist after migration: %v", err)
	}
	sess, err := Load("ses_legacy-json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages preserved through migration, got %d", len(sess.Messages))
	}
	if sess.Title != "old" {
		t.Fatalf("expected title preserved through migration, got %q", sess.Title)
	}
}
```

Both replacements keep the same setup/assertions style as the originals,
only updating the expected on-disk format — the behavior each test
guards (a new session gets *some* working format; an existing session's
content survives a save) is unchanged and still fully covered.

A third test, `TestLoadDoesNotFallBackToOtherProjects`
(`session_test.go:829`), seeds a session via `Save` (so it now lands in
`.sqlite`, not `.ojsonl`) and — because `GetStorageDir` resolves under the
real global data dir (`~/.local/share/opencode/...`), varying only the
project slug by `cwd` — explicitly cleans up the seeded file afterward to
avoid leaving it in the user's real data directory. Update the seeded-file
path and add the new `index.sqlite` file (written by `refreshIndexRow`,
Task 4) to that same cleanup so this test doesn't leak a stray file into
the user's real project data dir:

```go
	seededFile := filepath.Join(seededDir, sessID+".sqlite")
	if _, err := os.Stat(seededFile); err != nil {
		t.Fatalf("expected seeded sqlite file at %s: %v", seededFile, err)
	}
	t.Cleanup(func() {
		os.Remove(seededFile)
		os.Remove(filepath.Join(seededDir, "index.json"))
		os.Remove(filepath.Join(seededDir, "index.sqlite"))
	})
```

This replaces only the `seededFile := ...` line, the `if _, err :=
os.Stat(seededFile) ...` block, and the `t.Cleanup(...)` block in that
test — the rest of `TestLoadDoesNotFallBackToOtherProjects` (the
cross-project `Load` assertions) is unchanged.

- [x] **Step 6: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures.

- [x] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat: sqlite write path — new sessions direct, legacy migrates on write"
```
