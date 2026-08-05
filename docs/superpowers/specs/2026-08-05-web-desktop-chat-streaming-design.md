---
title: Stabilize web and desktop chat streaming
type: design
status: approved
created: 2026-08-05
---

# Stabilize web and desktop chat streaming

## Problem

The web and desktop applications share the React frontend. The session-tab
refactor moved the old session mirror out of `SessionPage`, but
`SessionTabSync` currently subscribes only to `status` SSE events. The server
continues to emit chat events, so logs show model activity while the chat UI
does not receive deltas or turn snapshots.

## Design

Use one persistent, unfiltered session mirror mounted by `SessionTabSync`.
Every chat event carries a session ID and is routed to the currently loaded
session. Keep the existing single visible-session `ChatState`; do not add a
session-state map as part of this focused fix. Tab and project changes reload
the selected session from durable history and do not allow stale events to
overwrite it.

The first message from a temporary tab carries the tab ID as a request ID. The
server emits `session_started` with the real session ID before model callbacks
begin, allowing the tab to remap before the first delta. The eventual HTTP
response remains the fallback for reconnects or missed frames.

Headless server subscribers are registered before initial history is loaded.
Events are filtered by session where appropriate. RC bridge events are
decorated with the bridge session ID when needed.

## Event/state contract

Chat events include a transport-level `session_id`; the initial creation event
also includes the request ID. The frontend handles messages, user
messages, thinking/text deltas, tool activity, turn boundaries,
question/permission prompts, status, and errors. `chatStore` keeps its
existing reducer semantics; `SessionTabSync` routes only events for the
currently loaded session.

The existing HTTP response remapping converts a temporary `new-*` tab to the
real session ID. The initial history snapshot and durable server save make the
first turn recoverable even if the first live delta arrives before remapping.

Paginated history remains authoritative for already-loaded older messages.
Incoming full snapshots append only new suffix messages when the current list
is a matching prefix; otherwise they replace the current session snapshot.

## Failure handling

EventSource remains persistent and browser reconnect behavior is retained.
The initial history snapshot makes reconnects recoverable. Unknown or stale
session events are ignored. On tab activation, the existing session
fetch/pagination path reloads durable state before live events are accepted.
HTTP submission failures clear the visible session's streaming state and
surface the error.

## Validation

Add Go tests for session-tagged broadcasts, subscriber setup ordering, and
session filtering. Add frontend tests for event routing, stale-session
filtering, pagination snapshot merging, and turn completion. Run targeted Go
server tests, the full Go suite as practical, and web TypeScript/build checks.
