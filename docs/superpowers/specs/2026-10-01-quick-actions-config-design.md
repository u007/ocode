# Configurable quick-action chips below the chat composer — design

Date: 2026-10-01
Status: approved in chat (design presented and accepted; cap set to 20). Awaiting spec review.

## Problem

The web/desktop composer has a hardcoded quick-action strip below the send row
(`web/src/components/Chat/QuickActionsBar.tsx`, driven by
`ChatInput.tsx:691`). Its three pills — Compact, Continue/Resume, Recap — are
baked into the component. Users cannot add their own one-click nudges, and the
three that exist cannot be relabelled, reordered, or removed.

Today the array is a literal and `runQuickAction` is a `switch` on three string
ids (`ChatInput.tsx:669`). Anything user-configurable must replace both, and the
three existing pills are the natural seed for that configuration rather than a
parallel special case.

## Decisions

Each was chosen explicitly in brainstorming, not inferred.

| Decision | Choice | Why |
| --- | --- | --- |
| Click behaviour | `fill` by default, per-chip `send` option | A custom chip's text is often a template worth reviewing before it costs a turn. Users who want one-click dispatch opt in per chip. |
| Storage | Server config (`ocodeconfig.json`) | Follows the user across projects, browsers, and the desktop `.app`. localStorage would strand the list per-browser. |
| Built-ins | Fully uniform; the 3 ship as starter presets that may be edited or deleted | One data model, one code path. |
| Uniqueness of Continue | Hidden `seed` marker preserves resume-if-interrupted | See "The one non-uniform case". |
| Cap | 20 chips | A longer strip wraps to several rows above the composer and the settings list becomes unusable. |
| Ordering | User-sortable; array order is the display order | Sorting is expected of any list people curate. |
| Chip messages | Dispatch exactly as if typed, so `/` and `!` work | A chip can reach every slash command and shell, with no special cases. |
| Icons | Per-chip picker from a fixed lucide allowlist | Keeps pill metrics uniform and needs no icon upload. |
| Empty session | Custom chips always visible; seeded ones keep the hide-until-history rule | "Run the tests" is useful on a fresh chat; Compact is not. |
| TUI | Not in scope | The request was explicitly desktop/web. |

## Data model

```json
"quick_actions": {
  "chips": [
    { "id": "compact",  "label": "Compact",  "icon": "archive",   "message": "/compact", "mode": "send", "seed": "compact" },
    { "id": "continue", "label": "Continue", "icon": "play",      "message": "continue", "mode": "send", "seed": "continue" },
    { "id": "recap",    "label": "Recap",    "icon": "file-text", "message": "/recap",   "mode": "send", "seed": "recap" }
  ]
}
```

All three starters carry a `seed`, not just Continue. The seed's two
derived duties both apply to the set: any `seed` means "requires history"
(hidden on an empty session), and `seed:"continue"` additionally means
"resume-if-interrupted". An earlier draft of this block showed `compact`
and `recap` without one; that was wrong and the implementation was written
against the correct form.

Every chip is uniform: `id`, `label`, `icon`, `message`, `mode`. Array order is
the sort order. `seed` is the single optional hidden field.

- `id` — stable identity. Required, non-empty, unique. The 3 starters use the
  slugs `compact` / `continue` / `recap`; the settings form mints ids for new
  chips.
- `label` — visible pill text. Required, non-empty after trim.
- `icon` — key from the allowlist. Empty/absent normalizes to `zap`.
- `message` — the text dispatched. Required, non-empty after trim. A leading
  `/` or `!` is significant and routes to the slash / shell pipelines.
- `mode` — `fill` | `send`. Absent normalizes to `send`, which reproduces
  today's behaviour for all three starters.
- `seed` — `compact` | `continue` | `recap`, or absent. Present only on
  shipped starters.

### Icon allowlist

