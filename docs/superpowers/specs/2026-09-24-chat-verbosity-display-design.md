---
type: Design
title: Chat Verbosity Display — Design Spec
description: 'Approved design spec: chat_verbosity config block (full/balanced/quiet presets + per-category overrides), GET/PUT /api/config/ocode/chat-verbosity with chat_verbosity_changed event, Settings → Chat display form, and a pure frontend resolver so the web/desktop chat can render with less verbosity while the latest thinking block stays expanded.'
tags:
  - design
  - spec
  - web
  - chat
  - settings
  - verbosity
  - superpowers
timestamp: 2026-09-24T08:56:00Z
---
# Chat Verbosity Display — Design Spec

**Status:** Approved design (2026-09-24). Specification only — none of the changes described here are implemented yet; every "will"/"planned" item below is work to be done, gated on the test-first plan in §14.

---

## 1. Context and evidence

Verified anchors in the current codebase (line numbers as of this writing):

- **Config layer** — `internal/config/ocodeconfig.go`: `OcodeConfig` struct (line 481) carries nested blocks such as `Compact CompactConfig` (line 482, struct at 296); the on-disk shape lives in `ocodeConfigFile` (line 821) with per-block file structs (e.g. `compactConfigFile`, line 746); defaults are applied in `defaultOcodeConfig()` (line 911); file→memory merge helpers such as `applyCompactConfig` (line 1715); section saves go through `withOcodeConfigLock` (line 1918 → `lockOcodeConfig` at 1876), a cross-process advisory lock with an atomic tmp+rename write (`writeOcodeConfigFile`, lines 1938–2108). Unknown on-disk keys round-trip through the `Extra` map (load at line 1561, write loop at 2068–2076), and canonical keys are listed in the write-path skip list (line 2072).
- **Settings API layer** — `internal/server/handler_config.go`: the GET/PUT-per-section pattern is `HandleGetCompactConfig` (line 1393) / `HandleSetCompactConfig` (line 1404): `readBodyJSON` → `writeError(w, http.StatusBadRequest, …)` on invalid input (validation examples: lines 1573, 1845–1853) → section save → in-memory `h.cfg.Ocode.<Block>` update under `h.mu` → `writeJSON` 200. Routes are registered in `internal/server/server.go` behind auth (lines 370–371: `GET/PUT /api/config/ocode/compact` wrapped in `s.authMiddleware`) with thin delegating wrappers (lines 2016–2021).
- **Event bus** — `internal/server/event_bus.go`: `Publish(event, project, sessionID, data)` (line 160) fans an envelope out to every subscriber of the process; `sessionScopedEvents` (line 53) lists events that require a session id (a global event must NOT be added there), and `criticalEvents` (line 35) is reserved for terminal turn events. Process-wide publishes with an empty session id are the established pattern (`h.bus.Publish("spending", "", "", …)`, `internal/server/emitters.go:209`; also `logs`, `git_status`).
- **Frontend transport** — `web/src/lib/eventBus.ts`: `on(event, handler)` (line 112) subscribes by event name; global (non-session-routed) events are consumed directly (e.g. `eventBus.on("spending", …)` in `web/src/App.tsx:131`), separate from the session-routed envelope path (`ROUTABLE_EVENTS`, `web/src/lib/sessionEvents.ts:184`).
- **Frontend API client** — `web/src/api/client.ts`: `fetchJSON<T>(path, init?, host?, projectPath?)` (line 446); config helpers follow `getCompactConfig` / `setCompactConfig` (lines 788–790) with the payload type declared alongside (e.g. `CompactConfig`, line 62).
- **Settings UI** — `web/src/components/Settings/SettingsPanel.tsx`: `SettingsGroupId` union (line 31), nav entries as `{ id, label }` (line 76 is `{ id: "compact", label: "Compact" }` — **"Compact" already means transcript auto-compaction**, hence the new group name below), form dispatch switch `renderGroup` (line 105, e.g. line 123–124). `CompactForm.tsx` is the house pattern for a config form: `loading`/`saving`/`error` state, `load()`/`save()` callbacks (lines 28–60), inline error text (line 73), disabled-while-saving Save button (line 119).
- **Chat rendering** — `web/src/components/Chat/TurnParts.tsx`: `TOOL_OUTPUT_PREVIEW_LINES = 20` (line 8, the tail preview that must be preserved); `ThinkingBlock` (line 51) defaults to open (`useState(true)`, line 60); `StatusBlock` (line 99, transient spinner line); `NoticeBlock` (line 116, transient `~` notice); `QuestionAnswerBlock` (line 161); memoized `ToolBlock` (line 206, memo rationale in the comment at 202–205) with initial open heuristic `useState(lineCount <= 50)` (line 235), header status `running…` / `awaiting your answer` (lines 284–285), the `Open question` button rendered outside the collapsible body (lines 287–295, body gate starts at 296), and the "… N earlier lines · click to expand" toggle (lines 343–353). None of the disclosure buttons currently expose `aria-expanded`.
- **Chat panel** — `web/src/components/Chat/ChatPanel.tsx`: render entries (`single` | `tool-group`) memoized at line 272; virtualization via `useVirtualizer` (line 346) with item keys derived from message-object identity (comment at 224) and `ref={virtualizer.measureElement}` (line 1139); live-buffer rendering dispatches `ThinkingBlock` / `AssistantText` / `StatusBlock` / `NoticeBlock` / tool parts (lines 1209–1220); search plumbing: `currentMatchEntryPos` (line 531), `scrollToIndex` (line 826), `attemptedJumpsRef` (line 844), `highlight` (line 1134); bottom-pin machinery via `atBottomRef` and ResizeObservers (lines 84–89, 692–763).
- **Store / types** — `web/src/stores/chatStore.tsx`: live parts are transient; `isTransientLivePart` = `status | notice` (lines 122–123). `web/src/api/types.ts`: `LivePart` union (lines 22–43).
- **Search** — server side is full-transcript and rendering-agnostic: `internal/server/handler_session_search.go` (`HandleSearchSession`, line 61; matches via `agent.MessageMatchesQuery`, line 100) returning indices only; client helpers in `web/src/lib/sessionSearch.ts` (`serverIndexToLocal` 45, `olderPrefixFetch` 89, `inWindowMatchCount` 113, `buildJumpTargets` 139).
- **Compaction summaries** — `web/src/components/Chat/CompactionNotice.tsx` is already collapsed by default (`useState(false)`, line 40); no new override applies to it in v1.
- **Desktop parity** — the desktop shell embeds the same React SPA and server (see `skills/ocode-desktop` and `skills/ocode-web`); there is no separate desktop frontend to change.
- **Not present anywhere yet** — repository-wide search finds no `chat_verbosity`, `ChatVerbosity`, `chat-verbosity`, or `chatVerbosity` in `internal/` or `web/src/`. This document specifies net-new work.

