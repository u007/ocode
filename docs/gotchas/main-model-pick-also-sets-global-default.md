---
type: Gotcha
title: Main model pick is per chat; only no-session/draft picks set the global default
description: Picking "main" in the sidebar scopes to the session; writing the global default only happens for new/draft chats. Resolution order, persistence, clear-sentinel behavior, and remote-host routing.
tags:
  - model
  - session
  - global-default
  - sidebar
  - gotcha
timestamp: 2026-09-21T16:07:27Z
---
# Main model pick is per chat; only no-session/draft picks set the global default

A main-model pick (`ModelDialog` `handleSelect` `case "main"`, `internal/server/handler_session_state.go`, `web/src/components/Chat/commands.ts`) is **per chat**, not global. It writes the global default (`cfg.Model`) in exactly one case: a new/draft chat or no session at all.

- **Real session** (`sessionId` present and not `new-*`) -> `api.setSessionModel(id, modelId, ...hostArgs)` ONLY (scoped to this chat; no `SET_MODEL`, no `setConfigModel`).
- **Draft `new-*` tab** -> local `SET_SESSION_MODEL` (rides the session slice; sent as the request model on the first `api.chat`); the server pins it at creation (`internal/server/handler.go:1279` writes `meta["model"] = model`).
- **No session id** -> `SET_MODEL` + `api.setConfigModel(modelId, ...hostArgs)` (the default that NEW chats start from).

`/models <name>` (`commands.ts:1979`) resolves typed names to `ModelInfo.name` (provider-qualified id; `name` = id, `model` = bare name, `display_name` = label) and follows the same three arms (`getSessionId`).

## Resolution order

`effectiveSessionModel` (`internal/server/handler_session_state.go:617`):

1. Non-empty `metadata["model"]` (`session.ReadMetadataForDir`) -> that pinned value (`handler_session_state.go:625`).
2. Key present but EMPTY (`""`) (`handler_session_state.go:625-629`) -> `followDefault = true`; must NOT fall through to step 3 or 4 (`internal/server/handler_session_state.go:634` skips both when `followDefault` is set).
3. Key absent and not Pulse and `id != ""` -> newest usage-ledger row for this session (`usage.LastModelForSession` at `internal/usage/usage.go:386`; skip for Pulse sessions at `handler_session_state.go:634`).
4. Otherwise the process-wide config model (`h.cfg.Model`, `handler_session_state.go:646`); Pulse uses `h.cfg.Ocode.PulseModel` when set (`handler_session_state.go:650`).

`HandleChat` (`internal/server/handler.go:1279`) writes `meta["model"] = model` at creation for every new chat (even with no explicit pick), so a later default change does NOT move existing chats.

`setSessionModelOverride` (`internal/server/handler_session_state.go:697`) writes the `""` sentinel for a clear; deleting the key is avoided so the chat does not silently recover the model from the ledger. `pinSessionModel` (`internal/server/handler_session_state.go:666`) only writes when the key is absent.

## Caveats

- Per-session override wins over the global default; clearing (`""`) means "follow the current default" (`handler_session_state.go:706`).
- A remote session's model picks route to its own host (`hostArgs`); new chats on that host start from the host's config (`ModelDialog` `main` case line:528).
- The `main` pick does NOT auto-promote to the global default; only a draft or no-session pick does (`commands.ts:2020-2029`).

## Cross-reference

- Symptoms of chats following the last-switched model (before the pin/ledger/clear-sentinel fix): `gotchas/chat-model-follows-last-switched-model.md`.
