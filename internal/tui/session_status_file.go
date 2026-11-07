package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/paths"
)

// sessionStatusDirOverride redirects the status directory. Tests set it; the
// empty string means the real <GlobalDataDir>/session-status.
var sessionStatusDirOverride string

// statusSeq orders status writes. Every call takes the next value synchronously,
// so a write that finishes late on its goroutine cannot overwrite a newer state.
var statusSeq atomic.Uint64

var (
	statusWriteMu sync.Mutex
	// statusLastSeq is the newest sequence written per session id.
	statusLastSeq = map[string]uint64{}
)

// sessionStatusRecord is the on-disk shape. pid and updated_at let a reader
// discard a record left behind by a crashed or switched-away TUI.
type sessionStatusRecord struct {
	SessionID  string `json:"session_id"`
	TurnActive bool   `json:"turn_active"`
	PID        int    `json:"pid"`
	UpdatedAt  string `json:"updated_at"`
}

func sessionStatusDir() (string, error) {
	if sessionStatusDirOverride != "" {
		return sessionStatusDirOverride, nil
	}
	gd, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gd, "session-status"), nil
}

func newSessionStatusRecord(sessionID string, turnActive bool) sessionStatusRecord {
	return sessionStatusRecord{
		SessionID:  sessionID,
		TurnActive: turnActive,
		PID:        os.Getpid(),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// writeSessionStatusFile records the TUI session's turn state so external
// readers (the `ocode session` CLI, a remote server) can observe it. The file
// write runs on its own goroutine, never inside Update.
func writeSessionStatusFile(sessionID string, turnActive bool) {
	if sessionID == "" {
		return
	}
	seq := statusSeq.Add(1)
	rec := newSessionStatusRecord(sessionID, turnActive)
	crashguard.Go(func() { writeSessionStatusRecord(seq, rec) })
}

// writeSessionStatusNow is the synchronous form, for the quit path: the process
// is about to exit, so a goroutine might never finish.
func writeSessionStatusNow(sessionID string, turnActive bool) {
	if sessionID == "" {
		return
	}
	seq := statusSeq.Add(1)
	writeSessionStatusRecord(seq, newSessionStatusRecord(sessionID, turnActive))
}

// writeSessionStatusRecord writes rec unless a newer state for the same session
// already landed. The write is temp-file-plus-rename, so readers never see a
// torn file.
func writeSessionStatusRecord(seq uint64, rec sessionStatusRecord) {
	statusWriteMu.Lock()
	defer statusWriteMu.Unlock()
	if seq <= statusLastSeq[rec.SessionID] {
		return
	}
	statusLastSeq[rec.SessionID] = seq
	dir, err := sessionStatusDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = writeFileAtomic(filepath.Join(dir, rec.SessionID+".json"), b)
}

// writeFileAtomic writes data to a temp file beside path, then renames it over
// path. A failed write removes the temp file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Chmod(0o644)
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}

// syncTurnStatusFile writes the TUI's turn state when the session id or the
// streaming flag differs from the last write. It is the one turn-end hook: call
// it wherever m.streaming or m.sessionID changes. When the id changed while a
// turn was active, the old session is marked idle first.
func (m *model) syncTurnStatusFile() {
	memo := &m.statusFile
	if memo.synced && memo.id == m.sessionID && memo.active == m.streaming {
		return
	}
	if memo.synced && memo.id != m.sessionID && memo.active {
		writeSessionStatusFile(memo.id, false)
	}
	memo.synced = true
	memo.id = m.sessionID
	memo.active = m.streaming
	writeSessionStatusFile(m.sessionID, m.streaming)
}

// turnStatusMemo is the last turn state written to the status file, so an
// unchanged state is not rewritten on every call.
type turnStatusMemo struct {
	synced bool
	id     string
	active bool
}