The allowlist is an explicit set, not a free-text field, so validation can reject
a bad key instead of rendering a blank. It must be declared **once**, in Go, and
mirrored in TS only as the key → lucide-component map used for rendering; the Go
set is the validation authority and a key in one but not the other is a
typecheck/test failure, not a silent mismatch.

```
zap  archive  play  file-text  search  refresh-cw  terminal  git-branch
hammer  bug  flask-conical  shield-check  list-checks  wand-sparkles
package  book-open  file-code  messages-square  rocket  scissors
wrench  eye  gauge  chart-no-axes-column
```

`archive`, `play`, and `file-text` back the three starters. All 24 names were
checked against the installed `lucide-react@1.17.0` (1962 icons available) and
every one resolves to a real icon module. If the dependency is upgraded,
re-verify — a bad import is a build error, and `npm run typecheck` is the gate.

### The one non-uniform case

Compact and Recap survive as pure text: `/compact` and `/recap` are real slash
commands, and `dispatchCommand` already routes them. **Continue cannot be.** Its
built-in behaviour is context-aware: when a turn was interrupted the pill calls
`handleResume()` instead of sending anything. A uniform label+icon+message entry
cannot reproduce that — edit it to "keep going" and clicking it mid-interruption
would start a *new* turn instead of resuming.

Note what this deliberately gives up: the old hardcoded pill relabelled itself to
"Resume" while a turn was interrupted. The shipped pill **does not**. The label is
user-controlled, so it stays whatever the user set; the tooltip gains
`— resume the interrupted turn` instead. Pinned by a test named "keeps the
configured label".

So the Continue preset carries `seed: "continue"`, which enables
resume-if-interrupted. The seed governs **only** that one behaviour. Label,
icon, mode, sort position, and deletion all stay fully user-controlled.

### Two derived states, deliberately not stored

- **Dim while compacting.** `disabled = compacting && chipDispatchesCompact(chip)`,
  computed in `ChatInput` where `compacting` already lives. A pure helper in
  `lib/quickActions.ts` that matches a normalised `/compact` message. A user who
  points a chip at `/compact` inherits correct dimming for free. Storing a flag
  would drift the moment the message is edited.
- **Requires history.** `seed != null` ⇒ keep today's `hasConversation` hide.
  Custom chips have no seed, so they always show. This is the mechanism behind
  the "empty session" decision above.

## Seed ownership — Go is the single source

The three starters carry icons, seed markers, and behaviour. Duplicating that
list in TS and Go is a known way this repo has shipped a bug: the chat-verbosity
preset matrix existed in four places and silently drifted (§9 of the spec table
disagreed with the resolver, and the tests were written from the wrong source, so
the whole chain agreed with itself and disagreed only with the spec).

Therefore: **Go seeds and returns resolved chips.** `GET /api/config/ocode/quick-actions`
returns the starters when the key is absent. The TS store holds a degraded
fallback used only when the GET fails, and it is explicitly *not* a second
authority — it is the previous behaviour rendered so a failed fetch degrades
rather than blanking the strip.

## Validation (Go)

- absent `chips` → seed the 3 starters
- `len(chips) > 20` → error
- empty chips array → legal, and means "no strip" (the user removed everything)
- per chip: `id` non-empty and unique; `label` non-empty after trim; `message`
  non-empty after trim; `icon` in the allowlist; `mode` in `{fill, send}`;
  `seed` in `{compact, continue, recap}` when present
- unknown field anywhere in the block → error (strict decode, so a typo'd key
  surfaces instead of being silently dropped)
- `delete(raw, "quick_actions")` in the unknown-key sweep, or the repo's
  unknown-key detection trips on a known key

Follow the closest existing precedent for shape and error wrapping:
`applyChatVerbosityConfig` and its `Validate` counterpart in
`internal/config/ocodeconfig.go`.

## HTTP API

```
GET  /api/config/ocode/quick-actions   → { chips: [...], revision: string }
PUT  /api/config/ocode/quick-actions   ← { chips: [...] }        (full replace)
```

