---
type: Concept
title: 'TUI User Interaction: Slash Command Queuing'
description: 'How slash commands entered during streaming or compaction are queued, which commands are instant, and which are queued by design. Amended 2026-10-07: web/desktop /btw no longer records the aside into the conversation — both surfaces now run the same independent side query and neither writes the transcript.'
resource: CLAUDE.md
tags:
  - tui
  - commands
  - queue
  - web
timestamp: 2026-10-07T12:40:26Z
---
# TUI User Interaction: Slash Command Queuing

**Description:** How slash commands entered during streaming or compaction are queued, which commands are instant, and which are queued by design. Amended 2026-10-07: web/desktop `/btw` no longer records the aside into the conversation — both surfaces now run the same independent side query and neither writes the transcript.
- **Status:** Implemented. TUI queue + `isInstantCmd` documented; web/desktop per-tab queue + `isInstantCommand` bypass documented 2026-10-04; web-vs-TUI compaction queueing documented as a platform difference 2026-10-04; web/desktop `/btw` switched to the TUI's independent side query 2026-10-07.

- TUI supports `/commands` and `!shell`.
- **Slash command queuing.** All slash commands entered while the agent is
  streaming or compacting are queued (`m.queuedItems`, a unified queue
  preserving insertion order) and executed one-at-a-time after the current
  work ends — not run immediately. Only `/exit`, `/quit`, `/q` bypass the
  queue unconditionally. Synchronous local UI/config commands that do not
  start a new agent request may also bypass the queue ("instant" commands —
  e.g. `/model`, `/small-model`, `/explorer-model`, `/recap`, `/mask`,
  `/permissions`, `/discover`, theme/editor pickers). Drain `m.queuedItems`
  in `agentStreamDoneMsg` and `compactFinishedMsg` handlers, after input
  items are processed, so a command never fires while another stream is in
  flight.
  - **The `isInstantCmd` boolean chain in `handleCommand`
    (`internal/tui/model.go`) is the sole, authoritative list of instant
    commands — do not duplicate it here.** When adding a new synchronous
    local command, add it there (the single chokepoint covering every
    caller: enter key, palette, keybinds, leader shortcuts, hotkeys); this
    doc only explains the *category*, not the membership.
  - `/btw`/`/by-the-way` IS instant: it runs an independent side-query loop
    on its own child agent + client (`Agent.AskLoopAsync`), so it never touches
    the main turn's `OnDelta`/`OnUsage` callbacks and runs concurrently with an
    in-flight stream. A mid-stream snapshot may carry an assistant tool_call
    with no result yet; `repairToolCallSequence` synthesises a placeholder
    before send so the request stays valid. The web/desktop `/btw` now uses the
    SAME side-query mechanism (see below).
  - **Queued by design (mutates persistent state mid-stream, so it must
    wait for the current turn to end):** `/doc-sync`,
    `/agents limit <n>`.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host
  terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader
  sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.

## Web / Desktop SPA: per-tab queue and instant commands

The web SPA has its OWN per-tab queue (`web/src/lib/tabQueue.ts`, mirroring the
TUI's `queuedItems`), and whether a typed item is queued or dispatched
immediately is decided in `handleSend`
(`web/src/components/Chat/ChatInput.tsx`). The web queue originally had no
instant-command list at all, so every `/command` and `!shell` typed while busy
was queued. `/btw` and `/by-the-way` now bypass it
(`web/src/lib/instantCommands.ts`), for TUI parity.

- **Membership is a persistence-safety predicate.** A command belongs in the
  instant set only once its server handler writes nothing to the session
  transcript underneath a live turn — writing mid-turn is what breaks
  persistence: the stored transcript stops being a prefix of the in-memory
  snapshot, so every later live snapshot is dropped
  (`session.liveAppendStart` → `samePrefix`, `internal/session/sqlitestore.go`,
  `internal/session/session.go`) and the turn-end sync save reports
  `ErrTranscriptConflict`, both only logged. `/btw` qualifies because its
  handler writes nothing to the transcript while a turn is live: `HandleBtw`
  runs an independent side query that neither injects into a live turn nor
  appends to the transcript. A command that starts or mutates a turn must stay
  queued.
- **`HandleBtw` runs an independent side query — it never writes the
  transcript.** `Handler.HandleBtw` (`internal/server/handler_btw.go`) starts
  `agent.AskLoopAsync` on the session's live agent: a child with its own client,
  tool-capable but non-interactive (`agent.BtwExcludedTools` removes the
  interactive/dispatch tools). Nothing the aside, its tool activity or its
  answer produces is persisted — the aside is not injected and nothing is
  appended. Progress streams over the session-scoped bus event `btw` with
  phases `started|activity|delta|done|error`. `POST /api/sessions/{id}/btw`
  replies `202`; `DELETE /api/sessions/{id}/btw` cancels; a second `/btw`
  replaces (cancels) the first; `/reset-id` cancels the run and drops its
  registry entry (`internal/server/handler_reset_id.go`).
- **Compaction queueing differs by platform.** The web SPA queues EVERY command
  while a compaction is active, instant ones included: in `handleSend`
  (`web/src/components/Chat/ChatInput.tsx:860`) the queue condition is
  `compactionActive || (!isInstantCommand(trimmed) && (effectiveBusy || drainingRef.current.has(sessionTabId)))`
  (`web/src/components/Chat/ChatInput.tsx:904`), and `compactionActive` is the
  first, unconditional clause — so `/btw` does NOT bypass compaction on the web.
  The TUI does NOT withhold instant commands while compacting: its gate is
  `(m.streaming || m.compacting || len(m.pendingCompactUIIdx) > 0) && !isExitCmd && !isInstantCmd`
  (`internal/tui/model.go:9055`), so an instant command dispatches immediately.
  The web is conservative because compaction persists through `applyCompactResult`
  (`internal/server/agent_session.go`), which calls `h.replaceSession`
  (`internal/server/agent_session.go`): it replaces the stored transcript
  wholesale, so anything written mid-compaction can be dropped. `/btw` itself
  writes nothing, but the web still withholds it — the `compactionActive`
  clause is unconditional.
- **Web and TUI `/btw` share one mechanism.** Both run the same independent
  side query (`agent.AskLoopAsync`, `agent.BtwExcludedTools`) and NEITHER writes
  the transcript. On the web the progress renders in a docked, non-blocking
  `BtwPanel` above the composer (state in `web/src/lib/btwStore.ts`, fed by the
  `btw` bus event); closing it (X, or Esc with focus inside) cancels the run.
- **Tests:** `internal/server/handler_btw_test.go`,
  `web/src/lib/btwStore.test.ts`,
  `web/src/components/Chat/BtwPanel.test.tsx`,
  `web/src/App.btw.test.tsx`,
  `web/src/components/Chat/commands.btw.test.tsx`. Instant-list membership is
  covered by `web/src/lib/instantCommands.test.ts` and
  `web/src/components/Chat/ChatInput.instantCommands.test.tsx`.
