package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// Probe: does a large content-ask sentinel survive TruncateToolResult parse?
func TestZZProbeSentinelTruncation(t *testing.T) {
	big := strings.Repeat("x", 192*1024)
	sentinel := tool.SentinelPermissionAsk + `{"tool_name":"some_mcp","scope":"content","rule":"content.some_mcp","untrusted_source":"MCP some_mcp","untrusted_content":"` + big + `"}`
	trunc := TruncateToolResult("probeid", sentinel)
	t.Logf("sentinel len=%d truncated len=%d hasPrefix=%v", len(sentinel), len(trunc), strings.HasPrefix(trunc, tool.SentinelPermissionAsk))
	req, ok := parseSentinelForProbe(trunc)
	t.Logf("parse ok=%v req.ToolName=%q", ok, req.ToolName)
}

func parseSentinelForProbe(content string) (PermissionRequest, bool) {
	var req PermissionRequest
	payload := strings.TrimPrefix(content, tool.SentinelPermissionAsk)
	if payload == content || payload == "" {
		return req, false
	}
	if err := jsonUnmarshalProbe(payload, &req); err != nil {
		return req, false
	}
	return req, req.ToolName != ""
}
