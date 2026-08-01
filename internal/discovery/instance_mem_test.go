package discovery

import (
	"os"
	"testing"
)

func TestRSSBytesForCurrentProcessIsNonZero(t *testing.T) {
	rss, err := RSSBytes(os.Getpid())
	if err != nil {
		t.Fatalf("RSSBytes(self): %v", err)
	}
	if rss == 0 {
		t.Fatal("expected a non-zero RSS for the running test process")
	}
}

func TestRSSBytesUnknownPIDErrors(t *testing.T) {
	// PID 0 is never a real process id on any supported platform.
	if _, err := RSSBytes(0); err == nil {
		t.Fatal("expected an error for pid 0")
	}
}
