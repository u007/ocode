# Spec: Zed-compatible ACP mode (`ocode acp`)

Status: draft, approved for implementation
Owner: james
Scope: rewrite of `internal/acp` so `ocode acp` speaks the real Agent Client
Protocol (ACP, agentclientprotocol.com) and ocode appears in Zed's agent panel
like Claude Code / Codex / Gemini CLI.

## Background

The current `internal/acp/acp.go` implements a homegrown line-delimited JSON
protocol (`{"type":"message",...}` → `{"type":"response",...}`). It is not the
Agent Client Protocol: Zed's first message (JSON-RPC `initialize`) is rejected
with "unknown message type". Nothing else in the repo or the VS Code plugin
consumes the old protocol, so it is replaced outright — no compatibility shim.

There is **no official Go SDK** for ACP (the `agent-client-protocol` repo is
Rust + TypeScript only; the Go module path resolves but contains no Go files).
The JSON-RPC layer and protocol types are hand-rolled inside `internal/acp`.

## Protocol surface

Transport: JSON-RPC 2.0, newline-delimited JSON over stdio. stdin = client→agent,
stdout = agent→client. stdout carries protocol frames ONLY; all diagnostics go
to stderr (headless mode — `emitDebug`'s stderr fallback is correct here).
Writes to stdout are serialized behind a single mutex/encoder because agent
callbacks fire from HTTP goroutines.

Target protocol version: `1` (integer, per ACP schema).

### Agent-side methods (ocode implements, Zed calls)

| Method | Behaviour |
|---|---|
| `initialize` | Negotiate version (respond with min(client, 1)); return `agentCapabilities` (`loadSession: false` for v1, `promptCapabilities: { embeddedContext: true, image: false, audio: false }`), empty `authMethods` (ocode auth comes from its own config/keychain), and `agentInfo` (name "ocode", version from `internal/version`). |
| `authenticate` | Always returns method-not-supported error; never advertised. |
| `session/new` | Create agent + empty history (reuse current `getOrCreateSession` construction: `agent.NewClient`, `tool.LoadBuiltins`, `LoadExternalTools`). Honor `cwd` param as the session working directory. Accept and ignore `mcpServers` for v1 (ocode loads MCP from its own config; documented limitation). Returns generated `sessionId` (reuse `session.NewSessionID`). |
| `session/load` | Not supported in v1 (`loadSession: false`); returns error if called. |
| `session/prompt` | One turn. Flatten prompt content blocks → user message (see below), run `agent.Step` on a goroutine, stream `session/update` notifications, reply with `stopReason` when the turn ends. Reject a second concurrent prompt for the same session. |
| `session/cancel` (notification) | Trigger the agent's stop channel; the in-flight `session/prompt` must still resolve, with `stopReason: "cancelled"`. |
| `session/set_mode` | Optional; not in v1. |

Errors use standard JSON-RPC codes: `-32700` parse error, `-32601` method not
found, `-32602` invalid params, `-32603` internal. Unknown **notifications**
are ignored silently (no error reply), per JSON-RPC.

### Prompt content mapping (client → agent)

- `text` blocks → concatenated into the user message.
- `resource` blocks (embedded context: file Zed @-mentioned, with full text) →
  appended as fenced context sections labeled with the file URI, after the text.
- `resource_link` blocks → appended as a one-line reference (URI only); the
  agent can read it with its own Read tool.
- `image`/`audio` → rejected via capabilities (never sent by Zed when
  capabilities say unsupported).

### Streaming updates (agent → client, `session/update` notifications)

Bridged from the existing agent callbacks; this is the heart of the rewrite:

| ocode hook | ACP update `sessionUpdate` kind |
|---|---|
| `OnDelta(kind="text")` | `agent_message_chunk` (text content block) |
| `OnDelta(kind="reasoning")` | `agent_thought_chunk` |
| `OnMessage` assistant message containing tool calls | `tool_call` (status `pending`) per call — carries `toolCallId` (LLM call id), `title` (tool name + primary arg), and a `kind` mapped from the ocode tool name (`read`→read, `write`/`edit`→edit, `bash`→execute, `grep`/`glob`→search, `webfetch`→fetch, else `other`) |
| tool execution start/finish (wrapping `HandleToolCall` inside the Step loop is NOT possible from outside; instead derive from `OnMessage` tool-result messages) | `tool_call_update` → status `completed` with the result text as content, or `failed` on error result |
| `OnUsage` | not part of ACP v1 updates; ignored |
| Plan/todo updates | out of scope v1 |

Because deltas already streamed the assistant text, the final assistant message
from `OnMessage` is NOT re-emitted as a chunk (would duplicate). The bridge
tracks whether any delta arrived for the current turn; if the provider streamed
nothing (non-streaming client), emit the full text as one `agent_message_chunk`
at message time.

`stopReason` mapping: normal completion → `end_turn`; cancel → `cancelled`;
step error → JSON-RPC error on the `session/prompt` call itself.

### Permission flow

Set `OnPermissionAsk` on the main agent (same approach as `runcli` and
sub-agents — the comment in `agent.go` confirms `HandleToolCall` acts directly
on the returned level when the hook is set, bypassing the TUI sentinel).

The hook issues a **client-bound JSON-RPC request** `session/request_permission`
with options: allow-once, allow-always, reject-once. Map the outcome:
`selected: allow-once/always` → `PermissionAllow` (always additionally persists
via the existing auto-grant path only if ocode's permission manager supports it;
otherwise treat as allow-once for v1), `cancelled`/reject → `PermissionDeny`.
The hook blocks until Zed responds — this requires the stdin read loop to keep
dispatching responses while a prompt turn is in flight (read loop never blocks
on turn completion).

### Client-bound fs methods

`fs/read_text_file` / `fs/write_text_file` (unsaved-buffer passthrough) are
**out of scope for v1** — ocode tools read from disk as today. Tracked as a
follow-up in TODO.md because it is the headline editor-context feature.

## Architecture / files

- `internal/acp/acp.go` — entry (`Run`), stdio transport, JSON-RPC framing,
  request dispatch, outbound write serialization, client-bound request
  bookkeeping (id ↔ pending response channel).
- `internal/acp/types.go` — protocol structs (initialize, session/*, content
  blocks, tool-call updates). Hand-written from the ACP schema, only the
  fields ocode uses.
- `internal/acp/bridge.go` — per-session state: agent construction, callback
  wiring (deltas, messages, permission), prompt-turn lifecycle, cancel.
- `internal/acp/acp_test.go` — rewritten tests (see below).
- `main.go` — help text: "Agent Client Protocol" (fix the wrong expansion).
- `README.md` `internal/acp/` line (currently mislabeled "Anthropic prompt
  caching") + `skills/ocode-usage/SKILL.md` acp rows.
- `docs/zed.md` — new user doc: Zed `agent_servers` config snippet, what works,
  limitations (no unsaved-buffer fs, MCP comes from ocode config).

Session persistence stays as today (`session.Save` after each turn) so ACP
sessions appear in the normal session picker.

## Testing

Unit tests drive `Run` over in-memory pipes with a fake LLM client (existing
test seams in `internal/agent` / `acp_test.go` patterns):

1. Handshake: `initialize` → correct version, capabilities, agentInfo.
2. `session/new` → sessionId returned; `cwd` respected.
3. Prompt turn: ordering is `session/update` chunks → `tool_call` →
   `tool_call_update(completed)` → prompt response `end_turn`.
4. Non-streaming fallback emits exactly one full-text chunk (no duplication).
5. `session/cancel` mid-turn → prompt resolves `cancelled`.
6. Permission round-trip: tool requiring permission → `session/request_permission`
   issued → client allows → tool runs; client rejects → tool denied.
7. Protocol errors: malformed JSON (-32700), unknown method (-32601), prompt
   for unknown session (-32602), concurrent prompt rejected.

Manual verification: Zed `settings.json` `agent_servers` entry pointing at the
dev binary; confirm panel chat, tool-call rendering, permission dialog, cancel.

## Out of scope (v1, tracked in TODO.md)

- `fs/read_text_file` / `fs/write_text_file` (unsaved buffer contents)
- `session/load` (history replay), `session/set_mode`
- Plan (`plan` update kind), slash commands (`available_commands_update`)
- Forwarding Zed-configured `mcpServers` into ocode's MCP manager
- Image/audio prompt blocks, terminal capability
