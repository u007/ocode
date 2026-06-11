# TODO

## `/rc` full live mirror — follow-ups (built, not yet run end-to-end)

The 2-way live mirror (TUI↔web: user messages, thinking/text token deltas, tool
calls/results, turn snapshot) is implemented across `internal/server`
(`rc_bridge.go` broadcast fan-out, `handler_sse.go` `HandleSessionMessages`),
`internal/tui/model.go` (broadcast sites in `deltaMsg`/`streamMsgEvent`/`streamDone`/
user-submit), and the web app (`connectSessionMirror`, store `live` buffer,
`TurnParts`/`MessageBubble`/`ChatPanel`). Compiles, typechecks, unit-tested — but
**not verified live** (interactive TUI). Open items:

- **Verify end-to-end.** Run `curl -N "http://localhost:PORT/api/chat/messages?token=TOK"`,
  type in the TUI, confirm event order: `user_message` → `thinking`/`text` deltas
  → `tool_start`/`tool_result` → `messages` + `turn_done`. Then both-directions
  in the browser. If `turn_done` arrives for a TUI-originated turn, the
  `pendingRC==nil` end-of-handler snapshot path is confirmed.
- **Optimistic echo removed.** Web-typed messages now render only after the
  round-trip `user_message` broadcast — invisible on localhost, a perceptible
  delay over Tailscale. Decide whether to re-add optimistic-add with dedup.
- **`tool_result` carries no call-id** (`Tool: "tool"`), so concurrent tool
  results can mis-pair in the *live* view; the `turn_done` snapshot heals it.
  Thread the tool name/call-id through `tool_result` for correct live pairing.
- **`SET_STREAMING: true` + autoscroll fire on every token delta** — fine on
  localhost, potentially janky on long turns over a network. Throttle if needed.
- **Browser "Stop" is local-only** — during a TUI-originated turn it re-locks the
  input on the next delta. No web cancel path exists; add one if desired.
- **Committed tool rendering is per-message** (assistant `tool_calls` block +
  separate `tool` result block) rather than paired. The live view pairs them;
  consider pairing in `ChatPanel` for the committed snapshot too.

## `/ide` VS Code integration — deferred backends & limits

The `/ide` command (internal/ide + TUI wiring) connects to VS Code via the
**Claude Code extension's** WebSocket+MCP lock-file protocol (`~/.claude/ide/*.lock`).
It auto-enables when running inside a VS Code terminal (`TERM_PROGRAM=vscode`)
unless `ide_mode` is set in ocode.json. Deferred / out of scope for now:

- **opencode-extension backend not implemented.** opencode's own extension
  (`sst-dev.opencode`) only POSTs a one-shot `@file#Lx-y` to an HTTP server
  (`/app`, `/tui/append-prompt`) on a keypress — no live selection tracking. A
  `/ide opencode` mode (ocode serving those endpoints + reading
  `_EXTENSION_OPENCODE_PORT`) was scoped but not built; the Claude Code backend
  supersedes it for live data. Add only if a user explicitly wants the opencode
  extension path.
- **No editor jump-to from ocode.** We read selection/open-tabs but don't yet
  drive VS Code to a location (the extension exposes an `openFile` MCP tool we
  could call).
- **`at_mentioned` insert is best-effort.** Inserts `@rel#Lstart-Lend` into the
  input; relies on the extension emitting the event (Cmd+Alt+K style).

## Clickable file paths in messages — known limitations

Auto-detected, clickable file paths were added to rendered chat messages (web
`MessageBubble` + TUI transcript and agent drill-in). Open in `$EDITOR`/`$VISUAL`
with system-opener fallback. Deferred / limited behavior:

- **TUI click ignores `:line` suffix.** A path like `handler.go:42` opens the
  file but does not jump to the line (the shared `createEditorOpener` has no
  line-jump support). Web jumps only for code-family GUI editors (`--goto`).
- **Web cannot open terminal editors.** The server is headless (no TTY), so
  `vim`/`nano`/etc. can't run from a browser click — it falls back to the system
  opener. Only GUI editors (`code`, `cursor`, `zed`, …) or the OS default work.
- **Paths split across a visual-line wrap boundary** linkify only the first
  segment (TUI). Acceptable; full-token reconstruction across wraps not done.
- **Web path resolution uses the server process `os.Getwd()`** (mirrors
  `handleFileContent`). If a session cwd ever differs from the launch dir,
  relative paths won't resolve.
