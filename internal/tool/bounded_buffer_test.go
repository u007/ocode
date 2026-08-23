package tool

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestBoundedBufferKeepsHeadAndCountsDropped verifies the runaway guard: once
// the cap is reached the buffer keeps the FIRST cap bytes and counts the rest
// as dropped, rather than growing without limit.
//
// Head-retention (not a tail ring) is deliberate: truncateOutput shows the
// first 30000 chars of the result, so keeping the head means what the user
// sees is genuinely the start of the command's output. A tail ring would show
// bytes from deep inside a runaway stream and silently misrepresent it.
func TestBoundedBufferKeepsHeadAndCountsDropped(t *testing.T) {
	b := newBoundedBuffer(10)

	n, err := b.Write([]byte("0123456789ABCDEF"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 16 {
		t.Fatalf("Write must report the full input as consumed, got %d want 16", n)
	}
	if got := b.String(); got != "0123456789" {
		t.Fatalf("expected the first 10 bytes retained, got %q", got)
	}
	if b.Dropped() != 6 {
		t.Fatalf("expected 6 dropped bytes, got %d", b.Dropped())
	}
}

// TestBoundedBufferUnderCapRetainsEverything confirms the guard is inert for
// normal commands: below the cap nothing is dropped and no notice is warranted.
func TestBoundedBufferUnderCapRetainsEverything(t *testing.T) {
	b := newBoundedBuffer(1024)
	if _, err := b.Write([]byte("hello world")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := b.String(); got != "hello world" {
		t.Fatalf("expected full retention, got %q", got)
	}
	if b.Dropped() != 0 {
		t.Fatalf("expected 0 dropped, got %d", b.Dropped())
	}
}

// TestBoundedBufferNeverShortWrites is the io.Copy contract guard. The pump
// does io.Copy(io.MultiWriter(buf, ...), pipe); io.Copy treats a short write as
// ErrShortWrite and ABORTS the copy. If the bounded buffer reported only the
// bytes it retained, hitting the cap would tear down the pump mid-command,
// killing live streaming and the process output ring with it.
func TestBoundedBufferNeverShortWrites(t *testing.T) {
	b := newBoundedBuffer(4)
	src := strings.NewReader(strings.Repeat("x", 4096))

	written, err := io.Copy(b, src)
	if err != nil {
		t.Fatalf("io.Copy aborted: %v — bounded buffer must never short-write", err)
	}
	if written != 4096 {
		t.Fatalf("io.Copy reported %d bytes, want 4096", written)
	}
	if b.Len() != 4 {
		t.Fatalf("expected retention capped at 4 bytes, got %d", b.Len())
	}
	if b.Dropped() != 4092 {
		t.Fatalf("expected 4092 dropped, got %d", b.Dropped())
	}
}

// TestBoundedBufferMultiWriterFanoutUnaffected confirms that capping one sink
// does not starve the others in the pump's io.MultiWriter — the process output
// ring and the live emit sink must still receive every byte after the cap.
func TestBoundedBufferMultiWriterFanoutUnaffected(t *testing.T) {
	capped := newBoundedBuffer(4)
	var sibling bytes.Buffer

	mw := io.MultiWriter(capped, &sibling)
	if _, err := io.Copy(mw, strings.NewReader(strings.Repeat("y", 512))); err != nil {
		t.Fatalf("io.Copy aborted: %v", err)
	}
	if sibling.Len() != 512 {
		t.Fatalf("sibling writer must receive all 512 bytes, got %d", sibling.Len())
	}
	if capped.Len() != 4 {
		t.Fatalf("capped writer must retain only 4 bytes, got %d", capped.Len())
	}
}
