---
type: Gotcha
title: Main model pick also sets the global default
description: Picking a main model in the sidebar writes it globally; resolution order, persistence, and caveats.
tags:
  - model
  - session
  - global-default
  - sidebar
  - gotcha
timestamp: 2026-09-21T16:07:27Z
---
# Main model pick also sets the global default

Picking a main model in the sidebar model picker (web/desktop `ModelDialog` `handleSelect` case `"main"`) now does **two things**: it scopes the pick to the current session AND writes the same model as the **global default** (`cfg.Model`).

## Resolution order (no explicit override → global default)

`effectiveSessionModel` (`internal/server/handler_session_state.go:416`) falls back to `h.cfg.Model` for new sessions with no per-session override. The global write flows through `HandleSetModel` (`internal/server/handler_config.go:39`): sets `cfg.Model` → `SaveLastModel` → `SaveRecentModel` → `pushStatusSnapshot`. Survives restart because `config.Load` prefers `last_model` (`internal/config/config.go:230`). TUI parity: `finishModelSwitch` (`internal/tui/model.go:9450`) already did the same.

## Three dispatch arms (all also fire global `SET_MODEL`)

- **Real session** → `api.setSessionModel(id, model, host)` + `api.setConfigModel(model, host)`
- **Draft `new-*` tab** → local `SET_SESSION_MODEL` + `api.setConfigModel(model, host)`
- **No session** → `api.setConfigModel(model, host)` only

## Caveats

- An **explicit per-session override still wins** — other open tabs are unaffected.
- **`Clear (not set)`** clears only the session override, not the global default.
- **Remote sessions** write the global model to their OWN host (`hostArgs`), so a remote new session uses the host config, not the local one.

## Divergence from draft spec

`superpowers/specs/2026-09-18-per-session-sidebar-settings-design.md` described new sessions starting from global defaults with an explicit "Set as global default" promote action. The main model is now auto-promoted on pick; other sidebar settings still require the explicit promote.
