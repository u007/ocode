---
type: Concept
title: MCP Apps widgets and plugin MCP servers
description: How an MCP tool's ui:// widget is listed, called and rendered in the web/desktop chat (sandboxed srcdoc iframe, JSON-RPC over postMessage), which host methods are implemented, how plugin MCP servers merge into the config, and the known limits.
resource: internal/mcp/client.go
tags:
  - mcp
  - mcp-apps
  - plugins
  - web
timestamp: 2026-11-12T09:08:40Z
---
# MCP Apps widgets and plugin MCP servers

An MCP Apps widget is HTML that an MCP server declares for one of its tools. The
web and desktop chat renders it in a sandboxed iframe under the tool call. The
TUI has no widget: it shows the tool's text result only.

A plugin can also declare stdio MCP servers in a `.mcp.json`. Those servers are
merged into the in-memory config. User-facing rules for plugin servers are in
[plugins.md, Plugin MCP Servers](../plugins.md#plugin-mcp-servers); this page
summarises them and links to the code.

## What a widget is

A tool whose `tools/list` entry carries `_meta.ui.resourceUri` declares a widget.
The URI must start with `ui://` to be served (`internal/server/handler_mcp_app.go:93`).
ocode also reads the flat `ui/resourceUri` spelling some servers use
(`internal/mcp/client.go:1062`).

`_meta.ui.visibility` lists the audiences of the tool:

- `"model"`: the model may call the tool. An absent or empty list also means
  model-visible.
- `"app"`: a widget may call the tool through the host.

A tool whose visibility lacks `"model"` is not registered for the model at all
(`internal/mcp/client.go:1110`). It can be called only from a widget.

## End-to-end flow

1. **Listing.** `MCPClient.ListTools` records each listed tool's UI resource and
   visibility (`internal/mcp/client.go:1075`), then keeps only the model-visible
   tools.
2. **Model call.** The agent runs a model-visible tool through `ExecuteCtx`, which
   carries the tool call ID (`internal/mcp/client.go:90`). For a successful call of
   a tool with a UI resource, `recordCallOutcome` stores the raw result under that
   call ID (`internal/mcp/client.go:1239`).
3. **Attach to the tool message.** `handleToolCallWithContext` calls
   `keepMCPAppRef` (`internal/agent/agent.go:4022`). `keepMCPAppRef` keeps a
   `MCPAppRef` only when the call ran (`internal/agent/mcp_app.go:17`).
   `takeMCPAppRef` then puts it on the tool message as `Message.MCPApp`, and gives
   none when the result is a pending ask (`internal/agent/mcp_app.go:54`). The
   message is built at `internal/agent/agent.go:2164`.
4. **Stream.** The live path broadcasts a `tool_result` event carrying the ref
   (`internal/server/handler.go:1208`). Stream replay sends the same
   `ToolResultEvent` (`internal/server/handler_sse.go:286`; the type is at
   `internal/server/handler_sse.go:40`).
5. **Web card.** `ToolBlock` renders `McpAppView` under the block when the tool
   message has an `mcp_app` ref (`web/src/components/Chat/TurnParts.tsx:630`).
6. **Resource.** `McpAppView` fetches `GET /api/sessions/{id}/mcp-app/resource`
   (`web/src/api/client.ts:1885`). `HandleMCPAppResource` runs `resources/read` and
   returns the HTML inside JSON (`internal/server/handler_mcp_app.go:85`). The HTML
   is wrapped with a CSP meta by `buildMcpAppSrcDoc`
   (`web/src/components/Chat/mcpApp/mcpAppHost.ts:68`) and set as `srcdoc` on an
   iframe with `sandbox="allow-scripts"` (`web/src/components/Chat/mcpApp/McpAppView.tsx:365`).
7. **Protocol.** The protocol is JSON-RPC 2.0 over `postMessage`. The host accepts a
   message only when `event.origin` is `"null"` and `event.source` is the frame's
   window (`web/src/components/Chat/mcpApp/McpAppView.tsx:275`, `:277`). It then
   hands the message to `createMcpAppHost` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:115`).
8. **Widget tool call.** A widget `tools/call` request becomes
   `POST /api/sessions/{id}/mcp-app/tools/call` (`web/src/api/client.ts:1898`).
   `HandleMCPAppToolCall` checks `AppToolAllowed` (`internal/server/handler_mcp_app.go:200`)
   and calls `CallToolRaw` (`internal/server/handler_mcp_app.go:204`).

## Host methods

The host (`web/src/components/Chat/mcpApp/mcpAppHost.ts`) implements:

| Method | Kind | Behaviour |
| --- | --- | --- |
| `ui/initialize` | request | Answers with the protocol version the widget asked for, or `2026-01-26`. Returns `hostInfo`, `hostCapabilities` (`serverTools`, `openLinks`, `logging`) and `hostContext` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:145`). |
| `ui/notifications/initialized` | notification | Once per host. Sends `ui/notifications/tool-input` (the call's arguments) and `ui/notifications/tool-result` (the raw MCP result, or the tool's text output when there is none) (`web/src/components/Chat/mcpApp/mcpAppHost.ts:226`). |
| `tools/call` | request | Forwards to the tools/call endpoint. An upstream JSON-RPC error keeps its numeric code (`web/src/components/Chat/mcpApp/mcpAppHost.ts:159`). |
| `ui/open-link` | request | Accepts `http:` and `https:` only. The view never opens the URL itself: it stages a confirmation that the user answers with Open (`web/src/components/Chat/mcpApp/McpAppView.tsx:237`, `web/src/components/Chat/mcpApp/mcpAppHost.ts:189`). |
| `ui/request-display-mode` | request | `inline` or `fullscreen`. The switch happens on the next tick (`web/src/components/Chat/mcpApp/McpAppView.tsx:238`, `web/src/components/Chat/mcpApp/mcpAppHost.ts:207`). |
| `ui/notifications/size-changed` | notification | Sets the inline height, clamped. Ignored in fullscreen (`web/src/components/Chat/mcpApp/mcpAppHost.ts:237`). |
| `ping` | request | Empty result (`web/src/components/Chat/mcpApp/mcpAppHost.ts:216`). |
| `ui/notifications/host-context-changed` | notification, host to widget | Sent when the theme changes (`notifyContext`, `web/src/components/Chat/mcpApp/McpAppView.tsx:311`). |
| `ui/resource-teardown` | request, host to widget | Best effort on teardown, then the host stops posting (`web/src/components/Chat/mcpApp/mcpAppHost.ts:286`). |

