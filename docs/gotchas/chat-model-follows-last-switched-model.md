---
type: Gotcha
title: Chat model followed last switched global model (fixed)
tags:
  - gotcha
  - chat
  - model
  - usage-ledger
  - web-desktop
timestamp: 2026-10-28T05:52:51Z
description: Web/desktop chat that changed to the last model switched globally; fixed by pinning per-chat model at creation, recovering legacy chats from the usage ledger, and making "clear model" an explicit empty sentinel.
---

Symptom: switching back to an existing chat (e.g. `ses_2026-10-25-001110-0cee49be`, `ses_2026-10-25-171757-e3cc7a66`, `ses_2026-10-26-154657-6cc1bc9a`) ran the model picked MOST RECENTLY anywhere (`opencode-go/deepseek-v4.1-flash`) instead of the chat's actual last model (`openrouter/nvidia/nemotron-3.5-super-vl:free`). A "new chat" model selection (`ModelDialog` with no session, `/models <name>`) was not supposed to retroactively change existing chats.

Root cause (three compounding):
- `effectiveSessionModel` (`internal/server/handler_session_state.go:617`) resolved a session with no `metadata["model"]` override through `cfg.Model` — the same process-wide default that `/models` writes. Changing the default therefore moved every override-less chat.
- `HandleChat` (`internal/server/handler.go:1292`) persisted `meta["model"]` only when the request named a model explicitly, so most chats had no pin.
- Clearing a model previously deleted the key, making it indistinguishable from a legacy chat that never had one — the ledger fallback would have silently restored the cleared value (`internal/usage/usage.go:386`).

Fix (three parts):
1. `HandleChat` (`internal/server/handler.go:1292`) now always writes `meta["model"] = model` at session creation, freezing a new chat to the default in effect then. `ReadMetadataForDir` (`internal/session/session.go:2157`) replaces `LoadForDir` on the hot status-poll path.
2. `effectiveSessionModel` (`internal/server/handler_session_state.go:617`) reads a 3-state metadata: key absent = legacy → consult usage ledger (`LastModelForSession`); key present-empty (`""`) = explicit "follow default"; value = pinned. Pulse sessions skip the ledger (`internal/usage/usage.go:386`).
3. `pinSessionModel` (`internal/server/handler_session_state.go:666`) writes the resolved model back to transcript metadata on the chat's next turn (`HandleChat`, `HandleSendMessage` incl. rewind, `executeTurnJob` for retries/auto-continue). `setSessionModelOverride` (`internal/server/handler_session_state.go:697`) writes the `""` clear sentinel. The web `/models` command is now scoped: `PUT /api/sessions/:id/model` for an existing chat; only a draft (`new-*`) or session-less tab changes the global default and resolves typed names against `GET /api/models` (`name` = provider-qualified id; `model` = bare name; `display_name` = label).

Invariants (do not break):
1. Never resolve an override-less chat from the process-wide default without first consulting the ledger (`internal/usage/usage.go:386`) — that is the reported bug.
2. Never pin over an EXISTING `model` key, including the `""` clear sentinel; `pinSessionModel` (`internal/server/handler_session_state.go:666`) re-checks inside the metadata write lock.
3. Never derive a model from the ledger for chats with `model=""` (explicit clear) or for Pulse sessions.
4. `effectiveSessionModel` (`internal/server/handler_session_state.go:617`) runs every status poll: keep the read metadata-only; no transcript load, no unconditional ledger rescan.
5. Ledger rows are only attributed by `recordTurnUsage` for a session's own turns; surfaces that record without a session id (e.g. TUI) cannot recover a model this way.

Tests: `internal/server/handler_session_model_test.go` (`TestHandleChatPinsNewSessionModelAtCreation`, `TestEffectiveSessionModelRecoversLegacyModel`, `TestClearSessionModelIgnoresLedgerModel`, `TestPinSessionModel`, updated `TestPerSessionModelOverrideIsolation`); `internal/usage/last_model_test.go`; `web/src/components/Chat/commands.models.test.tsx`. Verified at HEAD: `go build` + `go vet` + `gofmt -l` clean; targeted Go tests pass; `vite build` + `tsgo --noEmit` clean; affected web tests pass.

Live evidence: three chats (`ses_2026-10-25-001110-0cee49be`, `ses_2026-10-25-171757-e3cc7a66`, `ses_2026-10-26-154657-6cc1bc9a`) had NO `model` metadata but usage-ledger rows for `openrouter/nvidia/nemotron-3.5-super-vl:free`; with the process default on `opencode-go/deepseek-v4.1-flash` they resolved to the wrong model before the fix and to the ledger model after.
