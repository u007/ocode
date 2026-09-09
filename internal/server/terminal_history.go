//go:build !windows

package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/paths"
)

const (
	terminalHistoryDefaultPage = 64 * 1024
	terminalHistoryMaxPage     = 256 * 1024
)

var (
	errTerminalHistoryDisabled = errors.New("terminal history disabled")
	errTerminalHistoryMissing  = errors.New("terminal history not found")
	errTerminalHistoryPastEnd  = errors.New("terminal history offset is past end")
	errTerminalHistoryChanged  = errors.New("terminal history snapshot changed")
)

// terminalHistory is an append-only disk log of a terminal's full pty output,
// keyed by terminal_id under the project-scoped ocode global data dir. It is
// written at the single fan-out point (terminalSession.deliver), before any
// buffering/capping for the websocket, so websocket replay limits never lose
// history. The log remains readable after the session closes or the server
// restarts.
type terminalHistory struct {
	mu     sync.Mutex
	abs    string // file path on disk
	file   *os.File
	size   int64 // current persisted byte length while file is open
	closed bool
}

// newTerminalHistory opens (or lazily disables) the append-only log for
// project/terminalID. Only named, resumable ids get a disk log. Anonymous
// sockets get a no-op history.
func newTerminalHistory(project, terminalID string) *terminalHistory {
	if terminalID == "" || strings.HasPrefix(terminalID, "anon-") {
		return &terminalHistory{}
	}
	abs, err := terminalHistoryPath(project, terminalID)
	if err != nil {
		log.Printf("terminal %s: history disabled, cannot resolve data dir: %v", terminalID, err)
		return &terminalHistory{}
	}
	f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("terminal %s: history disabled, cannot open log file: %v", terminalID, err)
		return &terminalHistory{}
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		log.Printf("terminal %s: history disabled, cannot stat log file: %v", terminalID, err)
		return &terminalHistory{}
	}
	return &terminalHistory{abs: abs, file: f, size: fi.Size()}
}

// terminalHistoryDir resolves the project-scoped directory for terminal logs
// under the ocode global data dir. The project slug is derived from the
// shell's workdir so terminals of different projects never share a log.
func terminalHistoryDir(project string) (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	root := paths.ProjectRoot(project)
	dir := filepath.Join(base, "project", paths.ProjectSlug(root), "terminal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// terminalHistoryPath returns a collision-resistant path for a terminal id.
// The id is supplied by a browser, so it must never become a path component;
// hashing also avoids collisions such as sanitized IDs "a/b" and "a-b".
func terminalHistoryPath(project, terminalID string) (string, error) {
	if terminalID == "" || strings.HasPrefix(terminalID, "anon-") {
		return "", errTerminalHistoryDisabled
	}
	dir, err := terminalHistoryDir(project)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(terminalID))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".log"), nil
}

// readTerminalHistoryRange opens the log independently of any live session.
// This is the restart-safe read path used by the HTTP handler. The returned
// end is the file size observed before the read, providing an append-only
// snapshot cursor for the caller.
func readTerminalHistoryRange(project, terminalID string, offset, max int64) ([]byte, int64, error) {
	return readTerminalHistoryRangeAt(project, terminalID, offset, max, nil)
}

// readTerminalHistoryRangeAt reads from a caller-pinned snapshot when
// snapshotEnd is non-nil. An append-only log may grow, but it must not shrink
// or change the cursor while a frontend walks its pages.
func readTerminalHistoryRangeAt(project, terminalID string, offset, max int64, snapshotEnd *int64) ([]byte, int64, error) {
	abs, err := terminalHistoryPath(project, terminalID)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, errTerminalHistoryMissing
		}
		return nil, 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	end := fi.Size()
	if snapshotEnd != nil {
		if *snapshotEnd < 0 || end < *snapshotEnd {
			return nil, end, errTerminalHistoryChanged
		}
		end = *snapshotEnd
	}
	if offset < 0 {
		return nil, 0, errors.New("terminal history offset must be non-negative")
	}
	if offset > end {
		return nil, end, errTerminalHistoryPastEnd
	}
	if max <= 0 {
		max = terminalHistoryDefaultPage
	}
	if max > terminalHistoryMaxPage {
		max = terminalHistoryMaxPage
	}
	if remaining := end - offset; max > remaining {
		max = remaining
	}
	buf := make([]byte, max)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, end, err
	}
	return buf[:n], end, nil
}

// write appends one delivered pty chunk to the log. Caller holds the session
// mutex; this lock also serializes reads/removal against the live descriptor.
func (h *terminalHistory) write(p []byte) {
	if h == nil || len(p) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil || h.closed {
		return
	}
	written := 0
	for written < len(p) {
		n, err := h.file.Write(p[written:])
		if n > 0 {
			written += n
			h.size += int64(n)
		}
		if err != nil {
			log.Printf("terminal %s: history write failed: %v", filepath.Base(h.abs), err)
			return
		}
		if n == 0 {
			log.Printf("terminal %s: history write made no progress", filepath.Base(h.abs))
			return
		}
	}
}

// byteLen returns the current persisted byte length.
func (h *terminalHistory) byteLen() int64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.size
}

// readRange reads from a live history descriptor, reopening the log after the
// session has closed. HTTP callers should use readTerminalHistoryRange so they
// also receive an append-only snapshot end cursor.
func (h *terminalHistory) readRange(offset, max int64) ([]byte, error) {
	if h == nil || h.abs == "" {
		return nil, errTerminalHistoryDisabled
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	f := h.file
	closeAfter := false
	if f == nil || h.closed {
		var err error
		f, err = os.Open(h.abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, errTerminalHistoryMissing
			}
			return nil, err
		}
		closeAfter = true
	}
	if closeAfter {
		defer f.Close()
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if offset >= fi.Size() {
		return []byte{}, nil
	}
	if max <= 0 {
		max = terminalHistoryDefaultPage
	}
	if max > terminalHistoryMaxPage {
		max = terminalHistoryMaxPage
	}
	if remaining := fi.Size() - offset; max > remaining {
		max = remaining
	}
	buf := make([]byte, max)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

// close flushes and closes the log file. The on-disk log survives; explicit
// terminal deletion uses remove instead.
func (h *terminalHistory) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil || h.closed {
		return
	}
	if err := h.file.Sync(); err != nil {
		log.Printf("terminal %s: history sync failed: %v", filepath.Base(h.abs), err)
	}
	if err := h.file.Close(); err != nil {
		log.Printf("terminal %s: history close failed: %v", filepath.Base(h.abs), err)
	}
	h.closed = true
}

// remove closes and deletes the log file. It is used only for an explicit tab
// close, not for shell exit or websocket detach.
func (h *terminalHistory) remove() {
	if h == nil {
		return
	}
	h.mu.Lock()
	abs := h.abs
	if h.file != nil && !h.closed {
		_ = h.file.Close()
	}
	h.closed = true
	h.file = nil
	h.mu.Unlock()
	if abs != "" {
		_ = os.Remove(abs)
	}
}
