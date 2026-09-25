---
type: Design
title: Chat Verbosity Display — Design Spec
description: 'Approved design spec for chat verbosity display, implemented 2026-09-25 (local web/desktop SPA only): chat_verbosity config (full/balanced/quiet presets + per-category overrides), GET/PUT /api/config/ocode/chat-verbosity with chat_verbosity_changed as an invalidation-only event (payload never applied, reconnect refetch), Settings → Chat display form, ChatPanel-owned virtualized disclosure state with policy revision, single ToolBlock outer/inner gate model, latest-thinking invariant, and strict no-silent-normalization failure handling. Design record with as-built status notes.'
tags:
  - design
  - spec
  - web
  - chat
  - settings
  - verbosity
  - superpowers
timestamp: 2026-09-25T16:19:13Z
---
# Chat Verbosity Display — Design Spec

**Status:** Approved design (2026-09-24), **amended the same day after advisor review**; **implemented 2026-09-25** (local web/desktop SPA only — TUI rendering and remote-host config fetching unchanged, per the v1 scope in §3). The body below remains the design record as approved, plus amendment 7 below: future-tense wording describes what was built, and the shipped implementation follows it (shared chat_verbosity config, authenticated GET/PUT /api/config/ocode/chat-verbosity, invalidation-only chat_verbosity_changed, ChatPanel-owned disclosure with policy revision, single ToolBlock outer/inner gate, latest-thinking invariant, strict 400 failure handling with no silent normalization, and virtualizer scroll anchoring), except as corrected by **amendment 7** (the §8 dynamic `Follow preset` label ships in the same change, and the shipped resolver's `balanced`/`tool_calls` cell — which had diverged to `expanded` — is realigned to §9's `collapsed`); and except that §11 search force-open ships **transient** — a `forceOpen` render prop derived from the current match, never written into the disclosure map — so a collapsed block reverts to its manual/policy state when the match cursor moves away and a manual choice is never overwritten. For the as-built file map and regression tests see skills/ocode-web/SKILL.md item 35 and the 2026-09-25 CHANGES.md entry.

**Amendment note (2026-09-24):** six advisor-reviewed changes are incorporated throughout: (1) `chat_verbosity_changed` is an invalidation signal only — its payload is never applied (§5, §6, §7); (2) disclosure state is owned by `ChatPanel` in a policy-versioned map because virtualized rows unmount (§7, §10, §11); (3) precise "latest thinking" semantics (§9, §10); (4) a single `ToolBlock` implementation with one outer call/details gate and one inner output gate (§10); (5) explicit virtualizer anchoring on policy changes (§11); (6) strict failure handling with no server-side normalization of malformed values (§6, §7, §12). **Amendment 7 (2026-09-25)** adds a seventh change: (7) §8's per-category override selects show a dynamic `Follow preset — <Expanded|Collapsed>` label whose suffix is the §9 cell for the currently selected draft preset (the option value stays `preset`), and the `balanced`/`tool_calls` resolver cell is aligned to §9 (`collapsed`) — §9 itself is unchanged.

**Amendment 7 — resolver divergence note:** the shipped resolver — `web/src/lib/chatVerbosity.ts` (`resolveChatDisplayPolicy`) and `internal/config.ResolveChatVerbosityPolicy` (`internal/config/ocodeconfig.go`) — had diverged from §9 for the `balanced`/`tool_calls` cell: both resolved `expanded` under `balanced` (collapsing tool-call details only under `quiet`) instead of §9's `collapsed`. The code is being aligned to the spec — §9 is the source of truth and is not amended — and `skills/ocode-web/SKILL.md` item 35(b) is updated in the same change.

---

## 1. Context and evidence

Verified anchors in the current codebase (line numbers as of this writing):

