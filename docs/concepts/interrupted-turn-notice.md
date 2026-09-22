---
type: Concept
title: Interrupted Turn Notice
description: How ocode detects a turn cut off after an answered ask and surfaces a Continue action to the user.
tags:
  - chat
  - turns
  - asks
  - ui
  - server
timestamp: 2026-09-22T02:34:08Z
---
When a turn ends with an answered ask but no assistant reply, ocode treats it as **interrupted**. The concept below covers how the server detects this state, how the client consumes it, and where the limits lie.

## Problem

A persisted answer with no reply makes a cut-off turn look finished to anyone reading the transcript. `pending_asks` only flags **unanswered** sentinels, so an answered ask that never received a reply was invisible. Reported on remote session `ses_2026-09-21-101152-d376c0b6`.

## Server detection

`GET /api/sessions/{id}/state` returns `interrupted` (omitempty). The check lives in `Handler.sessionInterrupted` (`internal/server/session_interrupted.go`) and requires all of:

- no active turn,
- no in-flight turn job (the per-session turn lock `executeTurnJob` holds `persist→bootstrap→turn`),
- not parked on an ask (agent lock free, or no resident agent),
- an unfinished tail.

All probes use non-blocking `TryLock` because every tab polls `/state`. Any probe failure is fail-open → `false`.

## Tail rule

`session.TranscriptTailUnfinished` (`internal/session/transcript_tail.go`) classifies the tail:

- **complete** — assistant row with content or notice
- **waiting** — trailing tool round has an unanswered sentinel
- **unfinished** — user row, answered-ask tool row, tool_calls-only assistant, empty/content-less assistant, or other tool row
- **empty transcript** — complete

## Stored read

`session.StoredTranscriptStateForDir` (`internal/session/revision.go`) returns revision + last row in a single sqlite open. `StoredRevisionForDir` is a thin wrapper around it.

## Client

`SessionSlice.interrupted` (`web/src/stores/chatStore.tsx`) is distinct from `wasInterrupted` (set by user-Stop, blocks sending). `interrupted` is set in `applyReconcileState` (`web/src/lib/sessionEvents.ts`); remote sessions compute it on the host.

## UI

`ChatPanel` renders the notice as an inline row at the transcript end — it is **not** a transcript entry, so it never appears in the message list or search index. The row has `role="status"` and is suppressed while `wasInterrupted`, a turn is active, streaming, live parts exist, a pending ask is open, the transcript is empty, the tab is `new-*`, or there is a load error. Continue hides optimistically; App sends the literal `"continue"` message.

## Accepted behaviour & limits

Truncate-mid-round and failed-bootstrap tails read as interrupted after the user walks away. Limits: single-process lock visibility (another process's lock is invisible), no TUI surface, and crash-log marker is deferred.
