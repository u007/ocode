package server

import (
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// An empty projectRoot must bind the agent to the server's project dir, never
// leave workDir empty (which falls back to the process cwd — "/" for the
// desktop .app — and makes relative tool paths fail confinement).
func TestBuildAgentSessionEmptyProjectRootUsesHandlerWorkDir(t *testing.T) {
	h := NewHandler()
	h.workDir = t.TempDir()
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession("sess-wd", "opencode-go/deepseek-v4-flash", nil, "")
	if err != nil {
		t.Fatalf("buildAgentSession: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := as.agent.WorkDir(); got != h.workDir {
		t.Fatalf("agent workDir = %q, want %q", got, h.workDir)
	}
}
