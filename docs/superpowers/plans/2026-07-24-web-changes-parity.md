# Web Changes Tab Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the existing `internal/changes.Registry` over a new `/api/changes` REST surface and render it as a new **Changes** tab in the web app (file list, unified diff, whole-file/block undo), bringing web/desktop to parity with the TUI's changes tab.

**Architecture:** The backend package (`internal/changes`) and its TUI integration are already fully built — this plan only adds four HTTP handlers that wrap the existing `Registry` methods (mirroring the `handler_runs.go` pattern: `Handler.activeAgentForRuns(sessionID)` resolves the live `*agent.Agent`, then `.Changes()` gets the registry), plus a new web panel that follows `GitPanel.tsx`'s two-pane list+diff layout.

**Tech Stack:** Go (`net/http`, `encoding/json`), React + TypeScript, Tailwind, Radix UI (`Dialog`, `Tooltip` — already in the codebase, no new dependencies).

## Global Constraints

- No test framework exists in `web/` (no vitest/jest/testing-library, no `*.test.tsx` files anywhere in the repo) — frontend tasks are verified via `npx tsc --noEmit` and `npm run build` only. Do not write fabricated component tests.
- `internal/changes` itself is not modified by this plan — its API was already shaped for HTTP exposure. Only new files in `internal/server` and `web/src`.
- Backend errors from `UndoFile`/`UndoBlock`/`LatestToolCall` are wrapped via `errors.Join` — always check with `errors.Is(err, changes.ErrNotUndoable)` / `errors.Is(err, changes.ErrNoChanges)`, never `==`.
- The new per-file undo routes (`/api/changes/undo-file`, `/api/changes/undo-block`) are a distinct mechanism from the pre-existing global `/api/files/undo`/`/api/files/redo` (`snapshot.Undo()`/`Redo()`). Do not merge or alias them.
- `FileChange` (Go struct) has no JSON tags — a DTO with explicit `camelCase` tags is required, matching the `agentRunDTO` pattern in `handler_runs.go`. `FileStatus.String()` returns TUI glyphs (`"+"`/`"M"`/`"-"`) and must NOT be reused for the web JSON status field, which uses the words `"added"`/`"modified"`/`"deleted"`.
- New Changes tab is inserted between `files` and `git` in `TopTabs.tsx`'s `mainTabs` array (current order: `chat, files, git, status, logs, cron, assets`).

---

## Task 1: `fileChangeDTO` + `GET /api/changes` handler

**Files:**
- Create: `internal/server/handler_changes.go`
- Test: `internal/server/handler_changes_test.go`

**Interfaces:**
- Consumes: `changes.Registry.List() []changes.FileChange` (existing, `internal/changes/changes.go:183`), `changes.FileChange` fields (`OriginalPath, Status, FirstBackupPath, Undoable, UndoAllTCID, ChangeCount, Authors, CreatedAt, UpdatedAt, LastBashCommand, LastBashExitCode`), `changes.ChangeAuthor{AgentID, AgentName, Changes}`, `changes.FileStatus` (`FileAdded=0, FileModified=1, FileDeleted=2`), `Handler.activeAgentForRuns(sessionID string) *agent.Agent` (`internal/server/handler_runs.go:93`), `agent.Agent.Changes() *changes.Registry` (`internal/agent/agent.go:333`).
- Produces: `type fileChangeDTO struct{...}` with JSON tags, `func buildFileChangeDTO(fc changes.FileChange) fileChangeDTO`, `func statusName(s changes.FileStatus) string`, `func (h *Handler) changesSnapshot(sessionID string) []fileChangeDTO`, `func (h *Handler) HandleListChanges(w http.ResponseWriter, r *http.Request)`. Later tasks call `h.changesSnapshot` and the DTO type.

- [ ] **Step 1: Write the failing test**

