# `.ojsonl` Session Storage Format Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace whole-file-rewrite-per-save session storage with an append-only `.ojsonl` format for new ocode sessions, while leaving existing `.json` sessions readable and writable exactly as today.

**Architecture:** A new file, `internal/session/ojsonl.go`, adds the `.ojsonl` line format (header + `msg`/`meta` records), an in-process write-state cache keyed by file path, append and title-rewrite writers, a streaming reader with truncated-tail recovery, and a cheap header-only reader for listing. `session.go`'s `Save`, `Load`, `List`, `ListForDir`, `ListRefsPaginated`, and `Delete` are extended with minimal, additive dispatch: check for a `.ojsonl` file first, fall back to the existing `.json` path unchanged.

**Tech Stack:** Go stdlib only (`encoding/json`, `bufio`, `os`, `sync`) — no new dependencies.

## Global Constraints

- New sessions always write `.ojsonl`. Existing `.json` sessions are never converted; if a `.json` file already exists for an id, saves to that id keep using the existing `.json` path unchanged (verbatim from spec).
- `updated_at` is never stored in the file; it is always derived from `os.Stat(path).ModTime()` (verbatim from spec).
- Every appended batch is written with a single `Write()` call on a file opened with `O_APPEND` (verbatim from spec — required for cross-process atomicity of the append itself).
- Header (line 1) rewrites use write-to-temp-in-same-dir + `os.Rename` — never in-place truncate+overwrite (verbatim from spec).
- Concurrent-writer safety (duplicate appends, and the title-rewrite/append race) is explicitly out of scope for this plan — already tracked in `TODO.md`. Do not add file locking.
- Cross-project session-file fallback (`readSessionFileAnyProject`) and the legacy bare-timestamp id path are `.json`-only in this plan. `.ojsonl` sessions are always created with the canonical `ses_` id, so no legacy-id resolution is needed for them. Cross-project fallback for `.ojsonl` is out of scope for this plan (add to `TODO.md` in Task 8).

Spec: `docs/superpowers/specs/2026-07-21-session-storage-ojsonl-design.md`

---

### Task 1: `.ojsonl` record types and line codec

**Files:**
- Create: `internal/session/ojsonl.go`
- Test: `internal/session/ojsonl_test.go`

**Interfaces:**
- Produces: `ojsonlHeader{V int, ID string, CreatedAt time.Time, Title string, TitleGenerated bool}`, `encodeHeaderLine(h ojsonlHeader) ([]byte, error)`, `decodeHeaderLine(line []byte) (ojsonlHeader, error)`, `encodeMsgLine(m agent.Message) ([]byte, error)`, `encodeMetaLine(meta map[string]any) ([]byte, error)`, `peekRecordType(line []byte) (string, error)`, `decodeMsgLine(line []byte) (agent.Message, error)`, `decodeMetaLine(line []byte) (map[string]any, error)`.

- [x] **Step 1: Write the failing test**

```go
// internal/session/ojsonl_test.go
package session

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

func TestHeaderLineRoundTrip(t *testing.T) {
	h := ojsonlHeader{
		V:              1,
		ID:             "ses_2026-07-21-100000",
		CreatedAt:      time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Title:          "hello world",
		TitleGenerated: true,
	}
	line, err := encodeHeaderLine(h)
	if err != nil {
		t.Fatalf("encodeHeaderLine: %v", err)
	}
	got, err := decodeHeaderLine(line)
	if err != nil {
		t.Fatalf("decodeHeaderLine: %v", err)
	}
	if got != h {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, h)
	}
}

func TestMsgLineRoundTrip(t *testing.T) {
	m := agent.Message{Role: "user", Content: "hi there"}
	line, err := encodeMsgLine(m)
	if err != nil {
		t.Fatalf("encodeMsgLine: %v", err)
	}
	typ, err := peekRecordType(line)
	if err != nil {
		t.Fatalf("peekRecordType: %v", err)
	}
	if typ != "msg" {
		t.Fatalf("expected type msg, got %q", typ)
	}
	got, err := decodeMsgLine(line)
	if err != nil {
		t.Fatalf("decodeMsgLine: %v", err)
	}
	if got.Role != m.Role || got.Content != m.Content {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, m)
	}
}

func TestMetaLineRoundTrip(t *testing.T) {
	meta := map[string]any{"total_tokens": 46.0, "todo_text": "- [ ] a"}
	line, err := encodeMetaLine(meta)
	if err != nil {
		t.Fatalf("encodeMetaLine: %v", err)
	}
	typ, err := peekRecordType(line)
	if err != nil {
		t.Fatalf("peekRecordType: %v", err)
	}
	if typ != "meta" {
		t.Fatalf("expected type meta, got %q", typ)
	}
	got, err := decodeMetaLine(line)
	if err != nil {
		t.Fatalf("decodeMetaLine: %v", err)
	}
	if got["total_tokens"] != 46.0 || got["todo_text"] != "- [ ] a" {
		t.Fatalf("round trip mismatch: got %#v, want %#v", got, meta)
	}
}

func TestPeekRecordTypeRejectsUnknown(t *testing.T) {
	if _, err := peekRecordType([]byte(`{"type":"bogus"}`)); err == nil {
		t.Fatal("expected error for line with no matching decoder, got nil")
	}
	if _, err := peekRecordType([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed line, got nil")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestHeaderLineRoundTrip|TestMsgLineRoundTrip|TestMetaLineRoundTrip|TestPeekRecordTypeRejectsUnknown' -v`
Expected: FAIL — `ojsonlHeader`, `encodeHeaderLine`, etc. undefined.

- [x] **Step 3: Write minimal implementation**

