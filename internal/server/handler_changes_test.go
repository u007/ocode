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
