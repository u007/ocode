package agent

import (
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/tool"
)

// testPreloadedContext is the stub "Context and rules" block every
// newTestAgent starts with. Non-empty on purpose: an empty preloaded context
// means "not preloaded" and sends BasePromptMessages back through LoadContext.
const testPreloadedContext = "\n--- test context ---\n(preloaded by newTestAgent)\n"

// newTestAgent is NewAgent plus a preloaded context. Every Step goes
// PrepareMessages → BasePromptMessages → LoadContext, which spawns git about
// five times per call (git show / git diff per context file, git rev-parse for
// memory); with the Xcode git shim at ~130ms a spawn that is ~0.5s per Step.
// Tests that assert on the real loader (context_test.go, prompt_*_test.go,
// agent_test.go and friends) keep calling NewAgent directly.
func newTestAgent(client LLMClient, tools []tool.Tool, cfg *config.Config, lspMgr *lsp.Manager) *Agent {
	a := NewAgent(client, tools, cfg, lspMgr)
	a.SetPreloadedContext(testPreloadedContext)
	return a
}

// truncate shortens s for log lines. Shared by the integration-tagged tests
// and permission_controlflow_test.go, so it lives in an untagged file.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