## 2. Goals

1. Let users pick how verbose the **rendered** chat is in the web UI and desktop app, via a shared, server-persisted setting (not per-browser localStorage).
2. Provide three presets — `full`, `balanced`, `quiet` — plus per-category overrides so a user can mix (e.g. Quiet preset but always-expanded older thinking).
3. Guarantee that the **latest thinking block is always expanded by default** under `full`, `balanced`, and `quiet`.
4. Change display only: transcript data, stored messages, search indices, and the LLM context are untouched.
5. Keep the default (`full`) byte-compatible with today's rendering so existing users see zero change until they opt in.
6. Propagate a save to every client attached to the same server process without a reload, via a process-wide `chat_verbosity_changed` event.

## 3. Non-goals / scope

- **No localStorage** for this setting (explicit user decision); the source of truth is `ocodeconfig.json` on the server.
- **TUI unchanged in v1** — the TUI has its own render path (`internal/tui/tool_render.go`) and will ignore `chat_verbosity`.
- **Remote-host behavior out of scope for v1** — no `host`-threaded variant of the endpoint (unlike some config APIs that accept `host` in `fetchJSON`). Note that rendering always happens in the local SPA, so the local verbosity setting governs how transcripts from remote-hosted sessions are *displayed* too; what is out of scope is fetching/storing verbosity *from* a remote server or per-host settings.
- **No DB migration** — config file only; nothing in sqlite/session storage changes.
- No new override for compaction summaries (already collapsed), permission dialogs (modal), or question asks.
- No per-project or per-session verbosity in v1 (see §17).

## 4. User decisions (approved)