- **Config layer** — `internal/config/ocodeconfig.go`: `OcodeConfig` struct (line 481) carries nested blocks such as `Compact CompactConfig` (line 482, struct at 296); the on-disk shape lives in `ocodeConfigFile` (line 821) with per-block file structs (e.g. `compactConfigFile`, line 746); defaults are applied in `defaultOcodeConfig()` (line 911); file→memory merge helpers such as `applyCompactConfig` (line 1715); section saves go through `withOcodeConfigLock` (line 1918 → `lockOcodeConfig` at 1876), a cross-process advisory lock with an atomic tmp+rename write (`writeOcodeConfigFile`, lines 1938–2108). Unknown on-disk keys round-trip through the `Extra` map (load at line 1561, write loop at 2068–2076), and canonical keys are listed in the write-path skip list (line 2072).
- **Settings API layer** — `internal/server/handler_config.go`: the GET/PUT-per-section pattern is `HandleGetCompactConfig` (line 1393) / `HandleSetCompactConfig` (line 1404): `readBodyJSON` → `writeError(w, http.StatusBadRequest, …)` on invalid input (validation examples: lines 1573, 1845–1853) → section save → in-memory `h.cfg.Ocode.<Block>` update under `h.mu` → `writeJSON` 200. Routes are registered in `internal/server/server.go` behind auth (lines 370–371: `GET/PUT /api/config/ocode/compact` wrapped in `s.authMiddleware`) with thin delegating wrappers (lines 2016–2021).
- **Event bus** — `internal/server/event_bus.go`: `Publish(event, project, sessionID, data)` (line 160) fans an envelope out to every subscriber of the process. The envelope struct (`Envelope`, lines 16–21) is exactly `{Event, Project, SessionID, Seq, Data}` — **it carries no `host` field** (this premise is load-bearing for §5/§7). `sessionScopedEvents` (line 53) lists events that require a session id (a global event must NOT be added there), and `criticalEvents` (line 35) is reserved for terminal turn events. Process-wide publishes with an empty session id are the established pattern (`h.bus.Publish("spending", "", "", …)`, `internal/server/emitters.go:209`; also `logs`, `git_status`).
- **Frontend transport** — `web/src/lib/eventBus.ts`: `on(event, handler)` (line 112) subscribes by event name; `onReconnect(handler)` (line 130) fires every handler on stream re-establishment and on seq-gap reconciliation, per the documented reliability contract (lines 21–31: "state fetch + transcript refetch — never event replay"). One fetch-based SSE stream per host feeds a single subscriber path, and handlers receive only the envelope (`BusEnvelope` = `{event, project?, session_id?, seq, data}` — again **no host field**). Global (non-session-routed) events are consumed directly (e.g. `eventBus.on("spending", …)` in `web/src/App.tsx:131`), separate from the session-routed envelope path (`ROUTABLE_EVENTS`, `web/src/lib/sessionEvents.ts:184`).
- **Frontend API client** — `web/src/api/client.ts`: `fetchJSON<T>(path, init?, host?, projectPath?)` (line 446); config helpers follow `getCompactConfig` / `setCompactConfig` (lines 788–790) with the payload type declared alongside (e.g. `CompactConfig`, line 62).
- **Settings UI** — `web/src/components/Settings/SettingsPanel.tsx`: `SettingsGroupId` union (line 31), nav entries as `{ id, label }` (line 76 is `{ id: "compact", label: "Compact" }` — **"Compact" already means transcript auto-compaction**, hence the new group name below), form dispatch switch `renderGroup` (line 105, e.g. line 123–124). `CompactForm.tsx` is the house pattern for a config form: `loading`/`saving`/`error` state, `load()`/`save()` callbacks (lines 28–60), inline error text (line 73), disabled-while-saving Save button (line 119).
- **Chat rendering** — `web/src/components/Chat/TurnParts.tsx`: `TOOL_OUTPUT_PREVIEW_LINES = 20` (line 8, the tail preview that must be preserved); `ThinkingBlock` (line 51) defaults to open (`useState(true)`, line 60); `StatusBlock` (line 99, transient spinner line); `NoticeBlock` (line 116, transient `~` notice); `QuestionAnswerBlock` (line 161); memoized `ToolBlock` (line 206, memo rationale in the comment at 202–205) with initial open heuristic `useState(lineCount <= 50)` (line 235), header status `running…` / `awaiting your answer` (lines 284–285), the `Open question` button rendered outside the collapsible body (lines 287–295, body gate starts at 296), and the "… N earlier lines · click to expand" toggle (lines 343–353). None of the disclosure buttons currently expose `aria-expanded`.
- **Chat panel** — `web/src/components/Chat/ChatPanel.tsx`: render entries (`single` | `tool-group`) memoized at line 272; virtualization via `useVirtualizer` (line 346) with item keys derived from message-object identity (comment at 224) and `ref={virtualizer.measureElement}` (line 1139); live-buffer rendering dispatches `ThinkingBlock` / `AssistantText` / `StatusBlock` / `NoticeBlock` / tool parts (lines 1209–1220); search plumbing: `currentMatchEntryPos` (line 531), `scrollToIndex` (line 826), `attemptedJumpsRef` (line 844), `highlight` (line 1134); bottom-pin machinery via `atBottomRef` and ResizeObservers (lines 84–89, 692–763).
- **Store / types** — `web/src/stores/chatStore.tsx`: live parts are transient; `isTransientLivePart` = `status | notice` (lines 122–123). `web/src/api/types.ts`: `LivePart` union (lines 22–43).
- **Search** — server side is full-transcript and rendering-agnostic: `internal/server/handler_session_search.go` (`HandleSearchSession`, line 61; matches via `agent.MessageMatchesQuery`, line 100) returning indices only; client helpers in `web/src/lib/sessionSearch.ts` (`serverIndexToLocal` 45, `olderPrefixFetch` 89, `inWindowMatchCount` 113, `buildJumpTargets` 139).
- **Compaction summaries** — `web/src/components/Chat/CompactionNotice.tsx` is already collapsed by default (`useState(false)`, line 40); no new override applies to it in v1.
- **Desktop parity** — the desktop shell embeds the same React SPA and server (see `skills/ocode-desktop` and `skills/ocode-web`); there is no separate desktop frontend to change.
- **Repository error-handling rules** — any catch/log/rethrow added by the eventual implementation must follow the house conduct rules captured in `skills/kaizen/conduct-tuning-*/SKILL.md`: a caught error that is rethrown owes a structured log of what was attempted plus the error first; an empty catch is banned; the only carve-out is a known-benign/suppressed case marked with an inline `// intentionally not logged: <reason>` comment.
- **Not present anywhere yet** — repository-wide search finds no `chat_verbosity`, `ChatVerbosity`, `chat-verbosity`, or `chatVerbosity` in `internal/` or `web/src/`. This document specifies net-new work. (Pre-implementation observation from 2026-09-24; since implemented 2026-09-25 — see the Status note above.)

## 2. Goals

1. Let users pick how verbose the **rendered** chat is in the web UI and desktop app, via a shared, server-persisted setting (not per-browser localStorage).
2. Provide three presets — `full`, `balanced`, `quiet` — plus per-category overrides so a user can mix (e.g. Quiet preset but always-expanded older thinking).
3. Guarantee that the latest thinking block of the latest assistant turn is always expanded by default under `full`, `balanced`, **and `quiet`** (semantics in §9/§10).
4. Change display only: transcript data, stored messages, search indices, and the LLM context are untouched.
5. Keep the default (`full`) byte-compatible with today's rendering so existing users see zero change until they opt in.
6. Propagate a save to every client attached to the same server process without a reload, via a process-wide `chat_verbosity_changed` event consumed **strictly as an invalidation signal** — clients re-fetch the config and never apply the event payload (§5, §7).
7. Disclosure state must survive virtualization: rows unmounting and remounting during scroll must never lose or reset the reader's open/closed choices (§7, §11).

## 3. Non-goals / scope

- **No localStorage** for this setting (explicit user decision); the source of truth is `ocodeconfig.json` on the server.
- **TUI unchanged in v1** — the TUI has its own render path (`internal/tui/tool_render.go`) and will ignore `chat_verbosity`.
- **Remote-host behavior out of scope for v1** — no `host`-threaded variant of the endpoint (unlike some config APIs that accept `host` in `fetchJSON`). Note that rendering always happens in the local SPA, so the local verbosity setting governs how transcripts from remote-hosted sessions are *displayed* too; what is out of scope is fetching/storing verbosity *from* a remote server or per-host settings. This interacts with the event design: because the event envelope has **no host field** (§1), applying an event payload could never be attributed to a source and could cross-apply between remote and local configs — another reason v1 treats the event as invalidation-only and re-fetches the local config (§5, §7).
- **No DB migration** — config file only; nothing in sqlite/session storage changes.
- No new override for compaction summaries (already collapsed), permission dialogs (modal), or question asks.
- No per-project or per-session verbosity in v1 (see §17).

## 4. User decisions (approved)

