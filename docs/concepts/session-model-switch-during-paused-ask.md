---
type: Concept
title: Session model switch during a paused ask
description: A per-session model pick while the chat is idle — including paused on a permission or question ask — now reconciles the resident agent immediately, so the ask continuation runs on the picked model. Mid-turn picks remain a no-op and land on the next turn.
resource: internal/server/handler_session_state.go; internal/server/agent_session.go
tags:
  - session
  - model
  - permissions
  - asks
  - agent-rebuild
  - server
timestamp: 2026-11-14T18:24:47Z
---
# Session model switch during a paused ask

## What changed

`HandleSetSessionModel` and `HandleClearSessionModel`
(`internal/server/handler_session_state.go`) persist the per-session override and
then, when the session is resident (`lookupAgentSession(id) != nil && as.agent !=
nil`), call `reconcileProfileAgent(id, as, effective)`
(`internal/server/agent_session.go`).

`reconcileProfileAgent` is the gate: it returns the running session unchanged
while `h.sessions.IsTurnActive(id)` is true, and otherwise rebuilds via
`buildAgentSession` + `replaceAgentSession` (carrying `as.messages` across). So a
model picked on an idle session replaces the live agent immediately, and the
continuation that follows the user's answer re-Steps on the new model.

## Why it matters

A turn paused on a permission or question ask is **not turn-active**: `Step`
returns once the ask sentinel is in the transcript, and the turn only resumes when
the user (or the auto-continue question judge) answers it. Before the idle
reconcile, a pick in that window only wrote metadata and pushed a status
snapshot; the answer path (`HandleResolvePermission`, `HandleAnswerQuestion`)
re-Steps on the resident `as.agent`, so the continuation ran on the old model
until the next fresh turn. The same holds for a paused round with several
unresolved asks: the session stays idle until every ask is resolved.

## Invariants

- **The rebuild is idle-only.** `reconcileProfileAgent` is a no-op while
  `IsTurnActive`; a mid-turn pick lets the in-flight turn finish on the old model
  and lands on the next turn (`TestModelPickDuringActiveTurnAppliesToFollowingTurn`).
- **The paused-ask behavior is pinned by
  `TestHandleResolvePermissionUsesModelPickedWhilePaused`** in
  `internal/server/handler_permissions_resolve_test.go`: it asserts the session is
  not turn-active while paused, that the pick replaces the resident agent, and
  that the post-resolve continuation model is the picked one.
- **The transcript must cross the rebuild intact.** `buildAgentSession` is called
  with `as.messages`, so the open ask stays on the tail and the resolver still
  finds it. Losing the transcript would strand the ask, which is worse than
  re-modelling it.
- **`replaceAgentSession` always shares the old child-ask registry** with the
  replacement (`as.childAsks = old.childAsks`) and only denies/shuts down the old
  agent when the session is idle, so a turn racing the swap still finds its asks
  answerable. A sub-agent ask parked in `childAsks` is mid-turn by construction,
  so a pick during one is still a mid-turn no-op; the sharing covers the race
  window, it is not a re-model path.
- **A failed rebuild does not fail the pick.** The error is logged, the override
  stays persisted, and the session rebuilds on its next turn.
- **Do not add a second idle/turn check in the handlers.** The turn-activity gate
  lives in `reconcileProfileAgent`, which every rebuild caller uses; duplicating
  it in the setter/clearer lets the two drift. The reconcile runs outside
  `h.mu`, per the server's map-lock rule.

## Related

- `gotchas/main-model-pick-also-sets-global-default.md` — override resolution order.
- `gotchas/profile-switch-window-id-divergence.md` — the mid-turn no-op contract.
- `concepts/pending-ask-suppresses-advisor-auto-continue-goal.md` — asks,
  turn-activity and the child-ask registry.
