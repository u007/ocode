# Task 5: Read path — `.sqlite` first, orphan cleanup

**Files:**
- Modify: `internal/session/session.go:308-368` (`Load`, `LoadForDir`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `readSqliteSession(path string) (*Session, error)` (Task 2); `sqliteSessionPath(dir, id string) string` (Task 1); `fileExists`, `sessionCandidateIDs`, `ojsonlSessionPath`, `clearOjsonlWriteState` (existing).
- Produces: `cleanupMigrationOrphans(dir, id string)`; modified `Load`/`LoadForDir` with unchanged signatures.

- [x] **Step 1: Write the failing tests**

```go
// internal/session/session_test.go — add to existing file

func TestLoadReadsMigratedSqliteSession(t *testing.T) {
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	if err := Save("ses_loadsqlite1", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s, err := Load("ses_loadsqlite1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", s.Messages)
	}
}

func TestLoadStillReadsUnmigratedLegacySession(t *testing.T) {
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	storageDir, _ := GetStorageDir()
	if err := saveOjsonl(storageDir, "ses_stilllegacy1", "legacy title", []agent.Message{{Role: "user", Content: "still here"}}, nil); err != nil {
		t.Fatalf("saveOjsonl: %v", err)
	}

	s, err := Load("ses_stilllegacy1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Title != "legacy title" || len(s.Messages) != 1 {
		t.Fatalf("unexpected session: %+v", s)
	}
}

func TestLoadPrefersSqliteAndCleansUpOrphanLegacyFile(t *testing.T) {
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	storageDir, _ := GetStorageDir()
	id := "ses_orphan1"

	// Simulates a crash between migrateToSqlite writing+verifying the new
	// .sqlite file and deleting the old .ojsonl file.
	if err := writeSqliteSessionFull(storageDir, Session{
		ID:        id,
		Title:     "sqlite wins",
		Messages:  []agent.Message{{Role: "user", Content: "from sqlite"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}
	if err := saveOjsonl(storageDir, id, "stale ojsonl", []agent.Message{{Role: "user", Content: "from ojsonl"}}, nil); err != nil {
		t.Fatalf("saveOjsonl (simulate orphan): %v", err)
	}

	s, err := Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Title != "sqlite wins" {
		t.Fatalf("expected .sqlite to win, got %+v", s)
	}

	if fileExists(ojsonlSessionPath(storageDir, id)) {
		t.Fatalf("expected orphaned .ojsonl file to be cleaned up after a successful .sqlite Load")
	}
}
```

Add `"time"` to `session_test.go`'s imports if not already present.

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestLoadReadsMigratedSqliteSession|TestLoadStillReadsUnmigratedLegacySession|TestLoadPrefersSqliteAndCleansUpOrphanLegacyFile' -v`
Expected: FAIL — `TestLoadPrefersSqliteAndCleansUpOrphanLegacyFile` fails
because `Load` doesn't check `.sqlite` yet, so it returns the `.ojsonl`
content ("from ojsonl") instead. (`TestLoadReadsMigratedSqliteSession` and
`TestLoadStillReadsUnmigratedLegacySession` already pass by coincidence —
sqlite via the `.ojsonl` fallback path since `readSessionFile` only knows
`.json`, so a `.sqlite`-only session currently fails to load. Confirm this
concretely: run each test individually and check which fail.)

- [x] **Step 3: Implement the `.sqlite`-first dispatch and orphan cleanup**

Replace `Load` and `LoadForDir` in `internal/session/session.go`
(currently `session.go:308-368`):

```go
func Load(id string) (*Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}
	return loadFromDir(dir, id)
}

// LoadForDir loads a session from the storage associated with wd. It is used
// by multi-project servers; Load continues to use the process/session workdir.
func LoadForDir(wd, id string) (*Session, error) {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return nil, err
	}
	return loadFromDir(dir, id)
}

// loadFromDir is the shared load core for Load/LoadForDir: try .sqlite
// first (authoritative once a session has migrated — see saveToDir), then
// .ojsonl, then fall back to the legacy .json candidates exactly as
// before.
func loadFromDir(dir, id string) (*Session, error) {
	for _, candidate := range sessionCandidateIDs(id) {
		sqlitePath := sqliteSessionPath(dir, candidate)
		if !fileExists(sqlitePath) {
			continue
		}
		s, err := readSqliteSession(sqlitePath)
		if err != nil {
			return nil, err
		}
		s.Messages = removeIncompleteToolRequests(s.Messages)
		cleanupMigrationOrphans(dir, candidate)
		return s, nil
	}

	// Check for .ojsonl next (try both the bare id and the canonical prefixed form).
	for _, candidate := range sessionCandidateIDs(id) {
		ojsonlPath := ojsonlSessionPath(dir, candidate)
		if !fileExists(ojsonlPath) {
			continue
		}
		s, err := loadOjsonlSession(ojsonlPath)
		if err != nil {
			return nil, err
		}
		s.Messages = removeIncompleteToolRequests(s.Messages)
		return s, nil
	}

	path, data, err := readSessionFile(dir, id)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s is corrupt: %w", path, err)
	}
	s.Messages = removeIncompleteToolRequests(s.Messages)
	return &s, nil
}

// cleanupMigrationOrphans removes a leftover .json/.ojsonl file for a
// session that already has a valid .sqlite file. This can only happen if
// migrateToSqlite's process was killed after the new file was written and
// verified but before the old one was removed (see saveToDir). Best-effort:
// a failure here just means the orphan lingers — harmless, since .sqlite
// always wins on Load — until the next successful Load of the same id.
func cleanupMigrationOrphans(dir, id string) {
	jsonPath := filepath.Join(dir, id+".json")
	if fileExists(jsonPath) {
		if err := os.Remove(jsonPath); err != nil {
			log.Printf("session: cleanup orphan %s: %v", jsonPath, err)
		}
	}
	ojsonlPath := ojsonlSessionPath(dir, id)
	if fileExists(ojsonlPath) {
		if err := os.Remove(ojsonlPath); err != nil {
			log.Printf("session: cleanup orphan %s: %v", ojsonlPath, err)
		}
		clearOjsonlWriteState(ojsonlPath)
	}
}
```

This removes the duplication that previously existed between `Load` and
`LoadForDir` (they had near-identical bodies) as a direct consequence of
adding the same third `.sqlite` branch to both — a minimal, motivated
dedup, not a standalone refactor.

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestLoadReadsMigratedSqliteSession|TestLoadStillReadsUnmigratedLegacySession|TestLoadPrefersSqliteAndCleansUpOrphanLegacyFile' -v`
Expected: PASS (all 3 tests).

- [x] **Step 5: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures. In particular
`TestLoadFallsBackToLegacyBareTimestamp` and
`TestLoadFallsBackToCanonicalPrefixedID` (pre-existing, exercise
`sessionCandidateIDs` fallback for `.json`) must still pass — the new
`.sqlite` loop runs the same candidate-id loop first and simply finds no
`.sqlite` file for those fixtures, falling through unchanged.

- [x] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat: sqlite-first read path with migration-orphan cleanup"
```
