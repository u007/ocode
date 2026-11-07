---
type: Gotcha
title: useViewerControl seeding, live buffer and one-directive-per-tick rules
description: Rules for useViewerControl, the hook that applies VIEWER_CONTROL directives from the chat transcript to the preview. Seed committed history only once the session is initialized, read mid-turn results from the live buffer, apply committed messages only past the anchor, and apply one directive per macrotask. Also covers the sentinel-scan consequence - a pasted or read-in directive drives the user's own viewer.
resource: web/src/lib/viewerControl.ts; web/src/lib/viewerControl.test.ts; web/src/lib/viewerContext.ts; internal/tool/viewer_control.go
tags:
  - web
  - viewer-control
  - chat
  - transcript
  - react-hooks
  - gotcha
timestamp: 2026-11-07T02:07:18Z
status: active
---
# useViewerControl: seeding, live buffer and one directive per tick

`useViewerControl(sessionId)` (`web/src/lib/viewerControl.ts`) scans the chat
transcript for `VIEWER_CONTROL:{…}` lines and applies each one to the session's
preview. The `viewer_control` tool only emits the line. The hook does the work.
Four rules keep it from replaying history or racing itself.

## The four rules

### 1. Seed committed history only once the session is `initialized`

Until `chatStore` reports the session `initialized`, the hook seeds nothing.
When it does, every directive already in the transcript is marked handled and
never applied. The seeding runs once per session (`seeded` flag).

The reason: seeding an empty transcript, before history has loaded, would make
every directive loaded later look new. A reload or session switch would then
replay all of them against the viewer. Do not drop the `initialized` gate.

### 2. Mid-turn results come from the live buffer

A tool result from the turn in flight sits in the store's `live` buffer. It is
not in `messages` until the turn ends. That is why `preview_open` fires only at
turn end. `viewer_control` cannot wait for that: the hook reads `live` as well as
`messages`, so the directive applies as soon as its result arrives, and the agent
sees the outcome in the same turn.

### 3. Committed messages apply only past the anchor

`anchor` is the server index of the last committed message that was scanned.
Committed messages with a higher server index are new, and they apply once
everything pending has applied. Older messages prepended by scrolling back
through history have lower indexes and stay silent.

The index is the transcript position on the server: `windowStartServerIndex`
plus the local index. It is not the `Message` object. The turn-end `messages`
commit replaces every message object, so an identity anchor (`messages.indexOf`)
was lost at each turn end. A later prepend then rescanned the whole transcript
and re-applied an old `VIEWER_CONTROL` line. The server index survives both the
commit and the prepend, because a prepend shifts local positions but not server
ones.

When `windowStartServerIndex` is -1 (the slice holds only live SSE rows before
its first page, or a mid-turn `MERGE_SNAPSHOT`), `adoptTranscript` marks the
loaded transcript as seen without applying it. History and new rows cannot be
told apart there, so a committed directive in that window is deliberately not
applied. Live tool results still apply.

Known limit (recorded in `TODO.md`): a client-only `ADD_MESSAGE` row, such as a
`/help` reply, counts toward the tail index but has no server index. After the
next turn-end commit the tail index can run ahead of the server by that many
rows, so the first server rows at or below the anchor go unscanned. Those are
usually the user's own prompt, and live tool results still apply.

### 4. One directive per macrotask

The `ready` ref allows one apply at a time. The next directive waits for a
`setTimeout(0)` tick, and the tick bumps a state value so the effect runs again.
The reason is ordering. A `find` must render (its matches counted and written to
the record) before a `find_next` behind it in the same batch can run. Applying
two directives in one effect run breaks that order.

The anchor moves only when nothing pending is left. A directive that is still
waiting for a tick is therefore found again on the next pass.

## Other rules

- Each id applies once (the `handled` set). If an id repeats, `parseViewerControls`
  keeps the first occurrence.
- A directive that fails to parse is logged and skipped, and nothing is applied.
  Failures include bad JSON, an unknown action, and a `find` with no query.
- The outcome (applied, or failed with its error) is written to the record's
  `control` field and pushed, so `viewer_state` reports it.
- The hook is passed the active tab only while the chat side panel is the active
  kind. In every other state it gets null and does nothing, so directives in a
  background session never apply.

## The sentinel is scanned everywhere

As with `PREVIEW_OPEN`, `parseViewerControls` scans every transcript string. That
covers user text, pasted text, file contents a tool read, and every tool output.
A directive that parses and is valid drives the user's own viewer, whatever its
source. Invalid ones are skipped. During development, briefs that quoted example
directives were skipped harmlessly, because the examples did not parse. A parsing
example would have acted.

Do not add a source filter as a quick fix. The transcript is the transport, so a
filter would change the design, and that needs a decision.

## Verification

- Unit tests: `web/src/lib/viewerControl.test.ts`.
- Not yet verified inside a real turn. The unit tests cover the hook rules, not a
  live model call.

## Related

- `docs/concepts/reader-mode-viewer-context.md`: the viewer context record, the
  `viewer_control` and `viewer_state` tools, and the support matrix.
- `docs/concepts/preview-find.md`: the find engines that `find` directives drive.
