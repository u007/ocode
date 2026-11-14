package server

import (
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// mcpCache holds the process-wide MCP tool enumeration result. MCP server
// config is identical for every session on this process (see Handler.cfg),
// so the blocking connect/handshake/ListTools work MCP requires is done once
// via warm() instead of once per session's first message. Every agentSession
// creation site applies the cached result via wait() before starting the
// agent's first Step.
type mcpCache struct {
	ready chan struct{}
	tools []tool.Tool
	errs  []string
}

func newMCPCache() *mcpCache {
	return &mcpCache{ready: make(chan struct{})}
}

// warm kicks off the blocking MCP enumeration in the background. Call once,
// as early as possible (Handler construction), so it has usually finished
// by the time a real user message arrives.
//
// It uses the process-wide shared client registry: a reload of an unchanged
// server reuses its live client instead of spawning a second subprocess, and a
// removed or changed server's client is retired by the loader itself.
func (c *mcpCache) warm(cfg *config.Config) {
	go func() {
		res := agent.LoadMCPToolsForConfigShared(cfg)
		c.tools = res.Tools
		c.errs = res.Errors
		close(c.ready)
	}()
}

// wait blocks until the background enumeration has completed and returns its
// result. It returns immediately once warm() has finished, which is the
// common case since warm() starts at process boot.
func (c *mcpCache) wait() ([]tool.Tool, []string) {
	<-c.ready
	return c.tools, c.errs
}

// waitTimeout returns the MCP tools with a bounded wait. When the background
// enumeration has not finished within timeout, it returns nil tools/errors and
// timedOut=true so the caller can proceed without stragglers and emit a
// warning event instead of hanging the session bootstrap. The nil return on
// timeout avoids a data race on c.tools with the still-running warm goroutine.
func (c *mcpCache) waitTimeout(timeout time.Duration) ([]tool.Tool, []string, bool) {
	select {
	case <-c.ready:
		return c.tools, c.errs, false
	case <-time.After(timeout):
		return nil, nil, true
	}
}

// toolsNow reports how many tools this cache holds right now, without waiting:
// 0 while the enumeration is still in flight. It is the non-blocking read used
// by the reload's swap decision, where waiting on a straggling warm would
// re-introduce the unbounded stall the async protocol exists to remove.
func (c *mcpCache) toolsNow() int {
	select {
	case <-c.ready:
		return len(c.tools)
	default:
		return 0
	}
}
