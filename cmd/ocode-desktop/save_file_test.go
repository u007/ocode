package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const testSaveNonce = "9f2c4e1a7b3d5f60a1b2c3d4e5f60718"
const testSaveHost = "127.0.0.1:52100"
const testSavePage = "https://127.0.0.1:52100/session/abc?token=t"

func testSaveOrigin() *application.OriginInfo {
	return &application.OriginInfo{Origin: testSavePage}
}

func startMessage(t *testing.T, req saveStartRequest) string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return msgSaveFileStart + testSaveNonce + ":" + string(body)
}

func chunkMessage(t *testing.T, req saveChunkRequest) string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return msgSaveFileChunk + testSaveNonce + ":" + string(body)
}

func endMessage(t *testing.T, req saveEndRequest) string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return msgSaveFileEnd + testSaveNonce + ":" + string(body)
}

func resetSaveSessions(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		pendingSavesMu.Lock()
		for id, s := range pendingSaves {
			s.tmp.Close()
			os.Remove(s.tmp.Name())
			delete(pendingSaves, id)
		}
		pendingSavesMu.Unlock()
	})
}

func TestStartPendingSave(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()

	cases := []struct {
		name     string
		message  string
		origin   *application.OriginInfo
		wantName string
		wantSize int64
		wantOK   bool
	}{
		{"valid", startMessage(t, saveStartRequest{ID: "a1", Name: "resume.pdf", Size: 123}), page, "resume.pdf", 123, true},
		{"path separators stripped", startMessage(t, saveStartRequest{ID: "a2", Name: "../../etc/x.pdf", Size: 1}), page, "x.pdf", 1, true},
		{"backslash separators stripped", startMessage(t, saveStartRequest{ID: "a3", Name: `dir\a.pdf`, Size: 1}), page, "a.pdf", 1, true},
		{"zero-byte file is valid", startMessage(t, saveStartRequest{ID: "a4", Name: "empty.txt", Size: 0}), page, "empty.txt", 0, true},
		{"empty name refused", startMessage(t, saveStartRequest{ID: "b1", Name: "", Size: 1}), page, "", 0, false},
		{"dot name refused", startMessage(t, saveStartRequest{ID: "b2", Name: "..", Size: 1}), page, "", 0, false},
		{"slash only refused", startMessage(t, saveStartRequest{ID: "b3", Name: "/", Size: 1}), page, "", 0, false},
		{"negative size refused", startMessage(t, saveStartRequest{ID: "b4", Name: "a.bin", Size: -1}), page, "", 0, false},
		{"over limit refused", startMessage(t, saveStartRequest{ID: "b5", Name: "a.bin", Size: desktopSaveMaxBytes + 1}), page, "", 0, false},
		{"at limit refused-or-ok above 64 MiB", startMessage(t, saveStartRequest{ID: "b6", Name: "a.bin", Size: desktopSaveMaxBytes}), page, "a.bin", desktopSaveMaxBytes, true},
		{"bad session id refused", startMessage(t, saveStartRequest{ID: "has/slash", Name: "a.bin", Size: 1}), page, "", 0, false},
		{"empty session id refused", startMessage(t, saveStartRequest{ID: "", Name: "a.bin", Size: 1}), page, "", 0, false},
		{"not json refused", msgSaveFileStart + testSaveNonce + ":{nope", page, "", 0, false},
		{"wrong nonce refused", msgSaveFileStart + "0000" + testSaveNonce[4:] + `:{"id":"c1","name":"a.bin","size":1}`, page, "", 0, false},
		{"empty nonce refused", msgSaveFileStart + ":" + `{"id":"c2","name":"a.bin","size":1}`, page, "", 0, false},
		{"no origin refused", startMessage(t, saveStartRequest{ID: "c3", Name: "a.bin", Size: 1}), nil, "", 0, false},
		{"external frame refused", startMessage(t, saveStartRequest{ID: "c4", Name: "a.bin", Size: 1}), &application.OriginInfo{Origin: "https://evil.example/"}, "", 0, false},
		{"empty body refused", msgSaveFileStart + testSaveNonce + ":", page, "", 0, false},
		{"other message refused", msgClipboardWrite + testSaveNonce + ":x", page, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, size, ok := startPendingSave(tc.message, tc.origin, testSaveHost, testSaveNonce)
			if ok != tc.wantOK || name != tc.wantName || size != tc.wantSize {
				t.Fatalf("startPendingSave() = (%q, %d, %v); want (%q, %d, %v)", name, size, ok, tc.wantName, tc.wantSize, tc.wantOK)
			}
			if ok {
				// Clean up the temp the valid case just opened.
				pendingSavesMu.Lock()
				for id, s := range pendingSaves {
					s.tmp.Close()
					os.Remove(s.tmp.Name())
					delete(pendingSaves, id)
				}
				pendingSavesMu.Unlock()
			}
		})
	}
}