```go
// internal/server/handler_changes_test.go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/snapshot"
)

// newChangesHandler builds a Handler with one session whose agent has a
// registry containing one modified file, backed by a real snapshot.Store
// so RenderDiff/UndoFile/UndoBlock have real backup bytes to work with.
func newChangesHandler(t *testing.T) (*Handler, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(filePath, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	if err := store.Backup(filePath, "tc-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := agent.NewAgent(nil, nil, nil, nil)
	if err := a.Changes().AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	const sessionID = "sess-1"
	h.agents[sessionID] = &agentSession{agent: a}
	return h, sessionID, filePath
}

func TestHandleListChangesReturnsFile(t *testing.T) {
	h, sessionID, filePath := newChangesHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/changes?session="+sessionID, nil)
	h.HandleListChanges(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []fileChangeDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].OriginalPath != filePath {
		t.Errorf("path = %q, want %q", got[0].OriginalPath, filePath)
	}
	if got[0].Status != "modified" {
		t.Errorf("status = %q, want %q", got[0].Status, "modified")
	}
	if !got[0].Undoable {
		t.Error("expected Undoable=true for a snapshot-tracked change")
	}
	if len(got[0].Authors) != 1 || got[0].Authors[0].AgentID != "main" {
		t.Errorf("unexpected authors: %+v", got[0].Authors)
	}
}

func TestHandleListChangesEmptyWhenNoSession(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/changes", nil)
	h.HandleListChanges(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("expected empty array for unknown session, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleListChanges -v`
Expected: FAIL — `h.HandleListChanges undefined`, `fileChangeDTO undefined`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/server/handler_changes.go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/changes"
)

// changeAuthorDTO mirrors changes.ChangeAuthor for the web.
type changeAuthorDTO struct {
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	Changes   int    `json:"changes"`
}