1. **Shared server config, not localStorage** — persisted in `ocodeconfig.json`, served over an authenticated API, same lifecycle as every other Settings block.
2. **Preset plus per-category overrides** — a single preset selects a baseline; each category can override with `preset` (follow), `expanded`, or `collapsed`.
3. **Latest thinking stays expanded** — the latest thinking block defaults to expanded under **all three presets** (`full`, `balanced`, `quiet`), independent of any override; this is an invariant of the resolver, not a configurable key.

The user also approved the approach, architecture, UI, and rendering/validation sections as proposed.

## 5. Configuration schema and data flow

New global section in `ocodeconfig.json`:

```json
{
  "chat_verbosity": {
    "preset": "balanced",
    "overrides": {
      "older_thinking": "preset",
      "tool_calls": "preset",
      "tool_output": "expanded",
      "activity_notices": "preset"
    }
  }
}
```

- `preset`: `full` | `balanced` | `quiet`. **Absent section ⇒ default `preset: "full"` with all overrides `preset`** — backward compatible; an existing config file without the key behaves exactly like today.
- `overrides`: exactly four categories — `older_thinking`, `tool_calls`, `tool_output`, `activity_notices` — each `preset` | `expanded` | `collapsed`. Absent override ⇒ `preset`.
- There is **no** `latest_thinking` key: it is not configurable (see §9).
- Go types (planned): `ChatVerbosityConfig` + `ChatVerbosityOverrides` in `internal/config/ocodeconfig.go`, a `chatVerbosityConfigFile` field on `ocodeConfigFile` (`json:"chat_verbosity,omitempty"`), `defaultChatVerbosityConfig()` returning `full`/all-`preset`, a load-merge following the `applyCompactConfig` pattern, and `SaveOcodeChatVerbosity(cfg)` persisting through `withOcodeConfigLock`. The write path must handle the key canonically like `compact` (payload entry + the Extra skip list at line 2072) so it never double-writes; an absent section is re-materialized only when the user saves. An **older ocode binary** reading a config that contains `chat_verbosity` preserves it via the `Extra` round-trip (lines 1561, 2068).

Data flow:

```
Settings form ── PUT /api/config/ocode/chat-verbosity ──▶ handler validates (400 on bad values)
                                                          │
                                                          ├─ SaveOcodeChatVerbosity (config lock, atomic write)
                                                          ├─ h.cfg.Ocode.ChatVerbosity = req  (in-memory, under h.mu)
                                                          └─ h.bus.Publish("chat_verbosity_changed", "", "", req)
                                                                    │  (process-wide, empty project/session;
                                                                    │   NOT in sessionScopedEvents, NOT criticalEvents)
        ┌───────────────────────────────────────────────────────────┘
        ▼
eventBus.on("chat_verbosity_changed")  ──▶ module-level verbosity store updates
        │                                     │
        │  (baseline: GET once on first       ▼
        │   subscriber; fetch error ⇒        useChatVerbosity() ──▶ resolved effective modes
        │   store stays at Full)                    │
        ▼                                           ▼
   Settings form (own GET/PUT + inline errors)   ChatPanel ──▶ MessageBubble / live parts
                                                        └──▶ TurnParts (ThinkingBlock, ToolBlock, NoticeBlock)
```

## 6. API

Authenticated (wrapped in `authMiddleware`, mirroring `server.go:370–371`):

- **`GET /api/config/ocode/chat-verbosity`** → `200` with the normalized config:

  ```json
  { "preset": "full",
    "overrides": { "older_thinking": "preset", "tool_calls": "preset",
                   "tool_output": "preset", "activity_notices": "preset" } }
  ```

  Absent section ⇒ the default above. GET **normalizes** on read (unknown preset ⇒ `full`, unknown override value ⇒ `preset`) so a hand-edited file can never reach the renderer; the resolver additionally defends against malformed payloads (§7).

- **`PUT /api/config/ocode/chat-verbosity`** — body is the same shape. Validation (strict, no silent coercion — the house rule: the client sends what it has, the server 400s malformed input):
  - invalid JSON body → `400` `"invalid request body"`
  - unknown `preset` → `400` `"preset must be one of: full, balanced, quiet"`
  - unknown override value → `400` `"overrides.<category> must be one of: preset, expanded, collapsed"`
  - unknown override category key → `400` `"unknown override category: <key>"`
  - save failure → `500` with the error text (existing `writeError` pattern)

  On success: persist via `SaveOcodeChatVerbosity` (the normal config save/lock path), update `h.cfg.Ocode.ChatVerbosity` under `h.mu`, publish `chat_verbosity_changed` process-wide with the saved config as payload (empty project and session id, exactly like `spending`), and respond `200` with the saved config.

