package tool

import (
	"errors"
	"strings"
	"testing"
)

func TestReadCappedBody(t *testing.T) {
	// Under limit: clean.
	b, over, err := readCappedBody(strings.NewReader("hello"), 100)
	if err != nil || over || string(b) != "hello" {
		t.Fatalf("under-limit: %q %v %v", b, over, err)
	}
	// Exactly at limit: not over.
	b, over, err = readCappedBody(strings.NewReader(strings.Repeat("x", 100)), 100)
	if err != nil || over || len(b) != 100 {
		t.Fatalf("at-limit: len=%d %v %v", len(b), over, err)
	}
	// Over limit: capped + flagged.
	b, over, err = readCappedBody(strings.NewReader(strings.Repeat("x", 101)), 100)
	if err != nil || !over || len(b) != 100 {
		t.Fatalf("over-limit: len=%d %v %v", len(b), over, err)
	}
	// Reader error propagates.
	_, _, err = readCappedBody(errReader{}, 10)
	if err == nil {
		t.Fatal("expected error propagation")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
