package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// blockingCtxTool runs until its context ends, the way a browser action waits
// on a CDP reply.
type blockingCtxTool struct {
	started chan struct{}
	once    sync.Once
	failErr error
}

func (b *blockingCtxTool) Name() string        { return "block_until_done" }
func (b *blockingCtxTool) Description() string { return "test" }
func (b *blockingCtxTool) Parallel() bool      { return false }
func (b *blockingCtxTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "block_until_done"}
}
func (b *blockingCtxTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (b *blockingCtxTool) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	b.once.Do(func() { close(b.started) })
	if b.failErr != nil {
		return "", b.failErr
	}
	<-ctx.Done()
	return "", ctx.Err()
}

// TestHandleToolCallWithImagesStopCancelsRunningTool pins that cancelling the
// turn's context stops a running tool, and that the cancelled call still gets a
// tool result (ToolCancelledResult, nil error) rather than an orphan.
func TestHandleToolCallWithImagesStopCancelsRunningTool(t *testing.T) {
	bt := &blockingCtxTool{started: make(chan struct{})}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.Permissions().SetRule("block_until_done", PermissionAllow)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		text, _, err := ag.handleToolCallWithImages(ctx, "block_until_done", json.RawMessage(`{}`), nil, "tc-stop")
		done <- outcome{text, err}
	}()
	<-bt.started
	cancel()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("cancelled call returned error %v, want a tool result", got.err)
		}
		if got.text != tool.ToolCancelledResult {
			t.Fatalf("cancelled call text = %q, want ToolCancelledResult", got.text)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not interrupt the running tool")
	}
}

// TestHandleToolCallWithImagesOrdinaryErrorStillReturned pins that a failure
// that is not a cancellation keeps its error: only cancellation is rewritten.
func TestHandleToolCallWithImagesOrdinaryErrorStillReturned(t *testing.T) {
	bt := &blockingCtxTool{started: make(chan struct{}), failErr: errors.New("chrome said no")}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.Permissions().SetRule("block_until_done", PermissionAllow)

	_, _, err := ag.handleToolCallWithImages(context.Background(), "block_until_done", json.RawMessage(`{}`), nil, "tc-err")
	if err == nil || err.Error() != "chrome said no" {
		t.Fatalf("ordinary error = %v, want it returned", err)
	}
}
