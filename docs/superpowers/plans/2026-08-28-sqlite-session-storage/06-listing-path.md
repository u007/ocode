# Task 6: Listing — union migrated (indexed) and legacy (scanned) sessions

**Files:**
- Modify: `internal/session/session.go:465-507` (`List`), `session.go:509-568` (`ListRefsForDir`), `session.go:737-824` (`ListRefsPaginated`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `readSqliteSession` (Task 2); `queryIndexMetas`, `mergeMetas` (Task 3); `sqliteSessionPath` (Task 1); `mapDirEntries`, `readOcodeMeta`, `readOjsonlListMeta` (existing).
- Produces: `legacyOcodeMetas(dir string, entries []os.DirEntry) []ocodeMeta`; modified `List`, `ListRefsForDir`, `ListRefsPaginated` with unchanged signatures. (`ListRefs` and `ListAll` are unchanged — `ListRefs` calls `ListRefsPaginated`, and `ListAll` has no external callers per this plan's INDEX.md — both inherit the fix for free/are out of scope respectively.)

- [x] **Step 1: Write the failing tests**

```go
// internal/session/session_test.go — add to existing file

func TestListRefsForDirIncludesMigratedAndLegacySessions(t *testing.T) {
	// ListRefsForDir(wd) resolves the actual storage dir itself
	// (GetStorageDirForPath), so fixtures must be written there, not into
	// wd directly.
	wd := t.TempDir()
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		t.Fatalf("GetStorageDirForPath: %v", err)
	}

	// One migrated (.sqlite) session, one still-legacy (.ojsonl) session.
	if err := writeSqliteSessionFull(dir, Session{
		ID: "ses_mixed-sqlite", Title: "sqlite one",
		Messages: []agent.Message{{Role: "user", Content: "hi"}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}
	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_mixed-sqlite", Title: "sqlite one", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("upsertIndexRow: %v", err)
	}
	if err := saveOjsonl(dir, "ses_mixed-ojsonl", "ojsonl one", []agent.Message{{Role: "user", Content: "hey"}}, nil); err != nil {
		t.Fatalf("saveOjsonl: %v", err)
	}

	refs, err := ListRefsForDir(wd)
	if err != nil {
		t.Fatalf("ListRefsForDir: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (1 migrated + 1 legacy), got %d: %+v", len(refs), refs)
	}
	byID := map[string]Ref{}
	for _, r := range refs {
		byID[r.ID] = r
	}
	if byID["ses_mixed-sqlite"].Title != "sqlite one" {
		t.Fatalf("missing/wrong migrated session ref: %+v", refs)
	}
	if byID["ses_mixed-ojsonl"].Title != "ojsonl one" {
		t.Fatalf("missing/wrong legacy session ref: %+v", refs)
	}
}

func TestListRefsPaginatedIncludesMigratedSessions(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_paginated-sqlite", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	refs, total, err := ListRefsPaginated(0, 0)
	if err != nil {
		t.Fatalf("ListRefsPaginated: %v", err)
	}
	if total != 1 || len(refs) != 1 || refs[0].ID != "ses_paginated-sqlite" {
		t.Fatalf("expected the migrated session listed, got total=%d refs=%+v", total, refs)
	}
}

func TestListIncludesSqliteSessionFullContent(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_list-sqlite", "", []agent.Message{{Role: "user", Content: "full content"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].Messages) != 1 || sessions[0].Messages[0].Content != "full content" {
		t.Fatalf("expected sqlite session's full content via List, got %+v", sessions)
	}
}

func TestListSkipsIndexSqliteFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_list-skip-index", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected index.sqlite to be skipped, not treated as a session, got %d sessions: %+v", len(sessions), sessions)
	}
}
```

Add `"time"` to `session_test.go`'s imports if not already present (it
will already be there after Task 4/5's steps if executed in order).

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestListRefsForDirIncludesMigratedAndLegacySessions|TestListRefsPaginatedIncludesMigratedSessions|TestListIncludesSqliteSessionFullContent|TestListSkipsIndexSqliteFile' -v`
Expected: FAIL — migrated sessions don't appear in any listing yet, and
`List()` has no `.sqlite` case so `TestListIncludesSqliteSessionFullContent`
returns zero sessions.

- [x] **Step 3: Implement the listing union**

Add `legacyOcodeMetas` to `internal/session/session.go` (near
`readOcodeMeta`, e.g. directly above `ListRefsForDir`):

```go
// legacyOcodeMetas scans dir for un-migrated .json and .ojsonl session
// files and returns their metadata. Shared by ListRefsForDir and
// ListRefsPaginated, which previously duplicated this scan verbatim.
func legacyOcodeMetas(dir string, entries []os.DirEntry) []ocodeMeta {
	metas := mapDirEntries(dir, entries, ".json", func(path string, e os.DirEntry) (ocodeMeta, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		meta, err := readOcodeMeta(path, info.ModTime())
		if err != nil {
			log.Printf("session list: read meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})
	ojsonlMetas := mapDirEntries(dir, entries, ".ojsonl", func(path string, e os.DirEntry) (ocodeMeta, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		meta, err := readOjsonlListMeta(path, info.ModTime())
		if err != nil {
			log.Printf("session list: read ojsonl meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})
	return append(metas, ojsonlMetas...)
}
```

Replace `ListRefsForDir` (currently `session.go:513-568`):

```go
func ListRefsForDir(wd string) ([]Ref, error) {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	metas := legacyOcodeMetas(dir, entries)
	indexMetas, err := queryIndexMetas(dir)
	if err != nil {
		log.Printf("session list: query index: %v", err)
		indexMetas = nil
	}
	metas = mergeMetas(metas, indexMetas)

	refs := make([]Ref, 0, len(metas))
	for _, meta := range metas {
		refs = append(refs, Ref{
			ID:        meta.ID,
			Title:     meta.Title,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Source:    SourceOcode,
		})
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})

	return refs, nil
}
```

In `ListRefsPaginated` (currently `session.go:737-824`), replace only the
metadata-gathering block — from `metas := mapDirEntries(dir, entries,
".json", ...)` through `metas = append(metas, ojsonlMetas...)` — with:

```go
	metas := legacyOcodeMetas(dir, entries)
	indexMetas, err := queryIndexMetas(dir)
	if err != nil {
		log.Printf("session list: query index: %v", err)
		indexMetas = nil
	}
	metas = mergeMetas(metas, indexMetas)
```

The rest of `ListRefsPaginated` (the `allRefs` build-up, the Claude-clone
dedup, sort, and pagination slicing) is unchanged — it already consumes
`metas` generically via `meta.CloneOf`, which both legacy and indexed
`ocodeMeta` values populate the same way.

Replace `List` (currently `session.go:465-507`):

```go
func List() ([]Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || e.Name() == "index.json" || e.Name() == "index.sqlite" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		ext := filepath.Ext(e.Name())
		id := strings.TrimSuffix(e.Name(), ext)
		switch ext {
		case ".sqlite":
			s, err := readSqliteSession(path)
			if err == nil {
				s.Messages = removeIncompleteToolRequests(s.Messages)
				sessions = append(sessions, *s)
			}
		case ".json":
			// A migrated session's .sqlite is authoritative; a .json left
			// behind by the narrow migrateToSqlite crash window (see Task
			// 4/5) would otherwise show up twice.
			if fileExists(sqliteSessionPath(dir, id)) {
				continue
			}
			data, err := os.ReadFile(path)
			if err == nil {
				var s Session
				if err := json.Unmarshal(data, &s); err == nil {
					s.Messages = removeIncompleteToolRequests(s.Messages)
					sessions = append(sessions, s)
				}
			}
		case ".ojsonl":
			if fileExists(sqliteSessionPath(dir, id)) {
				continue
			}
			s, err := loadOjsonlSession(path)
			if err == nil {
				s.Messages = removeIncompleteToolRequests(s.Messages)
				sessions = append(sessions, *s)
			}
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}
```

`strings` is already imported in `session.go` (used elsewhere, e.g.
`sessionCandidateIDs`).

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestListRefsForDirIncludesMigratedAndLegacySessions|TestListRefsPaginatedIncludesMigratedSessions|TestListIncludesSqliteSessionFullContent|TestListSkipsIndexSqliteFile' -v`
Expected: PASS (all 4 tests).

- [x] **Step 5: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures. In particular
`TestListRemovesIncompleteToolRequests` and
`TestListAllIncludesOnlyCurrentDirClaudeSessions` (pre-existing) must
still pass — they exercise `List`/`ListAll` against `.ojsonl`-only
fixtures with no `.sqlite` file present, so the new `.sqlite` branch and
sibling-check are both no-ops for them.

- [x] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat: listing unions migrated (indexed) and legacy (scanned) sessions"
```