1. **Shared server config, not localStorage** — persisted in `ocodeconfig.json`, served over an authenticated API, same lifecycle as every other Settings block.
2. **Preset plus per-category overrides** — a single preset selects a baseline; each category can override with `preset` (follow), `expanded`, or `collapsed`.
3. **Latest thinking stays expanded** — the latest thinking block defaults to expanded under **all three presets** (`full`, `balanced`, `quiet`), independent of any override; this is an invariant of the resolver, not a configurable key.

The user also approved the approach, architecture, UI, and rendering/validation sections as proposed.

**Advisor-reviewed amendments (2026-09-24)** — the same review produced six amendments, incorporated throughout this revision without changing any of the three decisions above: event invalidation-only semantics; ChatPanel-owned virtualized disclosure state with an explicit policy revision; precise latest-turn thinking semantics; a single ToolBlock implementation (one outer call/details gate, one inner output gate); virtualizer anchoring via supported APIs on policy changes; and strict failure handling (retain last cached policy, explicit Full compatibility default with a structured warning when there is no cache, hard 400s for invalid enums, no server-side normalization).

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
- Go types (planned): `ChatVerbosityConfig` + `ChatVerbosityOverrides` in `internal/config/ocodeconfig.go`, a `chatVerbosityConfigFile` field on `ocodeConfigFile` (`json:"chat_verbosity,omitempty"`), `defaultChatVerbosityConfig()` returning `full`/all-`preset`, a load-merge following the `applyCompactConfig` pattern, and `SaveOcodeChatVerbosity(cfg)` persisting through `withOcodeConfigLock`. The write path must handle the key canonically like `compact` (payload entry + the Extra skip list at line 2072) so it never double-writes; an absent section is re-materialized only when the user saves. An **older ocode binary** reading a config that contains `chat_verbosity` preserves it via the `Extra` round-trip (lines 1561, 2068). Loading must **not** normalize malformed enum values in the file — malformed stored values are surfaced as API errors (§6), never silently rewritten.

Data flow (amended):

```
Settings form ── PUT /api/config/ocode/chat-verbosity ──▶ handler validates (400 on bad values,
                                                          never silently normalized)
                                                          │
                                                          ├─ SaveOcodeChatVerbosity (config lock, atomic write)
                                                          ├─ h.cfg.Ocode.ChatVerbosity = req  (in-memory, under h.mu)
                                                          └─ h.bus.Publish("chat_verbosity_changed", "", "", savedCfg)
                                                                    │  (process-wide, empty project/session;
                                                                    │   NOT in sessionScopedEvents, NOT criticalEvents)
                                                                    │  envelope = {event, project, session_id, seq, data}
                                                                    │  — NO host field, so the payload is UNATTRIBUTABLE
        ┌───────────────────────────────────────────────────────────┘
        ▼
eventBus.on("chat_verbosity_changed")  ──▶ store IGNORES env.data (invalidation signal only)
        │                                   └─▶ re-GET /api/config/ocode/chat-verbosity
eventBus.onReconnect(...)  ──────────────▶ same re-GET (events missed during an outage/gap)
        ▼
module-level verbosity store: validate payload → resolveChatVerbosity →
        │                         bump policyRevision iff the effective policy changed → retain as last-good cache
        │  (fetch OK)                    │  (fetch FAIL)
        ▼                                ▼
   useChatVerbosity()             retain last cached config (§6/§7 failure contract);
        │                          if NO cache exists ⇒ Full as an EXPLICIT compatibility
        │                          default + structured warning log + error surfaced to Settings
        ▼                                │
   ChatPanel ──▶ disclosure map (policy-versioned, survives row unmount) ──▶ TurnParts
   Settings form (own GET/PUT + inline errors — shows load/serve errors verbatim)
```

Two invariants of this flow:

- **The event payload is never applied.** `chat_verbosity_changed` only triggers a local `GET`. The envelope has no host field on either side (§1), so a payload could not be attributed to the server it came from; re-fetching is always the local, non-host-threaded endpoint, which makes remote/local cross-application impossible. Remote hosts remain out of v1 (§3).
- **Reconnect re-fetches.** SSE streams reconnect with backoff and fire `onReconnect` handlers on re-establishment and seq-gap (§1, eventBus contract) — exactly the moments when a `chat_verbosity_changed` may have been missed. The store re-fetches on `onReconnect` too; it never relies on event replay.

## 6. API

Authenticated (wrapped in `authMiddleware`, mirroring `server.go:370–371`):

- **`GET /api/config/ocode/chat-verbosity`** → `200` with the stored config:

  ```json
  { "preset": "full",
    "overrides": { "older_thinking": "preset", "tool_calls": "preset",
                   "tool_output": "preset", "activity_notices": "preset" } }
  ```

  Absent section ⇒ the default above (an absent section is a default, not a malformed value). **Malformed stored values are NOT normalized on read** (amendment 6): if the stored section contains an invalid `preset` or override value (e.g. a hand-edited file), GET responds `400` with `invalid stored chat_verbosity: <detail>` naming the offending field, and the server logs the rejection. The renderer then follows the failure contract in §7 (retain last cached policy; explicit Full fallback with a structured warning if there is no cache), and Settings shows the error (§8). The resolver's client-side defense (§7) remains purely as a crash-prevention backstop and should be unreachable against this API.

- **`PUT /api/config/ocode/chat-verbosity`** — body is the same shape. Validation (strict, no silent coercion anywhere server-side — the house rule: the client sends what it has, the server 400s malformed input):
  - invalid JSON body → `400` `"invalid request body"`
  - unknown `preset` → `400` `"preset must be one of: full, balanced, quiet"`
  - unknown override value → `400` `"overrides.<category> must be one of: preset, expanded, collapsed"`
  - unknown override category key → `400` `"unknown override category: <key>"`
  - save failure → `500` with the error text (existing `writeError` pattern)

  **Server-side invalid enum values are hard 400 errors; malformed values are never silently normalized** — on PUT or on GET (stored data).

  On success: persist via `SaveOcodeChatVerbosity` (the normal config save/lock path), update `h.cfg.Ocode.ChatVerbosity` under `h.mu`, publish `chat_verbosity_changed` process-wide with the saved config as payload (empty project and session id, exactly like `spending`), and respond `200` with the saved config. The payload exists for debuggability only — **clients must ignore it** (§5); dropping it later would not break the contract.

