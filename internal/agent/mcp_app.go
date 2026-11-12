package agent

import (
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/mcp"
	"github.com/u007/ocode/internal/tool"
)

// keepMCPAppRef takes the MCP Apps result of the call toolCallID named, for the
// tool name. It always takes, so a result cannot outlive its call. The ref is
// kept under toolCallID only when the call ran on the server (ran is reported
// by the tool); a call the user or a gate refused never ran, and gets no widget.
// It is called from handleToolCallWithContext, the one path every execution
// goes through.
func (a *Agent) keepMCPAppRef(name, toolCallID string) {
	t, ok := a.tools[name].(tool.MCPAppTool)
	if !ok || t.MCPAppResource() == "" {
		return
	}
	raw, ran := t.TakeMCPAppResult(toolCallID)
	if !ran || toolCallID == "" {
		return
	}
	st, ok := a.tools[name].(tool.MCPServerTool)
	if !ok {
		return
	}
	server := st.MCPServer()
	a.mcpAppRefs.Store(toolCallID, &MCPAppRef{
		Server:      server,
		Tool:        strings.TrimPrefix(name, server+"_"),
		ResourceURI: t.MCPAppResource(),
		Result:      raw,
	})
}

// takeMCPCallImage takes the image that the call toolCallID returned, when name
// is an MCP tool that can return one. The image is removed whether or not the
// caller uses it.
func (a *Agent) takeMCPCallImage(name, toolCallID string) ([]byte, string, bool) {
	ct, ok := a.tools[name].(tool.MCPCallImageTool)
	if !ok {
		return nil, "", false
	}
	return ct.TakeCallImage(toolCallID)
}

// takeMCPAppRef returns and removes the MCP Apps ref kept for toolCallID. It
// returns nil when result is a permission or question ask: the call did not
// complete, so its widget must not attach to the message. The ref is removed
// either way.
func (a *Agent) takeMCPAppRef(toolCallID, result string) *MCPAppRef {
	v, ok := a.mcpAppRefs.LoadAndDelete(toolCallID)
	if !ok || tool.UnansweredAsk(result) {
		return nil
	}
	return v.(*MCPAppRef)
}

// MCPClientForServer returns the live MCP client behind this agent's tools of
// the named server, or nil when there is none. The web server serves MCP Apps
// widgets on the same connection the agent's tools use.
func (a *Agent) MCPClientForServer(server string) *mcp.MCPClient {
	a.toolsMu.RLock()
	defer a.toolsMu.RUnlock()
	for name := range a.mcpTools {
		mt, ok := a.tools[name].(mcp.MCPTool)
		if ok && mt.MCPServer() == server {
			return mt.Client()
		}
	}
	return nil
}

// PermissionMode returns the agent's current permission mode. It is the same
// unlocked read Step makes, so an HTTP path can call it without the session's
// turn lock. An agent always has a permission manager; a bare test agent that
// has none reports normal.
func (a *Agent) PermissionMode() PermissionMode {
	if a.permissions == nil {
		return PermissionModeNormal
	}
	return a.permissions.Mode()
}

// Config returns the configuration this agent was built with, read-only. It is
// the config its MCP tools are loaded from (see LoadMCPTools).
func (a *Agent) Config() *config.Config {
	return a.config
}