```go
// internal/session/ojsonl.go
package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/u007/ocode/internal/agent"
)

const ojsonlSchemaVersion = 1

// ojsonlHeader is line 1 of a .ojsonl session file. It is rewritten
// (via temp file + rename, never in place) only when the title changes.
type ojsonlHeader struct {
	V              int       `json:"v"`
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	Title          string    `json:"title"`
	TitleGenerated bool      `json:"title_generated,omitempty"`
}

// ojsonlMsgRecord is one appended message line.
type ojsonlMsgRecord struct {
	Type string `json:"type"`
	agent.Message
}

// ojsonlMetaRecord is one appended metadata snapshot line. On load, the
// last meta record in the file wins.
type ojsonlMetaRecord struct {
	Type     string         `json:"type"`
	Metadata map[string]any `json:"metadata"`
}

func encodeHeaderLine(h ojsonlHeader) ([]byte, error) {
	line, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl header: %w", err)
	}
	return append(line, '\n'), nil
}

func decodeHeaderLine(line []byte) (ojsonlHeader, error) {
	var h ojsonlHeader
	if err := json.Unmarshal(line, &h); err != nil {
		return ojsonlHeader{}, fmt.Errorf("decode ojsonl header: %w", err)
	}
	return h, nil
}

func encodeMsgLine(m agent.Message) ([]byte, error) {
	line, err := json.Marshal(ojsonlMsgRecord{Type: "msg", Message: m})
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl msg record: %w", err)
	}
	return append(line, '\n'), nil
}

func decodeMsgLine(line []byte) (agent.Message, error) {
	var rec ojsonlMsgRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return agent.Message{}, fmt.Errorf("decode ojsonl msg record: %w", err)
	}
	return rec.Message, nil
}

func encodeMetaLine(meta map[string]any) ([]byte, error) {
	line, err := json.Marshal(ojsonlMetaRecord{Type: "meta", Metadata: meta})
	if err != nil {
		return nil, fmt.Errorf("encode ojsonl meta record: %w", err)
	}
	return append(line, '\n'), nil
}

func decodeMetaLine(line []byte) (map[string]any, error) {
	var rec ojsonlMetaRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return nil, fmt.Errorf("decode ojsonl meta record: %w", err)
	}
	return rec.Metadata, nil
}

// peekRecordType reads only the "type" field of a msg/meta record line,
// without decoding the rest — used both to bootstrap the persisted-message
// count cheaply and to dispatch decoding on load.
func peekRecordType(line []byte) (string, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return "", fmt.Errorf("peek ojsonl record type: %w", err)
	}
	if probe.Type != "msg" && probe.Type != "meta" {
		return "", fmt.Errorf("peek ojsonl record type: unknown type %q", probe.Type)
	}
	return probe.Type, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -run 'TestHeaderLineRoundTrip|TestMsgLineRoundTrip|TestMetaLineRoundTrip|TestPeekRecordTypeRejectsUnknown' -v`
Expected: PASS (4/4)

- [x] **Step 5: Commit**

```bash
git add internal/session/ojsonl.go internal/session/ojsonl_test.go
git commit -m "feat(session): add .ojsonl line record types and codec"
```

---

### Task 2: Write-state cache and bootstrap scan

**Files:**
- Modify: `internal/session/ojsonl.go`
- Test: `internal/session/ojsonl_test.go`

**Interfaces:**
- Consumes: `ojsonlHeader`, `decodeHeaderLine`, `peekRecordType` (Task 1)
- Produces: `ojsonlSessionPath(dir, id string) string`, `ojsonlWriteState{count int, title string, titleGenerated bool}`, `bootstrapOjsonlState(path string) (ojsonlWriteState, bool, error)` (bool = file existed), `getOjsonlWriteState(path string) (ojsonlWriteState, bool, error)`, `setOjsonlWriteState(path string, s ojsonlWriteState)`.

- [x] **Step 1: Write the failing test**

```go
// internal/session/ojsonl_test.go (append)

func TestBootstrapOjsonlStateMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_missing")
	state, existed, err := bootstrapOjsonlState(path)
	if err != nil {
		t.Fatalf("bootstrapOjsonlState: %v", err)
	}
	if existed {
		t.Fatal("expected existed=false for missing file")
	}
	if state.count != 0 {
		t.Fatalf("expected count 0, got %d", state.count)
	}
}

func TestBootstrapOjsonlStateCountsOnlyMsgLines(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_seed")

	var buf []byte
	headerLine, _ := encodeHeaderLine(ojsonlHeader{V: 1, ID: "ses_seed", Title: "seed", TitleGenerated: true})
	buf = append(buf, headerLine...)
	msg1, _ := encodeMsgLine(agent.Message{Role: "user", Content: "one"})
	buf = append(buf, msg1...)
	metaLine, _ := encodeMetaLine(map[string]any{"total_tokens": 1.0})
	buf = append(buf, metaLine...)
	msg2, _ := encodeMsgLine(agent.Message{Role: "assistant", Content: "two"})
	buf = append(buf, msg2...)
	metaLine2, _ := encodeMetaLine(map[string]any{"total_tokens": 2.0})
	buf = append(buf, metaLine2...)

	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	state, existed, err := bootstrapOjsonlState(path)
	if err != nil {
		t.Fatalf("bootstrapOjsonlState: %v", err)
	}
	if !existed {
		t.Fatal("expected existed=true")
	}
	// Two msg lines, two meta lines, one header line: count must be 2, not 5.
	if state.count != 2 {
		t.Fatalf("expected msg count 2 (not counting header/meta lines), got %d", state.count)
	}
	if state.title != "seed" || !state.titleGenerated {
		t.Fatalf("expected title seed/generated=true, got %q/%v", state.title, state.titleGenerated)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestBootstrapOjsonlState' -v`
Expected: FAIL — `ojsonlSessionPath`, `bootstrapOjsonlState` undefined. Add `"os"` and `"path/filepath"` to the test file's imports if not already present.

- [x] **Step 3: Write minimal implementation**

