---
type: Concept
title: 'Sub-agent Transcripts: OnSubAgentMessage and Child Sessions'
description: Why dispatched child messages never reach the parent's OnMessage/OnDelta and how each run is persisted under its own child session.
resource: CLAUDE.md
tags:
  - subagent
  - session
  - transcript
timestamp: 2026-09-30T07:37:09Z
---
# Sub-agent Transcripts: OnSubAgentMessage and Child Sessions

A dispatched child's messages must NEVER reach the parent's `OnMessage` /
`OnDelta`. Both hosts hang their transcript on `OnMessage` — the server's
`wireLivePersist` appends every message to the parent session file, the TUI
appends to `m.messages` (whose `raw` is replayed to the parent LLM next turn)
— so forwarding there wrote background children's turns into the parent as
if the parent had done the work (and misordered tool results against the
parent's own `agent_status` call). `internal/agent/subagent.go`
(`attachRunTranscript`) instead routes each child message to
`run.appendTranscript`, `Agent.OnSubAgentMessage(run, msg)`, and
`TaskTool.persistChild`, which saves the run under its own child session
`run.SessionID` = `<parent>_child_<agent>_<ts>` (title `Child: <agent>`;
metadata `parent_session_id`, `agent_name`, `run_id`, `status` =
`running` → `done`/`failed`). The persister comes from
`Agent.SetChildSessionPersistence` and MUST be a live async save (server:
`session.SaveAsyncForDir(projectRoot, …)`; TUI: `session.SaveAsync`) — it
fires per streamed child message. Live sub-agent UI comes from the run
transcript (`/api/agents/runs/stream`, TUI agent strip), not the parent chat.
