package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/u007/ocode/internal/crashguard"
)

// msgSaveFile* is the page → shell message triplet asking the shell to save a
// file the page built in memory. WKWebView drops a blob <a download> click
// without any error (Wails v3 installs no WKDownload delegate), so the page
// hands the bytes here and the shell shows the native save panel. Chunked
// because a single bridge string for a large file exceeds JavaScript string
// limits and multiplies the webview's memory: the SPA streams the file as a
// sequence of start → chunk* → end messages, each bounded by maxSaveChunkBytes,
// and the shell appends them to a temp file.
const msgSaveFileStart = "ocode:save-file-start:"
const msgSaveFileChunk = "ocode:save-file-chunk:"
const msgSaveFileEnd = "ocode:save-file-end:"

// desktopSaveMaxBytes caps the decoded size of one save (2 GiB). The SPA
// enforces the same limit (MAX_DESKTOP_SAVE_BYTES in web/src/lib/wails.ts);
// keep them equal.
const desktopSaveMaxBytes = 2 << 30

// savedFileMode is what a finished download lands with. os.CreateTemp opens the
// temp 0600, which is right while the transfer is in flight but would leave the
// user's saved file unreadable to group/others; the pre-stream path wrote the
// destination directly with os.WriteFile(..., 0o644).
const savedFileMode = 0o644

// maxSaveChunkBytes caps one chunk's decoded bytes. The SPA sends 8 MiB
// chunks; this is the outer bound the shell accepts. The base64 payload is
// checked before decoding.
const maxSaveChunkBytes = 32 << 20

// pendingSaveIdleTimeout is how long a save session may sit without a chunk
// before ensurePendingSaveReaper deletes its temp file. A stalled or
// abandoned save must not leak gigabytes in the temp dir.
const pendingSaveIdleTimeout = 10 * time.Minute

type saveStartRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type saveChunkRequest struct {
	ID   string `json:"id"`
	Seq  int64  `json:"seq"`
	Data string `json:"data"`
}

type saveEndRequest struct {
	ID string `json:"id"`
}

// finishedSave is a complete save sitting in a temp file, waiting for the
// native save panel to pick the destination.
type finishedSave struct {
	name string
	path string
	size int64
}

type pendingSave struct {
	id           string
	name         string
	expected     int64
	received     int64
	seq          int64
	tmp          *os.File
	lastActivity time.Time
}

var (
	pendingSavesMu sync.Mutex
	pendingSaves   = map[string]*pendingSave{}
)

// validSaveID keeps ids to the opaque-token shape crypto.randomUUID produces.
// An id is not secret, but refusing control characters keeps the log lines
// built from it sane, and the length cap bounds work per message.
func validSaveID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// startPendingSave validates a start message and opens a temp file for the
// session. The temp file lives in os.TempDir() until the save panel picks a
// destination (rename, or copy+rename when the destination is on another
// volume). A refused start is logged and creates nothing.
func startPendingSave(message string, origin *application.OriginInfo, appHost, nonce string) (name string, expected int64, ok bool) {
	body, ok := pageBridgePayload(message, msgSaveFileStart, "save file", origin, appHost, nonce)
	if !ok {
		return "", 0, false
	}
	var req saveStartRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		log.Printf("ocode-desktop: refused save file: request is not valid JSON: %v", err)
		return "", 0, false
	}
	name = saveFileBaseName(req.Name)
	if name == "" {
		log.Printf("ocode-desktop: refused save file: no usable file name")
		return "", 0, false
	}
	if !validSaveID(req.ID) {
		log.Printf("ocode-desktop: refused save file %q: bad session id", name)
		return "", 0, false
	}
	if req.Size < 0 || req.Size > desktopSaveMaxBytes {
		refuseSaveStart(name, "size %d over the %d-byte limit", req.Size, desktopSaveMaxBytes)
		return "", 0, false
	}
	tmp, err := os.CreateTemp("", "ocode-save-*.part")
	if err != nil {
		refuseSaveStart(name, "cannot create temp file: %v", err)
		return "", 0, false
	}
	pendingSavesMu.Lock()
	if _, exists := pendingSaves[req.ID]; exists {
		pendingSavesMu.Unlock()
		tmp.Close()
		os.Remove(tmp.Name())
		refuseSaveStart(name, "duplicate session id")
		return "", 0, false
	}
	pendingSaves[req.ID] = &pendingSave{id: req.ID, name: name, expected: req.Size, tmp: tmp, lastActivity: time.Now()}
	pendingSavesMu.Unlock()
	return name, req.Size, true
}