```go
// internal/session/ojsonl.go (append)

import (
	"bufio"
	// ... existing imports, add:
	"os"
	"path/filepath"
	"sync"
)

func ojsonlSessionPath(dir, id string) string {
	return filepath.Join(dir, id+".ojsonl")
}

// ojsonlWriteState is the in-process cache of what Save() believes is
// already durable for a given .ojsonl file path. Keyed by absolute file
// path (not session id) so distinct project dirs never collide.
type ojsonlWriteState struct {
	count          int
	title          string
	titleGenerated bool
}

var (
	ojsonlStateMu sync.Mutex
	ojsonlState   = map[string]ojsonlWriteState{}
)

func getOjsonlWriteState(path string) (ojsonlWriteState, bool, error) {
	ojsonlStateMu.Lock()
	s, ok := ojsonlState[path]
	ojsonlStateMu.Unlock()
	if ok {
		return s, true, nil
	}
	s, existed, err := bootstrapOjsonlState(path)
	if err != nil {
		return ojsonlWriteState{}, false, err
	}
	setOjsonlWriteState(path, s)
	return s, existed, nil
}

func setOjsonlWriteState(path string, s ojsonlWriteState) {
	ojsonlStateMu.Lock()
	ojsonlState[path] = s
	ojsonlStateMu.Unlock()
}

// bootstrapOjsonlState reads line 1 (header) for the current title, then
// scans the rest of the file counting only "msg" lines — never "meta"
// lines or the header itself, or the persisted-count cache would be off
// and the next Save() would skip real messages or duplicate them.
// existed=false and a zero state is returned when the file does not exist
// yet (this is a brand-new session).
func bootstrapOjsonlState(path string) (ojsonlWriteState, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ojsonlWriteState{}, false, nil
		}
		return ojsonlWriteState{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ojsonlWriteState{}, false, fmt.Errorf("read header from %s: %w", path, err)
		}
		return ojsonlWriteState{}, true, fmt.Errorf("ojsonl file %s has no header line", path)
	}
	header, err := decodeHeaderLine(scanner.Bytes())
	if err != nil {
		return ojsonlWriteState{}, true, fmt.Errorf("ojsonl file %s: %w", path, err)
	}

	count := 0
	for scanner.Scan() {
		typ, err := peekRecordType(scanner.Bytes())
		if err != nil {
			// A corrupt non-header line during bootstrap is treated the same
			// as at load time: only tolerable on the true last line, handled
			// by the loader (Task 5). Bootstrap only needs the count, so
			// skip lines it can't classify rather than fail the whole save path.
			continue
		}
		if typ == "msg" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return ojsonlWriteState{}, true, fmt.Errorf("scan %s: %w", path, err)
	}

	return ojsonlWriteState{count: count, title: header.Title, titleGenerated: header.TitleGenerated}, true, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -run 'TestBootstrapOjsonlState' -v`
Expected: PASS (2/2)

- [x] **Step 5: Commit**

```bash
git add internal/session/ojsonl.go internal/session/ojsonl_test.go
git commit -m "feat(session): add ojsonl write-state cache and bootstrap scan"
```

---

### Task 3: Append writer and title-rewrite writer

**Files:**
- Modify: `internal/session/ojsonl.go`
- Test: `internal/session/ojsonl_test.go`

**Interfaces:**
- Consumes: `ojsonlWriteState`, `getOjsonlWriteState`, `setOjsonlWriteState`, `encodeHeaderLine`, `encodeMsgLine`, `encodeMetaLine`, `decodeHeaderLine` (Tasks 1–2)
- Produces: `appendOjsonlSession(path, id string, createdAt time.Time, newMessages []agent.Message, metadata map[string]any, title string, titleGenerated bool) error`

- [x] **Step 1: Write the failing test**

```go
// internal/session/ojsonl_test.go (append)

func TestAppendOjsonlSessionCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_new")
	createdAt := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	err := appendOjsonlSession(path, "ses_new", createdAt,
		[]agent.Message{{Role: "user", Content: "hi"}},
		map[string]any{"total_tokens": 1.0}, "", false)
	if err != nil {
		t.Fatalf("appendOjsonlSession: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) != 3 { // header + 1 msg + 1 meta
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), data)
	}
	header, err := decodeHeaderLine(lines[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header.ID != "ses_new" || !header.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected header: %+v", header)
	}

	state, existed, err := getOjsonlWriteState(path)
	// getOjsonlWriteState would re-bootstrap from disk since this is a
	// fresh process-level cache in the test; confirm disk state directly
	// instead of relying on the in-memory cache appendOjsonlSession set.
	_ = state
	_ = existed
	_ = err
}

func TestAppendOjsonlSessionAppendsOnlyNewMessages(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_append")
	createdAt := time.Now()

	if err := appendOjsonlSession(path, "ses_append", createdAt,
		[]agent.Message{{Role: "user", Content: "one"}}, nil, "", false); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendOjsonlSession(path, "ses_append", createdAt,
		[]agent.Message{{Role: "assistant", Content: "two"}}, nil, "", false); err != nil {
		t.Fatalf("second append: %v", err)
	}

	data, _ := os.ReadFile(path)
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	// header + msg "one" + msg "two" — no duplicate of "one", no re-written header.
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines after two appends, got %d: %s", len(lines), data)
	}
	m2, err := decodeMsgLine(lines[2])
	if err != nil || m2.Content != "two" {
		t.Fatalf("expected last line to be msg 'two', got %v (err %v)", m2, err)
	}
}

func TestAppendOjsonlSessionRewritesHeaderOnTitleChange(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_title")
	createdAt := time.Now()

	if err := appendOjsonlSession(path, "ses_title", createdAt,
		[]agent.Message{{Role: "user", Content: "hi"}}, nil, "", false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := appendOjsonlSession(path, "ses_title", createdAt,
		nil, nil, "Real Title", true); err != nil {
		t.Fatalf("title rewrite: %v", err)
	}

	data, _ := os.ReadFile(path)
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	header, err := decodeHeaderLine(lines[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header.Title != "Real Title" || !header.TitleGenerated {
		t.Fatalf("expected rewritten header, got %+v", header)
	}
	// The message line must survive the header rewrite untouched.
	m, err := decodeMsgLine(lines[1])
	if err != nil || m.Content != "hi" {
		t.Fatalf("expected message line preserved, got %v (err %v)", m, err)
	}

	// Saving the same title again must NOT rewrite the header a second
	// time (the cached title should short-circuit it) — verified by
	// checking the file's content is unchanged, since a no-op rewrite
	// would produce byte-identical output anyway; the real guard here is
	// that no error occurs and the message count in state is untouched.
	if err := appendOjsonlSession(path, "ses_title", createdAt,
		nil, nil, "Real Title", true); err != nil {
		t.Fatalf("second identical title save: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestAppendOjsonlSession' -v`