## 7. Frontend architecture (pure resolver + small store)

Planned module `web/src/lib/chatVerbosity.ts`:

```ts
type Preset    = "full" | "balanced" | "quiet";
type Override  = "preset" | "expanded" | "collapsed";
type Mode      = "expanded" | "collapsed";
type Category  = "older_thinking" | "tool_calls" | "tool_output" | "activity_notices";

interface ChatVerbosityConfig {
  preset: Preset;
  overrides: Record<Category, Override>;
}

interface EffectiveVerbosity {
  olderThinking:   Mode;
  latestThinking:  "expanded";          // invariant — literally always this value
  toolCalls:       Mode;
  toolOutput:      Mode;
  activityNotices: Mode;
}

// Pure: no I/O, no store access. null/undefined/malformed ⇒ Full defaults.
function resolveChatVerbosity(cfg: ChatVerbosityConfig | null | undefined): EffectiveVerbosity;
```

- Resolution rule: `effective(cat) = overrides[cat] !== "preset" ? overrides[cat] : presetTable[preset][cat]`; `latestThinking ≡ "expanded"` regardless of preset and overrides. Unknown enum strings defensively fall back (`preset`→`full` category default; bad override→`preset`).
- A module-level store in the same file (the `projectGitCounts.ts` / `sessionRevision.ts` pattern): fetch the baseline `GET` once on first subscriber, subscribe `eventBus.on("chat_verbosity_changed")` to apply pushes, expose `useChatVerbosity()` returning memoized `EffectiveVerbosity`. Multiple chat tabs share one fetch/subscription (no per-tab stampede). **On fetch failure the store stays at Full defaults** (goal 5 doubles as the failure mode).
- `ChatPanel` obtains modes from the hook and passes them down as **primitive props** to `MessageBubble` and directly to the live-buffer parts; `MessageBubble` forwards to `ThinkingBlock` / `ToolBlock`. Primitives matter: `ToolBlock` is `memo`ized specifically to keep per-token re-renders from cascading (TurnParts.tsx:202–205); passing a fresh object prop would defeat that memo on every render.
- The resolver never touches `chatStore` messages, never re-derives entries, and never runs in the streaming hot path — it recomputes only when the config changes.

## 8. Settings UI — new "Chat display" group

A new nav entry in `SettingsPanel.tsx`: `{ id: "chat-display", label: "Chat display" }` added to the same group section that contains `Compact`, plus a `renderGroup` case returning the new **`ChatDisplayForm`** (`web/src/components/Settings/ChatDisplayForm.tsx`). The label deliberately avoids "Compact", which already means transcript compaction (`CompactForm`, nav line 76). Desktop uses the identical form through the embedded SPA — no separate desktop frontend work.

Wireframe (house form styling: `text-xs text-muted-foreground` labels, small Save button):

```
Chat display
──────────────────────────────────────────────────────────────
Controls how much detail the rendered chat shows — thinking,
tool calls, tool output, and activity notices. Display only:
the stored transcript, search, and what the model sees are
unchanged.

  Preset
  (•) Full      Show everything (current default).
  ( ) Balanced  Hide older thinking and tool-call details;
                keep tool output with a short preview.
  ( ) Quiet     Show only headers; tool output and activity
                notices stay collapsed until opened.

  The latest thinking block always stays expanded.

  Per-category overrides
  Older thinking      [ Follow preset        ▾ ]
  Tool call details   [ Follow preset        ▾ ]
  Tool output         [ Follow preset        ▾ ]
  Activity notices    [ Follow preset        ▾ ]
                      options: Follow preset | Always expanded | Always collapsed

  [ Reset overrides ]                     [ Save changes ]
  Couldn't save chat display settings: <reason>     ← inline, role="alert", only on error
```

Behavior:

