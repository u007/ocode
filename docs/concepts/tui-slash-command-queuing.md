---
type: Concept
title: 'TUI User Interaction: Slash Command Queuing'
description: How slash commands entered during streaming or compaction are queued, which commands are instant, and which are queued by design.
resource: CLAUDE.md
tags:
  - tui
  - commands
  - queue
timestamp: 2026-09-30T07:37:10Z
---
# TUI User Interaction: Slash Command Queuing

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