Auth-wrapped like the other config routes. Registered in
`internal/server/server.go` beside the chat-verbosity pair. The PUT replaces the
**`quick_actions` value only** — not the whole config file — so sibling keys such
as `chat_verbosity` and `compact` are never read, rewritten, or clobbered.
`web/src/lib/compactConfig.ts` is the precedent for patching one key in place.

## Client architecture

| Concern | Home |
| --- | --- |
| Config store (cached, shared across session tabs, invalidated on bus events) | `web/src/lib/quickActions.ts` (new) — mirrors `lib/chatVerbosity.ts`: module-level cache, listener set, in-flight dedupe, `revision`, re-fetch on invalidation |
| Pure helpers | same file: `normalizeChip`, `seededChips`, `chipDispatchesCompact`, `visibleChips`, icon-key → component map |
| Settings form | `web/src/components/Settings/QuickActionsForm.tsx` (new) — mirrors `ChatDisplayForm.tsx`: load, local draft, Save, error surface, re-sync on external change |
| Reordering | `@dnd-kit` sortable, `UnifiedTabBar.tsx` precedent |
| Icon picker | select over the allowlist, rendered with the real lucide component so the chosen icon is visible in the list |
| Registration | one `case "quick-actions":` in `SettingsPanel.tsx` |
| API client | `getQuickActionsConfig` / `setQuickActionsConfig` in `api/client.ts`; types in `api/types.ts` |
| Strip | `ChatInput.tsx` — hardcoded array and `switch` deleted, strip reads configured chips |

`QuickActionsBar.tsx` is **not modified**. It already takes `actions` + `onSelect`
and is purely presentational; only the `icon` field means `ChatInput` must map
the stored key to a component, and that map lives in the lib so the form and the
composer share one source.

### Composer wiring

`useQuickActions()` is module-global, so N mounted session tabs cause one fetch.
Per click:

1. `seed == "continue"` and `wasInterrupted` → `handleResume()`, return. Wins
   over both modes — resume is not a text dispatch.
2. `mode == "fill"` → `updateDraft(chip.message)` and focus the textarea. No
   send, no queue push, no draft clearing.
3. `mode == "send"` → `runQuickDispatch(chip.message, kind)` where `kind` is
   `"command"` for a `/` prefix and `"message"` otherwise. This is the existing
   helper, so busy-turn queueing, the compaction barrier, and the drain-on-idle
   path are unchanged.

**`fill` replaces the draft rather than appending.** Clicking a chip states a
new intent, matching the slash-command menu. Flagged in chat as a reversible
call; if the user prefers append or prepend, it is a one-line change in step 2.

### The visibility gate moves from the wrapper to the chip

Today the whole strip is gated at the JSX level (`ChatInput.tsx:1358`):

```tsx
{hasConversation && (<QuickActionsBar actions={quickActions} onSelect={runQuickAction} />)}
```

A per-chip visibility rule cannot live there — one always-visible custom chip
would be suppressed by a wrapper meant for the seeded three. So the wrapper is
replaced by:

```tsx
{visibleChips.length > 0 && (
  <QuickActionsBar actions={visibleChips} onSelect={runQuickAction} />
)}
```

where `visibleChips` (a pure helper in `lib/quickActions.ts`) drops seeded chips
when `hasConversation` is false and keeps every custom chip. The strip therefore
disappears only when *no* chip is visible, which is also the new empty-list case
that today's three fixed pills never had to express. `hasConversation` stops
being read for the wrapper and is consulted only inside `visibleChips`.

## Behaviour matrix

| Chip | Mode | Empty session | Turn interrupted | Result |
| --- | --- | --- | --- | --- |
| seeded (any) | either | yes | — | hidden |
| seeded (any) | either | no | no | per mode |
| `seed=continue` | either | no | yes | `handleResume()` |
| custom | either | n/a | either | per mode |
| any with `message` = `/compact` | send | no | either | additionally `disabled` while compacting |

## Migration