- Loads via `GET` on mount (`loading` state, `CompactForm` pattern); load failure shows the inline error (role `alert`) and leaves the form at defaults; the renderer independently stays at Full (§7).
- Preset is a radio group; overrides are four labeled selects. All state is local until **Save changes**, which `PUT`s the whole section and disables the button while saving (`disabled={saving}`).
- **Reset overrides** sets the four selects back to "Follow preset" locally (still requires Save); it does not touch the preset.
- Inline errors for both load and save failures; silent success on save, consistent with the other settings forms.
- Saving publishes `chat_verbosity_changed`, so every open chat tab (and other windows on the same server) re-renders immediately — the form itself does not need to push state anywhere else.

## 9. Category behavior table

Preset defaults (what `preset` resolves to per category; overrides replace a cell verbatim):

| Category (config key)   | `full`                                  | `balanced`                              | `quiet`                                 |
|-------------------------|-----------------------------------------|-----------------------------------------|-----------------------------------------|
| Older thinking (`older_thinking`) | expanded                                | collapsed                               | collapsed                               |
| Tool-call details (`tool_calls`)  | expanded\*                              | collapsed                               | collapsed                               |
| Tool output (`tool_output`)       | expanded\*                              | expanded (20-line tail preview)         | collapsed                               |
| Activity notices (`activity_notices`) | expanded (each inline)              | expanded (each inline)                  | collapsed (grouped disclosure, §10)     |
| **Latest thinking** (`latest_thinking`) | **expanded (invariant)**           | **expanded (invariant)**                | **expanded (invariant)**                |

- `latest_thinking` has no config key and no override: the resolver hard-codes `expanded`. A user can still manually collapse it in the UI (§10); the invariant governs the *default*, and any policy change re-applies it open.
- `expanded` for tool output means the output region opens with rendering unchanged: outputs longer than 20 lines show the existing tail preview plus "… N earlier lines · click to expand" (`TOOL_OUTPUT_PREVIEW_LINES`, TurnParts.tsx:8, 343–353).
- `collapsed` for activity notices means grouping (§10), not silent deletion — the notices remain reachable one click away.
- \* **`full` compatibility nuance:** under `full` with no overrides, the two ToolBlock regions reproduce today's first-paint exactly, including the current initial-open heuristic `lineCount <= 50` (TurnParts.tsx:235): both disclosures default per that one evaluation at mount (details and output are coupled under `full`, as today). Today a tool that mounts while pending (`lineCount == 0`) therefore stays open through completion, while a >50-line output loaded from a committed transcript starts closed — both behaviors are preserved. Presets other than `full`, and any explicit override, replace the heuristic with the table value.

## 10. Rendering rules per component

**ThinkingBlock** (`TurnParts.tsx:51`)
- Older thinking blocks follow `olderThinking`; the **latest** thinking block (the newest `reasoning_content` in the rendered transcript, or the in-flight live thinking part) defaults to `latestThinking` = expanded under every preset.
- The block header ("🧠 Thinking" toggle) is always rendered; only the content region collapses.
- Manual collapse/expand always works — the invariant is about defaults, not about locking the user out.
- **Effective-default changes re-apply defaults and reset local state**: the two triggers are a policy change (event) or the block ceasing to be "latest" when newer thinking arrives — both recompute the block's effective default and discard its prior manual/search state. Nothing else resets local state: manual toggles persist until the default itself changes.

**ToolBlock** (`TurnParts.tsx:206`)
- The **concise header always stays visible** in every mode: tool name/args hint, line count, and the header status (`running…`, `awaiting your answer`).
- The single body gate is split into **two independent disclosures**: tool-call details (command block / raw args, today lines 298–312) and tool output (stream preview / result, lines 314–355). `tool_calls` sets the details default, `tool_output` sets the output default; each can be toggled without affecting the other.
- When output is expanded, the **existing 20-line tail preview is preserved** verbatim (tail window + earlier-lines toggle); `full` additionally keeps the `lineCount <= 50` initial heuristic per §9.
- The `Open question` button (lines 287–295) and header status indicators stay **outside both disclosures** — they are visible in all modes so a pending ask is never buried.
- A running tool's incremental stream (lines 314–320) is output-region content and follows `tool_output`; the `running…` status itself lives in the always-visible header.
- `memo` behavior must be preserved: modes arrive as primitive props (§7).