## 7. Frontend architecture (pure resolver + small store + ChatPanel-owned disclosure map)

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

// Pure: no I/O, no store access. null/undefined ⇒ Full defaults (the explicit
// compatibility default — the STORE decides when falling back is allowed and logs).
function resolveChatVerbosity(cfg: ChatVerbosityConfig | null | undefined): EffectiveVerbosity;
```

> **Note (implemented 2026-09-25):** As shipped, the resolved policy is `ChatDisplayPolicy` (`web/src/api/types.ts`), and the pure resolver is `resolveChatDisplayPolicy` (`web/src/lib/chatVerbosity.ts`) with a Go twin `config.ResolveChatVerbosityPolicy` (`internal/config/ocodeconfig.go`) — both resolvers must implement the §9 matrix in step. The shipped fields use the §6 wire-case names: `older_thinking`, `latest_thinking`, `tool_calls`, `tool_output`, `notices`, plus a constant `status: "expanded"` field that materializes §10's 'StatusBlock is never hidden or grouped'. Note the one non-casing rename: the config key `activity_notices` maps to the policy field `notices`, so anything reading a resolved cell must not index it with the config key. The CONFIG/override keys are unchanged from the `Category` union above.

- Resolution rule: `effective(cat) = overrides[cat] !== "preset" ? overrides[cat] : presetTable[preset][cat]`; `latestThinking ≡ "expanded"` regardless of preset and overrides. Unknown enum strings defensively fall back (`preset`→`full` category default; bad override→`preset`) **only** as a crash-prevention backstop: the API guarantees validated payloads (§6), so exercising this path indicates a defect and the store logs a structured warning when a *successful* fetch nonetheless fails validation.
- **Store behavior (amended):**
  - Baseline: `GET` once on first subscriber; expose `useChatVerbosity()` returning memoized `EffectiveVerbosity`. Multiple chat tabs share one fetch/subscription (no per-tab stampede); overlapping invalidations coalesce into a single in-flight GET.
  - **`eventBus.on("chat_verbosity_changed")` ignores `env.data` entirely** and schedules a re-GET. The payload is never merged, deep-compared, or used (unattributable envelope, §5).
  - **`eventBus.onReconnect` schedules the same re-GET** — events missed during an outage are recovered by refetch, never replay.
  - On a successful fetch: validate, resolve, and **bump `policyRevision` iff the resolved `EffectiveVerbosity` differs from the current one**; retain the config as the last-good cache.
  - **Failure contract:** retain the last cached config and keep rendering with it. If **no cached config exists**, fall back to Full **only as an explicit compatibility default**: emit a structured warning log (stable message prefix, e.g. `console.warn("[chat-verbosity] …", { reason: "no-cached-policy", error })`), expose the error so Settings can display it (§8), and render with Full. Goal 5 doubles as that compatibility mode. Any catch/log/rethrow in the implementation follows the repository rules from §1.
- **Disclosure state (amended — ChatPanel owns it, not the blocks):** virtualized rows unmount when scrolled off-screen, so `ThinkingBlock`/`ToolBlock` **must not** be the source of truth for open/closed state. `ChatPanel` owns a disclosure map keyed by **stable message/tool identity + region** — regions `thinking` (key `<messageId>`), `call` (key `<toolId>`), `output` (key `<toolId>`), plus `notices` for grouped notice runs — alongside the store's `policyRevision`:
  - A block's visible state = `map.get(key)` if present (a manual or search choice), else the policy default derived from `EffectiveVerbosity` (and the latest-thinking rule, §9/§10).
  - **The map is cleared/rebased only when `policyRevision` changes** (i.e. only when the effective policy changed). Manual choices survive scroll, unmount, remount, session switching within the tab's panel lifetime, and every render that does not change policy.
  - Search force-open writes into the same map and therefore latches the same way (§11).
  - `ThinkingBlock`/`ToolBlock` become **controlled renderers**: they receive primitives (region keys, `open` booleans derived by ChatPanel, a stable `onToggle(key)` callback) and hold no disclosure state of their own. This keeps `ToolBlock`'s `memo` effective (TurnParts.tsx:202–205) — primitives only, no fresh objects per render.
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
  Older thinking      [ Follow preset — Expanded   ▾ ]
  Tool call details   [ Follow preset — Expanded   ▾ ]
  Tool output         [ Follow preset — Expanded   ▾ ]
  Activity notices    [ Follow preset — Expanded   ▾ ]
                      options: Follow preset — <Expanded|Collapsed> |
                               Always expanded | Always collapsed
                      suffix = the §9 cell for the selected preset (Full is
                      selected above; under Balanced, "Tool call details"
                      reads "Follow preset — Collapsed")

  [ Reset overrides ]                     [ Save changes ]
  Couldn't save chat display settings: <reason>     ← inline, role="alert", only on error
```

Behavior:

- Loads via `GET` on mount (`loading` state, `CompactForm` pattern); load failure — including a `400` from a malformed stored section (§6) — shows the inline error (role `alert`) and leaves the form at defaults; the error text names the offending field when the server provides it. The renderer independently follows the §7 contract (retain last cached policy, else explicit Full fallback + warning).
- Preset is a radio group; overrides are four labeled selects. All state is local until **Save changes**, which `PUT`s the whole section and disables the button while saving (`disabled={saving}`).
- **Dynamic `Follow preset` label (amendment 7)** — the suffixed option keeps the `value` `"preset"`; only its visible TEXT changes to `Follow preset — <Expanded|Collapsed>`, computed from the §9 cell for the currently selected **draft** preset (via the same resolver the renderer uses), so it updates **before Save** whenever the preset radio changes. The option is always present: every category resolves definitively (`expanded` or `collapsed`) under all three presets. The §9 `full` `*` heuristic is not reflected in the suffix — under Full, both tool regions read `Follow preset — Expanded` regardless of the `lineCount <= 50` initial-open heuristic. The select keeps its per-category `aria-label`; tests query by option text, not the aria-label.
- **Reset overrides** sets the four selects back to the `preset` option locally — displayed as `Follow preset — <Expanded|Collapsed>`, with the suffix recomputed from the then-selected preset (still requires Save); it does not touch the preset.
- Inline errors for both load and save failures; silent success on save, consistent with the other settings forms.
- Saving publishes `chat_verbosity_changed`; every open chat tab (and other windows on the same server) receives it **as an invalidation signal and re-fetches** (§5) — the form itself does not need to push state anywhere else.

