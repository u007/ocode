---
type: Concept
title: MCP Apps widgets and plugin MCP servers
description: How an MCP tool's ui:// widget is listed, called and rendered in the web/desktop chat (sandboxed srcdoc iframe, JSON-RPC over postMessage), which host methods are implemented, how ui/open-link enters the shared owner-aware URL chooser and reports chooser failures, how plugin MCP servers merge into the config, and the known limits.
resource: internal/mcp/client.go
tags:
  - mcp
  - mcp-apps
  - plugins
  - web
timestamp: 2026-11-25T17:23:00Z
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
(`internal/mcp/client.go:1108`).

`_meta.ui.visibility` lists the audiences of the tool:

- `"model"`: the model may call the tool. An absent or empty list also means
  model-visible.
- `"app"`: a widget may call the tool through the host.

A tool whose visibility lacks `"model"` is not registered for the model at all
(`internal/mcp/client.go:1172`). It can be called only from a widget.

## End-to-end flow

1. **Listing.** `MCPClient.ListTools` records each listed tool's UI resource and
   visibility (`internal/mcp/client.go:1137`), then keeps only the model-visible
   tools.
2. **Model call.** The agent runs a model-visible tool through `ExecuteCtx`, which
   carries the tool call ID (`internal/mcp/client.go:90`). For a successful call of
   a tool with a UI resource, `recordCallOutcome` stores the raw result under that
   call ID (`internal/mcp/client.go:1301`).
3. **Attach to the tool message.** `handleToolCallWithContext` calls
   `keepMCPAppRef` (`internal/agent/agent.go:4269`). `keepMCPAppRef` keeps a
   `MCPAppRef` only when the call ran (`internal/agent/mcp_app.go:17`).
   `takeMCPAppRef` then puts it on the tool message as `Message.MCPApp`, and gives
   none when the result is a pending ask (`internal/agent/mcp_app.go:54`). The
   tool message takes the ref at `internal/agent/agent.go:2332`.
4. **Stream.** The live path broadcasts a `tool_result` event carrying the ref
   (`internal/server/handler.go:1269`). Stream replay sends the same
   `ToolResultEvent` (`internal/server/handler_sse.go:289`; the type is at
   `internal/server/handler_sse.go:40`).
5. **Web card.** `ToolBlock` renders `McpAppView` under the block when the tool
   message has an `mcp_app` ref (`web/src/components/Chat/TurnParts.tsx:657`).
6. **Resource.** `McpAppView` fetches `GET /api/sessions/{id}/mcp-app/resource`
   (`web/src/api/client.ts:1928`). `HandleMCPAppResource` runs `resources/read` and
   returns the HTML inside JSON (`internal/server/handler_mcp_app.go:85`). The HTML
   is wrapped with a CSP meta by `buildMcpAppSrcDoc`
   (`web/src/components/Chat/mcpApp/mcpAppHost.ts:68`) and set as `srcdoc` on an
   iframe with `sandbox="allow-scripts"` (`web/src/components/Chat/mcpApp/McpAppView.tsx:375`).
