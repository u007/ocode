---
type: Gotcha
title: Auto-permission settings not applied to a running chat
description: Unchecking a category in Settings → Permissions had no effect on an already-open chat because runtime reads used the agent's build-time snapshot and the PUT only wrote disk + handler config. Fixed by moving auto config onto PermissionManager behind atomics and pushing it to live agents.
resource: internal/agent/permissions.go; internal/agent/agent.go; internal/server/handler_config.go
tags:
  - gotcha
  - permissions
  - auto-permission
  - settings
  - live
  - config
  - agent
timestamp: 2026-09-24T03:27:56Z
---
Fixed 2026-09-24. Symptom: unchecking a category in **Settings → Permissions** ("Categories the judge must enforce" = `permissions.auto.relaxed_concerns`) had no effect on an already-open chat — the judge kept using the old config.

## Root cause

Two independent bugs compounded:

1. **Runtime reads used the agent's build-time snapshot** (`a.config.Ocode.Permissions.Auto`). The PUT only wrote `h.cfg` + disk; it never pushed to a running agent. A resident agent was rebuilt only on profile/model/credential change (`reconcileProfileAgent`, `internal/server/agent_session.go:430`), so a permissions-only change never landed on the next judge call.

2. **Profile-bound sessions hold a separate config.** `buildAgentSession` sets `effCfg = config.LoadEffectiveForProfile(prof)`; `EffectiveOcodeConfig` shallow-clones, so the global write never reached that session's agent.

**Nuances:** permissions are process-global — profiles do NOT override them. The separate-config issue was only the profile clone. `HandleSetPermissionModel` already pushed `enabled`/`model` live (override), so only the other auto fields were broken.

## Fix shipped

### 1. Live auto config on PermissionManager (atomics)

`internal/agent/permissions.go`:
- `autoConfig atomic.Pointer[config.AutoPermissionConfig]`
- `autoPermissionEnabled atomic.Bool`
- New `SetAutoPermissionConfig(cfg)` / `AutoPermissionConfig()` accessors
- `LoadFromOcode`, `SetMode`, `SetAutoPermissionEnabled`, `Clone`, `ExportConfig` updated
- Writers **replace the pointer**, never mutate in place

### 2. Runtime reads migrated off `a.config`

`internal/agent/agent.go`: new `Agent.autoPermissionConfig()`; all runtime reads migrated off `a.config.Ocode.Permissions.Auto`, including:
- `permission_interpreter.go`
- `permission_typesafe.go` (`configuredAutoJudgeMinConfidence`)
- `permissions.go` (configured auto-judge paths)

### 3. Live push from Settings

`internal/server/handler_config.go`: `HandleSetAutoPermissionConfig` pushes the new config to every live agent via `h.allAgents()` under `h.mu`.

## Rule

**Never read a running agent's process-wide policy config from `a.config`.** Read it through the PermissionManager (`Agent.autoPermissionConfig()`) so a Settings push is visible on the next judge call.

## Residuals

- **Remote-SSH:** in-process only. A remote-SSH chat runs on the host's `ocode serve --remote`; config syncs at connect time (`internal/remote/connect.go:442`), so a settings change requires that host to reconnect.
- **`autoGrants`** stay build-time in the manager (settings form round-trips them unchanged; not user-visible).

## Tests

- `internal/agent/permission_relaxed_concerns_test.go` — `TestSetAutoPermissionConfigAppliesLive` + `TestSetAutoPermissionConfigConcurrentReads` (passes `-race`)
- `internal/server/handler_config_test.go` — `TestHandleSetAutoPermissionConfigPushesToLiveAgents`
- All mutation-verified (revert → fail).

## Files changed

- `internal/agent/permissions.go` — live auto config on PermissionManager (atomics)
- `internal/agent/agent.go` — `Agent.autoPermissionConfig()`; runtime reads migrated off `a.config`
- `internal/agent/permission_interpreter.go` — reads via `autoPermissionConfig()`
- `internal/agent/permission_typesafe.go` — `configuredAutoJudgeMinConfidence` via `autoPermissionConfig()`
- `internal/server/handler_config.go` — `HandleSetAutoPermissionConfig` pushes to live agents
- `internal/agent/permission_relaxed_concerns_test.go` — new tests
- `internal/server/handler_config_test.go` — new test

## Related docs

- `concepts/auto-permission-enforced-categories.md` — the `permissions.auto.relaxed_concerns` config mechanism (that doc covers the config structure; this gotcha covers only the live-apply problem)
- `gotchas/profile-switch-window-id-divergence.md` — mentions `reconcileProfileAgent` at `agent_session.go:430` (the rebuild gate that previously blocked permission-only changes)