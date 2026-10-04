package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// Mimic the REAL flow for a flagged result: guardToolResult -> (as agent.go:2019-2021
// does) TruncateToolResult -> then the host parses the sentinel.
func TestZZProbeEndToEndBigContent(t *testing.T) {
	h := &contentGuardHarness{verdict: contentGuardVerdictFlagged, confidence: 0.95, concern: "data_exfiltration"}
	a := newContentGuardAgent(t, h)
	a.mcpTools = map[string]struct{}{"some_mcp": {}}

	for _, size := range []int{1000, 11000, 15000, 192 * 1024} {
		body := strings.Repeat("y", size)
		sentinel := a.guardToolResult(context.Background(), "some_mcp", `{}`, body)
		if !strings.HasPrefix(sentinel, tool.SentinelPermissionAsk) {
			t.Fatalf("size=%d: not a sentinel", size)
		}
		shaped := TruncateToolResult("someid", sentinel)
		req, ok := parseContentAsk(shaped)
		t.Logf("size=%d sentinelLen=%d afterTruncate=%d parses=%v tool=%q contentLen=%d",
			size, len(sentinel), len(shaped), ok, req.ToolName, len(req.UntrustedContent))
	}
	_ = config.Config{}
}
