package tui

import (
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/tool"
)

// testPreloadedContext is the stub "Context and rules" block every test agent
// starts with. It must be non-empty: an empty preloaded context means "not
// preloaded" and sends BasePromptMessages back through agent.LoadContext.
const testPreloadedContext = "\n--- test context ---\n(preloaded by newTestAgent)\n"

// newTestAgent is agent.NewAgent plus a preloaded context. Every sidebar
// render goes currentContextEstimate → buildAgentMessagesSnapshot →
// BasePromptMessages, and without a preloaded context that runs
// agent.LoadContext, which spawns git about five times per call (git show /
// git diff for each context file, git rev-parse for memory). With the Xcode
// git shim at ~130ms a spawn, one render cost ~0.5s and the sidebar / permission
// dialog tests took 10–50s each. Tests that exercise the real loader (askAgent
// preloads it itself, handleNewCmd clears it) are unaffected: both paths
// overwrite the stub.
func newTestAgent(client agent.LLMClient, tools []tool.Tool, cfg *config.Config, lspMgr *lsp.Manager) *agent.Agent {
	a := agent.NewAgent(client, tools, cfg, lspMgr)
	a.SetPreloadedContext(testPreloadedContext)
	return a
}

// newTestModel is newModel plus the same preloaded context on the model's
// agent, for tests that go through the real constructor instead of a literal.
func newTestModel(opts ...RunOptions) model {
	m := newModel(opts...)
	if m.agent != nil {
		m.agent.SetPreloadedContext(testPreloadedContext)
	}
	return m
}
