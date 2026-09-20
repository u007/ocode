---
type: Gotcha
title: Web/Desktop Chat Went Stale Because a Dead SSE Body Never Errors
description: 'Added a note about a second failure mode: turn_active:true with no heartbeat during permission/question continuations causes a false "stalled" badge; continuations must publish heartbeats.'
tags: []
timestamp: 2026-09-20T10:44:16Z
---
# Web/Desktop Chat Went Stale Because a Dead SSE Body Never Errors

**Type:** Gotcha  
**Description:** Gotcha: desktop/web chat "lost streaming, then loaded the reply from storage, then stopped updating". The fetch-based /api/events body can go silently dead (WKWebView suspend, sleep/wake, interface change) without erroring or ending, and the client ignored the server keepalive pings, so nothing ever reconnected.  
**Tags:** gotcha, web, desktop, sse, eventBus, reconnect  

---

## Problem

In the desktop app (and browser web UI) a chat session would intermittently
stop streaming. Sending still worked, the reply later appeared all at once
(transcript refetch), and afterwards the tab stayed "stopped" until a page
reload. It felt correlated with switching projects but was not.

## Root cause

`web/src/lib/eventBus.ts` holds one long-lived `fetch()` SSE stream on
`GET /api/events`. It reconnected only when the fetch threw or the body
reader returned `done`. A half-dead connection does neither: the reader just
never resolves. The server writes `: ping` every 20s
(`handler_events.go` `sseKeepaliveInterval`), but `readSSEStream` discards
comment frames and the bus tracked nothing about them, so a silent body was
indistinguishable from an idle one.

Consequences chained from there:

- deltas never arrive → no streaming;
- the turn watchdog (30s without `turn_heartbeat`) reconciles and, once the
  server reports the turn finished, `MERGE_SNAPSHOT`s the persisted
  transcript → "loaded from storage";
- the next turn's `turn_started` never arrives, so `turnActive` stays false
  and the watchdog has nothing to watch → "stale and stopped".

Project switching only *looked* causal: when the open-tab project set changes,
`setProjects()` does a controlled restart, which happened to heal the stream.

## Fix

`eventBus` now wraps the response body in a `TransformStream` that re-arms a
liveness timer on every raw chunk (pings included). No bytes for
`LIVENESS_TIMEOUT_MS` (45s, two missed pings) → warn, abort, reopen
immediately (no backoff — the connection is known dead), which fires the
existing reconnect/reconcile path including `live_frames` replay. A `window`
`online` event also restarts the stream so an interface change does not wait
out the window.

## Rule

Any long-lived fetch/SSE consumer must measure liveness on raw bytes and
reconnect itself; never rely on the browser reporting a dead socket. Keep the
client window > 2× the server keepalive interval.

---

## Related failure mode: false "stalled" badge during permission/question continuations

A different root cause produced the same symptom — a permanent false "stalled"
badge on the web project sidebar for sessions that were actively working:

- `runTurn` starts the `turn_heartbeat` ticker (10s) while it holds
  `turnActive=true`, but the permission-resolve continuation
  (`HandleResolvePermission`) and question-answer continuation
  (`HandleAnswerQuestion`) did **not** start a heartbeat even though they too
  hold `turnActive=true` while streaming. A continuation that ran for minutes
  therefore reported `turn_active:true` with zero `turn_heartbeat` events.
- The web stall watchdog (`useTurnWatchdog.ts`, threshold 30s) saw
  `turn_active:true` + no heartbeat for 30s and marked the session "stalled".
- The sidebar reconcile deliberately **keeps** the stall while the server
  reports `turn_active:true` — so the badge never cleared.

Fix: both continuations now call `(*Handler).startTurnHeartbeat(sessionID)`
(defined in `internal/server/agent_session.go`) right after
`setTurnActive(true)`, with `defer` to stop it on unwind. The server-side
contract is that **every** code path holding `turnActive=true` must publish
heartbeats for its duration — see `docs/superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md` (Part 03, Task 4) for the full contract.