## 9. Category behavior table

Preset defaults (what `preset` resolves to per category; overrides replace a cell verbatim):

| Category (config key)   | `full`                                  | `balanced`                              | `quiet`                                 |
|-------------------------|-----------------------------------------|-----------------------------------------|-----------------------------------------|
| Older thinking (`older_thinking`) | expanded                                | collapsed                               | collapsed                               |
| Tool-call details (`tool_calls`)  | expanded\*                              | collapsed                               | collapsed                               |
| Tool output (`tool_output`)       | expanded\*                              | expanded (20-line tail preview)         | collapsed                               |
| Activity notices (`activity_notices`) | expanded (each inline)              | expanded (each inline)                  | collapsed (grouped disclosure, §10)     |
| **Latest thinking** (`latest_thinking`) | **expanded (invariant)**           | **expanded (invariant)**                | **expanded (invariant)**                |

- `latest_thinking` has no config key and no override: the resolver hard-codes `expanded`, so the invariant holds in **Full, Balanced, and Quiet** alike. **Precise semantics (amendment 3):** *latest* means the **last thinking block in the latest assistant turn**. It stays expanded while that turn is streaming and **remains expanded after commit until a newer assistant turn begins**; at that point the block is older thinking and follows the `olderThinking` policy. A user can still manually collapse it in the UI (§10); the invariant governs the *default*, and any policy revision re-applies it open (§7).
- `expanded` for tool output means the output region opens with rendering unchanged: outputs longer than 20 lines show the existing tail preview plus "… N earlier lines · click to expand" (`TOOL_OUTPUT_PREVIEW_LINES`, TurnParts.tsx:8, 343–353).
- `collapsed` for activity notices means grouping (§10), not silent deletion — the notices remain reachable one click away.
- \* **`full` compatibility nuance:** under `full` with no overrides, the two ToolBlock regions reproduce today's first-paint exactly, including the current initial-open heuristic `lineCount <= 50` (TurnParts.tsx:235): one heuristic evaluation sets both regions' default (details and output are coupled under `full`, as today). Today a tool that mounts while pending (`lineCount == 0`) therefore stays open through completion, while a >50-line output loaded from a committed transcript starts closed — both behaviors are preserved. The heuristic is evaluated when a tool entry is first rendered and frozen in a ChatPanel-owned default memo (so virtualized unmount/remount does not silently re-roll a default when the line count changed while off-screen); the memo is cleared together with the disclosure map on a policy revision (§7), at which point a return to `full` re-evaluates it against the current line count. Presets other than `full`, and any explicit override, replace the heuristic with the table value.

## 10. Rendering rules per component

**ThinkingBlock** (`TurnParts.tsx:51`) — now a **controlled renderer**: no `useState` for open/closed; `ChatPanel` derives `open` from the disclosure map + policy defaults and passes primitives (§7).
- Older thinking blocks follow `olderThinking`; the **latest** thinking block is the last `reasoning_content` of the latest assistant turn (or the in-flight live thinking part while that turn streams) and defaults to `latestThinking` = expanded **in Full, Balanced, and Quiet**. It stays expanded through streaming and after commit until a newer assistant turn begins; then it is older thinking and its *default* follows `olderThinking` (a manual choice in the map still wins — §7).
- The block header ("🧠 Thinking" toggle) is always rendered; only the content region collapses.
- Manual collapse/expand always works and is written to the disclosure map — the invariant is about defaults, not about locking the user out; the choice survives virtualized unmount/remount because ChatPanel, not the row, holds it.
- **Default changes never clobber manual state; only a policy revision does.** A block ceasing to be "latest" changes its derived default only if the reader has no manual entry for it; `policyRevision` bumps clear the map (§7) and re-apply all defaults, including the latest-thinking invariant.

**ToolBlock** (`TurnParts.tsx:206`) — **one implementation, two gates (amendment 4).** The separate `tool_calls` and `tool_output` overrides are retained, but there is exactly **one outer call/details gate and one inner output gate inside this single memoized component** — never two independent ToolBlock implementations.
- The **concise header always stays visible** in every mode: tool name/args hint, line count, and the header status (`running…`, `awaiting your answer`).
- **Outer gate — tool-call details** (command block / raw args, today lines 298–312): default from `tool_calls`, state in the map under region `call`.
- **Inner gate — tool output** (stream preview / result, lines 314–355): default from `tool_output`, state in the map under region `output`. The gates are two sections of the one component body and toggle independently: the output section is **not nested inside the collapsed details section**, so `tool_calls: collapsed` + `tool_output: expanded` (the `balanced` row of §9) still renders output. Neither gate's state is derived from the other's.
- When output is expanded, the **existing 20-line tail preview is preserved** verbatim (tail window + earlier-lines toggle); `full` additionally keeps the single `lineCount <= 50` heuristic as the common default for both regions per §9.
- The `Open question` button (lines 287–295) and header status indicators (`running…`, `awaiting your answer`, pending/question affordances) stay **outside both gates** — always visible in all modes so a running tool or pending ask is never buried.
- A running tool's incremental stream (lines 314–320) is output-region content and follows `tool_output`; the `running…` status itself lives in the always-visible header.
- `memo` behavior must be preserved: open flags arrive as primitives plus one stable `onToggle` callback (§7).