**Activity notices / StatusBlock** (`TurnParts.tsx:99, 116`; live dispatch `ChatPanel.tsx:1214–1217`)
- Under `quiet` (or `activity_notices = collapsed`), **consecutive live `notice` parts group into one compact disclosure** — a single toggle row with an accessible count ("~ 3 activity notices", singular for one) that expands to show the run inline. Every run, including a run of length one, renders behind the disclosure so "collapsed" uniformly means no notice text is visible without a click. A non-notice part (`status`, `tool`, `text`) ends the current run.
- Under `expanded`, notices render individually as today.
- **`StatusBlock` is never hidden or grouped** under any preset or override — it is the "still working" spinner line and must always render.

**Never collapsed, no override in v1**
- User messages and assistant text.
- Questions: `QuestionAnswerBlock` (fully rendered, no collapse state) plus the `QuestionDialog` / `PermissionDialog` modals and the `Open question` affordance.
- Permission surfaces in general (dialogs and pending-ask chrome).
- Actionable error surfaces: turn-error banners, the ChatPanel load-failure alert, action error toasts.
- Compaction summaries (`CompactionNotice`) — already collapsed by default (line 40); no new override key targets them.

## 11. Search and virtualization

- **Search still searches the full transcript.** Server-side matching (`handler_session_search.go`) is unchanged and rendering-agnostic — it scans every stored message regardless of any collapse state; client helpers (`sessionSearch.ts`) and highlight rendering are untouched.
- **A selected match force-opens its containing collapsed block.** When `currentMatchEntryPos` (ChatPanel.tsx:531) points at an entry, that entry renders with all of its collapsible regions open — thinking content, tool details, tool output, and the tail-preview lift (full output) — so any highlight inside it is actually visible. The force-open sets the block's local open state and **latches** (stays open after the match cursor moves away) to avoid collapsing content under the reader and the height thrash that would yank the scroll position; the latch is cleared when a policy change re-applies defaults (§10). The existing jump machinery — `scrollToIndex` (line 826), `olderPrefixFetch` prefix loads, and the `attemptedJumpsRef` dedupe (line 844) — keeps working unchanged on top.
- **Virtualizer remeasure after policy changes.** Collapsing/expanding regions changes row heights, so a policy change must invalidate and re-measure row sizes. Mounted rows already refresh through the `measureElement` ref and its ResizeObserver wiring (ChatPanel.tsx:1139); the policy-change handler must ensure the measurement pass runs and that any cached sizes for off-screen rows are refreshed before the next scroll. Scroll anchoring is preserved through the existing machinery: a viewport pinned to the bottom (`atBottomRef`, ResizeObservers at lines 692–763) stays pinned after remeasure; a reader mid-transcript keeps their position via the existing `scrollMargin` sync (lines 218–357).
- **Stable keys.** Virtual item keys stay message/entry-identity based (comment at line 224); policy/mode values must never be folded into a key — a policy change may update props but must not remount the list (remounts would drop local open state and jump the scroll).

## 12. Edge cases