- **Not exercised with live mouse interaction.** Verified via render-test (custom
  `filelink` element renders to a clickable span), regex/detection unit tests,
  server security/validation tests, and reuse of the existing working
  selection-coordinate math — but a live hover/click walkthrough on each surface
  was not run.

## AST/LSP semantic tool — deferred work

The old ast-grep "code_rel" tool + `.sgindex` daemon were removed (they relied on a
persistent ast-grep index that doesn't exist; the daemon was a no-op). Replaced by an
opt-in, LSP-backed `ast` tool (`internal/tool/ast.go`) over `internal/lsp`. Disabled by
default; toggle with `/plugin enable ast` (persisted in ocode config `plugins.ast`).

Incomplete / best-effort:
- **Only gopls is validated end-to-end.** `internal/lsp/manager.go` maps `.rs`,
  `.py`, `.ts/.tsx/.js/.jsx` to rust-analyzer / pyright-langserver / typescript-language-server
  with correct stdio invocations, but these are **untested here**. Verify per-language
  before relying on them.
- **`callers` (incoming call hierarchy) is best-effort.** Requires the server to support
  `textDocument/prepareCallHierarchy` + `callHierarchy/incomingCalls`. gopls does; many
  servers don't, in which case it returns an error rather than results.
- **`lsp` and `ast` tools overlap.** Both go through `internal/lsp` (shared client +
  Manager + formatters), but `lsp` (position-based, always-on) and `ast` (name-based,
  opt-in) are two tools doing related work. Consider consolidating to one tool once the
  name-based UX proves out.
- **LSP servers are never `Close()`'d.** `lsp.Manager` has no lifecycle hook in the Tool
  interface, so each `/plugin disable ast` rebuild orphans the prior gopls until ocode
  exits (same pre-existing behavior as the old `lsp` tool — gopls is designed to be
  long-lived, but a second toggle spawns a fresh one). A Tool `Close()`/shutdown hook
  would let `rebuildAgentWithExternalTools` reclaim them.
- **Name resolution is heuristic.** `resolveSymbol` picks the first exact-name workspace
  symbol (then trailing-name match, then first hit). Ambiguous names (same symbol in
  multiple packages) resolve to one location; no disambiguation UI.

## Anthropic extended-thinking signatures (interleaved multi-turn)

When `ThinkingBudget > 0` and `anthropic-beta: interleaved-thinking-*` is enabled, Anthropic requires that prior assistant thinking blocks be replayed *with their original `signature` field* on subsequent turns or the request is rejected. The streaming SSE parser in `chatAnthropic` (`internal/agent/client.go`) captures the signature into a per-block field but discards it on completion; `Message` has no place to round-trip thinking blocks across turns, and `convertToAnthropicMessages` only emits `text` + `tool_use` blocks for assistant history. This matches the previous non-streaming behavior (parity), but interleaved-thinking multi-turn flows will fail. Fix requires: (1) persist thinking blocks + signatures on `Message`, (2) replay them in `chatAnthropic`'s outbound `messages`, (3) ensure compaction/repair paths preserve them. Out of scope for the streaming-thinking work that introduced this note.

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
- **Thread context into `LLMClient.Chat`.** Title generation (`title.go`) and compaction (`compact.go`) wrap `client.Chat` in `select { case <-ctx.Done() }`, but the inner goroutine ignores cancellation and keeps running until the HTTP client's 5-minute timeout fires. Adding `Chat(ctx, ...)` to the interface + propagating through all 4 providers and the test mocks would let these helpers actually cancel. Bounded leak today, but cost is real on slow networks. (from review-changes: 2026-05-24)
- **Drop `AgentTool` legacy shim.** The `agent` tool is no longer registered (`internal/agent/agent.go` only registers `task`). The type stays for transcript/permission compatibility. Remove once historical sessions don't need to round-trip — pick a date (e.g. 2026-08-01) and delete the type, the back-compat permission alias, and the TUI tool renderer branch. (from review-changes: 2026-05-24)

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

Anchored compaction landed 2026-05-27 (anchored summary, structured template, prune-before-summarise, custom summary model already wired). Still deferred:
- **Switch threshold to `usable(model)` not `ratio × window`.** opencode subtracts reserved-for-output from the input limit and triggers at actual usage ≥ usable. ocode still uses `0.85 × window`, which mis-fires on models whose effective input differs sharply from total context. Needs `ModelWindow` to expose input/output split.
- **`PRUNE_PROTECTED_TOOLS` list.** opencode protects `skill` tool outputs from pruning. ocode has no equivalent. Likely candidates here: outputs from `agent_status`, `task_status`, `wait`, and MCP tools marked durable. Until then, every large tool result is prunable.
- **Persisted on-disk prune sink.** Today `pruneToolResults` shrinks tool content in-memory before summarisation. The full output exists on disk via `internal/agent/truncate.go` only when the tool result was already large enough to be truncated at write time. Wire `pruneToolResults` to write any pruned content to disk + emit a `[full output: <path>]` reference so the agent can re-read it via the read tool.
- **Drop char-fallback flat 1.15× multiplier.** `shouldCompact` applies a flat 15% safety margin when `Usage` is missing. This hides per-content-type weighting (text vs reasoning vs tool JSON vs images). Replace with weighted estimation or log a warning + skip compaction when `Usage` is absent.
- **Surface compaction in TUI history.** The 📦 banner shows the count but not the structured summary content. A "view summary" affordance (expand banner, copy summary text, jump to splice point) would make multi-compaction sessions navigable.

## Provider prompt hybrid — Phase 3 (deferred)

Phase 1 (file-backed prompts) and Phase 2 (model-ID routing) landed 2026-05-27. Phase 3 was descoped after discovering markers are load-bearing for `prompt_shape_test.go`. Still deferred:
- **Cache `environmentPrompt()` output on Agent.** Today every `BasePromptMessages` call re-runs `os.Getwd`, `findWorkspaceRoot` (walks parents), and 3× `os.Stat`. Cache once per agent; invalidate on cwd or model change.
- **Resolve mode-vs-spec prompt ambiguity.** `BasePromptMessages` computes `Mode().SystemPrompt()` and then conditionally overrides it with `spec.SystemPrompt`. Two prompt sources, one wins, the other was computed for nothing. Pick one resolver and document the precedence.
- **Reconsider marker dedup.** The 5-marker `[ocode:*]` system with `existingPromptMarkers` scan provides idempotency and testability. If marker semantics drift further, revisit whether a simpler `Agent.prompted bool` + a separate test mechanism would be cleaner.

## Plugin system — `/plugin` command + native reimplementations ✅ (2026-05-29)

Implemented on branch `feat/plugin-auth-hooks`. All three subsystems shipped:

- **`/plugin` TUI command** — list, enable/disable, install (git+local with confirm flow), remove, info. `PluginConfig` with `Dir`/`Ref` fields in config. `internal/plugins/manager.go` with InstallGit/Local, RunOnInstall (direct exec, no shell), AutoRegisterMCP/UnregisterMCP.
- **Auth providers** — Cloudflare Workers AI (account ID prompt + BaseURL construction), Cloudflare AI Gateway (o-series max_tokens strip), OpenAI Codex (reuses OpenAI OAuth). `AccountID` field on `Credential`.
- **In-process hook pipeline** — `internal/hooks/pipeline.go` with ToolBefore/After, ChatParams, ShellEnv hook points. Wired into `executeToolCall`, `chatWithDelta` (save/restore GenericClient fields), and bash subprocess env injection in `tool/process.go`.

Deferred (CocoIndex plugin): see plan `docs/superpowers/plans/2026-05-28-cocoindex-plugin.md` — requires plugin system to be merged first.

## apply_patch parity with opencode — follow-up

- **Align remaining edge cases with upstream behavior.** Current parser/executor now supports opencode-style `*** Begin Patch` envelopes, `*** Add/Delete/Update File`, `*** Move to`, `@@` hunks, and rollback on failure. Next pass should compare against upstream behavior for duplicate context, repeated hunks, rename+update ordering, and exact failure modes.
- **Match upstream error strings where practical.** LLM behavior can be sensitive to familiar tool responses; aligning error wording may improve self-correction when a patch is malformed.
- **Add edge-case tests.** Cover move+update in one patch, EOF insertions via `*** End of File`, multiple hunks in one file, repeated-context matching, and whitespace-tolerant matching cases.
- **Consider importing or porting the upstream parser more literally.** If true byte-for-byte compatibility is a goal, the cleanest path is a closer structural port of the upstream opencode apply_patch parser rather than maintaining a merely compatible reimplementation.

## LLM auto-permission: interpreter execution (2026-06-08)

- **Surface the model's effect summary / reject reason in the human-ask prompt.**
  Interpreter auto-permission decisions are fully functional, but when the
  verifier declines (or the model defers) the reason currently lands only in the
  debug panel (`tier=auto_interp_*`). `PermissionRequest` has no reason field, so
  the TUI ask prompt does not yet show "interpreter execution (lang mode) — model
  summary: …". Add an optional `Reason`/`Summary` to `PermissionRequest` and
  render it in the permission dialog when present. Deferred to keep this change
  off the (currently broken) tui package.
- **Heredoc handling is a line-based pre-pass (`extractHeredocs`), not a
  rune-level tokenizer state.** This is intentional (far lower blast radius on the
  shared shell parser) and covers `<<DELIM`/`<<-`/quoted delimiters/multi-heredoc.
  If full shell-grammar heredoc fidelity is ever needed (e.g. heredocs mid-pipeline
  with interpreter not first word), revisit.

## TUI streaming render: residual O(N) viewport cost (2026-06-09)

- **renderTranscript no longer re-wraps/re-strips the whole transcript per delta.**
  Per-message cache now stores each block's wrapped + ANSI-stripped line slices;
  a streamed delta only re-renders the one changed message. Result: 1000-pair
  transcript dropped 87.7ms→27.6ms per render and 62MB→2.9MB allocs (543× fewer
  allocs), collapsing the GC pressure that was stalling the event loop. Realistic
  sessions (~100 pairs) are now ~2.8ms.
- **Residual (root cause):** the bubbles/v2 viewport's `SetContentLines` does two
  O(N) passes over the whole transcript on every delta — a reverse `ContainsAny`
  `\r\n` scan plus `maxLineWidth` (`ansi.StringWidth` per line). Confirmed via
  benchmark: 18019 lines / 3001 msgs = ~35ms/render at only 180 allocs/op, i.e.
  pure CPU in line-scanning, not allocation. Real session measured at 2747 lines /
  381 msgs = 8–12ms/render. The chat viewport has `SoftWrap=false` and never
  horizontal-scrolls, so `longestLineWidth` (the sole consumer of `maxLineWidth`)
  is computed-then-never-used — both scans are dead work.

- **Fixed (A + B), 2026-06-09:**
  - **A.** Coalesced the streaming render cadence (`lastDeltaRender` throttle in
    `applyThinkingDelta`) from 50ms→90ms while auto-scrolling, halving in-flight
    CPU with no perceptible animation loss (~11fps vs 20fps on a thinking stream).
  - **B.** Replaced the chat transcript's bubbles viewport with a reusable
    pre-wrapped, no-softwrap content surface (`internal/tui/fastviewport`) whose
    `SetContentLines` is O(1) (pointer assign, no scan) and whose `View`/scroll
    math is O(visible window). API-compatible with the subset the chat uses
    (Height/Width/YOffset/GotoBottom/GotoTop/AtBottom/ScrollUp/ScrollDown/
    SetYOffset/TotalLineCount/VisibleLineCount/SetContent/SetContentLines/Update/
    View); `scrollbarSetOffset` was made generic over a `scrollbarVP` interface so
    it serves both viewport types. `Update` is a no-op because the chat drives
    scrolling via explicit calls (keys are never forwarded; mouse wheel is handled
    by the parent) — verified via `shouldForwardToTranscriptViewport`.
  - **Result:** `renderTranscript` at 1000 pairs / 18019 lines dropped
    30.3ms→0.73ms (~41×); 100 pairs 2.8ms→0.19ms. The benchmark attribution that
    justified B: `SetContentLines` alone was 28.6ms of the 30.3ms (94%) at 0
    allocs — pure CPU in the two dead scans.

- **C. Deferred — tail-incremental assembly (only do if B is still not enough):**
  Even with B's O(1) `SetContentLines`, `renderTranscript` still rebuilds the full
  `transcriptLines`/`rawTranscriptLines` slices every delta (append loop over all
  messages + the toolNames prebuild) — O(messages), not O(tail). For pathological
  sessions (1000+ pairs / 90K+ lines) the next lever is to keep the assembled
  prefix cached and only re-append the tail message(s) that changed since the last
  render, making a streamed delta truly O(tail). This needs careful invalidation
  (width/theme change, message edit/delete, expand/collapse toggles all dirty the
  prefix) and must preserve the byte-identical unwrapped `nlAcc` region math that
  the click/selection/thinking/tool hit-testing depends on. Deferred: B should
  bring the common case (≤400 msgs) under ~2ms, below the perceptible threshold,
  so the prefix-cache complexity isn't justified until a real session proves
  otherwise.

## UI overhaul Part 3 review gaps (Tasks 18–19)

- [ ] Task 18: manual visual verification still pending — run `ocode serve`, switch theme in config, reload web, confirm colors follow (hex→HSL fix landed 2026-06-11 but was verified via unit math + typecheck only, not in a browser) (from review-changes: 2026-06-11)