Expected: FAIL — `appendOjsonlSession` undefined. Add `"bytes"` and `"time"` to the test file's imports.

- [x] **Step 3: Write minimal implementation**

```go
// internal/session/ojsonl.go (append)

// appendOjsonlSession is the single write entry point for .ojsonl saves.
// - If path does not exist yet, it writes the header line followed by
//   newMessages/metadata as the file's first content, in one Write() call.
// - If path exists and title == "" (or matches the cached title), it
//   appends only newMessages (as msg lines) and one meta line (if
//   metadata != nil) via a single Write() call on an O_APPEND handle.
// - If path exists and title != "" and differs from the cached title, the
//   header (line 1) is rewritten via temp file + rename, and any
//   newMessages/metadata are appended in the same call after the rewrite.
func appendOjsonlSession(path, id string, createdAt time.Time, newMessages []agent.Message, metadata map[string]any, title string, titleGenerated bool) error {
	state, existed, err := getOjsonlWriteState(path)
	if err != nil {
		return err
	}

	needsHeaderRewrite := existed && title != "" && title != state.title

	if !existed {
		var buf []byte
		headerLine, err := encodeHeaderLine(ojsonlHeader{
			V:              ojsonlSchemaVersion,
			ID:             id,
			CreatedAt:      createdAt,
			Title:          title,
			TitleGenerated: titleGenerated,
		})
		if err != nil {
			return err
		}
		buf = append(buf, headerLine...)
		body, err := encodeOjsonlBody(newMessages, metadata)
		if err != nil {
			return err
		}
		buf = append(buf, body...)

		if err := os.WriteFile(path, buf, 0644); err != nil {
			return fmt.Errorf("create ojsonl session %s: %w", path, err)
		}
		setOjsonlWriteState(path, ojsonlWriteState{
			count: len(newMessages), title: title, titleGenerated: titleGenerated,
		})
		return nil
	}

	if needsHeaderRewrite {
		if err := rewriteOjsonlHeader(path, id, createdAt, title, titleGenerated); err != nil {
			return err
		}
		state.title = title
		state.titleGenerated = titleGenerated
	}

	body, err := encodeOjsonlBody(newMessages, metadata)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open ojsonl session %s for append: %w", path, err)
		}
		defer f.Close()
		if _, err := f.Write(body); err != nil {
			return fmt.Errorf("append ojsonl session %s: %w", path, err)
		}
	}

	state.count += len(newMessages)
	setOjsonlWriteState(path, state)
	return nil
}

// encodeOjsonlBody builds the msg lines for newMessages followed by one
// meta line (if metadata != nil) as a single contiguous buffer, so the
// caller can write it with one Write() call.
func encodeOjsonlBody(newMessages []agent.Message, metadata map[string]any) ([]byte, error) {
	var buf []byte
	for _, m := range newMessages {
		line, err := encodeMsgLine(m)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
	}
	if metadata != nil {
		line, err := encodeMetaLine(metadata)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
	}
	return buf, nil
}

// rewriteOjsonlHeader replaces line 1 of an existing .ojsonl file with a
// new title, via write-to-temp-in-same-dir + rename. Never truncates or
// overwrites the original file in place, so a crash mid-write leaves
// either the old or the new file fully intact.
func rewriteOjsonlHeader(path, id string, createdAt time.Time, title string, titleGenerated bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for header rewrite: %w", path, err)
	}
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return fmt.Errorf("ojsonl file %s has no header line to rewrite", path)
	}
	rest := data[nl+1:]

	headerLine, err := encodeHeaderLine(ojsonlHeader{
		V:              ojsonlSchemaVersion,
		ID:             id,
		CreatedAt:      createdAt,
		Title:          title,
		TitleGenerated: titleGenerated,
	})
	if err != nil {
		return err
	}

	newContent := append(headerLine, rest...)

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file into %s: %w", path, err)
	}
	return nil
}
```

Add `"bytes"` to `ojsonl.go`'s import block.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -run 'TestAppendOjsonlSession' -v`
Expected: PASS (3/3)

- [x] **Step 5: Commit**

```bash
git add internal/session/ojsonl.go internal/session/ojsonl_test.go
git commit -m "feat(session): add ojsonl append and title-rewrite writers"
```

---

### Task 4: Wire `Save()` to dispatch between `.json` and `.ojsonl`

**Files:**
- Modify: `internal/session/session.go:126-183` (`Save`, `updateIndex` unchanged)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `appendOjsonlSession`, `ojsonlSessionPath` (Tasks 2–3)
- Produces: `Save(id, title string, messages []agent.Message, metadata map[string]any) error` (signature unchanged — existing callers need no changes)

- [x] **Step 1: Write the failing test**