func TestStartPendingSaveRefusesDuplicateID(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	msg := startMessage(t, saveStartRequest{ID: "dup", Name: "a.bin", Size: 1})
	if _, _, ok := startPendingSave(msg, page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("first start refused")
	}
	if _, _, ok := startPendingSave(msg, page, testSaveHost, testSaveNonce); ok {
		t.Fatal("duplicate session id accepted")
	}
	pendingSavesMu.Lock()
	n := len(pendingSaves)
	pendingSavesMu.Unlock()
	if n != 1 {
		t.Fatalf("sessions after duplicate start = %d, want 1", n)
	}
}

func TestAppendSaveChunkOutOfOrderAborts(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "ooo", Name: "a.bin", Size: 8}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	pendingSavesMu.Lock()
	tmpPath := pendingSaves["ooo"].tmp.Name()
	pendingSavesMu.Unlock()
	bad := chunkMessage(t, saveChunkRequest{ID: "ooo", Seq: 1, Data: base64.StdEncoding.EncodeToString([]byte("four"))})
	if appendSaveChunk(bad, page, testSaveHost, testSaveNonce) {
		t.Fatal("out-of-order chunk accepted")
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["ooo"]
	n := len(pendingSaves)
	pendingSavesMu.Unlock()
	if ok || n != 0 {
		t.Fatalf("session not aborted after out-of-order chunk (exists=%v, sessions=%d)", ok, n)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file still on disk after out-of-order abort: %v", err)
	}
}

func TestAppendSaveChunkOverrunAborts(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "over", Name: "a.bin", Size: 2}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	pendingSavesMu.Lock()
	tmpPath := pendingSaves["over"].tmp.Name()
	pendingSavesMu.Unlock()
	big := chunkMessage(t, saveChunkRequest{ID: "over", Seq: 0, Data: base64.StdEncoding.EncodeToString([]byte("four"))})
	if appendSaveChunk(big, page, testSaveHost, testSaveNonce) {
		t.Fatal("chunk exceeding declared size accepted")
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["over"]
	pendingSavesMu.Unlock()
	if ok {
		t.Fatal("session not aborted after overrun")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file still on disk after overrun abort: %v", err)
	}
}

func TestAppendSaveChunkUnknownSession(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	msg := chunkMessage(t, saveChunkRequest{ID: "ghost", Seq: 0, Data: base64.StdEncoding.EncodeToString([]byte("x"))})
	if appendSaveChunk(msg, page, testSaveHost, testSaveNonce) {
		t.Fatal("chunk for unknown session accepted")
	}
}

func TestAppendSaveChunkOversizeRefusedBeforeDecode(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "big", Name: "a.bin", Size: maxSaveChunkBytes + 1}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	// Longer than the base64 form of one chunk: refused before decoding.
	over := strings.Repeat("A", base64.StdEncoding.EncodedLen(maxSaveChunkBytes)+4)
	msg := msgSaveFileChunk + testSaveNonce + ":" + `{"id":"big","seq":0,"data":"` + over + `"}`
	pendingSavesMu.Lock()
	tmpPath := pendingSaves["big"].tmp.Name()
	pendingSavesMu.Unlock()
	if appendSaveChunk(msg, page, testSaveHost, testSaveNonce) {
		t.Fatal("oversized chunk accepted")
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["big"]
	pendingSavesMu.Unlock()
	if ok {
		t.Fatal("session not aborted after oversized chunk")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file still on disk after oversize abort: %v", err)
	}
}