// refuseSaveStart logs a refused start and shows the native error dialog. The
// ack for the start carries ok:false, so the SPA fails the save instead of
// reporting it as done.
func refuseSaveStart(name, format string, args ...any) {
	log.Printf("ocode-desktop: refused save file %q: "+format, append([]any{name}, args...)...)
	// intentionally not logged: the dialog is the user-visible half of the same failure
	if app := application.Get(); app != nil {
		showSaveError(app, name, fmt.Errorf(format, args...))
	}
}

// appendSaveChunk validates one chunk message and appends its decoded bytes to
// the session's temp file. Chunks must arrive in seq order; a mismatch or an
// over-run aborts the session (temp deleted) so a corrupted stream never
// reaches the user's disk.
func appendSaveChunk(message string, origin *application.OriginInfo, appHost, nonce string) bool {
	body, ok := pageBridgePayload(message, msgSaveFileChunk, "save file chunk", origin, appHost, nonce)
	if !ok {
		return false
	}
	var req saveChunkRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		log.Printf("ocode-desktop: refused save file chunk: request is not valid JSON: %v", err)
		return false
	}
	if !validSaveID(req.ID) {
		log.Printf("ocode-desktop: refused save file chunk: bad session id")
		return false
	}
	// Check the encoded length before decoding, so an oversized chunk is not
	// expanded in memory first.
	if len(req.Data) > base64.StdEncoding.EncodedLen(maxSaveChunkBytes) {
		abortPendingSave(req.ID, fmt.Errorf("chunk over the %d-byte limit", maxSaveChunkBytes))
		return false
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		abortPendingSave(req.ID, fmt.Errorf("chunk is not base64: %v", err))
		return false
	}
	pendingSavesMu.Lock()
	s, ok := pendingSaves[req.ID]
	if !ok {
		pendingSavesMu.Unlock()
		log.Printf("ocode-desktop: refused save file chunk: unknown session %q", req.ID)
		return false
	}
	switch {
	case req.Seq != s.seq:
		err = fmt.Errorf("out-of-order chunk %d (want %d)", req.Seq, s.seq)
	case s.received+int64(len(data)) > s.expected:
		err = fmt.Errorf("chunks exceed the declared %d-byte size", s.expected)
	default:
		if _, err = s.tmp.Write(data); err == nil {
			s.received += int64(len(data))
			s.seq++
			s.lastActivity = time.Now()
		}
	}
	pendingSavesMu.Unlock()
	if err != nil {
		abortPendingSave(req.ID, err)
		return false
	}
	return true
}

// abortPendingSave deletes a session and its temp file, and tells the user the
// save failed. A quiet delete would look like a no-op download.
func abortPendingSave(id string, err error) {
	pendingSavesMu.Lock()
	s, ok := pendingSaves[id]
	if ok {
		delete(pendingSaves, id)
	}
	pendingSavesMu.Unlock()
	if !ok {
		log.Printf("ocode-desktop: abort of unknown save session %q: %v", id, err)
		return
	}
	log.Printf("ocode-desktop: aborted save of %q: %v", s.name, err)
	s.tmp.Close()
	os.Remove(s.tmp.Name())
	if app := application.Get(); app != nil {
		showSaveError(app, s.name, err)
	}
}

// finishPendingSave closes a complete session and returns its temp path. An
// incomplete transfer aborts (temp deleted) rather than saving a truncated file.
func finishPendingSave(message string, origin *application.OriginInfo, appHost, nonce string) (finishedSave, bool) {
	body, ok := pageBridgePayload(message, msgSaveFileEnd, "save file end", origin, appHost, nonce)
	if !ok {
		return finishedSave{}, false
	}
	var req saveEndRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		log.Printf("ocode-desktop: refused save file end: request is not valid JSON: %v", err)
		return finishedSave{}, false
	}
	if !validSaveID(req.ID) {
		log.Printf("ocode-desktop: refused save file end: bad session id")
		return finishedSave{}, false
	}
	pendingSavesMu.Lock()
	s, ok := pendingSaves[req.ID]
	if ok {
		if s.received != s.expected {
			pendingSavesMu.Unlock()
			abortPendingSave(req.ID, fmt.Errorf("incomplete save: %d of %d bytes", s.received, s.expected))
			return finishedSave{}, false
		}
		delete(pendingSaves, req.ID)
	}
	pendingSavesMu.Unlock()
	if !ok {
		log.Printf("ocode-desktop: refused save file end: unknown session %q", req.ID)
		return finishedSave{}, false
	}
	if err := s.tmp.Close(); err != nil {
		path := s.tmp.Name()
		os.Remove(path)
		log.Printf("ocode-desktop: save of %q failed to close the temp file: %v", s.name, err)
		if app := application.Get(); app != nil {
			showSaveError(app, s.name, err)
		}
		return finishedSave{}, false
	}
	return finishedSave{name: s.name, path: s.tmp.Name(), size: s.received}, true
}

