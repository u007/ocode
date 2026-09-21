---
type: Gotcha
title: Remote SSH chat ignored the desktop active profile — proxy silent on window→profile mapping
description: A desktop profile with a custom key (e.g. opencode-go) applied to local chats but ignored on remote SSH projects — the remote kept using the base key despite the profile's key being synced to the host.
tags:
  - gotcha
  - profile
  - remote
  - ssh
  - auth
  - proxy
  - session
timestamp: 2026-09-20T18:07:46Z
---
# Remote SSH chat ignored the desktop active profile

Fixed 2026-09-21. Symptom: selecting a desktop profile with a custom key (e.g. opencode-go) had no effect on chat in a remote SSH project — it kept using the base key.

## Root cause: three missing links

Remote chat runs on the host's `ocode serve --remote` process (see `docs/superpowers/plans/2026-09-17-remote-project-agent-on-host/`, commit 76e0e015) — not the local server. Three things had to align and didn't:

1. **Credential sync is window-profile-blind.** `BuildSyncPayload()` (`internal/remote/sync.go`) pushes `auth.profiles.json` + `opencode.json` + `ocodeconfig.json` to the host at connect, but NOT `window-state.json`. So the remote's `Handler.windowProfiles` map (hydrated from its own nonexistent `window-state.json`, `internal/server/handler.go` ~464) was empty on arrival.
2. **Remote launches without `OCODE_PROFILE`** (`internal/server/serve.go` `launchServerCmd`) — base credential is the default regardless of local profiles.
3. **ProfileSwitcher only PUTs the LOCAL window profile** (`web/src/components/ProfileSwitcher.tsx`) — nothing informed the remote which profile a proxied window was bound to.

So `resolveSessionProfile` (`internal/server/agent_session.go`, order `OCODE_PROFILE` > window profile > global fallback) fell through to the base key, even though the profile's key had been synced to the host.

## Fix: proxy stamps the authoritative profile

Local proxy `HandleRemoteProxy` (`internal/server/handler_remote_proxy.go`) reads the request window id (`X-Window-Id` header → `windowId`/`window_id` query) and calls `injectProxiedActiveProfile`, which strips any client-supplied profile headers (a bare header is ignored — no forge) then stamps `X-Ocode-Active-Profile: <profile>` + `X-Ocode-Profile-Authoritative: 1`, or `X-Ocode-Profile-Reset: 1` for Default / no-profile windows.

New `Handler.applyProxiedActiveProfile` (`internal/server/handler_profiles.go`) is called from `HandleChat` and `HandleSendMessage` right after window-id normalization, updating `h.windowProfiles` in memory (delete on reset). Only the authoritative marker is trusted; nothing is persisted on the remote. `reconcileProfileAgent` rebuilds the agent on the new profile's key at the next turn boundary.

## Diagnostic technique

If a remote turn uses base keys, check (1) that the credential sync happened (`BuildSyncPayload`) and (2) that the window→profile mapping exists on the host — it won't, unless the proxy stamped it. `resolveSessionProfile` order is `OCODE_PROFILE` > window profile > global fallback.

## Stale-doc warning (corrected)

Comments on `internal/server/agent_session.go`'s `projectHostFor`/`SetProjectHost` used to claim the remote project's chat agent runs on the **LOCAL** server (only terminal/files/git forwarded). That is **wrong** — remote chat is proxied to the host. Ground truth: `web/src/hooks/useChat.ts` passes `projectHost` to `api.chat`/`api.sendMessage`, and `fetchJSON` prefixes `/api/remote/{host}`. Do not trust old comments asserting local execution.

## Tests

`internal/server/handler_remote_profile_test.go` — apply matrix (incl. bare-header ignored + reset clears), credential rebuild base→profile key, `HandleSendMessage` wiring, proxy forwarding + forged-header stripping. All mutation-verified by temporary revert.

## Files changed

- `internal/server/handler_remote_proxy.go` — `injectProxiedActiveProfile`; stamps authoritative profile headers from the request window id
- `internal/server/handler_profiles.go` — new `Handler.applyProxiedActiveProfile`; wired into `HandleChat`/`HandleSendMessage`
- `internal/server/handler.go` — `windowProfiles` hydration context (~464)
- `internal/server/agent_session.go` — `resolveSessionProfile` fallthrough documented; stale `projectHostFor`/`SetProjectHost` comments corrected
- `internal/remote/sync.go` — sync scope noted (`window-state.json` deliberately excluded; profile state is host-stamped via proxy)
- `internal/server/handler_remote_profile_test.go` — new test file
- `web/src/components/ProfileSwitcher.tsx` — context only: PUTs local window profile (unchanged; remote learns via proxy headers)
- `web/src/api/client.ts` — context only: request host threading
