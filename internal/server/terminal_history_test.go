//go:build !windows

package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestTerminalHistoryReopensAfterCloseAndReadsRanges(t *testing.T) {
	project := t.TempDir()
	id := "history-reopen-" + filepath.Base(project)
	history := newTerminalHistory(project, id)
	if history.file == nil {
		t.Fatal("history file was not opened")
	}
	t.Cleanup(history.remove)
	history.write([]byte("0123456789"))
	history.close()

	data, end, err := readTerminalHistoryRange(project, id, 2, 4)
	if err != nil {
		t.Fatalf("read reopened history: %v", err)
	}
	if string(data) != "2345" || end != 10 {
		t.Fatalf("reopened range = %q, end=%d; want %q, 10", data, end, "2345")
	}
	data, end, err = readTerminalHistoryRange(project, id, 10, 4)
	if err != nil {
		t.Fatalf("read exact-end history: %v", err)
	}
	if len(data) != 0 || end != 10 {
		t.Fatalf("exact-end range = %q, end=%d; want empty, 10", data, end)
	}
	if _, _, err := readTerminalHistoryRange(project, id, 11, 1); err != errTerminalHistoryPastEnd {
		t.Fatalf("past-end error = %v, want %v", err, errTerminalHistoryPastEnd)
	}
}

func TestTerminalHistoryHandlerPagesBase64AndPinsSnapshot(t *testing.T) {
	project := t.TempDir()
	id := "history-handler-" + filepath.Base(project)
	history := newTerminalHistory(project, id)
	t.Cleanup(history.remove)
	history.write([]byte("hello world"))
	history.close()

	h := NewHandler()
	h.workDir = project
	h.SetTerminalAccessPolicy(false, true)
	r := httptest.NewRequest(http.MethodGet, "/api/terminal/"+id+"/history?project="+project+"&offset=0&limit=5", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.HandleTerminalHistory(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("history status = %d, body=%s", w.Code, w.Body.String())
	}
	var first terminalHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if first.Offset != 0 || first.NextOffset != 5 || first.SnapshotEnd != 11 || first.EOF {
		t.Fatalf("first page metadata = %+v", first)
	}
	decoded, err := base64.StdEncoding.DecodeString(first.Data)
	if err != nil || string(decoded) != "hello" {
		t.Fatalf("first page data = %q, err=%v", decoded, err)
	}
	extra := newTerminalHistory(project, id)
	t.Cleanup(extra.remove)
	extra.write([]byte("!"))
	extra.close()

	pinned := first.SnapshotEnd
	r = httptest.NewRequest(http.MethodGet, "/api/terminal/"+id+"/history?project="+project+"&offset=5&limit=64&snapshot_end=11", nil)
	r.SetPathValue("id", id)
	w = httptest.NewRecorder()
	h.HandleTerminalHistory(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("pinned page status = %d, body=%s", w.Code, w.Body.String())
	}
	var last terminalHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &last); err != nil {
		t.Fatalf("decode last page: %v", err)
	}
	if last.SnapshotEnd != pinned || last.NextOffset != pinned || !last.EOF || last.State != "exited" {
		t.Fatalf("last page metadata = %+v", last)
	}
	decoded, err = base64.StdEncoding.DecodeString(last.Data)
	if err != nil || string(decoded) != " world" {
		t.Fatalf("last page data = %q, err=%v", decoded, err)
	}
}

func TestTerminalHistoryHandlerProjectAuthAndActiveOwnership(t *testing.T) {
	project := t.TempDir()
	other := t.TempDir()
	id := "history-auth-" + filepath.Base(project)
	history := newTerminalHistory(project, id)
	t.Cleanup(history.remove)
	history.write([]byte("private"))
	history.close()

	h := NewHandler()
	h.workDir = project
	h.SetTerminalAccessPolicy(false, true)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.HandleTerminalHistory(w, r)
		return w
	}
	if got := request("/api/terminal/" + id + "/history?project=" + other).Code; got != http.StatusForbidden {
		t.Fatalf("unregistered project status = %d, want %d", got, http.StatusForbidden)
	}
	if got := request("/api/terminal/" + id + "/history?project=" + project + "&offset=-1").Code; got != http.StatusBadRequest {
		t.Fatalf("negative offset status = %d, want %d", got, http.StatusBadRequest)
	}

	live := &terminalSession{id: id, project: other, resumable: true}
	h.terminalSessions.put(id, live)
	t.Cleanup(func() { h.terminalSessions.remove(id, live) })
	if got := request("/api/terminal/" + id + "/history?project=" + project).Code; got != http.StatusConflict {
		t.Fatalf("active cross-project status = %d, want %d", got, http.StatusConflict)
	}
}

func TestTerminalHistoryRouteRequiresServerAuth(t *testing.T) {
	project := t.TempDir()
	id := "history-route-auth-" + filepath.Base(project)
	history := newTerminalHistory(project, id)
	t.Cleanup(history.remove)
	history.write([]byte("ok"))
	history.close()

	s := New("127.0.0.1:4096", "user", "secret", nil)
	s.handler.workDir = project
	path := "/api/terminal/" + id + "/history?project=" + project
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	r = httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated history status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestTerminalHistoryRejectsUnconfiguredPublicAccess(t *testing.T) {
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, false)
	r := httptest.NewRequest(http.MethodGet, "/api/terminal/t1/history?project="+h.workDir, nil)
	r.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.HandleTerminalHistory(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unconfigured public history status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestTerminalHistoryPathDoesNotUseTerminalIDAsPath(t *testing.T) {
	project := t.TempDir()
	path, err := terminalHistoryPath(project, "../history/with spaces")
	if err != nil {
		t.Fatalf("terminalHistoryPath: %v", err)
	}
	if filepath.Base(path) == ".." || filepath.Base(path) == "." || filepath.Base(path) != filepath.Clean(filepath.Base(path)) {
		t.Fatalf("unsafe history path = %q", path)
	}
	other, err := terminalHistoryPath(project, "../history-with spaces")
	if err != nil {
		t.Fatalf("second terminalHistoryPath: %v", err)
	}
	if path == other {
		t.Fatal("distinct terminal IDs collided in history path")
	}
}
