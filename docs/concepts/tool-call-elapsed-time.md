---
type: Concept
title: Tool-call elapsed time (per-tool wall clock)
description: How ocode times every tool call in the agent loop, carries the duration as UI-only message state to the web, TUI and Pulse renderers, and the two-clock rule that keeps committed history and live blocks consistent.
resource: internal/agent/agent.go
tags:
  - tools
  - elapsed
  - duration
  - sse
  - web
  - tui
  - pulse
timestamp: 2026-11-16T04:29:05Z
status: active
---
# Tool-call elapsed time (per-tool wall clock)

Implemented 2026-11-16. Previously no surface showed how long an individual tool call took; this page is the cross-stack map from the Go stamp to each renderer. For the elapsed-row *single-owner* rule in the composer (the one place two bars could paint the same operation), see `concepts/compaction-config.md` §6. The transcript column these blocks render in is documented in `concepts/web-chat-transcript-layout.md`.

## The stamp pipeline (Go)

- `handleToolCallWithImages` (`internal/agent/agent.go`) records `toolStarts.Store(toolCallID, time.Now())` at entry, keyed by tool-call ID (`sync.Map`). Every tool call — parallel, sequential and DAG-scheduler dispatches — flows through this function.
- `stampToolDurations` (`internal/agent/agent.go`) runs **before** both the publish loop and `finishCancelledRound`, so normal and cancellation exits stamp. It `LoadAndDelete`s the recorded start, making the second call (from `finishCancelledRound`) a no-op for entries already consumed.
- The wall-clock is written on the tool-result `Message` as `ToolDurationMs` → JSON `tool_duration_ms` (`internal/agent/client.go`, `omitempty`). It is **ui-only**: provider payloads are built from explicit fields and never include it — same contract as `suggestions`.
- Rejected duplicate calls and calls that never ran carry `0`, which renderers treat as *not timed* and render nothing (`formatToolDuration` returns `""` for `ms <= 0`).

## Transport

- The SSE `tool_result` frame carries `duration_ms` (`internal/server/handler_sse.go`; `internal/server/handler.go`); the TUI's remote-control mirror carries it too (`internal/tui/model.go`).
- `tool_start` carries **no** timestamp by design. The web measures the live tick from the client clock at frame receipt (`LivePart.startedAt`, set in `web/src/lib/sessionEvents.ts`; rendered with `useNow(1000)`).
- Replayed `live_frames` re-stamp `startedAt = now` at replay time (accepted, same as `CommandActivityBar`).

## Renderers

**Web** — `ToolBlock` (`web/src/components/Chat/TurnParts.tsx`) renders the elapsed on **every** call, both states: pending → `running… · 1.2s` ticking once a second from `startedAt`; done → a static chip from the `durationMs` prop. History and live use different sources:

- Committed path pairs the tool message by `tool_call_id` (`resultById` in `ChatPanel.tsx` carries `tool_duration_ms`; `MessageBubble.tsx` passes `message.tool_duration_ms` for committed rows) — reloads show the agent-measured value.
- Live parts pass `part.startedAt` / `part.durationMs` (`chatStore.tsx` `LIVE_TOOL_RESULT`; `sessionEvents.ts` threads `duration_ms`).
- `humanizeDuration` (`web/src/lib/humanize.ts`) is the one humaniser: `<1s` → ms, `<60s` → one decimal, else `Xm Ys` / `Xh Ym`. Ask-sentinel rows (`question` / `permission`) show **no** duration — the block renders "awaiting your answer"/"dismissed"; the sentinel's wall-clock is meaningless to the user.

**TUI** — the tool-output box header shows `· <elapsed>` via `buildToolOutputBox` (`internal/tui/model.go`), and `rawKey.toolDuration` is part of the render-cache key so a finalized result re-renders. The detail view renders `tool result · <name> · <elapsed>` (`internal/tui/detail_view.go`). `formatToolDuration` (`internal/tui/tool_render.go`) renders `<1s` as ms, then delegates to `formatDuration` (`45s` / `2m3s`). The status bar's in-flight `⚙ name [time · Ns]` row is unchanged (it always showed elapsed).

**Pulse** — `usePulseTail` (`web/src/components/Pulse/usePulseTail.ts`) appends ` · <humanized>` to the ✓/✗ tool line; live events carry `duration_ms`, and the idle-replay path passes `m.tool_duration_ms`.

## The two-clock rule

Committed history renders the **agent-measured** duration (persisted on the message, survives reload). Live blocks render the **client estimate** from `tool_start` receipt until `tool_result` lands, then the server value replaces it. Never "unify" these by stamping `tool_start` server-side or by persisting the client estimate — the persisted value is the only one that survives reload and is the only one providers-adjacent state must not depend on.

Live blocks and command/compaction activity rows are separate owners; the single-owner rule for composer rows is in `concepts/compaction-config.md` §6 — do not add this chip to `CompactionStatus` or `CommandActivityBar`.
