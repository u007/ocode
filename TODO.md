# TODO

## Auth — deferred work

- **macOS Keychain backend.** File store at `~/.config/ocode/auth.json` (0600) is what ships. A self-contained `internal/auth/keyring_darwin.go` could shell out to `security` with a file fallback.
- **Background token-refresh goroutine.** Refresh is currently lazy on `HydrateEnv` + `ResolveKey`/`OAuthAccessToken`. A goroutine would help only for sessions that idle longer than a token lifetime without any tool use.
- **Per-provider base-URL override UI.** `Credential.BaseURL` is honoured by `NewClient` but there's no dialog stage to set it — populate `~/.config/ocode/auth.json` by hand for now.
- **Account population for Anthropic / OpenAI OAuth.** Copilot populates `Credential.Account` via `GET /user`. The Anthropic/OpenAI token responses don't reliably include an email; would need an extra `/me` or JWT `id_token` parse.

## Separated Agent System — core implementation complete; remaining work

Core infrastructure complete (2026-05-19):
- ✅ Agent registry (`internal/agent/agent_registry.go`) with agent definitions and lifecycle
- ✅ Agent permissions system (`internal/agent/agent_permissions.go`) with per-agent rules
- ✅ Child session tracking (`internal/agent/child_session.go`) with ID and metadata generation
- ✅ Agent loader (`internal/agent/agent_loader.go`) for filesystem-based agent definitions
- ✅ TaskTool updated to use registry and support hidden agents
- ✅ Child session persistence callback infrastructure (`Agent.SetChildSessionPersistence()`)

Remaining integration work:
- **Wire child session persistence callback.** `Agent.SetChildSessionPersistence()` needs to be called in `internal/runcli/run.go`, `internal/server/handler.go`, and `internal/tui/connect.go` (in `rebuildAgentClient()`) to enable child session recording.
- **Remove dead code.** `TaskTool.getToolsForSubAgent()` is unused; superseded by `getToolsForDef()`.
- **Surface permission diagnostics.** Log warnings from `buildPermissionManagerFromAgentWithDiags()` when agent-file permissions contain unsupported fields or unknown groups.
- **Test per-agent permission application.** Verify child agents receive the agent-definition permissions, not the parent's permissions.
- **Test child session persistence.** Verify child session ID is generated, messages persisted, and result includes session ID link.

## LLM provider layer — deferred work

- **Streaming provider adapters.** `internal/agent/llm_contract.go` defines stream event types and the optional `StreamingLLMClient` interface, but `GenericClient` still uses request/response chat. Next step is dedicated OpenAI-compatible, Anthropic, and Copilot adapters that emit `text_delta`, `thinking_delta`, tool-call, usage, and done events.

## Context compaction — deferred work

Async token-threshold compaction landed 2026-05-20 (fixes the 12 issues from the prior roast):
- ✅ Tool-pair-safe slicing (no orphan `role=tool` after compaction)
- ✅ Real token-usage triggers via `resp.Usage.PromptTokens` + `ModelWindow()`
- ✅ Tool-aware summary prompt (tool calls, results, reasoning included)
- ✅ Turn-boundary tail preservation (whole last user turn kept intact)
- ✅ Configurable thresholds (`compact.token_threshold`, `keep_recent_turns`, etc.)
- ✅ Summary call: context timeout + retry + structured debug logging
- ✅ Immediate post-Step trigger (no re-summarisation every turn)
- ✅ Persisted to session (TUI splices `m.messages`, calls `saveSession`)
- ✅ UI banner: `📦 Compacted N earlier messages`
- ✅ Mid-loop warning emitted when prompt tokens exceed window threshold

Deferred:
- **Mid-loop hard compaction.** A single Step with many tool calls can still blow past the window before returning. Today we only warn; the compaction runs after `streamDoneMsg`. Implementing in-loop compaction would require pausing the tool loop at a tool-pair-safe checkpoint, summarising, and resuming — non-trivial.
- **Retry the failed Step after compaction.** If the LLM call inside Step fails with a context-length error, the UI surfaces the error and the post-Step compaction never runs (Step returned early). Could detect context-length errors, run sync compaction, and replay the Step.
- **Streaming summary.** The summary client call is blocking. If it becomes the bottleneck on slow providers, switch to a streaming variant that lets the UI show partial summary text as it arrives.
- **Drop stale `pendingCompactUIIdx`.** If the user clears the session between compaction trigger and completion, the splice indices become stale. Today `applyCompactionResult` guards with bounds checks, but a session-generation counter would be cleaner.
