# Plan: Shared Chat Verbosity Display Settings

## Approved design

Implement the feature specified in `docs/superpowers/specs/2026-09-24-chat-verbosity-display-design.md`.

User decisions:
- Store the setting in shared ocode server config, not localStorage.
- Provide Full / Balanced / Quiet presets plus per-category overrides.
- Keep the latest thinking block expanded in Full, Balanced, and Quiet.
- Target the local web and desktop SPA; leave TUI and remote-host rendering unchanged in v1.
- Presentation-only: do not alter persisted transcript content, search indices, or LLM context.

Advisor amendments are mandatory:
- Treat `chat_verbosity_changed` as an invalidation signal only; re-fetch the local config and re-fetch on SSE reconnect.
- Keep disclosure state in `ChatPanel`, keyed by stable message/tool identity and region, so virtualized unmount/remount does not lose manual choices.
- Define latest thinking as the last thinking block in the latest assistant turn; it remains expanded through commit until a newer turn begins.
- Use one outer tool-details gate and one inner output gate; retain separate call/output overrides.
- On policy revision, anchor the first visible virtualizer item, call `measure()`, and restore the reader position.
- Retain the last cached policy; only use Full with a structured warning when no cache exists. Never silently normalize malformed server values.

## Constraints and working-tree safety

- The worktree has unrelated concurrent WIP. Do not revert, reset, clean, or overwrite unrelated changes.
- Do not commit unrelated files. Documentation bundle index/log files are auto-managed by the context agent.
- No database migration.
- Do not change TUI rendering or remote-host behavior in this task.
- Follow repository logging rules: no empty catches; log failures; preserve causes when rethrowing.

## User Expectation Checklist

- [ ] Settings → Chat display exposes Full, Balanced, and Quiet presets.
- [ ] Each category supports Use preset / Always expanded / Always collapsed overrides.
- [ ] Full preserves today’s rendering by default.
- [ ] Balanced collapses older thinking while keeping latest thinking expanded.
- [ ] Quiet collapses older thinking, tool details, and tool output while keeping latest thinking expanded.
- [ ] Collapsed content remains expandable and searchable; transcript and LLM context are unchanged.
- [ ] Running tools, status, questions, permissions, answers, and actionable errors remain visible.
- [ ] Web and desktop use the same setting and embedded SPA.
- [ ] Open web/desktop clients update after a save; reconnect recovers missed config changes.
- [ ] Settings, docs, focused tests, typecheck, build, and formatting checks pass or have clearly documented blockers.

> **Correction (2026-09-25).** Checklist items above under-specified the preset
> matrix, and they are the likely origin of a shipped divergence. Per spec §9,
> **Balanced collapses older thinking AND tool-call details** (tool output stays
> expanded with a 20-line tail preview; each activity notice stays inline), and
> **Quiet** collapses all four. The implementer matched this checklist's wording
> rather than §9's table, so both resolvers shipped with Balanced tool-call
> details expanded — contradicting the spec, the form's own Balanced
> description, and the tests. Both are now realigned to §9
> (`resolveChatDisplayPolicy` in `web/src/lib/chatVerbosity.ts`,
> `ResolveChatVerbosityPolicy` in `internal/config/ocodeconfig.go`). **§9 is the
> source of truth for the cells, not this checklist.** The same change also
> added the dynamic `Follow preset — <Expanded|Collapsed>` option label. See
> `.opencode/plans/2026-09-25-chat-display-follow-preset-value.md`.

## Implementation Steps

### 1. Establish the failing tests and inspect current seams

- [ ] Re-read the approved spec and the current relevant code before editing, especially any concurrent changes in `internal/config/ocodeconfig.go`, `internal/server/handler_config.go`, `internal/server/server.go`, `web/src/api/client.ts`, `web/src/components/Chat/ChatPanel.tsx`, `web/src/components/Chat/TurnParts.tsx`, and Settings files.
- [ ] Add/adjust Go tests first for:
  - default `chat_verbosity` config (`full`, all overrides `preset`);
  - valid enum persistence and reload;
  - invalid preset/override values returning 400 without rewriting the stored file;
  - GET/PUT handler in-memory update and response shape;
  - config-change event publication.
- [ ] Add frontend pure-policy tests first for:
  - all three preset matrices;
  - each override;
  - the latest-thinking invariant in every preset;
  - malformed/partial config handling;
  - effective policy revision changes.
- [ ] Add renderer tests first for controlled disclosure state, latest-turn behavior, tool call/output gates, notice grouping, and search force-open behavior.
- [ ] Add Settings form tests first for group registration, save/reset, loading/error states, and API arguments.
- [ ] Run the new tests to confirm they fail for the intended missing behavior before implementation.

### 2. Add the Go config contract and API

Primary files:
- `internal/config/ocodeconfig.go` and focused config tests.
- `internal/server/handler_config.go` and focused handler tests.
- `internal/server/server.go` route registration.
- `internal/server/handler_events_test.go` or the closest event-bus test file.

Implementation:
- [ ] Add typed `ChatVerbosityConfig`, override, and effective-policy types with JSON tags consistent with existing ocode config sections.
- [ ] Add defaults and explicit validation for `full|balanced|quiet` and `preset|expanded|collapsed`.
- [ ] Preserve unknown config fields through the existing `Extra` round-trip behavior.
- [ ] Add a safe save helper following existing lock/load-modify-write conventions; never silently normalize invalid values.
- [ ] Add authenticated GET/PUT `/api/config/ocode/chat-verbosity` handlers.
- [ ] Update `h.cfg` only after persistence succeeds.
- [ ] Publish `chat_verbosity_changed` after a successful save. The payload is diagnostic only; clients must not apply it directly.
- [ ] Return a stable response containing persisted intent and/or the effective policy needed by the frontend.
- [ ] Run focused Go tests, `gofmt`, and `go vet` before moving on.

