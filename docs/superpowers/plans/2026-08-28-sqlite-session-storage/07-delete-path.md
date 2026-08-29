# Task 7: Delete removes whichever format exists

**Files:**
- Modify: `internal/session/session.go:827-860` (`Delete`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `sqliteSessionPath` (Task 1); `deleteIndexRow` (Task 3); existing `ojsonlSessionPath`, `clearOjsonlWriteState`.
- Produces: modified `Delete` with unchanged signature.

- [x] **Step 1: Write the failing test**

```go
// internal/session/session_test.go — add to existing file

func TestDeleteRemovesMigratedSqliteSessionAndIndexRow(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_delete-sqlite1", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, _ := GetStorageDir()
	if !fileExists(sqliteSessionPath(dir, "ses_delete-sqlite1")) {
		t.Fatalf("test setup: expected .sqlite file to exist")
	}

	if err := Delete("ses_delete-sqlite1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if fileExists(sqliteSessionPath(dir, "ses_delete-sqlite1")) {
		t.Fatalf("expected .sqlite file removed")
	}
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	for _, m := range metas {
		if m.ID == "ses_delete-sqlite1" {
			t.Fatalf("expected index row removed, still present: %+v", m)
		}
	}
}

func TestDeleteStillRemovesLegacyOjsonlSession(t *testing.T) {
	tmpDir := t.TempDir()
	dir, err := GetStorageDirForPath(tmpDir)
	if err != nil {
		t.Fatalf("GetStorageDirForPath: %v", err)
	}
	if err := saveOjsonl(dir, "ses_delete-legacy1", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("saveOjsonl: %v", err)
	}

	SetWorkDir(tmpDir)
	defer SetWorkDir("")
	if err := Delete("ses_delete-legacy1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if fileExists(ojsonlSessionPath(dir, "ses_delete-legacy1")) {
		t.Fatalf("expected .ojsonl file removed")
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestDeleteRemovesMigratedSqliteSessionAndIndexRow|TestDeleteStillRemovesLegacyOjsonlSession' -v`
Expected: `TestDeleteRemovesMigratedSqliteSessionAndIndexRow` FAILs — `Delete`
doesn't remove `.sqlite` files or index rows yet.
`TestDeleteStillRemovesLegacyOjsonlSession` already passes (regression
check, confirm it still does after Step 3).

- [x] **Step 3: Implement `.sqlite` and index-row removal in `Delete`**

Replace `Delete` in `internal/session/session.go` (currently
`session.go:827-860`):

```go
// Delete removes a session file and updates the index — whichever
// on-disk format the session id currently exists in.
func Delete(id string) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	for _, p := range []string{
		sqliteSessionPath(dir, id),
		filepath.Join(dir, id+".json"),
		ojsonlSessionPath(dir, id),
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	clearOjsonlWriteState(ojsonlSessionPath(dir, id))

	if err := deleteIndexRow(dir, id); err != nil {
		log.Printf("session: delete index row for %s: %v", id, err)
	}

	// Update legacy index.json (write-only today, unused for reads — see
	// this plan's INDEX.md Global Constraints; kept as-is).
	indexPath := filepath.Join(dir, "index.json")
	var idx sessionIndex
	data, err := os.ReadFile(indexPath)
	if err == nil {
		json.Unmarshal(data, &idx) //nolint:errcheck
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]string)
	}
	delete(idx.Sessions, id)

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644)
}
```

The only changes from the current implementation: `sqliteSessionPath(dir,
id)` added to the removal loop, and the `deleteIndexRow` call added right
after it (non-fatal on error, logged — matches how `refreshIndexRow`
failures are handled in Task 4, since a stale index row just means that
one session briefly falls back to being invisible in listing rather than
corrupting anything, and the row would be corrected on that session's
next save if it somehow still existed, which it won't since we just
deleted its file).

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestDeleteRemovesMigratedSqliteSessionAndIndexRow|TestDeleteStillRemovesLegacyOjsonlSession' -v`
Expected: PASS (both tests).

- [x] **Step 5: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures. In particular any pre-existing test
exercising `Delete` against a `.ojsonl` fixture (e.g. around
`session_test.go:617`) must still pass unchanged.

- [x] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat: Delete removes sqlite sessions and their index row"
```