// fileChangeDTO mirrors changes.FileChange for the web "changes" tab. Status
// uses word labels ("added"/"modified"/"deleted"), NOT changes.FileStatus's
// glyph-based String() ("+"/"M"/"-"), which is TUI-only.
type fileChangeDTO struct {
	OriginalPath     string            `json:"originalPath"`
	Status           string            `json:"status"`
	FirstBackupPath  string            `json:"firstBackupPath"`
	Undoable         bool              `json:"undoable"`
	UndoAllTCID      string            `json:"undoAllTcId"`
	ChangeCount      int               `json:"changeCount"`
	Authors          []changeAuthorDTO `json:"authors"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
	LastBashCommand  string            `json:"lastBashCommand"`
	LastBashExitCode int               `json:"lastBashExitCode"`
}

// statusName maps changes.FileStatus to the web's word-label contract.
func statusName(s changes.FileStatus) string {
	switch s {
	case changes.FileAdded:
		return "added"
	case changes.FileModified:
		return "modified"
	case changes.FileDeleted:
		return "deleted"
	default:
		return "modified"
	}
}

func buildFileChangeDTO(fc changes.FileChange) fileChangeDTO {
	authors := make([]changeAuthorDTO, 0, len(fc.Authors))
	for _, a := range fc.Authors {
		authors = append(authors, changeAuthorDTO{
			AgentID:   a.AgentID,
			AgentName: a.AgentName,
			Changes:   a.Changes,
		})
	}
	return fileChangeDTO{
		OriginalPath:     fc.OriginalPath,
		Status:           statusName(fc.Status),
		FirstBackupPath:  fc.FirstBackupPath,
		Undoable:         fc.Undoable,
		UndoAllTCID:      fc.UndoAllTCID,
		ChangeCount:      fc.ChangeCount,
		Authors:          authors,
		CreatedAt:        fc.CreatedAt.Format(timeFormatRFC3339),
		UpdatedAt:        fc.UpdatedAt.Format(timeFormatRFC3339),
		LastBashCommand:  fc.LastBashCommand,
		LastBashExitCode: fc.LastBashExitCode,
	}
}

const timeFormatRFC3339 = "2006-01-02T15:04:05.999999999Z07:00"

// changesSnapshot builds the DTO list for the active session's registry.
// Returns an empty slice (never nil) when no agent/registry is active — the
// same "legitimate empty state" contract runsSnapshot uses.
func (h *Handler) changesSnapshot(sessionID string) []fileChangeDTO {
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		return []fileChangeDTO{}
	}
	list := ag.Changes().List()
	out := make([]fileChangeDTO, 0, len(list))
	for _, fc := range list {
		out = append(out, buildFileChangeDTO(fc))
	}
	return out
}

// HandleListChanges returns the current session's file-change list as JSON.
func (h *Handler) HandleListChanges(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.changesSnapshot(r.URL.Query().Get("session")))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/... -run TestHandleListChanges -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handler_changes.go internal/server/handler_changes_test.go
git commit -m "feat(server): add GET /api/changes handler wrapping internal/changes.Registry"
```

---

## Task 2: `GET /api/changes/diff` handler

**Files:**
- Modify: `internal/server/handler_changes.go`
- Modify: `internal/server/handler_changes_test.go`

**Interfaces:**
- Consumes: `changes.RenderDiff(backupPath, currentPath string) (string, error)` (`internal/changes/diff.go:20`), `h.changesSnapshot` (Task 1), `fileChangeDTO.OriginalPath`/`FirstBackupPath`.
- Produces: `type changeDiffDTO struct{ Path, Patch string }`, `func (h *Handler) HandleChangesDiff(w http.ResponseWriter, r *http.Request)`.

- [ ] **Step 1: Write the failing test**

```go
func TestHandleChangesDiffReturnsPatch(t *testing.T) {
	h, sessionID, filePath := newChangesHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/changes/diff?session="+sessionID+"&path="+filePath, nil)
	h.HandleChangesDiff(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
	var got changeDiffDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Path != filePath {
		t.Errorf("path = %q, want %q", got.Path, filePath)
	}
	if !strings.Contains(got.Patch, "-original") || !strings.Contains(got.Patch, "+changed") {
		t.Errorf("patch missing expected diff lines: %q", got.Patch)
	}
}

func TestHandleChangesDiffUnknownPath(t *testing.T) {
	h, sessionID, _ := newChangesHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/changes/diff?session="+sessionID+"&path=/nope", nil)
	h.HandleChangesDiff(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleChangesDiff -v`
Expected: FAIL — `h.HandleChangesDiff undefined`, `changeDiffDTO undefined`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/server/handler_changes.go`:

```go
type changeDiffDTO struct {
	Path  string `json:"path"`
	Patch string `json:"patch"`
}

// HandleChangesDiff returns the unified diff for one file in the session's
// change list. 404 if path isn't currently in the list (stale client row).
func (h *Handler) HandleChangesDiff(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	path := r.URL.Query().Get("path")

	list := h.changesSnapshot(sessionID)
	var found *fileChangeDTO
	for i := range list {
		if list[i].OriginalPath == path {
			found = &list[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "path not found in changes list")
		return
	}

	patch, err := changes.RenderDiff(found.FirstBackupPath, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render diff: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changeDiffDTO{Path: path, Patch: patch})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/... -run TestHandleChangesDiff -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handler_changes.go internal/server/handler_changes_test.go
git commit -m "feat(server): add GET /api/changes/diff handler"
```

---

## Task 3: `POST /api/changes/undo-file` and `POST /api/changes/undo-block` handlers

**Files:**
- Modify: `internal/server/handler_changes.go`
- Modify: `internal/server/handler_changes_test.go`

**Interfaces:**
- Consumes: `changes.Registry.UndoFile(path string) error`, `UndoBlock(path, toolCallID string) error`, `LatestToolCall(path string) (string, error)` (`internal/changes/undo.go`), `changes.ErrNotUndoable`, `changes.ErrNoChanges` (sentinel errors), `readBodyJSON(r *http.Request, v interface{}) error` (`internal/server/server.go:506`).
- Produces: `func (h *Handler) HandleUndoChangeFile(w http.ResponseWriter, r *http.Request)`, `func (h *Handler) HandleUndoChangeBlock(w http.ResponseWriter, r *http.Request)`.

- [ ] **Step 1: Write the failing test**

```go
func TestHandleUndoChangeFileRestoresContent(t *testing.T) {
	h, sessionID, filePath := newChangesHandler(t)

	body := strings.NewReader(`{"path":"` + filePath + `"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/changes/undo-file?session="+sessionID, body)
	h.HandleUndoChangeFile(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Errorf("file content = %q, want %q", data, "original\n")
	}
}

func TestHandleUndoChangeFileUnknownPath(t *testing.T) {
	h, sessionID, _ := newChangesHandler(t)

	body := strings.NewReader(`{"path":"/nope"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/changes/undo-file?session="+sessionID, body)
	h.HandleUndoChangeFile(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["error"] != "no_changes" {
		t.Errorf("error = %q, want %q", got["error"], "no_changes")
	}
}

func TestHandleUndoChangeBlockUndoesLatest(t *testing.T) {
	h, sessionID, filePath := newChangesHandler(t)

	body := strings.NewReader(`{"path":"` + filePath + `"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/changes/undo-block?session="+sessionID, body)
	h.HandleUndoChangeBlock(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Errorf("file content = %q, want %q", data, "original\n")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleUndoChange -v`
Expected: FAIL — `h.HandleUndoChangeFile undefined`, `h.HandleUndoChangeBlock undefined`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/server/handler_changes.go`:

```go
type undoChangeRequest struct {
	Path string `json:"path"`
}

// writeUndoError maps a changes.Registry undo error to an HTTP response.
// Errors from UndoFile/UndoBlock/LatestToolCall are wrapped via
// errors.Join, so callers MUST use errors.Is, never ==.
func writeUndoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, changes.ErrNotUndoable):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_undoable"})
	case errors.Is(err, changes.ErrNoChanges):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_changes"})
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// HandleUndoChangeFile restores a file to its pre-session state.
func (h *Handler) HandleUndoChangeFile(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	var req undoChangeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ag.Changes().UndoFile(req.Path); err != nil {
		writeUndoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// HandleUndoChangeBlock undoes the most recent tool call on a file.
func (h *Handler) HandleUndoChangeBlock(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	var req undoChangeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tcid, err := ag.Changes().LatestToolCall(req.Path)
	if err != nil {
		writeUndoError(w, err)
		return
	}
	if err := ag.Changes().UndoBlock(req.Path, tcid); err != nil {
		writeUndoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}
```

Add `"errors"` to the import block in `internal/server/handler_changes.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/... -run TestHandleUndoChange -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/server/...`
Expected: PASS (all pre-existing tests plus the new ones)

- [ ] **Step 6: Commit**

```bash
git add internal/server/handler_changes.go internal/server/handler_changes_test.go
git commit -m "feat(server): add POST /api/changes/undo-file and undo-block handlers"
```

---

## Task 4: Register routes in `server.go`

**Files:**
- Modify: `internal/server/server.go:106` (insert after the `/api/agents/runs` registration)

**Interfaces:**
- Consumes: `s.handler.HandleListChanges`, `HandleChangesDiff`, `HandleUndoChangeFile`, `HandleUndoChangeBlock` (Tasks 1–3), the existing `s.authMiddleware` wrapper and `s.mux.HandleFunc` pattern used by every other route in this function.
- Produces: four live HTTP routes: `GET /api/changes`, `GET /api/changes/diff`, `POST /api/changes/undo-file`, `POST /api/changes/undo-block`.

- [ ] **Step 1: Add the shim methods**

In `internal/server/server.go`, near the other handler shims (e.g. next to `handleGitStatus`/`handleGitDiff` around line 304):

```go
func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListChanges(w, r)
}

func (s *Server) handleChangesDiff(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChangesDiff(w, r)
}

func (s *Server) handleUndoChangeFile(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUndoChangeFile(w, r)
}

func (s *Server) handleUndoChangeBlock(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUndoChangeBlock(w, r)
}
```

- [ ] **Step 2: Register the routes**

In `internal/server/server.go`, right after line 106 (`s.mux.HandleFunc("GET /api/agents/runs", ...)`):

```go
	s.mux.HandleFunc("GET /api/changes", s.authMiddleware(s.handleListChanges))
	s.mux.HandleFunc("GET /api/changes/diff", s.authMiddleware(s.handleChangesDiff))
	s.mux.HandleFunc("POST /api/changes/undo-file", s.authMiddleware(s.handleUndoChangeFile))
	s.mux.HandleFunc("POST /api/changes/undo-block", s.authMiddleware(s.handleUndoChangeBlock))
```

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Run the full server test suite**

Run: `go test ./internal/server/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): wire /api/changes routes into the route table"
```

---

## Task 5: `types.ts` and `client.ts` additions

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: none (pure additive types + fetch wrappers), matches the JSON shape produced by Tasks 1–3 (`fileChangeDTO`, `changeDiffDTO`).
- Produces: `FileChangeStatus`, `ChangeAuthor`, `FileChange`, `ChangeDiff` types; `api.listChanges(session)`, `api.getChangeDiff(session, path)`, `api.undoChangeFile(session, path)`, `api.undoChangeBlock(session, path)`. Consumed by Task 6+ components.

- [ ] **Step 1: Add types**

In `web/src/api/types.ts`, add (near the existing `GitDiffFile` type):

```ts
export type FileChangeStatus = "added" | "modified" | "deleted";

export interface ChangeAuthor {
  agentId: string;
  agentName: string;
  changes: number;
}

export interface FileChange {
  originalPath: string;
  status: FileChangeStatus;
  firstBackupPath: string;
  undoable: boolean;
  undoAllTcId: string;
  changeCount: number;
  authors: ChangeAuthor[];
  createdAt: string;
  updatedAt: string;
  lastBashCommand: string;
  lastBashExitCode: number;
}

export interface ChangeDiff {
  path: string;
  patch: string;
}
```

- [ ] **Step 2: Add client methods**

In `web/src/api/client.ts`, add `FileChange` and `ChangeDiff` to the type-only import block at the top (alongside `GitDiffFile`), then add to the `api` object, near `getGitDiff`:

```ts
  listChanges: (session?: string) =>
    fetchJSON<FileChange[]>(
      `/api/changes${session ? `?session=${encodeURIComponent(session)}` : ""}`,
    ),
  getChangeDiff: (session: string | undefined, path: string) =>
    fetchJSON<ChangeDiff>(
      `/api/changes/diff?${session ? `session=${encodeURIComponent(session)}&` : ""}path=${encodeURIComponent(path)}`,
    ),
  undoChangeFile: (session: string | undefined, path: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-file${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
    ),
  undoChangeBlock: (session: string | undefined, path: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-block${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
    ),
```

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat(web): add changes API types and client methods"
```

---

## Task 6: `ChangesFileList.tsx` and `ChangesDiffView.tsx`

**Files:**
- Create: `web/src/components/Changes/ChangesFileList.tsx`
- Create: `web/src/components/Changes/ChangesDiffView.tsx`

**Interfaces:**
- Consumes: `FileChange`, `ChangeDiff` types (Task 5), `api.getChangeDiff` (Task 5), `Tooltip`/`TooltipTrigger`/`TooltipContent`/`TooltipProvider` from `@/components/ui/tooltip`.
- Produces: `export default function ChangesFileList(props: { files: FileChange[]; selectedPath: string | null; onSelect: (path: string) => void; onUndoFile: (path: string) => void; onUndoBlock: (path: string) => void })`, `export default function ChangesDiffView(props: { session?: string; path: string })`.

- [ ] **Step 1: Write `ChangesFileList.tsx`**

```tsx
import { useState } from "react";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@/components/ui/tooltip";
import type { FileChange } from "@/api/types";

const STATUS_BADGES: Record<string, { label: string; color: string }> = {
  added: { label: "+", color: "bg-green-500/20 text-green-400" },
  modified: { label: "M", color: "bg-yellow-500/20 text-yellow-400" },
  deleted: { label: "-", color: "bg-red-500/20 text-red-400" },
};

interface Props {
  files: FileChange[];
  selectedPath: string | null;
  onSelect: (path: string) => void;
  onUndoFile: (path: string) => void;
  onUndoBlock: (path: string) => void;
}

export default function ChangesFileList({ files, selectedPath, onSelect, onUndoFile, onUndoBlock }: Props) {
  const [expanded, setExpanded] = useState<string | null>(null);

  if (files.length === 0) {
    return <div className="p-3 text-xs text-zinc-600">No changes in this session yet.</div>;
  }

  return (
    <TooltipProvider>
      <div className="divide-y divide-zinc-800">
        {files.map((file) => {
          const badge = STATUS_BADGES[file.status] || STATUS_BADGES.modified;
          const isSelected = selectedPath === file.originalPath;
          const isExpanded = expanded === file.originalPath;
          const authorSummary = file.authors
            .map((a) => `${a.agentName} · ${a.changes}`)
            .join(", ");
          return (
            <div key={file.originalPath} className={isSelected ? "bg-zinc-800" : ""}>
              <button
                onClick={() => onSelect(file.originalPath)}
                className="w-full text-left px-3 py-1.5 flex items-center gap-2 text-sm hover:bg-zinc-800/50"
              >
                <span className={`inline-flex items-center justify-center w-5 h-5 rounded text-[10px] font-bold ${badge.color}`}>
                  {badge.label}
                </span>
                <span className="font-mono text-zinc-300 truncate flex-1">{file.originalPath}</span>
                {!file.undoable && (
                  <span className="text-[10px] text-zinc-500 shrink-0">(bash) ⚠</span>
                )}
              </button>
              <div className="px-3 pb-1.5 flex items-center gap-3 text-[11px] text-zinc-500">
                <span>{authorSummary}</span>
                <button
                  onClick={() => setExpanded(isExpanded ? null : file.originalPath)}
                  className="hover:text-zinc-300"
                >
                  {isExpanded ? "hide details" : "details"}
                </button>
                <div className="flex-1" />
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <button
                          disabled={!file.undoable}
                          onClick={() => onUndoBlock(file.originalPath)}
                          className="hover:text-zinc-300 disabled:opacity-40 disabled:hover:text-zinc-500"
                        >
                          undo last
                        </button>
                      </span>
                    </TooltipTrigger>
                    {!file.undoable && (
                      <TooltipContent>bash-only change — no backup to restore</TooltipContent>
                    )}
                  </Tooltip>
                </TooltipProvider>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <button
                          disabled={!file.undoable}
                          onClick={() => onUndoFile(file.originalPath)}
                          className="hover:text-zinc-300 disabled:opacity-40 disabled:hover:text-zinc-500"
                        >
                          undo file
                        </button>
                      </span>
                    </TooltipTrigger>
                    {!file.undoable && (
                      <TooltipContent>bash-only change — no backup to restore</TooltipContent>
                    )}
                  </Tooltip>
                </TooltipProvider>
              </div>
              {isExpanded && (
                <div className="px-3 pb-2 text-[11px] text-zinc-500 space-y-0.5">
                  <div>{file.changeCount} tool call(s)</div>
                  {file.lastBashCommand && <div className="font-mono">$ {file.lastBashCommand}</div>}
                  <div>created {file.createdAt}</div>
                  <div>updated {file.updatedAt}</div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </TooltipProvider>
  );
}
```

- [ ] **Step 2: Write `ChangesDiffView.tsx`**

```tsx
import { useEffect, useState } from "react";
import { api } from "@/api/client";

interface Props {
  session?: string;
  path: string;
}

export default function ChangesDiffView({ session, path }: Props) {
  const [patch, setPatch] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setPatch(null);
    setError(null);
    api
      .getChangeDiff(session, path)
      .then((res) => {
        if (!cancelled) setPatch(res.patch);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load diff");
      });
    return () => {
      cancelled = true;
    };
  }, [session, path]);

  if (error) return <div className="p-2 text-xs text-red-400">{error}</div>;
  if (patch === null) return <div className="p-2 text-xs text-zinc-500">Loading diff…</div>;

  return (
    <div className="p-2">
      <div className="text-xs text-zinc-500 mb-2 font-mono">{path}</div>
      <pre className="text-xs font-mono whitespace-pre-wrap">
        {patch.split("\n").map((line, i) => {
          let color = "text-zinc-400";
          if (line.startsWith("+") && !line.startsWith("+++")) color = "text-green-400";
          else if (line.startsWith("-") && !line.startsWith("---")) color = "text-red-400";
          else if (line.startsWith("@@")) color = "text-blue-400";
          return (
            <div key={i} className={color}>
              {line}
            </div>
          );
        })}
      </pre>
    </div>
  );
}
```

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors (both files are unused by any importer yet, but must still typecheck standalone)

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Changes/ChangesFileList.tsx web/src/components/Changes/ChangesDiffView.tsx
git commit -m "feat(web): add ChangesFileList and ChangesDiffView components"
```

---

## Task 7: `ChangesPanel.tsx` with undo confirm dialog

**Files:**
- Create: `web/src/components/Changes/ChangesPanel.tsx`

**Interfaces:**
- Consumes: `ChangesFileList` (Task 6), `ChangesDiffView` (Task 6), `api.listChanges`, `api.undoChangeFile`, `api.undoChangeBlock` (Task 5), `FileChange` type (Task 5), `Dialog`/`DialogContent`/`DialogHeader`/`DialogTitle`/`DialogFooter` from `@/components/ui/dialog`, `Button` from `@/components/ui/button`.
- Produces: `export default function ChangesPanel(props: { session?: string })`. Consumed by Task 8 (`App.tsx`, `SessionPage.tsx`, `TopTabs.tsx`).

- [ ] **Step 1: Write `ChangesPanel.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { FileChange } from "@/api/types";
import ChangesFileList from "./ChangesFileList";
import ChangesDiffView from "./ChangesDiffView";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

const REFRESH_INTERVAL = 10_000;

interface Props {
  session?: string;
}

type PendingUndo = { path: string; kind: "file" | "block" } | null;

export default function ChangesPanel({ session }: Props) {
  const [files, setFiles] = useState<FileChange[]>([]);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingUndo, setPendingUndo] = useState<PendingUndo>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await api.listChanges(session);
      setFiles(res);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load changes");
    } finally {
      setLoading(false);
    }
  }, [session]);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [refresh]);

  const confirmUndo = useCallback(async () => {
    if (!pendingUndo) return;
    try {
      if (pendingUndo.kind === "file") {
        await api.undoChangeFile(session, pendingUndo.path);
      } else {
        await api.undoChangeBlock(session, pendingUndo.path);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Undo failed");
    } finally {
      setPendingUndo(null);
    }
  }, [pendingUndo, session, refresh]);

  if (loading && files.length === 0) {
    return <div className="p-3 text-xs text-zinc-500">Loading changes…</div>;
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-3 border-b border-zinc-700">
        <label className="text-xs text-zinc-500 uppercase tracking-wider">Changes</label>
        {error && <div className="mt-1 text-xs text-red-400">{error}</div>}
      </div>
      <div className="flex-1 overflow-y-auto">
        <ChangesFileList
          files={files}
          selectedPath={selectedPath}
          onSelect={(path) => setSelectedPath(path === selectedPath ? null : path)}
          onUndoFile={(path) => setPendingUndo({ path, kind: "file" })}
          onUndoBlock={(path) => setPendingUndo({ path, kind: "block" })}
        />
      </div>
      {selectedPath && (
        <div className="border-t border-zinc-700 max-h-[40vh] overflow-y-auto">
          <ChangesDiffView session={session} path={selectedPath} />
        </div>
      )}
      <Dialog open={pendingUndo !== null} onOpenChange={(open) => !open && setPendingUndo(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {pendingUndo?.kind === "file"
                ? `Undo ${pendingUndo.path} to pre-session state?`
                : `Undo the most recent change to ${pendingUndo?.path}?`}
            </DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingUndo(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmUndo}>
              Undo
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors. If `Button`'s `variant` prop doesn't include `"destructive"` in this codebase's `button.tsx`, check `web/src/components/ui/button.tsx`'s `variant` union first and substitute the closest existing destructive-styled variant.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Changes/ChangesPanel.tsx
git commit -m "feat(web): add ChangesPanel with undo confirm dialog"
```

---

## Task 8: Wire the Changes tab into `TopTabs.tsx`, `App.tsx`, `SessionPage.tsx`

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/SessionPage.tsx`

**Interfaces:**
- Consumes: `ChangesPanel` (Task 7), the existing `mainTabs` array structure in `TopTabs.tsx`, `currentSessionId` (from `useChatState()` in `App.tsx`) and `state.sessionId` (in `SessionPage.tsx`) as the `session` prop value.
- Produces: a working `changes` tab reachable in both the desktop-embedded and hosted web app.

- [ ] **Step 1: Add the tab entry in `TopTabs.tsx`**

In `web/src/components/Layout/TopTabs.tsx`, the current `mainTabs` array (lines 15–22) is:

```ts
const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
```

Add a `History` icon import from `lucide-react` (check the existing import line for the icon list and append `History`), then insert a new entry between `files` and `git`:

```ts
const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "changes", label: "Changes", icon: History },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
```

- [ ] **Step 2: Render the panel in `App.tsx`**

In `web/src/App.tsx`, add the import next to the existing `GitPanel` import (line 14):

```ts
import ChangesPanel from "./components/Changes/ChangesPanel";
```

Then, next to the existing `{activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}` line (line 298), insert directly before it:

```tsx
            {activeTab === "changes" && <ChangesPanel session={currentSessionId ?? undefined} />}
```

- [ ] **Step 3: Render the panel in `SessionPage.tsx`**

In `web/src/pages/SessionPage.tsx`, add the import next to the existing `GitPanel` import (line 18):

```ts
import ChangesPanel from "../components/Changes/ChangesPanel";
```

Then, next to the existing `{activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}` line (line 425), insert directly before it:

```tsx
          {activeTab === "changes" && <ChangesPanel session={state.sessionId ?? undefined} />}
```

- [ ] **Step 4: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Build**

Run: `cd web && npm run build`
Expected: build succeeds

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx web/src/App.tsx web/src/pages/SessionPage.tsx
git commit -m "feat(web): wire Changes tab into TopTabs, App, and SessionPage"
```

---

## Task 9: Full verification pass

**Files:** none (verification only)

**Interfaces:**
- Consumes: everything built in Tasks 1–8.
- Produces: a verified, working Changes tab across backend and frontend.

- [ ] **Step 1: Full Go test suite**

Run: `go test ./internal/server/... ./internal/changes/... ./internal/agent/...`
Expected: PASS

- [ ] **Step 2: Full Go build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Full web typecheck and build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: no errors, build succeeds

- [ ] **Step 4: Manual smoke test**

Start the server (`go run ./cmd/ocode --headless` or the project's existing dev-run command — check `AGENTS.md` if unsure), open the web UI, run a session that edits a file via chat, open the **Changes** tab, confirm the file row appears with the correct status icon, click it to see the diff, click "undo file", confirm in the dialog, and verify the file reverts on disk.

- [ ] **Step 5: Commit any fixes found during smoke test**

If the manual smoke test surfaces a bug, fix it, re-run the affected test suite, and commit with a message describing the fix — do not silently patch without a commit.

---

## Self-Review Notes

- **Spec coverage:** §1 (user-facing summary) → Tasks 6–8 (list, diff, undo, empty state via `ChangesFileList`'s empty branch). §2 decisions → confirm dialog (Task 7), no-agent empty state (Task 1's `changesSnapshot` nil-agent branch, same contract as `runsSnapshot`), session scoping via `activeAgentForRuns` (Tasks 1–3), diff renderer reuse (Task 6), tab position (Task 8). §3 backend → Tasks 1–4. §4 web components → Tasks 5–7. §5 data flow (lazy diff fetch, refetch-not-optimistic) → Task 7's `confirmUndo`. §6 testing → Task 1–3 Go tests, Task 9 manual smoke, frontend build-only verification per the Global Constraints. §7 out of scope → no tasks add renames/watcher/cross-session-diff/per-hunk-undo/SSE, consistent with omission.
- **Placeholder scan:** no TBD/TODO, every step has real code or an exact command with expected output.
- **Type consistency:** `FileChange`/`ChangeAuthor`/`ChangeDiff` field names introduced in Task 5 are used identically in Tasks 6–7 (`file.originalPath`, `file.status`, `file.undoable`, `file.authors`, `res.patch`). `api.listChanges`/`getChangeDiff`/`undoChangeFile`/`undoChangeBlock` names match between Task 5's definitions and Task 7's usage. Backend DTO JSON tags (Task 1) match the TS field names (Task 5) exactly.
