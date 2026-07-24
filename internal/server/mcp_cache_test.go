package server

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// TestMCPCacheWaitBlocksUntilWarm proves the fix: wait() does not return
// before the background enumeration started by warm() has completed, and
// every caller after that (i.e. every session created after the first) sees
// the same already-computed result instead of re-running MCP enumeration.
func TestMCPCacheWaitBlocksUntilWarm(t *testing.T) {
	c := newMCPCache()

	done := make(chan struct{})
	go func() {
		tools, errs := c.wait()
		if tools != nil {
			t.Errorf("expected nil tools for empty MCP config, got %v", tools)
		}
		if errs != nil {
			t.Errorf("expected nil errs for empty MCP config, got %v", errs)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("wait() returned before warm() was ever called")
	case <-time.After(50 * time.Millisecond):
	}

	c.warm(&config.Config{})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wait() did not unblock after warm() completed")
	}
}

// TestMCPCacheSharedAcrossCallers proves multiple "sessions" (concurrent
// wait() callers) all observe the single warm() result, rather than each
// triggering its own MCP enumeration.
func TestMCPCacheSharedAcrossCallers(t *testing.T) {
	c := newMCPCache()
	c.warm(&config.Config{})

	for i := 0; i < 5; i++ {
		tools, errs := c.wait()
		if tools != nil || errs != nil {
			t.Fatalf("caller %d: expected empty result, got tools=%v errs=%v", i, tools, errs)
		}
	}
}
