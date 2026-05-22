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

## Sandboxed program execution — wrapper with halt-ask-resume

Goal: wrap bash/python (and other code execution) so the agent can halt on a file/network access, ask the user, then resume or block with access-denied.

Permission-detection fixes first (live bug in `internal/agent/permissions.go`):
- **Relative-path escape.** `Decide()` skips the workdir check for non-absolute paths (`if filepath.IsAbs(path) && !isWithinWorkDir`). `read ../../../etc/passwd` is allowed. Resolve every path against `workDir` first, then check the resolved absolute path.
- **Fail-open on extraction failure.** Empty path from `extractPathFromArgs` falls through to `pm.Check()` → `allow` for `read`/`write`. Should fail closed to `ask`.
- **Multi-file tools.** `apply_patch`/`multiedit` patch many files but only `params.Path` is checked. Enumerate every target.
- **Enforce at the tool, not just `Decide()`.** Put the workdir/sensitive check inside the file-open chokepoint so new callers/subagents/MCP can't bypass it.

Execution wrapper design:
- **Tier 1 — spawn-in-sandbox (cross-platform).** `sandbox-exec` profile (macOS) / `landlock`+namespace or `bwrap` (Linux). Workdir read-write, rest denied, network denied. Fail-closed; on violation surface "denial → widen scope → re-run".
- **Network ask-proxy.** Spawn child with `HTTP_PROXY`/`HTTPS_PROXY` → in-process proxy; sandbox blocks all other egress. Real halt-ask-resume per request, cross-platform.
- **Tier 2 — seccomp user-notif (Linux only).** Wrapper becomes a per-syscall supervisor: kernel parks the syscall, wrapper prompts, returns continue or `EPERM`. True mid-run halt-ask-resume. Gate behind `runtime.GOOS == "linux"`.
- **FUSE mount (optional).** Only cross-platform way to truly halt-and-resume per filesystem op; heavyweight (macFUSE = user-approved system extension). Defer unless per-file mid-run prompts become a hard requirement.
- Note: `sandbox-exec`/`landlock`/containers **cannot** resume mid-run — policy is fixed at spawn, violating syscall just fails. macOS has no unprivileged mid-run halt mechanism.
- Wire wrapper into `internal/tool/process.go` spawn path; generate sandbox profile per run; hook proxy/permission callback into the existing `PermissionResponse` flow.

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