**Activity notices / StatusBlock** (`TurnParts.tsx:99, 116`; live dispatch `ChatPanel.tsx:1214–1217`)
- Under `quiet` (or `activity_notices = collapsed`), **consecutive live `notice` parts group into one compact disclosure** — a single toggle row with an accessible count ("~ 3 activity notices", singular for one) that expands to show the run inline. Every run, including a run of length one, renders behind the disclosure so "collapsed" uniformly means no notice text is visible without a click. A non-notice part (`status`, `tool`, `text`) ends the current run.
- The group's open state lives in the same ChatPanel-owned map (region `notices`, keyed by the identity of the run's first message) so it also survives virtualized unmount/remount and clears only on a policy revision.
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
- **A selected match force-opens its containing collapsed block.** When `currentMatchEntryPos` (ChatPanel.tsx:531) points at an entry, that entry renders with all of its collapsible regions open — thinking content, tool details, tool output, and the tail-preview lift (full output) — so any highlight inside it is actually visible. The force-open **writes into the ChatPanel-owned disclosure map** (§7) and therefore **latches across virtualized unmount/remount**: it stays open after the match cursor moves away (no collapsing content under the reader, no height thrash that would yank the scroll position) until a policy revision clears the map. The existing jump machinery — `scrollToIndex` (line 826), `olderPrefixFetch` prefix loads, and the `attemptedJumpsRef` dedupe (line 844) — keeps working unchanged on top.
- **Disclosure state survives virtualization.** Rows unmount when scrolled off-screen and remount on return; because open/closed state lives in the map keyed by stable message/tool identity (§7), a remounted block re-derives exactly the state it had before unmounting. **No component-local disclosure state may be introduced anywhere in this feature** — a `useState`-style default inside a virtualized row would reset on every unmount and violate this contract.
- **Virtualizer remeasure + anchoring on policy changes (amended — exact intended behavior, using only supported APIs).** Collapsing/expanding regions changes row heights, so a `policyRevision` bump must remeasure while preserving the reader's position. The intended sequence, in `ChatPanel`'s revision-change effect:
  1. **Capture the anchor** from the current virtualizer state: the first visible item's index/key (`virtualizer.getVirtualItems()[0]`) and the pixel distance from that item's start to the scroll container's top (`scrollTop − virtualizer.getOffsetForIndex(index)`, accounting for the existing `scrollMargin` sync, lines 218–357). Record whether the viewport is bottom-pinned via the existing `atBottomRef` check (distance < 24).
  2. **Apply the revision** (map cleared, defaults re-derived) and call **`virtualizer.measure()`** — the supported invalidation call; mounted rows then re-report through the existing `measureElement` ref / ResizeObserver wiring (ChatPanel.tsx:1139) and off-screen rows are measured when they mount. Entry identity does not change with policy (stable keys, below), so the captured anchor index remains valid.
  3. **Restore the position.** If bottom-pinned: re-pin to the bottom through the existing bottom-pin machinery (ResizeObservers, lines 692–763). Otherwise: `virtualizer.scrollToIndex(anchorIndex, { align: "start" })`, then set `scrollTop = virtualizer.getOffsetForIndex(anchorIndex) + anchorDistance` to restore the sub-item pixel offset — i.e. the same item at the same within-item position, using only `getVirtualItems` / `getOffsetForIndex` / `scrollToIndex` / `measure`, all supported by the installed virtualizer. **No new virtualizer API is to be invented**; if the installed version lacks `getOffsetForIndex`, fall back to `scrollToIndex(anchorIndex, { align: "start" })` alone (item-top anchoring) rather than adding one.
  4. Bottom-pin behavior for a viewport that was *not* bottom-pinned must not engage — an at-bottom reader stays at bottom, a mid-transcript reader keeps their item+offset, and an active search jump (step 3 of the force-open flow above) still wins because it runs after this effect.
- **Stable keys.** Virtual item keys stay message/entry-identity based (comment at line 224); policy/mode values must never be folded into a key — a policy change may update props but must not remount the list (remounts would jump the scroll and defeat the anchor capture, independent of the fact that disclosure state itself now survives remounts).

## 12. Edge cases

| Case | Behavior |
|---|---|
| Config GET fails (network, older server without the endpoint → JSON 404 from `spaHandler`) | Store **retains the last cached config** and keeps rendering with it; if there is **no cache**, falls back to **Full as an explicit compatibility default** with a structured warning log, and `ChatDisplayForm` shows its inline load error; Save may still be attempted and surfaces its own error inline. |
| Stored section contains invalid enum values (hand-edited `ocodeconfig.json`) | **GET responds 400 naming the field — no server-side normalization** (§6). Client: same failure contract as above (retain cache → explicit Full + warning if none); Settings displays the server's error text. The file is never silently rewritten. |
| Fetch succeeds but payload fails client-side validation | Unreachable-by-design (server guarantees §6); if it happens, the resolver's defensive fallback prevents a crash and the store emits a structured warning — this is a defect signal, not a supported path. |
| `chat_verbosity_changed` event arrives | **Payload ignored.** Store re-fetches via local GET (§5). A malformed/hostile payload therefore cannot alter rendering. |
| Event missed (SSE outage, seq gap, reconnect) | `onReconnect` triggers the same re-fetch (§5); state converges without event replay. |
| Before the first config response resolves | Store holds the explicit Full default ⇒ no flash of collapsed content that later expands (or vice versa). |
| Two saves in quick succession | Invalidation coalesces into one in-flight GET; `policyRevision` bumps at most once, only if the effective policy actually changed. |
| Concurrent saves from two windows/processes | `withOcodeConfigLock` serializes read-modify-write; PUT replaces the whole section (last write wins). |
| Second ocode process sharing the same config file | The event is **process-wide only**; another process picks the value up on its next GET/restart, not live. Accepted for v1 (see §17). |
| Remote-hosted sessions / remote servers | Displayed by the local SPA ⇒ the local setting governs their rendering; no `host` param, no remote fetch (§3). Event envelopes carry no host field, so the payload is never applied and only the local GET runs — no remote/local cross-application. |
| Reader manually toggles a block, then scrolls it off-screen and back | Choice persists (disclosure map, §7/§11) — virtualized unmount/remount does not reset it. |
| Manual toggle, then a policy revision | The revision clears the map and re-applies defaults — by design; the only trigger that discards manual choices. |
| Search force-open, then cursor moves | Block stays open (map latch, §11) until the next policy revision. |
| Latest thinking while streaming / after commit / after a newer turn begins | Expanded (default) during streaming, still expanded after commit, until a newer assistant turn begins — then it is older thinking and follows policy (§9/§10). Holds in Full, Balanced, and Quiet. |
| Running tool under `quiet` | Header shows `running…`; live partial output follows `tool_output` (hidden while collapsed); the always-visible status keeps the turn from looking stalled. |
| Empty / no-message session | Nothing to style; resolver output unused; no errors. |
| TUI running against the same config file | Ignores `chat_verbosity` in v1 — zero TUI behavior change. |
| Downgrade to an older ocode | Key survives via `Extra` round-trip (ocodeconfig.go:1561, 2068); older builds render as today. |
| Desktop + web attached to one server | Both receive `chat_verbosity_changed` over their SSE streams and re-fetch (invalidation-only, §5) without reload; a missed event is covered by the reconnect re-fetch. |

## 13. Accessibility and performance