### 3. Add the frontend API client and shared policy store

Primary files:
- `web/src/api/types.ts`
- `web/src/api/client.ts`
- new `web/src/lib/chatVerbosity.ts` (pure resolver, types, cache/listener hook, reset-for-tests helper)
- focused tests for the client/store/resolver.

Implementation:
- [ ] Add typed API methods for GET/PUT chat verbosity config.
- [ ] Keep the module-level cache single and local-server scoped.
- [ ] On first consumer mount, fetch the config; deduplicate concurrent requests.
- [ ] On successful save, publish the returned policy to all mounted consumers.
- [ ] Subscribe to `eventBus.on("chat_verbosity_changed")` as an invalidation signal only; ignore `env.data` and re-fetch the local config.
- [ ] Re-fetch on `eventBus.onReconnect()` so a save made while SSE was down is recovered.
- [ ] Retain the last valid cached policy on transient failure. If no cache exists, use Full as an explicit compatibility default, emit a structured warning, and expose the error to Settings.
- [ ] Do not use a host-specific cache in v1; remote-host support is explicitly out of scope.

### 4. Implement controlled chat disclosure rendering

Primary files:
- `web/src/components/Chat/ChatPanel.tsx`
- `web/src/components/Chat/MessageBubble.tsx`
- `web/src/components/Chat/TurnParts.tsx`
- possibly a small new notice-group component beside `TurnParts.tsx`
- focused renderer and ChatPanel tests.

Implementation:
- [ ] Resolve the effective policy once per ChatPanel render and pass it to committed and live renderers.
- [ ] Identify the latest assistant turn and its last thinking block. Keep it expanded through live streaming and committed rendering until a newer assistant turn begins.
- [ ] Move thinking/tool disclosure state into a `ChatPanel` map keyed by stable message identity, tool-call identity, and region (`thinking`, `call`, `output`, `notices`).
- [ ] Give the map an explicit policy revision. Clear/rebase it only when the effective policy revision changes; an identical refetch must preserve manual choices.
- [ ] Make `ThinkingBlock` and `ToolBlock` controlled renderers; remove reliance on component-local disclosure state for behavior that must survive virtualization.
- [ ] Preserve the existing Full heuristic (`lineCount <= 50`) and 20-line expanded output tail.
- [ ] Use one outer tool-call/details gate and one inner output gate. Keep concise headers, running indicators, pending-question actions, and answered-question blocks visible.
- [ ] Group consecutive live notices in a derived view when collapsed; never mutate the live store and never group StatusBlock.
- [ ] Make search-selected matches force the containing disclosure open while retaining the full-transcript search index.
- [ ] On policy revision, capture the first visible virtualizer item and offset, call `virtualizer.measure()`, and restore the anchored position using supported TanStack Virtual APIs. Preserve bottom-pin behavior for tail-following readers.
- [ ] Run focused Chat/TurnParts tests and the full web typecheck before adding the Settings form.

### 5. Add the Chat display Settings group

Primary files:
- `web/src/components/Settings/SettingsPanel.tsx`
- new `web/src/components/Settings/ChatDisplayForm.tsx`
- focused Settings tests.

Implementation:
- [ ] Add `chat-display` to `SettingsGroupId`, the ocode group list, and `renderGroup` using the existing localized one-case pattern.
- [ ] Render preset radio cards, explanatory copy, four override selects, Reset overrides, Save changes, loading, success, and inline error states.
- [ ] Use the shared policy store for initial data and live updates; do not duplicate a second config cache in the form.
- [ ] Ensure keyboard labels, focus behavior, `aria-expanded`/selected semantics, and mobile/narrow Settings layout follow existing UI conventions.
- [ ] Keep the form scoped to the local server; do not add a remote host selector in v1.
- [ ] Run Settings tests and the full web test/typecheck/build gates.

### 6. Documentation and final validation

Primary docs:
- `CHANGES.md` (existing file; add a concise completed entry).
- `skills/ocode-web/SKILL.md` (shared SPA behavior and test/file map if the project convention requires it).
- `skills/ocode-desktop/SKILL.md` only if the desktop build/rebuild behavior needs a documented note.
- Do not hand-edit auto-managed `docs/index.md` or `docs/log.md`.

Validation:
- [ ] `gofmt` all changed Go files; `go vet ./...` or the strongest practical scoped equivalent.
- [ ] `go test` focused config/server packages, then broader Go tests; compare unrelated baseline failures instead of hiding them.
- [ ] `cd web && pnpm test -- <focused suites>` (or the repository’s available package-manager equivalent).
- [ ] `cd web && pnpm typecheck`.
- [ ] `cd web && pnpm build`.
- [ ] `git diff --check` and inspect only the intended diff; verify unrelated WIP remains untouched.
- [ ] Verify default Full behavior with a missing config, invalid enum rejection, save/reload persistence, event invalidation, reconnect recovery, virtualized disclosure persistence, latest-thinking behavior, search force-open, and status/question visibility.
- [ ] Update the checklist above with evidence and explicitly record any environment-only blockers.

## Rollout / Non-goals

- Existing users see no behavior change because the default is Full.
- No transcript migration or LLM-context change.
- No TUI implementation in this task.
- No remote-host policy propagation in this task; the event handler deliberately re-fetches the local endpoint.
- A later task may add host-aware policy propagation, TUI parity, or richer long-assistant-message folding.