// handleSaveMessage routes one save message to its handler, then acks the page.
// The page sends the next message only after this ack (see saveAckScript).
func handleSaveMessage(window application.Window, message string, origin *application.OriginInfo, appHost, nonce string) {
	var ok bool
	switch {
	case strings.HasPrefix(message, msgSaveFileStart):
		var name string
		var expected int64
		if name, expected, ok = startPendingSave(message, origin, appHost, nonce); ok {
			log.Printf("ocode-desktop: started save of %q (%d bytes)", name, expected)
		}
	case strings.HasPrefix(message, msgSaveFileChunk):
		// quiet: one log line per 8 MiB chunk would spam desktop.log
		ok = appendSaveChunk(message, origin, appHost, nonce)
	case strings.HasPrefix(message, msgSaveFileEnd):
		var fin finishedSave
		if fin, ok = finishPendingSave(message, origin, appHost, nonce); ok {
			openFinishedSave(window, fin)
		}
	default:
		return
	}
	script, built := saveAckScript(message, ok)
	if !built {
		log.Printf("ocode-desktop: save ack not sent: message has no usable session id")
		return
	}
	window.ExecJS(script)
}

// saveAck is the per-message ack the page waits for. Wails runs each page
// message on its own goroutine, so start, chunk and end sent back to back can
// be handled out of order; the page waiting for each ack keeps them ordered.
type saveAck struct {
	ID   string `json:"id"`
	Step string `json:"step"`
	Seq  int64  `json:"seq"`
	OK   bool   `json:"ok"`
}

// saveAckScript builds the ExecJS that dispatches the ack for one save message.
// The message is split as ocode:save-file-<step>:<nonce>:<json>; the nonce has
// no colon, so the JSON body is the fourth field. It returns false when the
// body carries no session id, so the page waits for a timeout instead.
func saveAckScript(message string, ok bool) (string, bool) {
	parts := strings.SplitN(message, ":", 4)
	if len(parts) != 4 {
		return "", false
	}
	var req struct {
		ID  string `json:"id"`
		Seq int64  `json:"seq"`
	}
	if err := json.Unmarshal([]byte(parts[3]), &req); err != nil || req.ID == "" {
		return "", false
	}
	detail, err := json.Marshal(saveAck{ID: req.ID, Step: strings.TrimPrefix(parts[1], "save-file-"), Seq: req.Seq, OK: ok})
	if err != nil {
		return "", false
	}
	return fmt.Sprintf(`window.dispatchEvent(new CustomEvent("ocode:save-ack",{detail:%s}))`, detail), true
}

// openFinishedSave shows the native save panel for a completed transfer and
// moves the temp file to the chosen path. The panel blocks on the Wails main
// thread, so the work runs in its own goroutine (the same rule as the print
// ready path). Cancelling deletes the temp file: no partial downloads land in
// the user's folders. The panel runs only after the end message is accepted, so
// a transfer that never completes shows no panel.
func openFinishedSave(window application.Window, fin finishedSave) {
	app := application.Get()
	if app == nil {
		log.Printf("ocode-desktop: refused save file %q: application not ready", fin.name)
		os.Remove(fin.path)
		return
	}
	crashguard.Go(func() {
		dialog := app.Dialog.SaveFile().SetFilename(fin.name).AttachToWindow(window)
		// intentionally not logged: without a home directory the panel opens in its own default folder
		if home, err := os.UserHomeDir(); err == nil {
			dialog.SetDirectory(filepath.Join(home, "Downloads"))
		}
		path, err := dialog.PromptForSingleSelection()
		if err != nil {
			log.Printf("ocode-desktop: save panel for %q failed: %v", fin.name, err)
			showSaveError(app, fin.name, err)
			os.Remove(fin.path)
			return
		}
		if path == "" {
			log.Printf("ocode-desktop: save of %q cancelled by user", fin.name)
			os.Remove(fin.path)
			return
		}
		if err := moveTempFile(fin.path, path); err != nil {
			log.Printf("ocode-desktop: save of %q to %q failed: %v", fin.name, path, err)
			showSaveError(app, fin.name, err)
			os.Remove(fin.path)
			return
		}
		log.Printf("ocode-desktop: saved %q (%d bytes)", fin.name, fin.size)
	})
}

