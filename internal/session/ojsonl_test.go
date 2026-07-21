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
