package session

import (
	"os"
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

func TestOjsonlSessionPath(t *testing.T) {
	path := ojsonlSessionPath("/tmp/sessions", "ses_abc")
	if path != "/tmp/sessions/ses_abc.ojsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}

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
	if state.count != 2 {
		t.Fatalf("expected count 2 (two msg lines), got %d", state.count)
	}
	if state.title != "seed" {
		t.Fatalf("expected title 'seed', got %q", state.title)
	}
	if !state.titleGenerated {
		t.Fatal("expected titleGenerated=true")
	}
}