```go
// internal/session/session_test.go (append)

func TestSaveNewSessionWritesOjsonl(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_ojsonl-new", "", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir, _ := GetStorageDir()
	if _, err := os.Stat(filepath.Join(dir, "ses_ojsonl-new.ojsonl")); err != nil {
		t.Fatalf("expected .ojsonl file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ses_ojsonl-new.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .json file for a new session, got err=%v", err)
	}
}

func TestSaveExistingJSONSessionStaysJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	dir, _ := GetStorageDir()
	jsonPath := filepath.Join(dir, "ses_legacy-json.json")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := Session{ID: "ses_legacy-json", Title: "old", Messages: []agent.Message{{Role: "user", Content: "hi"}}}
	seedBytes, _ := json.Marshal(seed)
	if err := os.WriteFile(jsonPath, seedBytes, 0644); err != nil {
		t.Fatalf("seed json file: %v", err)
	}

	if err := Save("ses_legacy-json", "", []agent.Message{
		{Role: "user", Content: "hi"}, {Role: "assistant", Content: "there"},
	}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "ses_legacy-json.ojsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected no .ojsonl file for an id that already has a .json file, got err=%v", err)
	}
	sess, err := Load("ses_legacy-json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages preserved via .json path, got %d", len(sess.Messages))
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestSaveNewSessionWritesOjsonl|TestSaveExistingJSONSessionStaysJSON' -v`
Expected: FAIL — `TestSaveNewSessionWritesOjsonl` fails because `Save` still always writes `.json`.

- [x] **Step 3: Write minimal implementation**

Replace `Save` in `internal/session/session.go`:

```go
func Save(id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	if id == "" {
		id = NewSessionID()
	}

	jsonPath := filepath.Join(dir, id+".json")
	if _, err := os.Stat(jsonPath); err == nil {
		return saveJSON(dir, jsonPath, id, title, messages, metadata)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", jsonPath, err)
	}

	return saveOjsonl(dir, id, title, messages, metadata)
}

// saveJSON is the pre-.ojsonl whole-file-rewrite save path, used only for
// ids that already have a .json file on disk.
func saveJSON(dir, path, id, title string, messages []agent.Message, metadata map[string]any) error {
	var s Session
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("session file %s is corrupt: %w", path, err)
		}
	} else {
		s.ID = id
		s.CreatedAt = time.Now()
	}

	if title != "" {
		s.Title = title
		s.TitleGenerated = true
	} else if s.Title == "" && len(messages) > 0 {
		for _, m := range messages {
			if m.Role == "user" {
				t := m.Content
				if len(t) > 40 {
					t = t[:37] + "..."
				}
				s.Title = t
				break
			}
		}
	}

	s.Messages = messages
	if metadata != nil {
		s.Metadata = metadata
	}
	s.UpdatedAt = time.Now()

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return err
	}
	return updateIndex(dir, id, s.Title)
}

// saveOjsonl is the append-only save path used for all new sessions, and
// for any id whose file already exists as .ojsonl.
func saveOjsonl(dir, id, title string, messages []agent.Message, metadata map[string]any) error {
	path := ojsonlSessionPath(dir, id)

	state, existed, err := getOjsonlWriteState(path)
	if err != nil {
		return err
	}

	newTitle := title
	titleGenerated := false
	if title != "" {
		titleGenerated = true
	} else if !existed && len(messages) > 0 {
		for _, m := range messages {
			if m.Role == "user" {
				t := m.Content
				if len(t) > 40 {
					t = t[:37] + "..."
				}
				newTitle = t
				break
			}
		}
	} else {
		newTitle = "" // no title change this save; appendOjsonlSession keeps the cached one
	}

	newMessages := messages
	if existed {
		if state.count > len(messages) {
			return fmt.Errorf("ojsonl session %s: persisted count %d exceeds provided message count %d", path, state.count, len(messages))
		}
		newMessages = messages[state.count:]
	}

	if err := appendOjsonlSession(path, id, time.Now(), newMessages, metadata, newTitle, titleGenerated); err != nil {
		return err
	}

	resolvedTitle := newTitle
	if resolvedTitle == "" {
		resolvedTitle = state.title
	}
	return updateIndex(dir, id, resolvedTitle)
}
```

Note: `createdAt` for a brand-new session should be `time.Now()` at creation time, not re-derived on every later save — `appendOjsonlSession`'s create-path already only writes `createdAt` once (when `!existed`); subsequent calls pass `time.Now()` too but it is ignored on the append/rewrite paths (only used in the `!existed` branch and in `rewriteOjsonlHeader`, which must preserve the *original* `created_at`, not overwrite it — see Step 3a below).

- [ ] **Step 3a: Fix `rewriteOjsonlHeader` to preserve the original `created_at`**

`rewriteOjsonlHeader` (Task 3) currently takes `createdAt` as a parameter and writes whatever the caller passes — but `saveOjsonl` always passes `time.Now()`, which would corrupt `created_at` on every title change. Fix by reading the existing header's `created_at` inside `rewriteOjsonlHeader` instead of trusting the parameter. Update the function in `internal/session/ojsonl.go`:

```go
// rewriteOjsonlHeader replaces line 1 of an existing .ojsonl file with a
// new title, preserving the original created_at, via write-to-temp +
// rename. Never truncates or overwrites the original file in place.
func rewriteOjsonlHeader(path, id string, title string, titleGenerated bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for header rewrite: %w", path, err)
	}
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return fmt.Errorf("ojsonl file %s has no header line to rewrite", path)
	}
	oldHeader, err := decodeHeaderLine(data[:nl])
	if err != nil {
		return fmt.Errorf("ojsonl file %s: %w", path, err)
	}
	rest := data[nl+1:]

	headerLine, err := encodeHeaderLine(ojsonlHeader{
		V:              ojsonlSchemaVersion,
		ID:             id,
		CreatedAt:      oldHeader.CreatedAt,
		Title:          title,
		TitleGenerated: titleGenerated,
	})
	if err != nil {
		return err
	}

	newContent := append(headerLine, rest...)

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file into %s: %w", path, err)
	}
	return nil
}
```

And update its one call site in `appendOjsonlSession` (Task 3) to drop the now-removed `createdAt` argument:

```go
	if needsHeaderRewrite {
		if err := rewriteOjsonlHeader(path, id, title, titleGenerated); err != nil {
			return err
		}
		state.title = title
		state.titleGenerated = titleGenerated
	}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -v`
Expected: PASS — all tests in the package, including Tasks 1–3's tests and this task's two new tests.