7. **Protocol.** The protocol is JSON-RPC 2.0 over `postMessage`. The host accepts a
   message only when `event.origin` is `"null"` and `event.source` is the frame's
   window (`web/src/components/Chat/mcpApp/McpAppView.tsx:286`,
   `web/src/components/Chat/mcpApp/McpAppView.tsx:288`). It then
   hands the message to `createMcpAppHost` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:115`).
8. **Widget tool call.** A widget `tools/call` request becomes
   `POST /api/sessions/{id}/mcp-app/tools/call` (`web/src/api/client.ts:1941`).
   `HandleMCPAppToolCall` checks `AppToolAllowed` (`internal/server/handler_mcp_app.go:200`)
   and calls `CallToolRaw` (`internal/server/handler_mcp_app.go:204`).

## Host methods

The host (`web/src/components/Chat/mcpApp/mcpAppHost.ts`) implements:

| Method | Kind | Behaviour |
| --- | --- | --- |
| `ui/initialize` | request | Answers with the protocol version the widget asked for, or `2026-01-26`. Returns `hostInfo`, `hostCapabilities` (`serverTools`, `openLinks`, `logging`) and `hostContext` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:145`). |
| `ui/notifications/initialized` | notification | Once per host. Sends `ui/notifications/tool-input` (the call's arguments) and `ui/notifications/tool-result` (the raw MCP result, or the tool's text output when there is none) (`web/src/components/Chat/mcpApp/mcpAppHost.ts:231`). |
| `tools/call` | request | Forwards to the tools/call endpoint. An upstream JSON-RPC error keeps its numeric code (`web/src/components/Chat/mcpApp/mcpAppHost.ts:159`). |
| `ui/open-link` | request | Accepts `http:` and `https:` only. The host calls `opts.openLink(url)` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:189`, `web/src/components/Chat/mcpApp/mcpAppHost.ts:204`), wired to the owner-aware `requestURLOpen`. This enters the shared [URL chooser](url-open-chooser.md); success returns `{ result: {} }` after dispatch, not after the user chooses. A chooser refusal returns JSON-RPC `-32000`. There is no inline Open/Cancel row. |
| `ui/request-display-mode` | request | `inline` or `fullscreen`. The switch happens on the next tick (`web/src/components/Chat/mcpApp/McpAppView.tsx:249`, `web/src/components/Chat/mcpApp/mcpAppHost.ts:218`). |
| `ui/notifications/size-changed` | notification | Sets the inline height, clamped. Ignored in fullscreen (`web/src/components/Chat/mcpApp/mcpAppHost.ts:242`). |
| `ping` | request | Empty result (`web/src/components/Chat/mcpApp/mcpAppHost.ts:221`). |
| `ui/notifications/host-context-changed` | notification, host to widget | Sent when the theme changes (`notifyContext`, `web/src/components/Chat/mcpApp/McpAppView.tsx:321`). |
| `ui/resource-teardown` | request, host to widget | Best effort on teardown, then the host stops posting (`web/src/components/Chat/mcpApp/mcpAppHost.ts:288`). |

Any other request gets `-32601`. Unknown notifications are ignored.

### `ui/open-link` chooser failures

`McpAppView` reads the enclosing `URLOpenOwner`
(`web/src/components/Chat/mcpApp/McpAppView.tsx:140`) and passes it to
`requestURLOpen` (`web/src/components/Chat/mcpApp/McpAppView.tsx:237`). If the
chooser has no listener or is already open, `requestURLOpen` throws.
`McpAppView` catches that failure, logs `server`, `tool` and `error` — not the URL —
and rethrows (`web/src/components/Chat/mcpApp/McpAppView.tsx:241`).
`createMcpAppHost` catches the synchronous `openLink` error and responds with
`ERR_DEFAULT` (`-32000`) instead of leaving the widget waiting
(`web/src/components/Chat/mcpApp/mcpAppHost.ts:203`,
`web/src/components/Chat/mcpApp/mcpAppHost.ts:206`). The success response remains
`{ result: {} }` (`web/src/components/Chat/mcpApp/mcpAppHost.ts:209`).

## Image results

An MCP tool result that contains an image block keeps its first decodable image
under the call ID: `summarizeToolResult` decodes it (`internal/mcp/client.go:1270`) and
`recordCallOutcome` stores it (`internal/mcp/client.go:1301`).
`handleToolCallWithImages` consumes the per-call image on every path, so nothing
is retained (`internal/agent/agent.go:4135`). It attaches the image to the tool
message only when the current model supports vision
(`internal/agent/agent.go:4142`). Otherwise the text result alone is returned.

Which provider builders carry the image, verified in `internal/agent/client.go`:

- **Anthropic** (`buildAnthropicMessages`, `internal/agent/client.go:4377`): the image
  is an image block inside the `tool_result`, when the client supports vision.
- **OpenAI chat completions** (`convertToOpenAIMessages`, `internal/agent/client.go:2901`):
  the images are queued and sent in one user message after the run of tool messages.
  That message starts with the preface `Image(s) returned by the preceding read tool
  call(s):` (`internal/agent/client.go:2920`), which is the read tool's wording and is
  used for MCP images too.
- **OpenAI Responses** (`internal/agent/client.go:3139`) and **Gemini Interactions**
  (`internal/agent/client.go:1618`): the tool result is text only. An MCP image does not
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
  `internal/server/handler.go:793` at startup and `internal/server/handler.go:1051` on
  reload. The MCP tool cache is built from that config (`internal/server/mcp_cache.go:34`).
  Each session merges its own project's plugins (`internal/server/agent_session.go:551`).
  When the merge adds a server the shared config lacks, the session enumerates its own
  tools (`internal/server/agent_session.go:746`, `internal/server/handler_mcp.go:205`).
  Otherwise it uses the shared cache (`internal/server/agent_session.go:754`).
- **Web toggle.** `HandleSetMCPEnabled` refuses a server in `h.pluginMCP`, which holds
  only the shared merge (`internal/server/handler_mcp.go:126`). A project-local server is
  not in `h.cfg.MCP`, so the toggle answers 404 for it (`internal/server/handler_mcp.go:123`).

## Stdio transport

- `NewLocalClient` starts the process (`internal/mcp/client.go:312`). The stdout reader
  `readStdout` (`internal/mcp/client.go:359`) is the only reader.
- Replies are routed by JSON-RPC id (`routeLocalLine`, `internal/mcp/client.go:972`).
  Notifications, server-initiated requests, stale and unsolicited lines are dropped.
- The handshake is `initialize` with protocol `2025-06-18` (`internal/mcp/client.go:99`).
  It advertises the `io.modelcontextprotocol/ui` extension with `text/html;profile=mcp-app`
  (`internal/mcp/client.go:892`). A server that answers `initialize` with `-32601` is
  treated as having no handshake (`handshakeLocked`, `internal/mcp/client.go:884`).
- Per-call side channels (images, MCP Apps results) are keyed by tool call ID in a
  bounded map of 64 entries (`internal/mcp/callside.go:20`, `internal/mcp/callside.go:39`).
  The fields are at `internal/mcp/client.go:192`.
- `MCPTool.Definition` returns only name, description and parameters
  (`internal/mcp/client.go:74`), so the tools array does not change with `_meta`.

## Security boundaries

- **No SPA origin.** The widget is a `srcdoc` document with `sandbox="allow-scripts"`
  only (`web/src/components/Chat/mcpApp/McpAppView.tsx:375`). The widget iframe never
  sets `allow-same-origin`; other iframes in the web UI do set it for their own purposes. The CSP starts with `default-src 'none'`
  (`web/src/components/Chat/mcpApp/mcpAppHost.ts:43`), then adds only the declared domains.
- **Self-navigation.** The first `load` is the srcdoc document. A later `load` tears the
  host down and closes the view (`web/src/components/Chat/mcpApp/McpAppView.tsx:297`).
- **Widget tool calls.** `AppToolAllowed` admits only tools the server listed with
  `"app"` in visibility (`internal/mcp/client.go:1195`). Both `/mcp-app/` endpoints refuse a
  locked session with 403 (`internal/server/handler_mcp_app.go:67`). Widget calls go straight
  to the MCP server. They do not pass through the agent's permission prompt, and they keep no
  image or app result under a call ID, because `CallToolRaw` records nothing
  (`internal/mcp/client.go:1240`).
- **Links.** `ui/open-link` validates HTTP(S), then enters the same owner-aware
  app-level chooser as every ordinary clickable HTTP(S) URL. The owner comes from
  the enclosing chat tab, so a remote chat's `localhost` link stays on that
  remote host's browse origin. The first request wins; an already-open chooser
  refuses a second request rather than replacing the pending URL. A refusal is
  logged without the URL and returned to the widget as `-32000`. The host does
  not navigate the SPA or open a popup for the widget, and there is no second
  inline confirmation; see [Clickable HTTP(S) URL chooser](url-open-chooser.md).

## Known limits

- **TUI.** No widget. `internal/tui` has no MCP Apps code.
- **Remote and OAuth plugin servers** are not started (see `stdioServer` above).
- **Navigation window.** A widget that navigates itself can run script before the
  second `load` is detected. The host posts with target origin `"*"`
  (`web/src/components/Chat/mcpApp/McpAppView.tsx:219`), because an opaque-origin frame
  cannot be targeted by origin. A message sent in that window reaches whatever the frame
  now shows.
- **Client lifetime.** `MCPClient.Close` (`internal/mcp/client.go:1064`) is called by the
  shared loader (`internal/agent/mcp_clients.go`) when a client's config changed, its
  server was removed, or its `ListTools` failed; `CloseAllMCPClients` closes every cached
  client at server shutdown (`internal/server/handler_shutdown.go:30`). Per-session
  override enumerations still use fresh clients (`LoadMCPToolsForConfig`) and are not in
  the registry.
- **Widget calls** are not permission-checked (see Security boundaries).
- **Image providers.** Images reach the model only through the Anthropic and OpenAI
  chat-completions builders, and only on a vision-capable model.
