---
type: Concept
title: Pulse — cross-project live-sessions dashboard
description: 'Assistant drawer (docked chat, localStorage prefs, SSE tracking without a tab, model chooser). Focus mode (master-detail pane with inline ask resolution and reply). Concept doc for the Pulse cross-project live-sessions dashboard: GET /api/pulse contract, status/task derivation, scope=all cost model, todo_updated SSE, client store, card filter, hover-overlay contract, on-card activity feed (STREAM_ON_CARD, PulseStream wrap modes, tool entries, scrollable stream region with stick-to-bottom, STREAM_MAX_H height budget, PULSE_TAIL_ENTRIES, superseded min-h-0 gotcha), 2-column grid, jump sequence, entry points (no client router), desktop wiring, and known limits.'
tags:
  - pulse
  - dashboard
  - sessions
  - sse
  - server
  - web
  - desktop
  - api
timestamp: 2026-10-08T21:00:00Z
---
# Pulse — cross-project live-sessions dashboard

## Purpose

Pulse is one glanceable view of **every live chat session across all local
projects**, answering two questions: *what is running?* and *what is waiting on
me?* — with a one-click jump into any session. It is deliberately **not** a
session manager: no rename, delete, archive, or bulk actions. It is no longer
read-only, though: [focus mode](#focus-mode) resolves pending asks and sends
replies inline (user decision 2026-10-08). v1 is
**local-server only**; the desktop app renders the same SPA.

- Design spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
- Implementation plan: `docs/superpowers/plans/2026-09-24-pulse-dashboard/` (tasks 01–10 + INDEX)
- **Superseded spec items:** the spec's "route `/pulse`" and "pinned Pulse tab
  in `UnifiedTabBar`" were both wrong, and its shipped replacement — a
  hamburger main menu in the `ProjectSidebar` header — is itself superseded;
  all are replaced as described under
  [Entry points](#entry-points-and-the-no-client-router-correction).

## API: `GET /api/pulse`

Handler: `internal/server/handler_pulse.go` (gathers inputs, validates query).
Derivation: `internal/server/pulse_rows.go` — **all** of it (status precedence,
task fallback chain, inclusion windows, sort, cursor paging) is pure and
I/O-free. That split is the reason every rule below is testable without
starting a server.

Query parameters:

| Param | Meaning |
|---|---|
| `scope` | `live` (default, 24h idle window) \| `all` (7d window). Omitted ⇒ `live`; **present-but-empty `scope=` is 400** — a client computed a blank value, and reading it as `live` would hide that bug. |
| `cursor` | Opaque: base64url of `rank\|RFC3339Nano updated_at\|session_id`. |
| `limit` | 1–100, default 50. Malformed scope/limit/cursor ⇒ **400** (a client bug, not something to silently coerce). |

Response: `{items: PulseRow[], next_cursor: string|null}`. `next_cursor` is an
**explicit JSON `null` on the last page**, never an omitted key (`omitempty` is
deliberately off) — clients can distinguish "end of list" from "field missing".

### Row shape

`session_id, project_path, title, status, current_task{kind:todo|tool|text, text}|null,
todo{done,total,current,items[]}|null, pending_ask{kind:permission|question,
summary}|null, turn_started_at, updated_at, child_count`.

### Status precedence (`derivePulseStatus`)

First match wins:

1. pending permission → `needs_permission`
2. pending question → `needs_question`
3. turn running → `running`
4. last turn errored → `error`
5. else → `idle`

An ask **outranks `running`**: the turn is *paused* on the user, so "needs you"
is the truthful headline even when `turnActive` is still set, and a stale error
must never mask a fresh ask.

### `current_task` derivation (`derivePulseTask`)

- **nil for needs-you rows** — the ask is the headline; a task line would
  duplicate it.
- Otherwise the fallback chain: in-progress todo item → active tool (name plus
  args truncated to **80 runes** via `truncateRunes`, *not* bytes — a byte slice
  would split a multi-byte character into mojibake) → last assistant line → nil.

### Sort and paging

rank (`needs_*`=0, `running`=1, else 2, from `pulseStatusRank`) → `updated_at`
desc → `session_id` asc. The order is defined server-side in
`pulseStatusRank`/`buildPulseRows` and **mirrored** by `sortPulseRows` in
`web/src/stores/pulseStore.tsx` (which re-implements only the sort, never the
derivation rules). The two must stay in step, and each is pinned by tests on its
own side — changing one without the other desynchronizes server pages from the
client's optimistic re-sort.

### Child sessions

Child ids (`<parent>_child_<agent>_<ts>`, infix `_child_` as minted by
`internal/agent/child_session.go`) **never get their own row**. They are counted
onto the parent as `child_count`.

## `scope=all` cost model

`gatherDiskPulseInputs` reads **metadata only**: one directory scan plus one
indexed query per project — no transcript bodies, no todo file reads — so cost
scales with the number of *projects*, not with chat history. Consequences:

- Disk-only rows carry `todo: null` and show project + title + age (reading a
  todo file per historical session would make `all` scale with history).
- Live rows are merged **OVER** the disk listing (`gatherPulseInputs`), so a
  session that is both persisted and resident appears **once**, with its fresher
  live state.
- Remote-host projects are **skipped** (`p.Host != ""`): their transcripts live
  on another machine and v1 is local-only.

## `lastTurnErr` on `sessionEntry`

`internal/server/session_manager.go` carries `lastTurnErr` on the registry
entry. It exists because once `turn_active` drops, a failed turn is otherwise
**indistinguishable from an idle one** — the dashboard would silently downgrade
`error` to `idle`.

- **Recorded** where `turn_error` is published (session manager error path).
- **Cleared** by `setTurnActive(id, true)` (a new turn starts fresh).
- **Kept** by `setTurnActive(id, false)` (turn end must not erase the failure).
- `setTurnError` on an unknown session id is a **no-op** — otherwise an orphan
  entry would appear on the dashboard as a project-less ghost session.

## `todo_updated` SSE event

Payload: `{session_id, done, total, current, items[]}`, published from the
`todowrite` tool result via `Handler.publishTodoUpdated`
(`internal/server/handler.go`).

- The tool-result message carries only `ToolID`, so the callback keeps a
  **per-turn `ToolID → tool name` map** built from assistant messages
  (`toolNames`), consumed on read and capped by `todoToolNameMapCap` (=512) —
  the cap turns a pathologically long session into a cheap reset instead of
  unbounded growth. A result whose `ToolID` was never declared is skipped.
- Backed by `tool.ReadTodoSummary(projectRoot, sessionID)`
  (`internal/tool/todo_store.go`), which resolves `.ocode/todo/<id>.md` from the
  **passed** project root — the process-global `todoDir()` resolves from the
  *process cwd*, which is wrong for a server serving many projects. It reuses
  the existing item parser.
- **Not in `liveFrameEvents`** (see the comment in
  `internal/server/event_bus.go`): a plan is a momentary reading, so replaying a
  buffered copy into a mid-turn reload would show one the session has already
  moved past.
- A corrupt plan is **logged and skipped** (row emitted without a todo) rather
  than failing the dashboard; the same rule applies to a broken title index.

## The client store: `web/src/stores/pulseStore.tsx`

Seeded from `GET /api/pulse`, kept live by SSE events forwarded through
`pulseEventSink`. Deliberate non-behaviours, contractual on purpose:

- **Never synthesizes a row.** An event for an unknown session triggers one
  debounced 300ms refetch (`UNKNOWN_SESSION_REFETCH_MS`) — inventing a card the
  user cannot open is worse than one that arrives late.
- **A failed refetch keeps the last known rows** alongside the recorded error
  instead of blanking the dashboard (an error banner plus stale-but-real rows
  beats an empty view).
- **Reseeds on every `eventBus.onReconnect`** — events were lost while the bus
  was down; a refetch is the only truthful recovery, never a replay.

The store is intentionally NOT driven by the per-tab chat slices: a dashboard
must see sessions with no open tab.

## Card filter: `web/src/components/Pulse/pulseFilter.ts`

Pure, client-side, and deliberately **not** a server query — filtering already-resident
rows avoids a round trip per keystroke and keeps SSE-updated rows filtering
correctly without a refetch.

- `pulseProjectBasename(path)` — last path segment, trailing slashes stripped
  first so `/proj/alpha/` and `/proj/alpha` are one project rather than one
  labelled with an empty string. **Shared with `PulseCard`**, which labels the
  card from it: a filter matching on the full path while the card shows a
  basename makes the visible label and the searchable thing disagree, which
  reads as a broken filter.
- `pulseRowMatchesFilter(row, needle)` — case-insensitive substring of the
  project **basename** or the session title, both `.trim()`ed; an empty query
  matches everything. The title is checked separately from the basename rather
  than against the full path, so a query cannot match a directory segment the
  user never typed.

The filter is applied in `PulseView` (`filtered = rows.filter(...)`) and the
**section grouping is computed over the filtered set**, not the raw rows — so an
empty result collapses the sections entirely and shows the
`No sessions match "…"` line instead. Keyboard users get roving arrow-key
navigation over `[role="listitem"] button[aria-expanded]`: the card's header
button. Each card also has a "Focus session" icon button, which is deliberately
not a roving stop (it has no `aria-expanded`).

**The filter only sees loaded rows.** Paging is an explicit *Load more* button
(`PulseView.tsx`, driven by `loadMore`/`hasMore`/`nextCursor` in the store), not
infinite scroll, so the resident set is the first page plus whatever the user
chose to load. A query therefore cannot match a session still behind the
cursor. That is the accepted trade for keystroke-local filtering; if it ever
matters, the fix is a server-side `q` parameter, not a bigger client fetch.

## The card hover overlay

`PulseCard`'s header is one button inside a chrome `div`; hovering the chrome
`div` (so the stream region counts) or focusing the button expands an absolutely
positioned panel after `EXPAND_DELAY_MS` (150ms — a mouse crossing the grid
would otherwise flash a panel over every card it passes; focus is a deliberate
act so it expands with no delay at all). The panel is a **sibling** of the
button, so showing it cannot resize the card and its content stays out of the
button's click target and accessible name.

Content, in precedence order: tail error → tail entries → todo items →
`current_task` → `pending_ask`. Two rules that are easy to get wrong:

- **`current_task` is rendered for kind `tool` *and* kind `text`.** Kind `text`
  is the server's last-resort fallback — the last assistant line, from
  `derivePulseTask` — returned for exactly the `idle` and `error` rows that make
  up most of the dashboard. Gating on `tool` alone drops it, and the card then
  expands into an empty panel while that same line sits visible in the card body
  directly above it. Kind `todo` is deliberately *excluded*: the items list
  already contains the current item, so rendering it again duplicates a line.
- **The panel is never blank.** With no content at all it shows either
  `Loading…` (the seed fetch is in flight, `tail.loading`) or an explicit
  "nothing to preview" line. A blank bordered panel reads as a broken card, and
  the genuinely-empty case is real and reachable: a disk-only `scope=all` row
  carries `todo: null`, and a session whose assistant only ever made tool calls
  has no last assistant line either, so the server sends `current_task: null`.

`usePulseTail` (`web/src/components/Pulse/usePulseTail.ts`) supplies the tail and
is gated on `expanded || streamOnCard`, so a collapsed, non-live card subscribes
to and fetches nothing. Its `loading` flag tracks the **seed fetch only** — the
running path's `text` subscription never settles, so live deltas arriving after
the seed must not keep the spinner up. Every write is gated on a generation
counter, so a fetch that resolves after the session changed, or after the hook
was disabled, cannot write into the card that replaced it.

`usePulseTail` (`web/src/components/Pulse/usePulseTail.ts`) supplies the tail.
Its enablement is **not** simply `expanded`: a LIVE card (running, or paused on
an ask — the `STREAM_ON_CARD` set) enables it unconditionally, because it
streams on the card face, while every other status enables it only on
hover/focus. A collapsed idle card therefore subscribes to and fetches nothing,
but a collapsed running card is always subscribed. Its `loading` flag tracks
the **seed fetch only** — the running path's `text` subscription never settles,
so live deltas arriving after the seed must not keep the spinner up. Every
write is gated on a generation counter, so a fetch that resolves after the
session changed, or after the hook was disabled, cannot write into the card
that replaced it.

## The live card carries its own stream

A `running`, `needs_permission` or `needs_question` row renders its streaming
preview on the card face, not only in the hover overlay, and enables
`usePulseTail` unconditionally (`expanded || streamOnCard`, gated by the
`STREAM_ON_CARD` set). Every other status — `idle` and `error` — keeps the
existing hover/focus gate and still renders its preview in the overlay.

Why: watching a turn is the entire reason to open this dashboard, and
hover-gating the preview inverted that. It also made the preview unreachable on
a touch device, which has no hover at all.

The fetch-cost rationale that kept the gate is unchanged and still load-bearing.
A settled row is seeded from a 200-message transcript fetch (`IDLE_FETCH_LIMIT`)
and the dashboard pages up to 50 rows (`pulseDefaultLimit`), so seeding every
settled card up front would cost a request per card on open. Live rows number in
single digits, so their seed fetch and SSE `text` subscription follow work that
is actually happening rather than history.

## The on-card streaming block

A LIVE card — `running`, `needs_permission`, or `needs_question`, the
`STREAM_ON_CARD` set in `web/src/components/Pulse/PulseCard.tsx` — renders its
stream **on the card face**, not only in the hover overlay. The dashboard's job
is watching turns; requiring a hover to see what a running session is saying
inverted that, and made the preview unreachable on touch, which has no hover at
all. The stream renders in exactly ONE place per card — the overlay for every
other status — so the same entries are never on screen twice and `pulse-tail`
stays unique.

### Entries, not a text tail

`usePulseTail` returns `entries: PulseTailEntry[]` (`{kind: "text" | "tool",
text}`), an activity feed rather than prose alone. It used to read only `text`
frames, so a turn that was mostly tool calls showed one stale paragraph and
nothing else. One shared reducer (`Feed` in `usePulseTail.ts`) handles the seed
`live_frames` and the live bus events, so the two cannot produce different
entries for the same frames:

- `text` deltas extend the current text entry, or start one when the last entry
  is a tool entry; newlines split into separate text entries.
- `tool_start` appends `▸ <tool> <subject>`. The live payload's `command` field is
  the RAW `function.arguments` JSON, not a shell command, so `summarizeArgs`
  parses it and takes the first string among `command`, `file_path`, `path`,
  `pattern`, `query`, `url` (name only if none, or if it is not a JSON object).
  The subject is whitespace-flattened and clipped to 80 characters. The glyph is single-width on purpose (a wide emoji
  shifts the rest of the row in VS Code's renderer).
- `tool_result` for a known `call_id` appends ` ✓`, or ` ✗` when the output starts
  with `Error`/`error:`. Entries are tracked by identity under their `call_id`,
  not by array index, because the front of the feed is trimmed as it grows and
  would shift stored indexes. An unknown `call_id`, a missing one, or a repeat is
  ignored.
- `thinking` and `tool_output` are ignored (too noisy for a card), and the hook
  does not subscribe to them. It subscribes to exactly `text`, `tool_start`,
  `tool_result`.
- The hold-until-seeded ordering is unchanged but now holds every live event, not
  just text: a `tool_result` for a call only the seed knows about would otherwise
  be dropped as unknown.
- The idle/error path replays the LAST TURN of the fetched transcript through
  the same reducer: every message after the last real `user` message (an
  injected `[ocode:` notice is user-role but not a boundary; with no user
  message the whole slice is the turn). Assistant `content` becomes text
  entries, each `tool_calls[]` entry a `tool_start` (subject from the same
  `summarizeArgs` over `function.arguments`), and a
  `tool`-role message with `tool_call_id` a `tool_result`. A settled card
  therefore has the same shape as a running one.

`PULSE_TAIL_ENTRIES` (60) bounds the retained entries, tool entries included.
It is no longer the visible height: the region scrolls, so it only bounds the
DOM. `TEXT_BUFFER_CAP` still bounds the summed text characters.

`PulseStream` has two modes, chosen by its `wrap` prop:

- **On the card** (`wrap`): entries soft-wrap (`whitespace-pre-wrap break-words`,
  NO per-entry `truncate`). Model prose carries no newlines, so per-entry
  truncation reduced a running card to a single clipped line. Tool entries use
  `font-mono text-[11px] text-muted-foreground`.
- **In the overlay** (no `wrap`): each entry stays one truncated line. The
  overlay has no height budget, so wrapping there would turn the preview into a
  page-height panel.

### The card is not one giant button

A non-compact card is a chrome `div` (`flex h-full flex-col`, border,
background, `CARD_MIN_H`) containing two siblings: the header button (status,
title, task, plan; click jumps, double-click opens the ask, Enter focuses,
Shift+Enter jumps, Escape collapses, focus ring, `aria-expanded`), the "Focus
session" icon button (absolutely positioned top-right; a button may not contain
a button, so it is a sibling, and the header reserves right padding for it) and
the stream region
(`data-testid="pulse-stream-region"`). The stream used to live inside the
button, so a wheel over it scrolled the page grid and its text was part of the
click target. Pointer enter/leave for the overlay delay are on the chrome `div`
(for compact cards too) so hovering the stream or the focus button still
counts; focus/blur stay on the header button. Compact cards have the header
and focus buttons but no stream region.

### The scrollable stream region

The region is `min-h-0 flex-1 overflow-y-auto overscroll-contain` under the
`STREAM_MAX_H` cap, and wraps `PulseStream`. `onWheel` stops propagation so a
wheel never reaches the page scroller's React handlers, and `overscroll-contain`
stops native scroll chaining once the region hits an edge.

**Stick-to-bottom:** an effect on the entries scrolls the region to the bottom
unless the user scrolled up. `onScroll` records `atBottom = scrollHeight -
scrollTop - clientHeight < 8` in a ref (a ref, not state: it changes on every
scroll event and nothing renders from it). Scrolling back to the bottom resumes
following.

### Card height is a floor, not a hint

Non-compact cards reserve `CARD_MIN_H = "min-h-[16rem]"` on the chrome `div` and
are `h-full`, so every card in a grid row is one height. Both are needed:
`min-h` alone still lets a taller card stretch its row, and `h-full` alone gives
nowhere to stream into. The floor must stay ABOVE the content. When `min-h` sat
below the content the content governed instead: a card grew from 224px to 240px
as tail lines arrived, reflowing the whole grid row on every streaming delta.

### Height budget

The ceiling is `STREAM_MAX_H` = `max-h-[8rem]` (128px) on the stream region. This
ceiling — not the `CARD_MIN_H` floor — is what keeps the card height constant,
because `min-h` sets only a minimum and a `flex-1` child of an auto-height column
is sized from its own content (`flex-basis: 0%` caps nothing). The 128px is the
budget the original seven truncated 16.5px lines spent (7 × 16.5 + 6 × the 2px
`gap-0.5` = 127.5px); it is now a layout constant, since the region scrolls past
it rather than clipping.

### The `min-h-0` gotcha (superseded)

Before the region scrolled, the newest text stayed visible only because the
block was a capped `flex-col justify-end overflow-hidden` and each entry's
AUTOMATIC minimum size (its min-content height) stopped it shrinking, so excess
overflowed out of the TOP where the clip discarded it; `min-h-0` on an entry
flipped the overflow to the bottom and hid the text being streamed. **That no
longer applies:** the region is `overflow-y-auto` with no `justify-end`, entries
stack at their natural height, and stick-to-bottom decides what is visible.
`PulseCard.test.tsx` therefore no longer asserts the absence of
`min-h-0`/`shrink-0` on an entry; it still asserts `truncate` is absent from a
wrapping entry.

### Geometry verification (historical)

The 128px budget and the 256px card height were measured in headless Chromium
against the built CSS (400px card, one 250-line un-newlined entry) for the
earlier clip-based layout: the wrapping variant rendered 138 lines with the
region capped at 128px and the card 256px tall, identical to the truncate
baseline; uncapped, the same card measured 2236px. The scroll-based layout has
not been re-measured in a real browser, and jsdom cannot check layout, so none
of this is a unit-test assertion.

### Grid is capped at 2 columns

`grid-cols-1 sm:grid-cols-2 gap-3`, with no `lg:`/`xl:` override. The earlier
`xl:grid-cols-4` and then `lg:grid-cols-3` are gone: a live card carries a
scrolling activity feed of prose and tool lines, and wider cards give those
entries a readable width before they wrap. This deliberately overrides the
previous "capped at 3 columns" rule and the design spec's "up to 4", at the
product owner's request (2026-10-08).

Compact (Recent-section) cards are unchanged: one line, no reserved height,
hover-gated.

## Focus mode

A master-detail layout so one session can be watched and answered while the
others stay visible. `PulseView` swaps the grid for a `flex` row: the
`PulseFocusPane` (`flex-1`) on the left and a `w-80` column on the right
listing every OTHER row (the focused one is the pane) as a `compact` card under
the same Needs you / Running / Recent headings. Unfocused, the grid renders
exactly as before.

**State.** `focusedSessionId` is module state in `web/src/stores/pulseStore.tsx`
(`setPulseFocus`, `usePulseFocus`, built on `useSyncExternalStore`). Not React
state and not per-project persistence: it survives Cmd+J leaving and re-entering
the dashboard (PulseView unmounts), yet never reaches `saveViewStateForProject`,
because the dashboard is global.

**Entering.** The card's "Focus session" icon button (every card, compact
included) or `Enter` on a card header. Clicking the card still JUMPS, and
`Shift+Enter` is the keyboard spelling of that click. The Enter handler calls
`preventDefault`: a button's Enter keydown otherwise also fires a synthetic click
and would jump on every focus.

**Keyboard** (handler on the focus layout, `onFocusKeyDown`): `Escape` closes
focus; `ArrowDown`/`ArrowUp` and `j`/`k` step to the next/previous row in
reading order (Needs → Running → Recent over the filtered set), clamped at the
ends. All of it is ignored when the event target is in a text field or a
`[role="dialog"]`, and when `defaultPrevented`: a reply being typed must not
vanish on Escape, and the confined ask dialogs own their Escape (Radix handles
it in the capture phase) and option navigation. Stepping unmounts the
side-column card that held DOM focus, so an effect re-focuses the pane wrapper
when focus fell to `<body>`; otherwise the next key would never reach the
handler.

**Missing session.** If the focused id is not in `rows` (evicted, scope change)
the pane is replaced by "Session no longer listed" with a Close button, never a
blank pane.

**The pane** (`PulseFocusPane.tsx`), top to bottom: header (status glyph, project,
title, elapsed, "Open session" using the same `jumpTargetFor(row)` a card uses,
Close); the full plan; the activity feed (`usePulseTail(id, true, status)` through
the exported `PulseStream` with `wrap`, in a `flex-1 min-h-0 overflow-y-auto
overscroll-contain` region with NO max-height and stick-to-bottom via the shared
`useStickToBottom`); the pending ask; the reply box. `STATUS_META`, `TODO_MARK`,
`formatDuration`/`formatAgo`, `usePulseElapsed` and `jumpTargetFor` are exported
from `PulseCard.tsx` so the pane does not duplicate them.

**Ask resolution contract.** When `row.pending_ask` is set the pane fetches
`GET /api/sessions/:id/state` (re-fetched when the ask kind/summary or
`updated_at` changes; a stale response is dropped) and renders the first
permission (`kind: permission`) with `PermissionDialog` or the first question
(`kind: question`) with `QuestionDialog`. Decisions call `api.resolvePermission`
/ `api.answerQuestion` (and `api.cancelQuestion` for "Don't answer"). A failure
is shown inline in the pane AND logged with the session id; it is never
swallowed. Rows are NOT mutated optimistically: the ask clears when the next
`/api/pulse` refresh drops `pending_ask`, keeping one source of truth for status.
A hidden question leaves a "Show pending question" button.

**Why the dialogs are container-scoped.** Both dialogs require a
`ScopedDialogContainer`. The pane passes its own element (it is `relative`, which
the scrim needs), so the ask is confined to the pane. A viewport modal would
black out the very sessions focus mode keeps visible. The pane is rendered only
while it exists, so the `dialogScope.ts` surface gate (which keeps an ask from
mounting on an off-screen chat surface) does not apply: the pane IS the surface.

**Reply contract.** A textarea (Enter sends, Shift+Enter newline, IME composition
never sends) plus a Send button call `api.sendMessage(sessionId, text)`, an async
turn whose output rides SSE. It is disabled with a stated reason while the status
is `running` ("Turn in progress") or an ask is pending ("Answer the pending
request first"). Success clears the box; failure keeps the text and shows the
error. Slash commands are NOT interpreted: `ChatInput` dispatches a leading `/`
through its parent's `onSlashCommand` prop rather than an exported helper, so the
text goes to the model as typed.

## Assistant drawer

A chat drawer docked to the right of the dashboard (`PulseAssistantDrawer.tsx`),
talking to the global Pulse assistant session. The server side (session
creation, model slot, private project directory) is documented in
[pulse-assistant.md](pulse-assistant.md). The web side only assumes the contract:
`GET /api/pulse/assistant` returns `{session_id, model}` (ids start with
`pulse_`), `GET/PUT /api/config/pulse-model` is the model slot, and the session is
otherwise an ordinary one.

**Toggle.** A `MessageSquare` header button (`aria-label="Toggle assistant"`) or
the `a` key. The key is a document listener (`usePulseAssistantHotkey`) that
ignores modifiers, text fields and dialogs. With focus mode open the row is
pane | side column | drawer; the focus pane and side column keep their minimum
widths and the area beside the drawer scrolls horizontally only as a last resort.

**Persistence.** Open state and width live in `localStorage` under
`pulse.assistant.open` and `pulse.assistant.width` (`usePulseAssistantPrefs`), not
in the pulse store and not in per-project view state. They are per-viewer
conveniences (another window or device wants its own layout), nothing else reads
them, and the dashboard is global. Every read and write is guarded; a blocked or
throwing storage logs a `console.warn` naming the key and only costs the
remembered layout. Width is clamped to a 320px floor (default 420) and a CSS
`max-width: 60%` ceiling; the drag handle on the left edge (`role="separator"`,
also arrow-key resizable) clamps to the same bounds.

**The real chat surface.** The drawer renders `ChatPanel` for the transcript and
`ChatInput` for sending; there is no second renderer or composer. `ChatInput`'s
own `useChat` performs the send, so the turn is marked streaming exactly as in a
tab. A loading state ("Starting assistant…") and an error with a Retry button
cover the `GET /api/pulse/assistant` call, so the drawer is never blank.

**How the session is tracked for SSE, and why no store change was needed.** The
router (`sessionIsTracked` in `web/src/lib/sessionEvents.ts`) applies a session's
frames when it has an open tab OR a chat slice already exists. The assistant must
not get a tab (it would show up in a project's tab list and the Sessions tab
bar), so it is tracked through the slice clause: mounting `ChatPanel` hydrates the
session's slice and sending dispatches `SET_STREAMING`. Three things a tab would
have provided are restored explicitly: the stall watchdog (the drawer runs its own
`useTurnWatchdogAll` for the assistant id), host resolution
(`resolveSessionHost` returns undefined for `pulse_` ids, via
`lib/pulseAssistant.ts`, because the draft-tab fallback would otherwise inherit a
remote active project and send the turns to that host), and the ask dialogs
(`PermissionDialog`/`QuestionDialog` confined to the drawer, as App only renders
them for the active tab). Known limit: `SessionTabSync`'s reconnect reconcile
walks open tabs only, so a turn that ends during an SSE outage is repaired by the
watchdog rather than immediately.

**Model chooser.** The model name in the header opens the existing `ModelDialog`
with a new form-owned `pulse` purpose ("Select Assistant Model"): like recap/ocr
it hands the pick to `onPick` instead of writing anything itself. The drawer
calls `api.setPulseModel(model)` (empty clears) and then re-reads
`GET /api/pulse/assistant` for the effective model, since the server decides
slot-versus-default. Failures show inline. The change applies from the next turn.

**Settings entry point.** A `Settings2` gear in the drawer header
(`aria-label="Assistant settings"`) dispatches the window event
`ocode:open-settings-pulse-assistant` (`OPEN_PULSE_ASSISTANT_SETTINGS_EVENT` in
`lib/pulseAssistant.ts`), the same shape as `ocode:open-settings-profiles`. App
switches the view to Settings and the force-mounted `SettingsPanel` selects the
"Pulse assistant" section, which mounts `PulseAssistantForm`: the model slot
(the shared chooser's `pulse` purpose, "Use default" clears) and the system
prompt (monospace textarea, read-only built-in default under "Show default",
"Reset to default" clears the override). Nothing is optimistic: every save, reset
and model change round-trips and then re-reads
`GET /api/config/pulse-system-prompt` / `pulse-model`, so the form only shows what
the server holds. Both settings apply from the assistant's next turn. The drawer's
header model button and the form both write through `api.setPulseModel`; neither
keeps its own copy, each re-reads the server.

**Permission asks from write tools.** The assistant's write tools
(`session_send`, `session_command`, `permission_resolve`, `question_answer`) go
through the normal permission Ask, so its session raises `permission` frames like
any other. They are delivered by the same slice-based tracking described above,
and the drawer renders `PermissionDialog` (and `QuestionDialog`) confined to its
own chat surface, because App shows ask dialogs only for the active tab. Decisions
call `resolvePermission(requestId, "pulse_…", decision, undefined)`: the host is
always undefined (see `resolveSessionHost`), and "always allow" sends
`always_tool`/`always_rule` only after the dialog's confirm step. Without the
drawer open there is no dialog to answer, so a turn paused on such an ask waits
until the drawer is opened.

**Slash commands.** `ChatInput` routes a leading `/` through its parent's
`onSlashCommand`. The drawer passes none, so everything, including `/...`, goes to
the model through `sendMessage`; local instant commands do not apply to a
model-side helper.

## SSE routing order

`web/src/lib/sessionEvents.ts` forwards to `pulseEventSink` **before** the
`sessionIsTracked` gate — the tracked-session gate exists to protect the chat
slices from never-opened sessions, but a dashboard must see exactly those.
The sink is a no-op when no `PulseProvider` is mounted and never routes into
the chat store.

`todo_updated` must also be in the web's `SESSION_SCOPED_EVENTS` set; without it
the router returns before the forward and todo progress only appears on a full
refetch. The Go `sessionScopedEvents` (`internal/server/event_bus.go`) and web
`SESSION_SCOPED_EVENTS` lists must **stay in step** — same rule as the sort.

## Jump sequence: `web/src/lib/jumpToSession.tsx`

Clicking a card crosses a project boundary. The order is the whole contract:

1. `selectProject(project)` **then `await`** (it is async — it revalidates the
   project list).
2. `openSessionTab(id, title, projectPath)`.

The reverse order binds the tab to whichever project happened to be active —
an empty transcript and API calls against the wrong root. Project identity is
**path + host, never path alone**: a local project and a remote one may share a
path. An unknown project is **refused with a `console.error`** rather than
opening a tab bound to no project. `useJumpToPendingAsk` additionally opens the
side pane **after** the tab exists, because the pane key (`sideChatKey(id)`) is
per-session and would otherwise attach to nothing. The view transition is
supplied by App via `PulseJumpProvider` (the module never reaches into
`activeView` itself).

## Entry points and the "no client router" correction

**This app has no client router.** A view is the `activeView` state union in
`web/src/App.tsx`, so Pulse is the value `"pulse"` and there is **no `/pulse`
URL**. (The spec's "route `/pulse`" was wrong; corrected here.)

Entry points:

- **(a) Dashboard icon button beside the "Projects" heading** — a direct
  `LayoutDashboard` icon button in the `ProjectSidebar` header row, between the
  `h2 "Projects"` and the chevron-left collapse button:
  `data-testid="open-dashboard"`, `aria-label="Open dashboard"`, a Radix
  `Tooltip` reading "Dashboard — all projects", calling the `onOpenDashboard`
  prop. Two earlier shapes were rejected on the same grounds: the spec's
  pinned session-tab pill, because the dashboard is *global* — a per-project
  row would wrongly imply it is project-scoped — and the first shipped shape,
  a hamburger `Menu` popover whose single item was "Dashboard", which buried a
  global, all-projects view two clicks deep *inside* the project list and so
  read as project-scoped too. The `onOpenDashboard` prop is optional: omit it
  and the button is not rendered at all, so a half-mounted tree never offers a
  dead button. The button is also the first Radix `Tooltip` in
  `renderExpandedInner()`'s shared body, so that body now wraps itself in
  `TooltipProvider delayDuration={300}`: both callers (the mobile drawer branch
  and the desktop expanded column) previously rendered it with no provider
  ancestor, and Radix throws "Tooltip must be used within `TooltipProvider`"
  (only the collapsed rail had brought its own).
- **(b) Header badge** — `PulseBadge` renders `● running · ◆ needs you` and
  renders **nothing at zero** (a permanent "● 0 · ◆ 0" is noise).
- **(c) Cmd/Ctrl+J** to toggle in and out (`onTogglePulse` in
  `web/src/hooks/useKeyboard.ts`), returning to the **previous view** via
  `previousViewRef` in App.tsx. It carries the same xterm guard as Cmd+W: if
  `!metaKey` and the target is inside `.xterm`, it returns — Ctrl+J is
  readline's "kill line" and must not be stolen mid-command.

**`"pulse"` is deliberately NOT persisted** per project via
`saveViewStateForProject` — the dashboard is global, so persisting it would
make the *next* project open straight into it. Leaving Pulse therefore also
leaves the previously stored view untouched, so returning lands back where the
user was.

## Desktop

- **App menu "Dashboard…" on Cmd/Ctrl+Shift+J** — plain Cmd+J would be consumed
  by the native menu accelerator *before* the webview sees the key, stealing the
  in-app toggle. Same pattern for Windows/Linux in the File menu
  (`cmd/ocode-desktop/main.go`).
- **Tray "Open Pulse" item** (`window.Show()` + `window.Focus()`).
- Both `ExecJS` a `CustomEvent("ocode:open-pulse")` into the page, exactly like
  the existing `ocode:open-settings`, received by
  `web/src/components/Pulse/PulseShellSignal.tsx`. The load-bearing reason: the
  webview is served over plain `http://` by ocode's own `embed.FS`-backed
  server, so Wails' `wails://` scheme handler **never runs**, `window._wails` is
  never injected, and `window.EmitEvent` is a **structural no-op**, not a timing
  issue.
- Because the user invoked this from the tray/menu bar, the page then asks the
  shell to raise the window over the minimal `_wails.invoke` bridge:
  `invokeWails("ocode:focus-window")` → `application.Options.RawMessageHandler`
  (`web/src/lib/wails.ts` `focusDesktopWindow`) — otherwise the dashboard opens
  behind another app and reads as "nothing happened".
- **There is no dock-badge-click handler.** Wails v3 beta.12's `dock` service is
  display-only and `pkg/events` exposes no application-activated event, so a
  macOS dock click while ocode is running is indistinguishable from plain
  activation. The tray item is the reachable equivalent; the gap is recorded in
  `TODO.md`.

## Known limits

- **Cross-process blindness:** the desktop app and a separately-running dev
  server each have their own in-process session registry (see
  [cross-process-session-sync.md](cross-process-session-sync.md)), so each sees
  only *its own* live sessions.
- A session restored after server start, or idle-evicted at
  `defaultSessionIdleTimeout`, has no live state and appears only under
  `scope=all`.
- **Remote hosts are absent** (v1 local-only).
- **Disk-only rows have no todo** (cost model above), and a row with no todo, no
  ask, and no last assistant line has nothing for the hover overlay to show — it
  says so rather than opening a blank panel (see
  [The card hover overlay](#the-card-hover-overlay)).
- The running card's **activity feed** (prose plus tool lines) comes from
  buffered `text`/`tool_start`/`tool_result` frames while a turn runs and from
  the last turn of the transcript otherwise, because the server clears
  `live_frames` at turn end (`web/src/components/Pulse/usePulseTail.ts`). The
  transcript fetch is the last 200 messages, so a very long final turn shows
  only its tail.

## Tests

- `internal/server/pulse_rows_test.go` — pure derivation: status precedence,
  task fallback, windows, sort, cursor paging.
- `internal/server/handler_pulse_test.go` — validation (400s incl. empty
  `scope=`), merge, `next_cursor` null.
- `internal/server/session_manager_turn_error_test.go` — `lastTurnErr`
  clear/keep semantics.
- `internal/server/todo_updated_event_test.go` — event shape, unknown-ToolID
  skip, cap.
- `web/src/stores/pulseStore.test.tsx` — no synthesized rows, error keeps rows,
  reconnect reseed, `sortPulseRows` mirror.
- `web/src/lib/sessionEvents.pulse.test.ts` — sink forwarded before the
  tracked-session gate; `todo_updated` routing.
- `web/src/lib/jumpToSession.test.tsx` — select-then-open order, path+host
  identity, unknown-project refusal.
- `web/src/hooks/useKeyboard.pulse.test.ts` / `.browser.test.ts` — Cmd/Ctrl+J
  toggle, xterm guard.
- `web/src/components/Pulse/` — `PulseView` (including focus mode), `PulseFocusPane`, `PulseAssistantDrawer`, `PulseCard`, `PulseBadge`,
  `PulseShellSignal`, `pulseFilter`, `usePulseTail` suites.
  - `PulseCard` pins the overlay's **content**, not just that it appears: the
    `text`-kind task reaches it, and it is never a blank panel (loading vs
    genuinely-nothing are distinguished). Asserting only
    `expect(overlay()).toBeInTheDocument()` is what let an empty overlay ship
    green — every overlay test must also assert something *in* it.
  - `usePulseTail` pins the `loading` flag across the seed, the failure path,
    and the disabled state.

## References

- Assistant server side: [pulse-assistant.md](pulse-assistant.md)

- Endpoint: `internal/server/handler_pulse.go`
- Pure derivation: `internal/server/pulse_rows.go`
- Client store: `web/src/stores/pulseStore.tsx`
- Card, overlay, and filter: `web/src/components/Pulse/PulseCard.tsx`, `web/src/components/Pulse/usePulseTail.ts`, `web/src/components/Pulse/pulseFilter.ts`
- SSE routing: `web/src/lib/sessionEvents.ts`
- Jump helper: `web/src/lib/jumpToSession.tsx`
- View wiring: `web/src/App.tsx` (`activeView` union, `openPulse`/`togglePulse`)
- Desktop shell: `cmd/ocode-desktop/main.go`, `web/src/components/Pulse/PulseShellSignal.tsx`, `web/src/lib/wails.ts`
- Design spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
- Plan: `docs/superpowers/plans/2026-09-24-pulse-dashboard/`
