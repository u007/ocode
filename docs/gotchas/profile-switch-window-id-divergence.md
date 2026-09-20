---
type: Gotcha
title: Profile switch does not affect an already-open chat session (window-id divergence)
description: 'Selecting a profile in the ProfileSwitcher did not reflect on an already-open chat session — the session kept using base credentials. Root cause: the window id was re-derived independently per API call after SPA navigation stripped the URL param, so the pill targeted `main` while chat requests minted a fresh random window id the server had no profile bound to.'
resource: web/src/lib/windowId.ts; web/src/components/ProfileSwitcher.tsx; web/src/api/client.ts; internal/server/agent_session.go; internal/server/handler_profiles.go
tags:
  - gotcha
  - profile
  - window-id
  - session
  - frontend
  - server
  - auth
timestamp: 2026-09-20T10:47:26Z
---
**Type:** Gotcha  
**Description:** Selecting a profile in the ProfileSwitcher did not reflect on an already-open chat session — the session kept using base credentials/API keys. Root cause: the window id was re-derived independently per API call after SPA navigation stripped the URL param, so the pill targeted `main` while chat requests minted a fresh random window id the server had no profile bound to.  
**Resource:** web/src/lib/windowId.ts; web/src/components/ProfileSwitcher.tsx; web/src/api/client.ts; internal/server/agent_session.go; internal/server/handler_profiles.go  
**Tags:** gotcha, profile, window-id, session, frontend, server, auth  

---

Fixed 2026-09-20. Symptom: selecting a profile in the web/desktop top-right ProfileSwitcher had no effect on an already-open chat session — the session kept using the base credentials/API keys.

## Root cause: frontend window-id divergence

The profile state is keyed by a **per-window id**. Three independent code paths each derived the window id their own way, and they diverged after a navigation that dropped the URL query string:

1. **ProfileSwitcher** (`web/src/components/ProfileSwitcher.tsx`) resolved `windowId` once at module load via an inline `getWindowId()` call. When the id came from the `?windowId=main` URL param (the desktop shell threads this in via `cmd/ocode-desktop/main.go:312`/`:314`), it was **not persisted to sessionStorage**.
2. **API client** (`web/src/api/client.ts`) `sendMessage`/`chat` independently re-derived the window id per call: `?windowId=` → sessionStorage → else mint a random `win-xxxx`.
3. **Desktop deep-link redirect** (`web/src/pages/SessionPage.tsx:22`) does `navigate("/", {replace:true})` which **strips the query string**.

So after the redirect: the pill kept targeting `main`, but subsequent chat requests minted a fresh random id like `win-3b33dd72`. Server-side, `HandleSendMessage`/`HandleChat` bind the session to that new window id (`h.sessions.SetWindowID`), and `reconcileProfileAgent` (`internal/server/agent_session.go:430`) only rebuilds the agent on the window's active profile when `entry.WindowID != ""` — so the session stayed on a profile-less window and never picked up the new profile's credentials.

## Why the server never fixed it

- `handleSetWindowActiveProfile` (`internal/server/handler_profiles.go`, PUT `/api/window/{id}/activeProfile`) only **records** the profile and emits `profile.windowChanged` — it does **not** rebuild agents.
- The switch lands on the **next turn** via `reconcileProfileAgent` (`internal/server/agent_session.go:430`), which compares `resolveSessionProfile(entry)` (env `OCODE_PROFILE` > window profile > global fallback) + `auth.ProfileCredentialVersion()` + model against the built agent. It is a **no-op mid-turn**.
- `buildAgentSession` builds `agent.NewClientWithProfile(effCfg, model, prof)`; profile credentials come from `~/.local/share/ocode/auth.profiles.json` via `auth.GetProfileCredential(profile, provider)`. A keys-only profile (empty delta `{}`) is valid and **does** swap the key on reconcile — but only if the session is on the right window.
- Note: `effectiveSessionModel` (`handler_session_state.go`) ignores the window profile's `model` delta — a profile's model override does **not** currently change the session's model. That is a separate issue and was **not** changed in this fix.

## Fix shipped

New `web/src/lib/windowId.ts` — a single source of truth `getWindowId()` that **persists a URL-derived id to sessionStorage** (key `ocode.windowId`) before returning it. Callers updated:

- `ProfileSwitcher.tsx` — uses shared helper (no inline `getWindowId()`)
- `api.sendMessage` / `api.chat` — use shared helper (no per-call re-derivation)
- `lib/debug/frontendMemoryReporter.tsx` — uses shared helper

## Rule

**The per-window id must be resolved through one shared helper that persists the URL-derived value. Never re-derive it independently at a call site**, because any SPA navigation that drops the query string silently rebinds subsequent requests to a different window.

## Diagnostic technique that cracked it

A test that resolves the pill's window id, then strips the query string, then asserts the chat request's `X-Window-Id`/body `windowId` equals the pill's. It failed with:

```
expected 'win-3b33dd72' to be 'main'
```

## Server-side verification

`internal/server/agent_session_profile_cred_test.go` proves the reconcile path applies a credentials-only profile via the real PUT handler.

## Test-state trap

Any test calling `auth.Set`/`SetProfileCredential` bumps `auth.ProfileCredentialVersion()`, which makes hand-built `agentSession{credVersion: 0}` literals in sibling server tests look stale and trigger a rebuild. Fix: use `auth.SetProfileCredentialVersionForTest` to restore it.

## Files changed

- `web/src/lib/windowId.ts` — new shared helper (persists URL-derived id to sessionStorage)
- `web/src/components/ProfileSwitcher.tsx` — use shared `getWindowId()`
- `web/src/api/client.ts` — `sendMessage`/`chat` use shared `getWindowId()`
- `web/src/lib/debug/frontendMemoryReporter.tsx` — use shared `getWindowId()`
- `internal/server/agent_session_profile_cred_test.go` — new test proving reconcile applies credentials-only profile
