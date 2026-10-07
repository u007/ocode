---
type: Gotcha
title: A derived title/label must be bounded before it is persisted or rendered
description: 'A session title auto-derived from a multi-megabyte first user message was stored verbatim, poisoning every metadata endpoint and the shared tabs.json, and the raw label forced a full-string browser layout. Rule and fix pattern: cap on write AND on read, and bound the truncation work itself.'
resource: docs/concepts/cross-process-session-sync.md
tags:
  - session
  - title
  - tabs
  - performance
  - poll
  - web
  - gotcha
timestamp: 2026-10-06T16:30:25Z
---
## Symptom

A session's first user message can be multi-megabyte — a pasted "standup
assistant" prompt carrying five full commit messages/diffs is the real case.
The sqlite auto-title path (`internal/session/session.go` `persistToDir`,
brand-new-session branch) derived the title from that first user message and
stored it **verbatim**: a **1,421,992-character** title.

Because the title rides every endpoint that carries session metadata, one
oversized paste poisoned the whole UI's polling surface. Measured live:

| Surface | Size with the poisoned title |
|---|---|
| `GET /api/sessions/:id/state` (polled every 15s per open tab) | 1,559,821 bytes (title ≈ 91%) |
| `GET /api/tabs` | 1,561,733 bytes |
| `~/.local/share/opencode/tabs.json` (shared across processes) | 1,562,552 bytes |

Two compounding costs:

1. **Payload.** `web/src/hooks/useSessionRevisionSync.ts` polls `/state` every
   `REVISION_POLL_MS = 15_000` (`useSessionRevisionSync.ts:13`) for **every**
   open tab, and `/api/tabs` echoes the whole store, so one giant label
   multiplies across the poll and the shared tab store.
2. **Layout.** `UnifiedTabBar.tsx` renders the label as a
   `white-space:nowrap; text-overflow:ellipsis` span (`UnifiedTabBar.tsx:262`)
   plus a `title` attribute and an `aria-label`. Ellipsis is visual only — the
   browser still lays out the **entire** string on every reflow. Headless-
   Chromium measurement against the app's own CSS: **302 ms vs 11 ms** for a
   truncated label; the chat `<pre>` for the 1.4 MB message was **393 ms vs
   14 ms**. `truncateTitle`'s `Array.from(s)` also allocated one array slot per
   character (~14 ms/call).

## Rule

**A title or label derived from user content must be bounded before it is
persisted and before it is rendered.** A label is not a transcript: unlike the
transcript (paged, virtualized), a title is copied into every metadata response,
every tab-store write, and a DOM node — so an unbounded label is a
denial-of-service on the whole UI from a single paste. Bound it at **both** ends:

- **Write cap** — never persist an unbounded derived title.
- **Read cap** — truncate defensively on read, so an already-poisoned row
  self-heals with **no migration** and a legacy/verbatim row cannot ride the
  poll.
- **Bound the work, not just the output.** The truncation itself must not
  materialize the whole string (`Array.from(s)` on megabytes allocates per
  character); walk only a bounded prefix.

## Fix pattern

Server (`session.TruncateTitle` + `session.MaxStoredTitleRunes = 300`,
`session.go:888`/`session.go:896`):

- **Write path:** the auto-title branch calls
  `TruncateTitle(t, MaxStoredTitleRunes)` before storing —
  `internal/session/session.go:705`.
- **Read path:** `storedTitleForDir` (serving `StoredTitleForDir`, used by
  `/state`'s lazy tab hydration) returns
  `TruncateTitle(title, MaxStoredTitleRunes)` —
  `internal/session/revision.go:140`. This is what repairs an already-poisoned
  row **without rewriting it**.
- **Shared tab store:** `internal/tabs` `capTabTitles` (`tabs.go:59`) bounds
  every tab title on **load** (`tabs.go:143`) and on **write** (`ApplyBulk`,
  `tabs.go:292`), so a poisoned `tabs.json` self-heals on the next read.
- The stored cap (300) deliberately equals the web `MAX_TITLE_TOOLTIP_CHARS`
  so a long manual rename survives a reload; the VISIBLE label is clamped
  shorter per surface (web 80, TUI `maxExplicitTitleLen`, server
  `maxGeneratedTitleLen`). `TruncateTitle` trims leading whitespace before
  counting and collapses CR/LF runs to one space, matching the web helper.

### Persistence is bounded at both sqlite row-builders

The auto-title branch above was the first place the cap landed, but the
**stored** value is bounded at both sqlite row-builders too, so persistence
itself is capped and a row poisoned by an older binary heals on its next save:

- `writeSqliteSessionFull` — the `INSERT INTO meta` path (`sqlitestore.go:321`)
  applies `s.Title = TruncateTitle(s.Title, MaxStoredTitleRunes)`
  (`sqlitestore.go:334`) before marshalling metadata.
- `appendSqliteSessionOnce` — the `UPDATE meta SET title = …` path
  (`sqlitestore.go:429`) re-bounds `resolvedTitle` after resolving
  explicit-vs-carried-over title (`sqlitestore.go:513`); that re-bound is what
  heals an already-stored oversized title the next time the session saves new
  content.

This is **deliberately silent truncation** — the user asked for a hard limit, so
it is intentional behaviour, not a hidden fallback. It is safe because the title
is a label, never the transcript: the `messages` table is untouched.
`TruncateTitle` (`session.go:896`) is rune-based (it walks at most `maxRunes`
runes and never materializes a multi-megabyte string) and counts the `...`
**inside** the limit (`out[:maxRunes-3] + "..."`), so the result is at most
`MaxStoredTitleRunes` runes.

Client (`web/src/lib/title.ts`):

- `truncateTitle(s, maxLen = MAX_TITLE_CHARS)` (`title.ts:29`) collapses
  newlines, then walks only a `maxLen * 2`-code-unit window (a rune is ≤ 2
  UTF-16 units) before any `Array.from`, so the per-character allocation never
  sees megabytes.
- Visible label capped at `MAX_TITLE_CHARS = 80` (`title.ts:14`); hover tooltip
  at `MAX_TITLE_TOOLTIP_CHARS = 300` (`title.ts:22`) — a longer renamed title
  stays readable on hover but is still bounded.
- Consumers: `UnifiedTabBar.tsx` (label + `tooltipTitle`),
  `CoworkSidebar.tsx`, `projectStore.tsx` hydration, and `sessionEvents.ts`
  title dispatch.

**Only the title is capped. The transcript is never touched.**

## Repairing a live poisoned session

The read cap needs no migration, but the persisted row (and `tabs.json`) can be
rewritten by renaming the session (via the remote proxy for a remote project).
Measured on the live session:

| Surface | Before | After rename |
|---|---|---|
| `/api/sessions/:id/state` | 1,559,821 B | 262 B |
| `/api/tabs` | 1,561,733 B | 2,002 B |
| `tabs.json` | 1,562,552 B | 2,753 B |

## Tests (mutation-verified)

- Go: `internal/session/session_test.go`
  `TestTruncateTitleBoundsSingleLineAndRunes`,
  `TestStoredTitleForDirCapsOversizedStoredTitle`;
  `internal/tabs/tabs_test.go` (Set / ApplyBulk / load caps).
- Web: `web/src/lib/title.test.ts`.
- 4 Go mutants CAUGHT (write cap, read cap, tabs Set cap, tabs load cap);
  1 web mutant CAUGHT (raw `tab.title` → a 1.65M-char render).
- `go test -race ./internal/session ./internal/tabs` green; web affected suites
  535 tests green; `tsgo` clean.
