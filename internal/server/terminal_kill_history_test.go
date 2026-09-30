//go:build !windows

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Closing a terminal tab is a PERMANENT discard, not a detach: DELETE drops the
// shell AND its append-only disk history, because the transcript belongs to a tab
// that no longer exists. A detach (socket drop, reload) deliberately keeps both,
// so the two paths must not be conflated.
//
// This was previously only a comment in HandleTerminalKill
// (`sess.history.remove()`, handler_terminal.go:595) with no test behind it —
// the other history.remove() call sites in the tree are test cleanup helpers,
// not assertions about the kill path.
func TestTerminalKillDropsDiskHistory(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	h, _, wsURL := terminalTestHandler(t)

	const id = "term-kill-history"
	conn, _ := dialTerminal(t, wsURL, id)
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo history-marker-xyz\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, conn, "history-marker-xyz\r\n")

	sess := h.terminalSessions.lookup(id)
	if sess == nil {
		t.Fatal("terminal not registered")
	}
	historyPath := sess.history.abs
	if historyPath == "" {
		t.Fatal("history was not opened for this session")
	}
	// The transcript really landed on disk before the kill.
	if err := waitForFileContains(historyPath, "history-marker-xyz"); err != nil {
		t.Fatalf("history never recorded the output: %v", err)
	}

	// Route through a real mux: HandleTerminalKill reads r.PathValue("id"), which
	// httptest.NewRequest does not populate — calling the handler directly 404s.
	// Going through the mux also proves the route pattern still matches, the same
	// way terminalTestHandler registers it.
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/terminal/{id}", h.HandleTerminalKill)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/terminal/"+id, nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", rr.Code, rr.Body.String())
	}

	// The log file must be gone, not merely detached.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(historyPath); os.IsNotExist(err) {
			return // removed: the contract
		}
		if time.Now().After(deadline) {
			t.Fatalf("history file still present after DELETE: %s", historyPath)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func waitForFileContains(path, want string) error {
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(25 * time.Millisecond)
	}
}