func TestChunkedSaveRoundTrip(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	data := []byte("%PDF-1.7\nbytes: with colons and ümlauts\n")
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "rt", Name: "a.pdf", Size: int64(len(data))}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	chunk := chunkMessage(t, saveChunkRequest{ID: "rt", Seq: 0, Data: base64.StdEncoding.EncodeToString(data)})
	if !appendSaveChunk(chunk, page, testSaveHost, testSaveNonce) {
		t.Fatal("chunk refused")
	}
	fin, ok := finishPendingSave(endMessage(t, saveEndRequest{ID: "rt"}), page, testSaveHost, testSaveNonce)
	if !ok {
		t.Fatal("end refused")
	}
	defer os.Remove(fin.path)
	if fin.name != "a.pdf" || fin.size != int64(len(data)) {
		t.Fatalf("finished save = %q, %d bytes; want a.pdf, %d", fin.name, fin.size, len(data))
	}
	got, err := os.ReadFile(fin.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("temp file bytes mismatch: got %d bytes", len(got))
	}
}

func TestFinishPendingSaveIncompleteAborts(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "inc", Name: "a.bin", Size: 4}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	chunk := chunkMessage(t, saveChunkRequest{ID: "inc", Seq: 0, Data: base64.StdEncoding.EncodeToString([]byte("ab"))})
	if !appendSaveChunk(chunk, page, testSaveHost, testSaveNonce) {
		t.Fatal("chunk refused")
	}
	if _, ok := finishPendingSave(endMessage(t, saveEndRequest{ID: "inc"}), page, testSaveHost, testSaveNonce); ok {
		t.Fatal("incomplete save accepted")
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["inc"]
	pendingSavesMu.Unlock()
	if ok {
		t.Fatal("incomplete session still present after end refusal")
	}
}

func TestFinishPendingSaveEmptyFile(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "empty", Name: "e.txt", Size: 0}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	fin, ok := finishPendingSave(endMessage(t, saveEndRequest{ID: "empty"}), page, testSaveHost, testSaveNonce)
	if !ok {
		t.Fatal("end refused for empty file")
	}
	defer os.Remove(fin.path)
	if fin.size != 0 {
		t.Fatalf("finished save size = %d, want 0", fin.size)
	}
	got, err := os.ReadFile(fin.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty temp file has %d bytes", len(got))
	}
}

func TestReapAbandonedSaveTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "ocode-save-stale.part")
	fresh := filepath.Join(dir, "ocode-save-fresh.part")
	other := filepath.Join(dir, "other.part")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("pad"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-pendingSaveIdleTimeout - time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, old, old); err != nil {
		t.Fatal(err)
	}
	if got := reapAbandonedSaveTempFiles(dir); got != 1 {
		t.Fatalf("reapAbandonedSaveTempFiles() = %d, want 1", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp still on disk: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temp wrongly reaped: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("non-save file wrongly reaped: %v", err)
	}
}

func TestReapStalePendingSaves(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "stale", Name: "a.bin", Size: 1}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	pendingSavesMu.Lock()
	s := pendingSaves["stale"]
	tmpPath := s.tmp.Name()
	s.lastActivity = time.Now().Add(-pendingSaveIdleTimeout - time.Minute)
	pendingSavesMu.Unlock()
	if got := reapStalePendingSaves(time.Now()); got != 1 {
		t.Fatalf("reapStalePendingSaves() = %d, want 1", got)
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["stale"]
	pendingSavesMu.Unlock()
	if ok {
		t.Fatal("stale session still present after reap")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file still on disk after reap: %v", err)
	}
}

func TestReapStalePendingSavesLeavesFreshSessions(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "fresh", Name: "a.bin", Size: 1}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused")
	}
	if got := reapStalePendingSaves(time.Now()); got != 0 {
		t.Fatalf("reapStalePendingSaves() = %d, want 0", got)
	}
	pendingSavesMu.Lock()
	_, ok := pendingSaves["fresh"]
	pendingSavesMu.Unlock()
	if !ok {
		t.Fatal("fresh session reaped")
	}
}

func TestMoveTempFileRenames(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.part")
	dst := filepath.Join(dir, "dst.bin")
	data := []byte("payload")
	// 0600 mirrors os.CreateTemp, so the move has to normalize the mode.
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveTempFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("dst = %q, want %q", got, data)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src still on disk after rename: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != savedFileMode {
			t.Fatalf("dst mode = %o, want %o", perm, savedFileMode)
		}
	}
}

func TestMoveTempFileMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := moveTempFile(filepath.Join(dir, "nope.part"), filepath.Join(dir, "dst.bin")); err == nil {
		t.Fatal("move of missing source succeeded")
	}
}