| Case | Behavior |
|---|---|
| Config GET fails (network, older server without the endpoint → JSON 404 from `spaHandler`) | Resolver receives no config ⇒ renderer falls back to **Full** (today's rendering); `ChatDisplayForm` shows its inline load error; Save may still be attempted and surfaces its own error inline. |
| Malformed values on disk (hand-edited `ocodeconfig.json`) | GET normalizes (unknown preset ⇒ `full`, unknown override ⇒ `preset`); the resolver also defends client-side. PUT is strict: 400, never silently coerced. |
| Before the first config response resolves | Store holds Full ⇒ no flash of collapsed content that later expands (or vice versa). |
| Concurrent saves from two windows/processes | `withOcodeConfigLock` serializes read-modify-write; PUT replaces the whole section (last write wins). |
| Second ocode process sharing the same config file | The event is **process-wide only**; another process picks the value up on its next GET/restart, not live. Accepted for v1 (see §17). |
| Remote-hosted sessions | Displayed by the local SPA ⇒ the local setting governs their rendering; no `host` param, no remote fetch (§3). |
| Manual toggle, then a policy change | Policy change re-applies defaults (local state reset) — by design. |
| Search force-open, then cursor moves | Block stays open (latch, §11) until the next policy change. |
| Running tool under `quiet` | Header shows `running…`; live partial output follows `tool_output` (hidden while collapsed); the always-visible status keeps the turn from looking stalled. |
| Empty / no-message session | Nothing to style; resolver output unused; no errors. |
| TUI running against the same config file | Ignores `chat_verbosity` in v1 — zero TUI behavior change. |
| Downgrade to an older ocode | Key survives via `Extra` round-trip (ocodeconfig.go:1561, 2068); older builds render as today. |
| Desktop + web attached to one server | Both receive `chat_verbosity_changed` over their SSE streams and update without reload. |

## 13. Accessibility and performance

**Accessibility**
- Preset selector is a real radio group (fieldset/legend or `radiogroup` semantics) with arrow-key navigation; each option is a labeled `input[type=radio]`, not a styled div.
- Each override select has an associated `<label>` (house `text-xs text-muted-foreground` style).
- All disclosure toggles — ThinkingBlock header, both ToolBlock regions, the grouped-notices row — gain `aria-expanded` (currently absent in `TurnParts.tsx`) plus accessible names; the grouped-notices name includes its count.
- Errors render with `role="alert"`; the Save button is `disabled` and reads "Saving…" while in flight.
- No dialogs are introduced, so the project's Dialog focus conventions are not engaged; nothing here needs `data-dialog-default-action`.

**Performance**
- Resolver is pure and computed once per config change (`useMemo` / module store); it is not in the per-token streaming path.
- Modes reach `ToolBlock` as primitives so its `memo` (TurnParts.tsx:202–205) keeps working; no new object identity per render.
- Notice grouping is an O(n) pass over the small live buffer during render.
- One GET per app (shared store), one PUT per save, one event per save — no polling, no added backend load; transcript, search, and LLM flows are untouched.

## 14. Testing (write these FIRST — red, then implement)

TDD order: land the suites below failing against the current tree, then implement until green. House habit applies: **mutation-verify** each new test (temporarily revert the fix/feature, confirm the test fails, restore).

**Go — config and handler validation/persistence**
- `internal/config/ocodeconfig_test.go`: absent section ⇒ default `full`/all-`preset`; save→reload round-trip through the locked path preserves preset + overrides; canonical write handling (no double-write via `Extra`); unknown on-disk values normalize to defaults on read.
- `internal/server/handler_config_test.go` (use `testConfigHandler`, line 23 — isolated `HOME`): `GET` serves defaults; `PUT` with valid payload returns 200, persists to the throwaway file, and updates `h.cfg.Ocode.ChatVerbosity` in memory (mirror `TestHandleSetCompactConfigPersists`, line 97); `PUT` rejects bad preset → 400, bad override value → 400, unknown override category → 400, malformed body → 400 (mirror `TestHandleSetPermissionModeConfigRejectsInvalid`, line 326); `PUT` publishes `chat_verbosity_changed` on `h.bus` with payload = saved config, empty session id, and the event is absent from `sessionScopedEvents` (subscribe like `emitters_test.go:113`); route registration exists behind `authMiddleware` (`server.go`).

**TypeScript — resolver**
- `web/src/lib/chatVerbosity.test.ts`: table-driven presets → expected modes for all four categories; each override value beats the preset; `preset` override follows the preset; **`latestThinking === "expanded"` asserted across the full preset × override combination space** (the §4 invariant); `null`/`undefined`/missing fields/unknown enum strings fall back safely to Full semantics; resolver is pure (no store/IO).

**TypeScript — renderer**
- `TurnParts.test.tsx` additions: ThinkingBlock defaults (latest open under all three presets; older per policy), manual toggle still works, effective-default change resets local state; ToolBlock header (name, `running…`, `awaiting your answer`, `Open question`) visible with both regions collapsed in Quiet; details and output disclosures independent (opening one does not open the other); tail preview present when output expanded (>20 lines shows "… N earlier lines"); `full` reproduces the `lineCount <= 50` initial heuristic including the pending-mount case; NoticeBlock grouping (three consecutive notices → one disclosure, expand reveals all; a `status` part splits runs; lone notice → disclosure of one); StatusBlock never grouped/hidden under any preset; QuestionAnswerBlock and assistant/user text never collapse.
- ChatPanel integration, **committed and live paths**: modes reach `MessageBubble`/`TurnParts` from both `renderEntries` and the live-buffer dispatch; a search-selected match force-opens its containing collapsed block (and the latch survives cursor movement); a policy change triggers the virtualizer remeasure pass and preserves item keys (no remount) plus bottom-pin/scroll anchoring.
- Store/hook: baseline fetch applies; `chat_verbosity_changed` payload updates `useChatVerbosity()` output; fetch failure leaves Full.

**TypeScript — Settings form**
- `ChatDisplayForm.test.tsx` (+ a `SettingsPanel` group-dispatch case): loads and renders GET values; radio + four selects present with options; Reset overrides returns selects to "Follow preset" without touching the preset; Save issues `PUT` with the exact payload and is disabled while saving; save failure and load failure each render the inline `role="alert"` error; success renders no error.

**Gates before merge:** `go build ./...`, `go vet`, `go test ./internal/config ./internal/server`; `tsgo --noEmit`, focused Vitest runs for the files above, and `vite build`.

## 15. Rollout and compatibility

- **Default = `full` = today's rendering** (including the §9 initial-heuristic nuance): no user-visible change until they pick another preset. No feature flag needed.
- **No DB migration**; only `ocodeconfig.json` gains an optional section, materialized on first save.
- **Older ocode binary**: `chat_verbosity` survives round-trips via `Extra` — a downgrade does not strip the user's setting.
- **Older/absent server endpoint**: GET 404 ⇒ renderer Full, Settings inline error (graceful degradation to current behavior).
- **TUI** untouched; **remote** out of scope; display-only guarantees (transcript, search indices, LLM context unchanged) mean no data reindex or replay concerns.
- Process-wide event covers all clients of one server (desktop + web together); cross-process live sync is explicitly not in v1.

## 16. Expected files and components (planned — none of this exists yet)

| Area | Files |
|---|---|
| Config schema/save | `internal/config/ocodeconfig.go` (`ChatVerbosityConfig`, file struct, default, load-merge, `SaveOcodeChatVerbosity`, Extra write handling); `internal/config/ocodeconfig_test.go` |
| API + event | `internal/server/handler_config.go` (`HandleGetChatVerbosityConfig` / `HandleSetChatVerbosityConfig`, validation, `h.cfg` update, `Publish("chat_verbosity_changed", …)`); `internal/server/server.go` (GET/PUT routes behind `authMiddleware` + thin wrappers); `internal/server/handler_config_test.go` |
| API client | `web/src/api/client.ts` (`getChatVerbosityConfig` / `setChatVerbosityConfig`) and the `ChatVerbosityConfig` type (in `client.ts` beside `CompactConfig`, or `web/src/api/types.ts`) |
| Settings UI | `web/src/components/Settings/SettingsPanel.tsx` (new `chat-display` group id + label **"Chat display"** + `renderGroup` case); new `web/src/components/Settings/ChatDisplayForm.tsx` |
| Resolver/store/hook | new `web/src/lib/chatVerbosity.ts` (pure `resolveChatVerbosity`, module-level store, `useChatVerbosity`) |
| Chat rendering | `web/src/components/Chat/ChatPanel.tsx` (hook consumption, mode threading, search force-open, remeasure-on-policy-change); `web/src/components/Chat/MessageBubble.tsx` (forward modes); `web/src/components/Chat/TurnParts.tsx` (`ThinkingBlock`, `ToolBlock` split disclosures, `NoticeBlock` grouping, `aria-expanded`) |
| Focused tests | the Go/TS suites named in §14 (config, handler, resolver, TurnParts, ChatPanel committed/live, Settings form) |
| Docs on implementation | `skills/ocode-web/SKILL.md` (file map + a numbered rule covering the resolver invariant, the ToolBlock disclosure split, and the force-open latch); `CHANGES.md` dated entry; this spec is the design record |

## 17. Open questions

1. **Cross-process propagation** — a second `ocode serve` process (or a concurrently running TUI process) shares the config file but not the in-process event. Accept staleness until their next config read (v1 decision), or add file-mtime revalidation later?
2. **TUI parity** — should the TUI adopt `chat_verbosity` (its own render path in `internal/tui/tool_render.go`) in a v2? Not in v1.
3. **Scope of the setting** — global-only in v1; per-project or per-session verbosity later if wanted (would follow the per-session `permission_mode`/`model` override pattern, out of scope here).
4. **Lone notice under `collapsed`** — decided as disclosure-of-one for uniformity ("collapsed ⇒ no notice text without a click"); revisit after dogfooding if a single notice behind a toggle feels overwrought.
5. **Preset category mixes** — the `balanced`/`quiet` cell values in §9 are data-only: tuning them later changes only the preset table, not the schema, API, resolver contract, or tests beyond the table cases.