Any other request gets `-32601`. Unknown notifications are ignored.

## Image results

An MCP tool result that contains an image block keeps its first decodable image
under the call ID: `summarizeToolResult` decodes it (`internal/mcp/client.go:1208`) and
`recordCallOutcome` stores it (`internal/mcp/client.go:1239`).
`handleToolCallWithImages` takes that image on every path, so nothing is kept
(`internal/agent/agent.go:3896`). It attaches the image to the tool message only
when the current model supports vision (`internal/agent/agent.go:3903`). Otherwise
the text result alone is returned.

Which provider builders carry the image, verified in `internal/agent/client.go`:

- **Anthropic** (`buildAnthropicMessages`, `internal/agent/client.go:4337`): the image
  is an image block inside the `tool_result`, when the client supports vision.
- **OpenAI chat completions** (`convertToOpenAIMessages`, `internal/agent/client.go:2847`):
  the images are queued and sent in one user message after the run of tool messages.
  That message starts with the preface `Image(s) returned by the preceding read tool
  call(s):` (`internal/agent/client.go:2866`), which is the read tool's wording and is
  used for MCP images too.
- **OpenAI Responses** (`internal/agent/client.go:3115`) and **Gemini Interactions**
  (`internal/agent/client.go:1740`): the tool result is text only. An MCP image does not
  reach the model on these paths.

Other providers were not checked against this list.

## Plugin MCP servers

- **Files.** `.mcp.json` at the plugin root, and the Claude Code manifest's
  `mcpServers` field. `pluginMCPServers` reads both (`internal/plugins/mcp_servers.go:171`).
- **Stdio only.** `stdioServer` skips anything with a `url`, a non-stdio `type`, or
  no command (`internal/plugins/mcp_servers.go:106`). Remote and OAuth-backed plugin
  servers are therefore never started.
- **In memory only.** `MergeMCPServers` (`internal/plugins/mcp_servers.go:282`) and
  `MergeSharedMCPServers` (`internal/plugins/mcp_servers.go:292`) replace `cfg.MCP` with
  a new map. Nothing is written to `opencode.json`.
- **User config wins.** A name already in `cfg.MCP` is skipped, enabled or disabled
  (`internal/plugins/mcp_servers.go:323`).
- **Project-local plugins.** A plugin under the project's own `.opencode/plugins`
  contributes servers only with an explicit enabled entry in `external_plugins`
  (`internal/plugins/mcp_servers.go:317`).