**Accessibility**
- Preset selector is a real radio group (fieldset/legend or `radiogroup` semantics) with arrow-key navigation; each option is a labeled `input[type=radio]`, not a styled div.
- Each override select has an associated `<label>` (house `text-xs text-muted-foreground` style).
- All disclosure toggles — ThinkingBlock header, both ToolBlock gates, the grouped-notices row — gain `aria-expanded` (currently absent in `TurnParts.tsx`) plus accessible names; the value reflects the ChatPanel-derived open state (map entry or policy default), and the grouped-notices name includes its count.
- Errors render with `role="alert"`; the Save button is `disabled` and reads "Saving…" while in flight.
- No dialogs are introduced, so the project's Dialog focus conventions are not engaged; nothing here needs `data-dialog-default-action`.

**Performance**
- Resolver is pure and computed once per config change (`useMemo` / module store); it is not in the per-token streaming path.
- Disclosure map reads/writes are O(1) map operations owned by ChatPanel; blocks stay `memo`ized with primitive props (TurnParts.tsx:202–205) — no new object identity per render.
- Notice grouping is an O(n) pass over the small live buffer during render.
- One GET per app (shared store), plus one re-GET per save event and per reconnect; one PUT per save — no polling, no added backend load; transcript, search, and LLM flows are untouched.
- Remeasure/anchor runs once per actual policy change (revision bumps are deduped to effective changes), not per event or per render.

## 14. Testing (write these FIRST — red, then implement)

TDD order: land the suites below failing against the current tree, then implement until green. House habit applies: **mutation-verify** each new test (temporarily revert the fix/feature, confirm the test fails, restore).

**Go — config and handler validation/persistence**
- `internal/config/ocodeconfig_test.go`: absent section ⇒ default `full`/all-`preset`; save→reload round-trip through the locked path preserves preset + overrides; canonical write handling (no double-write via `Extra`); **malformed on-disk values are preserved un-normalized by load** (normalization is banned — they surface at the API instead).
- `internal/server/handler_config_test.go` (use `testConfigHandler`, line 23 — isolated `HOME`): `GET` serves defaults for an absent section; **`GET` on a stored invalid enum returns 400 naming the field and does not rewrite the file** (no silent normalization); `PUT` with valid payload returns 200, persists to the throwaway file, and updates `h.cfg.Ocode.ChatVerbosity` in memory (mirror `TestHandleSetCompactConfigPersists`, line 97); `PUT` rejects bad preset → 400, bad override value → 400, unknown override category → 400, malformed body → 400 (mirror `TestHandleSetPermissionModeConfigRejectsInvalid`, line 326); `PUT` publishes `chat_verbosity_changed` on `h.bus` with payload = saved config, empty session id, the event absent from `sessionScopedEvents` (subscribe like `emitters_test.go:113`), and **the marshaled envelope contains no `host` field** (pins the unattributable-payload premise of §5); route registration exists behind `authMiddleware` (`server.go`).

**TypeScript — resolver**
- `web/src/lib/chatVerbosity.test.ts`: table-driven presets → expected modes for all four categories; each override value beats the preset; `preset` override follows the preset; **`latestThinking === "expanded"` asserted across the full preset × override combination space** (the §4 invariant — Full, Balanced, Quiet); `null`/`undefined`/missing fields/unknown enum strings fall back safely to Full semantics; resolver is pure (no store/IO).

**TypeScript — store: event invalidation, reconnect, fallback logging (regression, amendment 1 + 6)**
- `web/src/lib/chatVerbosity.store.test.ts`:
  - **`chat_verbosity_changed` payload is never applied:** emit the event with a *different* config in `env.data`; assert the store state is unchanged and a fresh `GET` was issued; only the GET's response updates the store.
  - **Reconnect refetches:** firing the `eventBus.onReconnect` handlers triggers a re-GET; state converges to the GET result (no replay reliance).
  - **Failure retains cache:** a failed GET after a successful one keeps the last-good config with no user-visible change.
  - **Explicit Full fallback logging:** with no cached config, a failed GET resolves to Full **and** emits the structured warning (assert `console.warn` called with the stable prefix and `reason: "no-cached-policy"`); the error is exposed for Settings.
  - Invalidation coalescing: N events during one in-flight GET → one extra GET.

**TypeScript — disclosure state: virtualization, revision, latest-turn (regression, amendment 2 + 3)**
- ChatPanel integration:
  - **Persistence across virtualized unmount/remount:** collapse a thinking block and a tool's output region, drive them off-screen (rows unmount), scroll back (remount) → still collapsed; expand → still expanded. Same for the search force-open latch across unmount.
  - **Policy revision reset:** change the config so `EffectiveVerbosity` changes → map cleared, all defaults re-applied (manual collapses reopen per policy); a re-fetch returning an *identical* config must **not** bump the revision and must **not** clear manual choices.
  - **Latest-turn semantics:** the last thinking block of the latest assistant turn renders expanded while streaming, stays expanded after commit, and its default reverts to `olderThinking` only once a newer assistant turn begins — asserted under Full, Balanced, **and Quiet**; a manual collapse of the latest block persists across renders and is cleared only by a revision.
  - **Remeasure + anchoring:** a revision bump calls `virtualizer.measure()` and restores the first visible item + pixel offset (mid-transcript case) and bottom-pin (at-bottom case), and does not remount items (keys preserved).

**TypeScript — renderer**
- `TurnParts.test.tsx` additions: ThinkingBlock and ToolBlock are **controlled** — given `open` props they render accordingly regardless of mount history, and toggling calls `onToggle(regionKey)` without any internal state; ThinkingBlock defaults (latest open under all three presets; older per policy), ToolBlock header (name, `running…`, `awaiting your answer`, `Open question`) visible with both gates collapsed in Quiet; details and output gates independent (opening one does not open the other; collapsed details does not hide expanded output); tail preview present when output expanded (>20 lines shows "… N earlier lines"); `full` reproduces the `lineCount <= 50` initial default including the pending-mount case; NoticeBlock grouping (three consecutive notices → one disclosure, expand reveals all; a `status` part splits runs; lone notice → disclosure of one); StatusBlock never grouped/hidden under any preset; QuestionAnswerBlock and assistant/user text never collapse.
- ChatPanel integration, **committed and live paths**: modes reach `MessageBubble`/`TurnParts` from both `renderEntries` and the live-buffer dispatch; a search-selected match force-opens its containing collapsed block (and the latch survives cursor movement **and row unmount**).