`quick_actions` absent ⇒ GET returns the seeds, and **the loader never writes
anything**. So a fresh install renders the same three pills in the same order: the
feature is additive and its out-of-the-box state is today's behaviour.

What is *not* true is that the file stays byte-unchanged until you save from
settings. `writeOcodeConfigFile` rewrites the whole config whenever ANY setting is
saved, and its payload always includes `quick_actions`, so the first unrelated save
— a permission toggle, a plugin toggle — materialises the seeded block. That is
deliberate and load-bearing (without it, chip edits would never persist) and it
matches the sibling key added one line above, `chat_verbosity`, which is also
always written and also normalised. The observable behaviour is identical either
way, because the loader seeds exactly the values the payload would have written.

## Testing

Go:
- validation matrix — cap, duplicate ids, empty label/message, bad icon, bad
  mode, bad seed, unknown field, absent key
- GET seeds the 3 starters when the key is absent
- PUT round-trips a reordered/edited set; GET returns it unchanged
- **route registration through the real mux** — a handler-only test cannot catch
  a missing route, which has bitten this repo before (`handler_terminal_tabs_routes_test.go`)

TS:
- `lib/quickActions.ts` pure helpers — normalize, seed, compact detection, visibility
- `QuickActionsForm` — add, edit, delete, reorder, persist, cap at 20
- `ChatInput` — fill populates the draft without sending; send dispatches;
  resume kind wins over both; seeded hidden on an empty session while a custom
  chip shows; ordering respected; `/` and `!` route correctly
- **regression:** a default config reproduces today's exact three pills, in
  order, with the right icons and titles

Then `go build ./...`, `go vet`, `gofmt -l`, `npm run typecheck`, `vite build`,
and the affected vitest suites.

**Mutation-verify the new tests.** For each behavioural guard, break the
implementation and confirm the test fails — and confirm the mutant *compiles*
first, since a build-only failure is INVALID, not CAUGHT.

## Out of scope

- TUI parity (the request was desktop/web)
- Per-project chip lists
- Importing chips from prompt history
- Custom icon upload
- Making `fill` append instead of replace

## Files

**New**
- `internal/server/handler_quick_actions_test.go`
- `web/src/lib/quickActions.ts`
- `web/src/lib/quickActions.test.ts`
- `web/src/components/Settings/QuickActionsForm.tsx`
- `web/src/components/Settings/QuickActionsForm.test.tsx`
- `web/src/components/Chat/ChatInput.quickActions.test.tsx`

**Modified**
- `internal/config/ocodeconfig.go` — field, validation, seed, unknown-key sweep
- `internal/server/handler_config.go` — GET/PUT handlers
- `internal/server/server.go` — routes
- `web/src/api/client.ts`, `web/src/api/types.ts`
- `web/src/components/Settings/SettingsPanel.tsx` — one case
- `web/src/components/Chat/ChatInput.tsx` — replace array + switch
- `skills/ocode-web/SKILL.md`, `CHANGES.md`

**Unmodified (deliberately)**
- `web/src/components/Chat/QuickActionsBar.tsx` — already presentational

**Docs bundle** — a concept page via the context agent (`doc_write`), since
`docs/` is bundle-owned. `docs/index.md` and `docs/log.md` are auto-managed and
must not be hand-edited.

## Risks

- **Prompt-cache safety** — this adds no per-turn content to `tools` or `system`.
  The strip is a post-render UI concern, so the cached prefix is untouched.
- **A chip that is a destructive command** — a user can point a chip at
  `/clear`. That is their config, and it lands on the existing slash pipeline
  with the existing confirmation rules; this feature adds no new execution path.
- **Cross-window staleness** — like the other config surfaces, a save in one
  browser should reach others. Reuse the `eventBus` invalidation pattern from
  `lib/chatVerbosity.ts` rather than inventing a new one.
- **Icon drift** — the allowlist must reference icons that actually exist in the
  pinned `lucide-react`. A bad import is a build failure, so verify the
  typecheck rather than assuming.
