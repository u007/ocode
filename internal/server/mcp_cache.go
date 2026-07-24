package server

import (
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
func (c *mcpCache) warm(cfg *config.Config) {
	go func() {
		res := agent.LoadMCPToolsForConfig(cfg)
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