func TestSaveFileBaseName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"resume.pdf", "resume.pdf"},
		{"../../etc/passwd", "passwd"},
		{`..\..\win.ini`, "win.ini"},
		{"", ""},
		{"/", ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			if got := saveFileBaseName(tc.in); got != tc.want {
				t.Fatalf("saveFileBaseName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The two save caps are asserted equal in CHANGES.md, the concept doc and both
// code comments, but they live in different languages. Parse the TS constant so
// a one-sided edit fails here instead of after a whole transfer.
func TestDesktopSaveCapMatchesWeb(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "wails.ts"))
	if err != nil {
		t.Fatalf("read wails.ts: %v", err)
	}
	m := regexp.MustCompile(`(?m)^export const MAX_DESKTOP_SAVE_BYTES\s*=\s*([^;]+);`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("MAX_DESKTOP_SAVE_BYTES not found in web/src/lib/wails.ts")
	}
	got, err := intProduct(m[1])
	if err != nil {
		t.Fatalf("MAX_DESKTOP_SAVE_BYTES = %q: %v", m[1], err)
	}
	if got != desktopSaveMaxBytes {
		t.Fatalf("MAX_DESKTOP_SAVE_BYTES = %d, desktopSaveMaxBytes = %d; keep them equal", got, desktopSaveMaxBytes)
	}
}

// intProduct evaluates a `N * M * ...` literal of decimal integers (the form
// the TS constant uses), so the check needs no JS runtime.
func intProduct(expr string) (int64, error) {
	product := int64(1)
	for _, term := range strings.Split(strings.ReplaceAll(expr, "_", ""), "*") {
		n, err := strconv.ParseInt(strings.TrimSpace(term), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("term %q: %w", term, err)
		}
		product *= n
	}
	return product, nil
}

// The 2 GiB ceiling is decided at start, where the size is declared: exactly the
// cap must be accepted and anything above it refused. A real 2 GiB transfer is
// outside a unit test's budget, so this pins the boundary and the abort.
func TestSaveCapBoundaryAtStart(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "cap", Name: "a.bin", Size: desktopSaveMaxBytes}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused the exact desktop cap")
	}
	pendingSavesMu.Lock()
	tmpPath := pendingSaves["cap"].tmp.Name()
	pendingSavesMu.Unlock()
	abortPendingSave("cap", errors.New("test: do not transfer 2 GiB"))
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file survived the abort: %v", err)
	}
	for _, size := range []int64{desktopSaveMaxBytes + 1, -1} {
		if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "no", Name: "a.bin", Size: size}), page, testSaveHost, testSaveNonce); ok {
			t.Fatalf("start accepted declared size %d", size)
		}
	}
}

// End-to-end protocol at a size that still exercises chunk coalescing, the
// declared-size cross-check, the temp file, the mode and the move: start ->
// chunk* -> end -> rename, 24 MiB over three 8 MiB messages (the SPA's size).
func TestChunkedSaveLargeTransfer(t *testing.T) {
	resetSaveSessions(t)
	page := testSaveOrigin()
	const chunkBytes = 8 << 20
	const chunks = 3
	const total = chunks * chunkBytes
	if _, _, ok := startPendingSave(startMessage(t, saveStartRequest{ID: "big", Name: "big.bin", Size: total}), page, testSaveHost, testSaveNonce); !ok {
		t.Fatal("start refused a 24 MiB transfer")
	}
	chunk := make([]byte, chunkBytes)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(chunk)
	for seq := int64(0); seq < chunks; seq++ {
		msg := chunkMessage(t, saveChunkRequest{ID: "big", Seq: seq, Data: encoded})
		if !appendSaveChunk(msg, page, testSaveHost, testSaveNonce) {
			t.Fatalf("chunk %d refused", seq)
		}
	}
	fin, ok := finishPendingSave(endMessage(t, saveEndRequest{ID: "big"}), page, testSaveHost, testSaveNonce)
	if !ok {
		t.Fatal("finish refused a complete transfer")
	}
	if fin.size != total {
		t.Fatalf("finished size = %d, want %d", fin.size, total)
	}
	dst := filepath.Join(t.TempDir(), "saved.bin")
	if err := moveTempFile(fin.path, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != total {
		t.Fatalf("saved size = %d, want %d", fi.Size(), total)
	}
	if runtime.GOOS != "windows" {
		if perm := fi.Mode().Perm(); perm != savedFileMode {
			t.Fatalf("saved mode = %o, want %o", perm, savedFileMode)
		}
	}
}