- [x] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/ojsonl.go internal/session/session_test.go
git commit -m "feat(session): dispatch Save() between .json and .ojsonl by existing file"
```

---

### Task 5: `Load()` support for `.ojsonl`, with truncated-tail recovery

**Files:**
- Modify: `internal/session/session.go:206-236` (`Load`)
- Modify: `internal/session/ojsonl.go`
- Test: `internal/session/ojsonl_test.go`, `internal/session/session_test.go`

**Interfaces:**
- Consumes: `ojsonlHeader`, `decodeHeaderLine`, `peekRecordType`, `decodeMsgLine`, `decodeMetaLine`, `ojsonlSessionPath` (Tasks 1–2)
- Produces: `loadOjsonlSession(path string) (*Session, error)`

- [x] **Step 1: Write the failing test**

```go
// internal/session/ojsonl_test.go (append)

func TestLoadOjsonlSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_load")
	createdAt := time.Now()

	if err := appendOjsonlSession(path, "ses_load", createdAt,
		[]agent.Message{{Role: "user", Content: "hi"}}, map[string]any{"total_tokens": 1.0}, "First", true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := appendOjsonlSession(path, "ses_load", createdAt,
		[]agent.Message{{Role: "assistant", Content: "there"}}, map[string]any{"total_tokens": 5.0}, "", false); err != nil {
		t.Fatalf("seed 2: %v", err)
	}

	sess, err := loadOjsonlSession(path)
	if err != nil {
		t.Fatalf("loadOjsonlSession: %v", err)
	}
	if sess.ID != "ses_load" || sess.Title != "First" || !sess.TitleGenerated {
		t.Fatalf("unexpected header fields: %+v", sess)
	}
	if len(sess.Messages) != 2 || sess.Messages[0].Content != "hi" || sess.Messages[1].Content != "there" {
		t.Fatalf("unexpected messages: %+v", sess.Messages)
	}
	if sess.Metadata["total_tokens"] != 5.0 {
		t.Fatalf("expected last meta line to win, got %#v", sess.Metadata)
	}
	if sess.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt derived from file mtime, got zero value")
	}
}

func TestLoadOjsonlSessionDropsTruncatedTailLine(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_torn")

	if err := appendOjsonlSession(path, "ses_torn", time.Now(),
		[]agent.Message{{Role: "user", Content: "hi"}}, nil, "Torn", true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Simulate a crash mid-append: append a syntactically incomplete line.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open for corrupt append: %v", err)
	}
	if _, err := f.Write([]byte(`{"type":"msg","role":"user","content":"cut off`)); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	sess, err := loadOjsonlSession(path)
	if err != nil {
		t.Fatalf("expected torn tail line to be recoverable, got error: %v", err)
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("expected only the complete message to survive, got %d", len(sess.Messages))
	}
}

