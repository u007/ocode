---
type: Decision
title: Web Compact Feedback — Final Implementation
description: Superseding spec for the web compact feedback design — final implementation with backend status snapshot, transcript-persisted notice, and simplified composer states
tags:
  - web
  - compact
  - design
  - implemented
  - gotcha
  - backend
  - frontend
timestamp: 2026-09-19T05:10:34Z
---
# Web Compact Feedback — Final Implementation

**Type:** Decision  
**Description:** Final design for web/desktop compaction UX: backend status snapshot broadcast for Context gauge accuracy, transcript-persisted compaction notice, and simplified composer states (active/error only). Replaces the earlier queued/running/complete/error spec.  
**Tags:** web, compact, design, implemented, frontend, tui, backend  
**Supersedes:** `superpowers/specs/2026-09-18-web-compact-feedback-design.md` (deprecated)

---

# Web Compact Feedback — Final Implementation

**Date:** 2026-09-19
**Status:** Implemented
**Scope:** Backend + frontend compaction UX in ocode web/desktop

## Summary

Two independent problems existed after compaction:

1. **Context gauge staleness.** The CoworkSidebar "Context" gauge reads `tuiStatus.context_current_tokens`. Before this change, `HandleCompactSession` broadcast only a `"messages"` SSE event — the per-session `"status"` snapshot (and the gauge) kept the stale pre-compaction value until the next turn or tab refetch. Auto-compaction in `applyCompactResult` had the same gap.

2. **Compaction notice was transient and non-persisted.** The composer's `CompactionStatus` bottom bar showed "Compacted: X → Y messages" and persisted only in-memory (`useSyncExternalStore`). Page reloads and the server's post-compaction `messages` broadcast wiped it. The user lost the summary.

Both are fixed.

## Changes

### Backend

| Concern | What changed |
|---------|-------------|
| Status snapshot after manual compact | `HandleCompactSession` (handler.go) now calls `h.publishTurnStatusSnapshot(id)` after releasing `as.mu` |
| Status snapshot after auto-compact | `applyCompactResult` (agent_session.go) also calls `publishTurnStatusSnapshot` after releasing `as.mu` |
| Post-compaction context estimate | `agent.Agent` gained `compactedContextTokens atomic.Int64` + `CompactedContextTokens()` getter. `runCompact` records `CurrentContextEstimate` over the spliced transcript; `applySessionContext` (handler_session_state.go) falls back from `LastInputTokens()` to `CompactedContextTokens()` when the provider reading is 0 |

**Lock discipline:** `applySessionContext` must stay lock-free w.r.t. `agentSession.mu` because `publishTurnStatusSnapshot` is invoked from `runTurn` / permission-continuation while they hold `as.mu`. Both agent reads (`LastInputTokens`, `CompactedContextTokens`) are atomic.

### Frontend

| Concern | What changed |
|---------|-------------|
| Compaction notice moved into transcript | The web renders the persisted synthetic system message whose content starts with `[ocode:compaction-summary]` as a dedicated inline notice (`web/src/components/Chat/CompactionNotice.tsx`, wired in `MessageBubble.tsx`): collapsible header + summary body. Survives reloads and the server's post-compaction `messages` broadcast. |
| Composer states simplified | The `"complete"` compaction state was removed from `CompactionState`. On success `handleCompact` calls `clearCompaction(sessionId)` and returns no completion message. Only `active` (in-flight) and `error` states render the composer bar. |
| `clearCompaction` added | `web/src/lib/compactionState.ts` — clears the session's compaction state, used on success and on error dismiss. |

### Removed

- `CompactionStatus.tsx` "Complete" variant (spinner → before/after message counts)
- `"complete"` variant from `CompactionState` union
- Persistent composer bar for terminal states — replaced by transcript-inline notice

## Marker Contract

The server stores a synthetic system message with content beginning `[ocode:compaction-summary]` after each successful compaction. The frontend:

1. Detects this marker in the message stream
2. Renders it as a `CompactionNotice` (collapsible, inline in the transcript) instead of a raw system message
3. The notice is **persisted server-side** — it survives page reloads and SSE `messages` broadcasts

This is the only durable record of compaction. The composer bar is transient only.

## Context Gauge Accuracy

After compaction:

1. `HandleCompactSession` / `applyCompactResult` calls `publishTurnStatusSnapshot`
2. `applySessionContext` reads the agent's `CompactedContextTokens()` (atomic, lock-free)
3. The gauge renders the reduced post-splice estimate instead of "unknown"

Without step 2, the gauge shows the stale pre-compaction token count until the next `LastInputTokens` update (which happens at the start of the next turn — too late).

## Scope and Limitations

- **Manual `/compact` and auto-compaction** both publish the status snapshot (both paths covered)
- The `CompactionNotice` is rendered client-side from the message stream — the server does not inject it as a separate SSE event
- The gauge accuracy depends on the agent's `CurrentContextEstimate` being reasonably close to the actual post-splice token count; it is an estimate, not a measurement
- Session-scoped: each tab maintains its own composer state independently; the transcript message and gauge are shared via SSE

## Test Plan

| Scenario | Coverage |
|----------|----------|
| Gauge updates after manual compact | `publishTurnStatusSnapshot` called after `as.mu` release |
| Gauge updates after auto-compact | `applyCompactResult` calls `publishTurnStatusSnapshot` |
| Fallback to `CompactedContextTokens` | `applySessionContext` uses getter when `LastInputTokens` is 0 |
| CompactionNotice renders marker | `CompactionNotice.tsx` + `MessageBubble.tsx` inline render |
| No "complete" composer bar | `handleCompact` calls `clearCompaction`, no completion message |
| Survives reload | Server persists `[ocode:compaction-summary]` system message |