- **requiresAny.** Checked at merge time (`internal/plugins/mcp_servers.go:331`). A
  server whose requirement fails does not claim its name.
- **Web and desktop server.** The process-wide config merges only shared plugins:
  `internal/server/handler.go:741` at startup and `internal/server/handler.go:986` on
  reload. The MCP tool cache is built from that config (`internal/server/mcp_cache.go:30`).
  Each session merges its own project's plugins (`internal/server/agent_session.go:542`).
  When the merge adds a server the shared config lacks, the session enumerates its own
  tools (`internal/server/agent_session.go:543`, `internal/server/handler_mcp.go:188`).
  Otherwise it uses the shared cache (`internal/server/agent_session.go:707`).
- **Web toggle.** `HandleSetMCPEnabled` refuses a server in `h.pluginMCP`, which holds
  only the shared merge (`internal/server/handler_mcp.go:125`). A project-local server is
  not in `h.cfg.MCP`, so the toggle answers 404 for it (`internal/server/handler_mcp.go:120`).

## Stdio transport

- `NewLocalClient` starts the process (`internal/mcp/client.go:279`). The stdout reader
  `readStdout` (`internal/mcp/client.go:326`) is the only reader.
- Replies are routed by JSON-RPC id (`routeLocalLine`, `internal/mcp/client.go:911`).
  Notifications, server-initiated requests, stale and unsolicited lines are dropped.
- The handshake is `initialize` with protocol `2025-06-18` (`internal/mcp/client.go:99`).
  It advertises the `io.modelcontextprotocol/ui` extension with `text/html;profile=mcp-app`
  (`internal/mcp/client.go:831`). A server that answers `initialize` with `-32601` is
  treated as having no handshake (`handshakeLocked`, `internal/mcp/client.go:823`).
- Per-call side channels (images, MCP Apps results) are keyed by tool call ID in a
  bounded map of 64 entries (`internal/mcp/callside.go:20`, `internal/mcp/callside.go:39`).
  The fields are at `internal/mcp/client.go:181`.
- `MCPTool.Definition` returns only name, description and parameters
  (`internal/mcp/client.go:74`), so the tools array does not change with `_meta`.

## Security boundaries

- **No SPA origin.** The widget is a `srcdoc` document with `sandbox="allow-scripts"`
  only (`web/src/components/Chat/mcpApp/McpAppView.tsx:365`). The widget iframe never
  sets `allow-same-origin`; other iframes in the web UI do set it for their own purposes. The CSP starts with `default-src 'none'`
  (`web/src/components/Chat/mcpApp/mcpAppHost.ts:43`), then adds only the declared domains.
- **Self-navigation.** The first `load` is the srcdoc document. A later `load` tears the
  host down and closes the view (`web/src/components/Chat/mcpApp/McpAppView.tsx:288`).
- **Widget tool calls.** `AppToolAllowed` admits only tools the server listed with
  `"app"` in visibility (`internal/mcp/client.go:1133`). Both `/mcp-app/` endpoints refuse a
  locked session with 403 (`internal/server/handler_mcp_app.go:67`). Widget calls go straight
  to the MCP server. They do not pass through the agent's permission prompt, and they keep no
  image or app result under a call ID, because `CallToolRaw` records nothing
  (`internal/mcp/client.go:1178`).
- **Links.** `ui/open-link` waits for the user's confirmation, as every URL surface does.

## Known limits

- **TUI.** No widget. `internal/tui` has no MCP Apps code.
- **Remote and OAuth plugin servers** are not started (see `stdioServer` above).
- **Navigation window.** A widget that navigates itself can run script before the
  second `load` is detected. The host posts with target origin `"*"`
  (`web/src/components/Chat/mcpApp/McpAppView.tsx:219`), because an opaque-origin frame
  cannot be targeted by origin. A message sent in that window reaches whatever the frame
  now shows.
- **Client lifetime.** `MCPClient.Close` exists (`internal/mcp/client.go:1002`), but no
  production code calls it. Replacing the cache (`internal/server/handler.go:995`) leaves
  the old clients running, and so does each session's own enumeration. In
  `LoadMCPToolsForConfig`, a server whose `ListTools` call (`internal/agent/agent.go:7044`)
  fails is skipped with `continue`, without Close, so its process keeps running.
- **Widget calls** are not permission-checked (see Security boundaries).
- **Image providers.** Images reach the model only through the Anthropic and OpenAI
  chat-completions builders, and only on a vision-capable model.
