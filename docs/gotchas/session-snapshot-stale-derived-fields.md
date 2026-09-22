---
type: Gotcha
title: 'Session-tagged snapshot: override base fields but recompute ALL derived fields'
description: 'Durable rule: every session-tagged TUIStatus snapshot must pin MainModel/CWD AND recompute derived fields (ModelPrompt) — overriding without recomputing leaves stale .OCODE.md/Kaizen banner in the web sidebar.'
tags:
  - gotcha
  - session
  - snapshot
  - model-prompt
  - sidebar
  - tui-status
  - derived-field
timestamp: 2026-09-21T08:12:40Z
---
# Session-tagged snapshot: override base fields but recompute ALL derived fields

**Symptom:** After switching the model in the web/desktop UI, the sidebar "◆ Model prompt" row still showed the ORIGINAL model's conduct (`.OCODE.md` + Kaizen directives). UI-only bug — the injected prompt was already correct on the agent side, but the sidebar banner displayed stale data.

**Cause:** `buildStatusSnapshot` (`internal/server/handler_tui_status.go:91`) computes `ModelPrompt = buildModelPromptInfo(cwd, snap.MainModel)` from the process-wide `cfg.Model` + server workDir. Session-tagged builders then overwrite `MainModel` with the session's effective model but did NOT recompute `ModelPrompt`.

Affected builders:
- `HandleSessionStatus` (`internal/server/handler_session_state.go`)
- `pushSessionStatusSnapshot` (`internal/server/handler_session_state.go`)
- `publishTurnStatusSnapshot` (`internal/server/handler_session_state.go`)
- `finishSessionTitle` (`internal/server/title_gen.go`) — also never pinned the effective model at all.

The web main-model pick is a per-session override: `ModelDialog.tsx:361` → `PUT /api/sessions/{id}/model` → `HandleSetSessionModel` → `pushSessionStatusSnapshot`.

**NOT a logical bug:** the next turn rebuilds the agent from `desiredModel := h.effectiveSessionModel(id)` (`internal/server/handler.go:1237` → `reconcileProfileAgent` at `agent_session.go:430/447`) and the prompt loads `LoadModelContextWithSourceAt(a.modelContextRoot(), a.client.GetModel())` (`internal/agent/agent.go:5316`). Kaizen gating uses the live client model (`internal/agent/discovery_glue.go:254`). Agent model-context cache is invalidated on `applySpecModel`/`SetWorkDir` (`agent.go:5263/2850`). The TUI-bridged path was already correct (`internal/tui/model.go:15714-15720` memoizes `computeModelPromptInfo` keyed on (agent client model, workDir)).

## Durable rule

> **Every session-tagged `TUIStatus` snapshot must pin `MainModel`/`CWD` to the session's effective values AND recompute any field DERIVED from them (`ModelPrompt`).**
>
> `buildStatusSnapshot` derives `MainModel`/`CWD`/`ModelPrompt` from the global `cfg.Model` + server workDir. Overriding `MainModel`/`CWD` without recomputing `ModelPrompt` (and any future derived field) leaves stale derived data in the web sidebar.

## Fix pattern

`applySessionModelPrompt(snap, baseModel, baseCWD)` (`internal/server/model_context.go`) recomputes the banner **only when `MainModel`/`CWD` changed**, guarded to avoid a second disk scan (resolving a `.OCODE.md` shells out to git for tracked files via `agent.readContextFileAt`). When neither changed, the existing `buildStatusSnapshot` value is kept.

Wired into all four session-tagged builders at `handler_session_state.go:175`, `:400`, `:481` and `title_gen.go:152`.

## Tests

`internal/server/model_context_test.go`: `TestSessionStatusRecomputesModelPromptForOverride` + `TestPushSessionStatusSnapshotRecomputesModelPrompt` — both mutation-verified (fail at HEAD before the fix).