---
type: Concept
title: 'TUI User Interaction: Slash Command Queuing'
description: 'How slash commands entered during streaming or compaction are queued, which commands are instant, and which are queued by design. Amended 2026-10-04: the web/desktop SPA per-tab queue and its instant-command bypass are now documented; web-vs-TUI compaction queueing is documented as a platform difference — the web queues every command during compaction (compactionActive short-circuits ahead of isInstantCommand), while the TUI''s !isInstantCmd guard lets instant commands dispatch immediately.'
resource: CLAUDE.md
tags:
  - tui
  - commands
  - queue
  - web
timestamp: 2026-10-04T05:53:07Z
---
# TUI User Interaction: Slash Command Queuing

**Description:** How slash commands entered during streaming or compaction are queued, which commands are instant, and which are queued by design. Amended 2026-10-04: the web/desktop SPA's per-tab queue and its instant-command bypass are now documented, and web-vs-TUI compaction queueing is documented as a platform difference — the web queues every command during compaction (`compactionActive` short-circuits ahead of `isInstantCommand`), while the TUI's `!isInstantCmd` guard lets instant commands dispatch immediately.
- **Status:** Implemented. TUI queue + `isInstantCmd` documented; web/desktop per-tab queue + `isInstantCommand` bypass documented 2026-10-04; web-vs-TUI compaction queueing documented as a platform difference 2026-10-04.

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
    before send so the request stays valid.
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

- **Membership is a persistence-safety predicate.** `web/src/lib/instantCommands.ts`
  states this in its own header comment: a command belongs in the instant set
  only once its server handler has a mid-turn path that keeps the message inside
  `as.messages` (the shape `Handler.tryEnqueueInjection` provides). A command
  that starts or mutates a turn must stay queued.
- **`HandleBtw` injects into a live turn.** It calls `h.tryEnqueueInjection`
  (`internal/server/handler.go`) — the same path a message sent mid-turn takes —
  instead of appending to the transcript. With no live turn it falls back to the
  unchanged `session.AppendUserMessageForDir` tail-insert with bounded retry
  (`internal/server/handler.go`). This is not an optimisation: a mid-turn
  transcript append makes the stored transcript stop being a prefix of the
  in-memory snapshot, so `session.liveAppendStart` → `samePrefix`
  (`internal/session/sqlitestore.go`, `internal/session/session.go`) drops every
  later live snapshot, and the turn-end sync save reports
  `ErrTranscriptConflict`. Both failures are only logged, so the rest of the turn
  silently vanishes on reload.
- **Compaction queueing differs by platform.** The web SPA queues EVERY command
  while a compaction is active, instant ones included: in `handleSend`
  (`web/src/components/Chat/ChatInput.tsx:885`) the queue condition is
  `compactionActive || (!isInstantCommand(trimmed) && (effectiveBusy || drainingRef.current.has(sessionTabId)))`
  (`web/src/components/Chat/ChatInput.tsx:893`), and `compactionActive` is the
  first, unconditional clause — so `/btw` does NOT bypass compaction on the web.
  The TUI does NOT withhold instant commands while compacting: its gate is
  `(m.streaming || m.compacting || len(m.pendingCompactUIIdx) > 0) && !isExitCmd && !isInstantCmd`
  (`internal/tui/model.go:9055`), so an instant command dispatches immediately.
  The web is conservative because compaction persists through `applyCompactResult`
  (`internal/server/agent_session.go`), which calls `h.replaceSession`
  (`internal/server/agent_session.go`): it replaces the stored transcript
  wholesale, so a concurrently recorded aside would be dropped. The TUI's `/btw`
  side-query never writes the main turn's transcript, so it is safe there.
- **Web and TUI `/btw` differ in mechanism on purpose.** The TUI runs an
  independent side-query child agent (`Agent.AskLoopAsync`) that never touches
  the main turn or transcript; the web records the aside into the conversation
  via `tryEnqueueInjection`. They are not identical.
- **Tests:** `internal/server/handler_btw_test.go`,
  `web/src/lib/instantCommands.test.ts`,
  `web/src/components/Chat/ChatInput.instantCommands.test.tsx`.