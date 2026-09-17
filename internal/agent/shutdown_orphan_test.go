package agent

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// blockingTool blocks in Execute until released, simulating an orphan-recovery
// re-execution of a tool stuck on its own I/O (e.g. `git push` on a credential
// prompt). Its 30s ctx does not cancel the underlying work.
type blockingTool struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingTool) Name() string        { return "blocking_tool" }
func (b *blockingTool) Description() string { return "" }
func (b *blockingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "blocking_tool"}
}
func (b *blockingTool) Parallel() bool { return true }
func (b *blockingTool) Execute(_ json.RawMessage) (string, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return "done", nil
}

// TestShutdownBoundsOrphanRecoveryWait is the regression guard for con #4:
// Agent.Shutdown() used to call orphanRecoveryWG.Wait() with no timeout, so a
// blocked orphan-recovery tool hung every Shutdown caller (TUI model switch,
// /new, idle eviction) indefinitely. The wait is now bounded, and the store is
// retired so the straggler's late write cannot re-seed the global registry.
func TestShutdownBoundsOrphanRecoveryWait(t *testing.T) {
	bt := &blockingTool{entered: make(chan struct{}), release: make(chan struct{})}
	a := NewAgent(&MockClient{}, []tool.Tool{bt}, nil, nil)
	a.permissions = nil

	tc := ToolCall{ID: "orphan-1", Type: "function"}
	tc.Function.Name = "blocking_tool"
	tc.Function.Arguments = `{}`

	// Kick off recovery in the background with a stop channel that never
	// closes, so recoverOneOrphanedToolCall parks on its 30s ctx select while
	// the tool goroutine stays blocked. This is exactly the straggler state.
	neverStop := make(chan struct{})
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		a.recoverOrphanedToolCalls([]Message{
			{Role: "assistant", Content: "go", ToolCalls: []ToolCall{tc}},
		}, neverStop)
	}()

	select {
	case <-bt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking tool never started")
	}

	start := time.Now()
	a.Shutdown()
	elapsed := time.Since(start)

	// Must return on the bound, not hang on the blocked tool.
	if elapsed > orphanRecoveryShutdownWait+2*time.Second {
		t.Fatalf("Shutdown took %v; expected it bounded near %v", elapsed, orphanRecoveryShutdownWait)
	}
	if !a.snapshotStore.Retired() {
		t.Fatal("Shutdown did not retire the snapshot store")
	}

	close(bt.release)
	<-recoveryDone
}

// TestShutdownFastWhenNoOrphanRecovery pins the no-straggler path: with
// nothing tracked, Shutdown must not pay the bound at all.
func TestShutdownFastWhenNoOrphanRecovery(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	start := time.Now()
	a.Shutdown()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Shutdown with no orphan recovery took %v; should be near-instant", elapsed)
	}
}