**TypeScript — Settings form**
- `ChatDisplayForm.test.tsx` (+ a `SettingsPanel` group-dispatch case): loads and renders GET values; radio + four selects present with options; Reset overrides returns selects to the suffixed `Follow preset — <Expanded|Collapsed>` option (suffix recomputed from the then-selected preset) without touching the preset; Save issues `PUT` with the exact payload and is disabled while saving; save failure, load failure, and a **400 from a malformed stored section** each render the inline `role="alert"` error (field name preserved); success renders no error.
- **Dynamic `Follow preset` label (amendment 7):** each override select's first option has `value="preset"` and text `Follow preset — <Expanded|Collapsed>` matching the §9 cell of the currently selected draft preset; changing the preset radio updates the suffix **before** Save; the option exists under Full, Balanced, and Quiet; under Full the `*` heuristic is not reflected (both tool regions read `Expanded`); the select's per-category `aria-label` is unchanged, and assertions target option text.

**Gates before merge:** `go build ./...`, `go vet`, `go test ./internal/config ./internal/server`; `tsgo --noEmit`, focused Vitest runs for the files above, and `vite build`.

## 15. Rollout and compatibility

- **Default = `full` = today's rendering** (including the §9 initial-heuristic nuance): no user-visible change until they pick another preset. No feature flag needed.
- **No DB migration**; only `ocodeconfig.json` gains an optional section, materialized on first save.
- **Older ocode binary**: `chat_verbosity` survives round-trips via `Extra` — a downgrade does not strip the user's setting.
- **Older/absent server endpoint**: GET 404 ⇒ store retains cache or takes the explicit Full fallback with a warning; Settings shows an inline error (graceful degradation to current behavior).
- **Malformed stored config is loud, not silent**: GET 400 + Settings error + client warning — never a silently normalized render (§6, §12).
- **Missed events self-heal**: invalidation-only consumption plus the reconnect re-fetch means a save converges on every attached client even if its SSE event was lost (§5).
- **TUI** untouched; **remote** out of scope (event envelopes carry no host; payload never applied, §3/§5); display-only guarantees (transcript, search indices, LLM context unchanged) mean no data reindex or replay concerns.
- Process-wide event covers all clients of one server (desktop + web together); cross-process live sync is explicitly not in v1.

## 16. Expected files and components (planned — none of this exists yet)

| Area | Files |
|---|---|
| Config schema/save | `internal/config/ocodeconfig.go` (`ChatVerbosityConfig`, file struct, default, load-merge **without value normalization**, `SaveOcodeChatVerbosity`, Extra write handling); `internal/config/ocodeconfig_test.go` |
| API + event | `internal/server/handler_config.go` (`HandleGetChatVerbosityConfig` / `HandleSetChatVerbosityConfig`, strict validation incl. **GET-side 400 on stored invalid enums**, `h.cfg` update, `Publish("chat_verbosity_changed", …)`); `internal/server/server.go` (GET/PUT routes behind `authMiddleware` + thin wrappers); `internal/server/handler_config_test.go` |
| API client | `web/src/api/client.ts` (`getChatVerbosityConfig` / `setChatVerbosityConfig`) and the `ChatVerbosityConfig` type (in `client.ts` beside `CompactConfig`, or `web/src/api/types.ts`) |
| Settings UI | `web/src/components/Settings/SettingsPanel.tsx` (new `chat-display` group id + label **"Chat display"** + `renderGroup` case); new `web/src/components/Settings/ChatDisplayForm.tsx` |
| Resolver/store/hook | new `web/src/lib/chatVerbosity.ts` (pure `resolveChatVerbosity`, module-level store with **invalidation-only event handler, `onReconnect` refetch, last-good cache, `policyRevision`, structured Full-fallback warning**, `useChatVerbosity`) |
| Chat rendering | `web/src/components/Chat/ChatPanel.tsx` (hook consumption, **ChatPanel-owned disclosure map + policy-revision clearing + frozen `full` heuristic memo**, mode threading, search force-open into the map, **measure + anchor on revision**); `web/src/components/Chat/MessageBubble.tsx` (forward primitives); `web/src/components/Chat/TurnParts.tsx` (`ThinkingBlock`/`ToolBlock` become controlled, **single ToolBlock with one outer call/details gate + one inner output gate**, `NoticeBlock` grouping, `aria-expanded`) |
| Focused tests | the Go/TS suites named in §14 (config, handler, resolver, **store invalidation/reconnect/fallback logging**, **disclosure persistence/revision reset/latest-turn semantics**, TurnParts, ChatPanel committed/live, Settings form) |
| Docs on implementation | `skills/ocode-web/SKILL.md` (file map + numbered rules covering the resolver invariant, the invalidation-only event contract, the ChatPanel-owned disclosure map (no component-local disclosure state under virtualization), the single-ToolBlock gate model, and the force-open latch); `CHANGES.md` dated entry; this spec is the design record |

## 17. Open questions

1. **Cross-process propagation** — a second `ocode serve` process (or a concurrently running TUI process) shares the config file but not the in-process event. Accept staleness until their next config read (v1 decision), or add file-mtime revalidation later?
2. **TUI parity** — should the TUI adopt `chat_verbosity` (its own render path in `internal/tui/tool_render.go`) in a v2? Not in v1.
3. **Scope of the setting** — global-only in v1; per-project or per-session verbosity later if wanted (would follow the per-session `permission_mode`/`model` override pattern, out of scope here).
4. **Lone notice under `collapsed`** — decided as disclosure-of-one for uniformity ("collapsed ⇒ no notice text without a click"); revisit after dogfooding if a single notice behind a toggle feels overwrought.
5. **Preset category mixes** — the `balanced`/`quiet` cell values in §9 are data-only: tuning them later changes only the preset table, not the schema, API, resolver contract, or tests beyond the table cases.
6. **Event payload retention** — the server still publishes the saved config as `data` for debuggability although clients ignore it; optionally drop the payload in a future revision without breaking the invalidation contract (§6).
7. **Frozen `full` heuristic across off-screen line-count changes** — the default memo (§9) deliberately does not re-roll when a tool's line count crosses 50 while unmounted; confirm this matches the "byte-compatible first-paint" intent after dogfooding (first render is identical; only an off-screen mutation + remount could diverge from today's `useState` re-evaluation).