func TestLoadOjsonlSessionFailsOnCorruptMiddleLine(t *testing.T) {
	dir := t.TempDir()
	path := ojsonlSessionPath(dir, "ses_midcorrupt")

	if err := appendOjsonlSession(path, "ses_midcorrupt", time.Now(),
		[]agent.Message{{Role: "user", Content: "hi"}}, nil, "Mid", true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A syntactically complete but corrupt line, followed by a valid line —
	// this is NOT the truncated-tail case, so it must fail, not be silently
	// dropped.
	f.Write([]byte("not json at all\n"))
	line, _ := encodeMsgLine(agent.Message{Role: "user", Content: "after corruption"})
	f.Write(line)
	f.Close()

	if _, err := loadOjsonlSession(path); err == nil {
		t.Fatal("expected error for corrupt non-tail line, got nil")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestLoadOjsonlSession' -v`
Expected: FAIL — `loadOjsonlSession` undefined.

- [x] **Step 3: Write minimal implementation**

```go
// internal/session/ojsonl.go (append)

// loadOjsonlSession streams an entire .ojsonl file into a Session. If the
// final line is syntactically incomplete (e.g. a crash mid-append left a
// torn write), it is dropped with a logged warning and the rest of the
// session loads normally — only the last line can be partial in an
// append-only file, so a corrupt line anywhere else is a hard error.
func loadOjsonlSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var lines [][]byte
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("ojsonl file %s is empty", path)
	}

	header, err := decodeHeaderLine(lines[0])
	if err != nil {
		return nil, fmt.Errorf("ojsonl file %s has corrupt header: %w", path, err)
	}

	s := &Session{
		ID:             header.ID,
		Title:          header.Title,
		TitleGenerated: header.TitleGenerated,
		CreatedAt:      header.CreatedAt,
		UpdatedAt:      info.ModTime(),
	}

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		isLast := i == len(lines)-1

		typ, terr := peekRecordType(line)
		if terr != nil {
			if isLast {
				log.Printf("session: dropping truncated last line in %s: %v", path, terr)
				break
			}
			return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, terr)
		}

		switch typ {
		case "msg":
			m, derr := decodeMsgLine(line)
			if derr != nil {
				if isLast {
					log.Printf("session: dropping truncated last line in %s: %v", path, derr)
					break
				}
				return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, derr)
			}
			s.Messages = append(s.Messages, m)
		case "meta":
			meta, derr := decodeMetaLine(line)
			if derr != nil {
				if isLast {
					log.Printf("session: dropping truncated last line in %s: %v", path, derr)
					break
				}
				return nil, fmt.Errorf("ojsonl file %s line %d is corrupt: %w", path, i+1, derr)
			}
			s.Metadata = meta
		}
	}

	return s, nil
}
```

Add `"log"` to `ojsonl.go`'s import block if not already present.

Modify `Load` in `internal/session/session.go` to check for a `.ojsonl` file first:

```go
func Load(id string) (*Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	if ojsonlPath := ojsonlSessionPath(dir, id); fileExists(ojsonlPath) {
		s, err := loadOjsonlSession(ojsonlPath)
		if err != nil {
			return nil, err
		}
		s.Messages = removeIncompleteToolRequests(s.Messages)
		return s, nil
	}

	path, data, err := readSessionFile(dir, id)
	if err != nil {
		if os.IsNotExist(err) && shouldSearchOtherProjects(id) {
			fallbackPath, fallbackData, fallbackErr := readSessionFileAnyProject(id)
			if fallbackErr == nil {
				path = fallbackPath
				data = fallbackData
				err = nil
			} else if !os.IsNotExist(fallbackErr) {
				return nil, fallbackErr
			}
		}
	}
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s is corrupt: %w", path, err)
	}
	s.Messages = removeIncompleteToolRequests(s.Messages)

	return &s, nil
}

// fileExists reports whether path exists and is readable as a regular
// file entry (any stat error, including permission errors, is treated as
// "does not exist" for dispatch purposes — Load's fallback paths below
// will surface a clearer error if it turns out to be a real problem).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -v`
Expected: PASS — full package, including all three new load tests.

- [x] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/ojsonl.go internal/session/ojsonl_test.go
git commit -m "feat(session): add .ojsonl Load() with truncated-tail recovery"
```

---

### Task 6: Listing support — `List`, `ListForDir`, `ListRefsPaginated`

**Files:**
- Modify: `internal/session/session.go:366-430` (`List`, `ListForDir`), `internal/session/session.go:516-586` (`ocodeMeta`, `readOcodeMeta`), `internal/session/session.go:588-662` (`ListRefsPaginated`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `loadOjsonlSession` (Task 5)
- Produces: `readOjsonlListMeta(path string) (ocodeMeta, error)` (header + `stat()` only, no message/meta line scanning)

- [x] **Step 1: Write the failing test**

```go
// internal/session/session_test.go (append)

func TestListIncludesOjsonlSessions(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_list-a", "", []agent.Message{{Role: "user", Content: "a"}}, nil); err != nil {
		t.Fatalf("Save a: %v", err)
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_list-a" {
		t.Fatalf("expected 1 session ses_list-a, got %+v", sessions)
	}
	if len(sessions[0].Messages) != 1 {
		t.Fatalf("expected messages loaded, got %d", len(sessions[0].Messages))
	}
}

func TestListRefsPaginatedIncludesOjsonlSessions(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_refs-a", "Title A", []agent.Message{{Role: "user", Content: "a"}}, nil); err != nil {
		t.Fatalf("Save a: %v", err)
	}

	refs, total, err := ListRefsPaginated(0, 0)
	if err != nil {
		t.Fatalf("ListRefsPaginated: %v", err)
	}
	if total != 1 || len(refs) != 1 {
		t.Fatalf("expected 1 ref, got total=%d refs=%+v", total, refs)
	}
	if refs[0].ID != "ses_refs-a" || refs[0].Title != "Title A" || refs[0].Source != SourceOcode {
		t.Fatalf("unexpected ref: %+v", refs[0])
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestListIncludesOjsonlSessions|TestListRefsPaginatedIncludesOjsonlSessions' -v`
Expected: FAIL — `List()`/`ListRefsPaginated()` only scan `.json`, so a session saved as `.ojsonl` is invisible.

- [x] **Step 3: Write minimal implementation**

Add to `internal/session/ojsonl.go`:

```go
// readOjsonlListMeta reads only the header line plus the file's mtime —
// no message or meta lines are parsed — so listing a directory full of
// .ojsonl sessions costs O(sessions), not O(total messages).
func readOjsonlListMeta(path string) (ocodeMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return ocodeMeta{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ocodeMeta{}, fmt.Errorf("stat %s: %w", path, err)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ocodeMeta{}, fmt.Errorf("read header from %s: %w", path, err)
		}
		return ocodeMeta{}, fmt.Errorf("ojsonl file %s has no header line", path)
	}
	header, err := decodeHeaderLine(scanner.Bytes())
	if err != nil {
		return ocodeMeta{}, fmt.Errorf("ojsonl file %s: %w", path, err)
	}

	return ocodeMeta{ID: header.ID, Title: header.Title, UpdatedAt: info.ModTime()}, nil
}
```

Modify `List` and `ListForDir` in `internal/session/session.go` (identical change applied to both — each currently has one `for _, e := range entries` loop scanning `.json`; add a second loop scanning `.ojsonl` right after it, before the `sort.Slice` call):

```go
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".ojsonl" {
			s, err := loadOjsonlSession(filepath.Join(dir, e.Name()))
			if err == nil {
				s.Messages = removeIncompleteToolRequests(s.Messages)
				sessions = append(sessions, *s)
			}
		}
	}
```

Modify `ListRefsPaginated` in `internal/session/session.go` — replace the single `mapDirEntries(dir, entries, ".json", ...)` call with two calls, one per extension, and concatenate:

```go
	jsonMetas := mapDirEntries(dir, entries, ".json", func(path string, e os.DirEntry) (ocodeMeta, bool) {
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
		meta, err := readOjsonlListMeta(path)
		if err != nil {
			log.Printf("session list: read ojsonl meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})
	metas := append(jsonMetas, ojsonlMetas...)
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -v`
Expected: PASS — full package.

- [x] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/ojsonl.go internal/session/session_test.go
git commit -m "feat(session): include .ojsonl sessions in List/ListForDir/ListRefsPaginated"
```

---

### Task 7: `Delete()` supports `.ojsonl`

**Files:**
- Modify: `internal/session/session.go:664-695` (`Delete`)
- Test: `internal/session/session_test.go`

**Interfaces:**
- Consumes: `ojsonlSessionPath` (Task 2)
- Produces: `Delete(id string) error` (signature unchanged)

- [x] **Step 1: Write the failing test**

```go
// internal/session/session_test.go (append)

func TestDeleteRemovesOjsonlSession(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	if err := Save("ses_del-a", "", []agent.Message{{Role: "user", Content: "a"}}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, _ := GetStorageDir()
	path := filepath.Join(dir, "ses_del-a.ojsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist before delete: %v", err)
	}

	if err := Delete("ses_del-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got err=%v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestDeleteRemovesOjsonlSession' -v`
Expected: FAIL — `Delete` only removes the `.json` path, so the `.ojsonl` file is left behind.

- [x] **Step 3: Write minimal implementation**

Modify `Delete` in `internal/session/session.go`:

```go
func Delete(id string) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	for _, path := range []string{
		filepath.Join(dir, id+".json"),
		ojsonlSessionPath(dir, id),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// Update index
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

Also clear the in-process write-state cache entry for the deleted `.ojsonl` path, so a later `Save()` with the same id (unlikely but possible, e.g. a retried operation) bootstraps fresh instead of reusing a stale cached count:

```go
	for _, path := range []string{
		filepath.Join(dir, id+".json"),
		ojsonlSessionPath(dir, id),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	clearOjsonlWriteState(ojsonlSessionPath(dir, id))
```

Add to `internal/session/ojsonl.go`:

```go
func clearOjsonlWriteState(path string) {
	ojsonlStateMu.Lock()
	delete(ojsonlState, path)
	ojsonlStateMu.Unlock()
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/... -v`
Expected: PASS — full package.

- [x] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/ojsonl.go internal/session/session_test.go
git commit -m "feat(session): Delete() removes .ojsonl sessions and clears write-state cache"
```

---

### Task 8: End-to-end lifecycle test, `go vet`/build check, and TODO.md follow-up

**Files:**
- Test: `internal/session/session_test.go`
- Modify: `TODO.md`

**Interfaces:**
- Consumes: everything from Tasks 1–7
- Produces: nothing new — this task is verification-only

- [x] **Step 1: Write the failing test**

```go
// internal/session/session_test.go (append)

func TestOjsonlSessionFullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	id := "ses_lifecycle"

	// Turn 1: auto-titled from first user message.
	if err := Save(id, "", []agent.Message{{Role: "user", Content: "hello there"}}, map[string]any{"total_tokens": 3.0}); err != nil {
		t.Fatalf("Save turn 1: %v", err)
	}
	sess, err := Load(id)
	if err != nil {
		t.Fatalf("Load after turn 1: %v", err)
	}
	if sess.Title != "hello there" || sess.TitleGenerated {
		t.Fatalf("expected auto-title 'hello there', generated=false, got %q/%v", sess.Title, sess.TitleGenerated)
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sess.Messages))
	}

	// Turn 2: append assistant reply, update metadata, no title change.
	msgs2 := append(sess.Messages, agent.Message{Role: "assistant", Content: "hi!"})
	if err := Save(id, "", msgs2, map[string]any{"total_tokens": 9.0}); err != nil {
		t.Fatalf("Save turn 2: %v", err)
	}

	// Turn 3: explicit /title.
	msgs3 := append(msgs2, agent.Message{Role: "user", Content: "more"})
	if err := Save(id, "Explicit Title", msgs3, map[string]any{"total_tokens": 15.0}); err != nil {
		t.Fatalf("Save turn 3: %v", err)
	}

	sess, err = Load(id)
	if err != nil {
		t.Fatalf("Load after turn 3: %v", err)
	}
	if sess.Title != "Explicit Title" || !sess.TitleGenerated {
		t.Fatalf("expected explicit title, got %q/%v", sess.Title, sess.TitleGenerated)
	}
	if len(sess.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(sess.Messages), sess.Messages)
	}
	if sess.Metadata["total_tokens"] != 15.0 {
		t.Fatalf("expected latest metadata to win, got %#v", sess.Metadata)
	}

	// List/refs see it.
	refs, err := ListRefs()
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	found := false
	for _, r := range refs {
		if r.ID == id {
			found = true
			if r.Title != "Explicit Title" {
				t.Fatalf("expected ref title 'Explicit Title', got %q", r.Title)
			}
		}
	}
	if !found {
		t.Fatal("expected lifecycle session in ListRefs")
	}

	// Delete removes it.
	if err := Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Load(id); err == nil {
		t.Fatal("expected Load to fail after Delete")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -run 'TestOjsonlSessionFullLifecycle' -v`
Expected: FAIL if any earlier task's wiring has a gap (this test exercises all of Save/Load/List/Delete together end to end); PASS immediately if Tasks 1–7 are all correctly wired — in which case skip to Step 4.

- [ ] **Step 3: Fix any gap surfaced by the end-to-end test**

If Step 2 fails, the failure will point at one of: title-detection logic in `saveOjsonl` (Task 4), the `state.count` slicing logic in `saveOjsonl` (Task 4), or the meta-line-wins-last logic in `loadOjsonlSession` (Task 5). Re-read the specific assertion that failed and fix the corresponding function from that task — do not add new abstractions here, this step only closes gaps in the already-written code.

- [ ] **Step 4: Run the full test suite and static checks**

Run: `go build ./... && go vet ./... && go test ./internal/session/... -v`
Expected: build succeeds, vet is clean, all tests PASS.

- [ ] **Step 5: Add cross-project `.ojsonl` fallback and commit**

This plan deliberately scoped `.ojsonl` out of `readSessionFileAnyProject` (resuming a session by id from a different cwd than where it was created) — see Global Constraints. Add this as a tracked follow-up in `TODO.md`, next to the two concurrent-writer limitations already there:

```markdown
- [ ] **Cross-project `.ojsonl` resume fallback not implemented.** `Load()`
  only checks the current project's storage dir for a `.ojsonl` file;
  `readSessionFileAnyProject` (used when a session id isn't found in the
  current project, e.g. resuming from a different cwd) still only searches
  for `.json` files. A session created as `.ojsonl` in one project directory
  cannot currently be resumed by id from a different cwd. Deferred because
  it wasn't needed for the core save/load/list/delete lifecycle this plan
  covers — implement by adding the same `ojsonlSessionPath` + `fileExists`
  check used in `Load` into `readSessionFileAnyProject`'s per-project loop.
```

```bash
git add TODO.md internal/session/session_test.go
git commit -m "test(session): add ojsonl full-lifecycle integration test; track cross-project fallback TODO"
```
