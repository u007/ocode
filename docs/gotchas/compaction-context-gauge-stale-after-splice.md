---
type: Gotcha
title: 'After Compaction: Publish Status Snapshot + Record Estimate or Context Gauge Goes Stale'
description: 'Gotcha: after compaction the server must publish a status snapshot and the agent must record a post-splice estimate, else the Context gauge goes stale; the compaction notice is the persisted [ocode:compaction-summary] system message'
tags:
  - compact
  - gotcha
  - status-snapshot
  - context-gauge
  - lock-discipline
timestamp: 2026-09-19T05:11:05Z
---
# After Compaction: Publish Status Snapshot + Record Estimate or Context Gauge Goes Stale

**Type:** Gotcha  
**Description:** After splicing the transcript during compaction, the server must publish a status snapshot and the agent must record a post-splice context estimate. Without both, the web Context gauge shows stale pre-compaction values. The compaction notice is a persisted `[ocode:compaction-summary]` system message, not a composer bar.  
**Tags:** compact, gotcha, status-snapshot, context-gauge, lock-discipline

---

# After Compaction: Publish Status Snapshot + Record Estimate or Context Gauge Goes Stale

## The Problem

The CoworkSidebar "Context" gauge reads `tuiStatus.context_current_tokens`. After compaction splices the transcript, the pre-compaction token count remains in the status snapshot until the next explicit snapshot publish. The user sees a misleadingly high context usage until the next turn or tab refetch.

## Root Cause

`HandleCompactSession` and `applyCompactResult` (auto-compaction) both splice the transcript and clear `LastInputTokens`, but neither published the per-session `"status"` SSE event afterward. The status snapshot still carried the old token count.

## The Fix (two parts)

1. **Publish snapshot after compaction.** Both `HandleCompactSession` (handler.go) and `applyCompactResult` (agent_session.go) now call `h.publishTurnStatusSnapshot(id)` after releasing `as.mu`.

2. **Record post-splice estimate.** `agent.Agent` gained `compactedContextTokens atomic.Int64` + `CompactedContextTokens()`. `runCompact` records `CurrentContextEstimate` over the spliced transcript. `applySessionContext` (handler_session_state.go) falls back from `LastInputTokens()` to `CompactedContextTokens()` when the provider reading is 0.

## Lock Discipline

`applySessionContext` reads from the agent **without holding `as.mu`**. This is safe because:
- `publishTurnStatusSnapshot` is invoked from `runTurn` / permission-continuation while they hold `as.mu`
- Both `LastInputTokens` and `CompactedContextTokens` are `atomic.Int64`
- Holding a lock in `applySessionContext` would deadlock because the caller already holds `as.mu`

**Rule:** never add a mutex lock inside `applySessionContext` for agent reads — use atomics only.

## The Marker Contract

After successful compaction, the server stores a synthetic system message with content starting `[ocode:compaction-summary]`. This is the **only durable record** of compaction — it survives page reloads and SSE broadcasts.

The frontend renders it as a `CompactionNotice` (collapsible, inline in the transcript via `MessageBubble.tsx`). The old composer `CompactionStatus` "Complete" bar was removed. On success, `handleCompact` calls `clearCompaction(sessionId)` — no completion message is returned.

**Do not** re-add a "complete" composer state or a transient toast for compaction. The `[ocode:compaction-summary]` system message IS the notice.

## Related Files

| File | Role |
|------|------|
| `internal/server/handler.go` | `HandleCompactSession` — manual compact endpoint |
| `internal/server/agent_session.go` | `applyCompactResult` — auto-compaction goroutine |
| `internal/server/handler_session_state.go` | `applySessionContext` — fallback to `CompactedContextTokens` |
| `internal/agent/agent.go` | `compactedContextTokens` field, `CompactedContextTokens()` getter |
| `web/src/components/Chat/CompactionNotice.tsx` | Renders the `[ocode:compaction-summary]` notice |
| `web/src/components/Chat/MessageBubble.tsx` | Wires CompactionNotice into message stream |
| `web/src/lib/compactionState.ts` | Composer state (active/error only; `clearCompaction` added) |