// moveTempFile renames tmp to dst, falling back to a copy through a sibling
// temp file when the rename fails (destination on another volume, or a
// filesystem without rename support). The sibling temp keeps dst atomic: the
// user never sees a half-written file at the chosen path.
func moveTempFile(tmp, dst string) error {
	// intentionally not logged: a failed chmod leaves the temp's 0600, which is
	// no worse than refusing the save.
	_ = os.Chmod(tmp, savedFileMode)
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	src, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer src.Close()
	sib, err := os.CreateTemp(filepath.Dir(dst), "ocode-save-*.part")
	if err != nil {
		return err
	}
	if _, err := io.CopyBuffer(sib, src, make([]byte, 1<<20)); err != nil {
		sib.Close()
		os.Remove(sib.Name())
		return err
	}
	if err := sib.Close(); err != nil {
		os.Remove(sib.Name())
		return err
	}
	if err := os.Rename(sib.Name(), dst); err != nil {
		os.Remove(sib.Name())
		return err
	}
	// intentionally not logged: same as the rename path above.
	_ = os.Chmod(dst, savedFileMode)
	os.Remove(tmp)
	return nil
}

// ensurePendingSaveReaper starts the stale-session reaper. Called once at app
// start; sessions die by their own end message otherwise. It also sweeps temp
// files abandoned by a previous process that died mid-transfer.
func ensurePendingSaveReaper() {
	reapAbandonedSaveTempFiles(os.TempDir())
	ticker := time.NewTicker(time.Minute)
	crashguard.Go(func() {
		for now := range ticker.C {
			reapStalePendingSaves(now)
		}
	})
}

// reapAbandonedSaveTempFiles removes temp files left by a process that died
// mid-transfer. Called once at app start — no live sessions exist yet — with
// the directory to scan (os.TempDir() in production, a test dir in tests).
// Only files past the idle timeout are touched, so a fresh temp from a
// just-started save is never swept.
func reapAbandonedSaveTempFiles(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "ocode-save-*.part"))
	if err != nil {
		log.Printf("ocode-desktop: could not scan temp dir for abandoned saves: %v", err)
		return 0
	}
	removed := 0
	cutoff := time.Now().Add(-pendingSaveIdleTimeout)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil {
			log.Printf("ocode-desktop: could not remove abandoned save temp %q: %v", path, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Printf("ocode-desktop: removed %d abandoned save temp file(s)", removed)
	}
	return removed
}

// reapStalePendingSaves removes sessions that stopped making progress. Takes
// the time so tests can drive it without sleeping. Returns how many were reaped.
func reapStalePendingSaves(now time.Time) int {
	pendingSavesMu.Lock()
	var stale []*pendingSave
	for id, s := range pendingSaves {
		if now.Sub(s.lastActivity) > pendingSaveIdleTimeout {
			stale = append(stale, s)
			delete(pendingSaves, id)
		}
	}
	pendingSavesMu.Unlock()
	for _, s := range stale {
		log.Printf("ocode-desktop: reaping stalled save of %q (%d/%d bytes)", s.name, s.received, s.expected)
		s.tmp.Close()
		os.Remove(s.tmp.Name())
	}
	return len(stale)
}

// saveFileBaseName reduces a page-supplied name to a safe file name. It treats
// both separators as path breaks, and returns "" for names that would leave
// the chosen folder or name nothing.
func saveFileBaseName(raw string) string {
	base := filepath.Base(strings.ReplaceAll(raw, `\`, "/"))
	if base == "." || base == ".." || base == "/" {
		return ""
	}
	return base
}

// showSaveError surfaces a failed save as a native alert. The page's save promise
// rejects too, but nothing in the page shows that to the user, so a log line
// alone would look like the download did nothing.
func showSaveError(app *application.App, name string, err error) {
	app.Dialog.Error().
		SetTitle("Could not save file").
		SetMessage(fmt.Sprintf("%s: %v", name, err)).
		Show()
}
