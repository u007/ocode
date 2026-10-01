---
type: Concept
title: Desktop durable share token (two-credential model)
description: 'Desktop durable share token: two-credential model, revocable share URLs, auth scoping, persistence.'
tags:
  - desktop
  - share
  - auth
  - token
  - share-dialog
  - persistence
  - security
timestamp: 2026-10-01T08:26:05Z
---
# Desktop durable share token (two-credential model, revocable links)

**Type:** Concept  
**Description:** Why the desktop share URL carries a SECOND, persisted credential instead of the per-launch webview token — restart-proof links, immediate revocation, auth scoping, and the honest security posture.  
**Tags:** desktop, share, auth, token, share-dialog, wails, persistence, security  

---

# Desktop durable share token (2026-10-01)

The desktop app mints a **fresh random auth token on every launch** (the "launch
token": `internal/desktop/boot.go:74` `StartServer` → `hex.EncodeToString` over 16
random bytes, handed to `server.New` as the server password). The Share dialog's
"Share Entire Desktop" URL embeds that token as `?token=…`, and the *port* half of
the URL was already made sticky earlier (`desktop-port`), so before this change any
URL already handed to another device **401'd the moment ocode restarted**: new
launch token, stale link. The fix persists the *credential* half too — but as a
**second** credential, never as the webview's own one.

## 1. Two credentials, deliberately — the share token is never the launch token

| | Launch token | Share token |
|---|---|---|
| Minted | every `StartServer` run | once, persisted (`desktop-share-token`), rotated on demand |
| Held by | the webview SPA (module load) | other devices, inside shared URLs |
| Rotating it | n/a (dies with the process) | revokes every shared link |
| Checked in `checkAuth` | yes — the `s.password` side of `tokenMatches` | yes — the second branch, when installed |

`Server.SetShareToken` (`internal/server/share_token.go:22`) installs an **optional
second credential** that `checkAuth` accepts alongside the launch token;
`server.go:113` documents the guard `shareTokenMu`. `tokenMatches`
(`internal/server/share_token.go:62`) is the one comparison point: launch token by
plain byte equality (unchanged behaviour), share token only when non-empty — so a
server with no share token installed can never authenticate an empty string
(`TestEmptyShareTokenDoesNotAuthenticateEmptyToken`).

**Why it must be a second credential (the load-bearing design choice):**

- **Immediate revocation without self-logout.** The local window authenticates with
  the launch token, not the share token. `RotateShareToken`
  (`internal/server/share_token.go:44`) therefore makes every outstanding link stop
  authenticating *at once* while the local webview keeps its session — no reload,
  no forced re-login.
- **Persisting the launch token instead would have made revocation wait for a
  restart.** The SPA caches `_token` **at module load**
  (`web/src/api/client.ts:339`), so a webview running with token A cannot accept a
  server that was just handed token B: every API call 401s until the webview
  reloads. A second credential sidesteps this entirely.
- **The token generator stays in exactly one place.** `ShareTokenStore`
  (`internal/desktop/share_token.go:41`) holds only `rotate`/`live`/`set` function
  references into the server plus the file path: the server generates and holds the
  live value, the store is the single disk writer. `RotateShareToken`'s doc comment
  explicitly refuses to do persistence — the server has no config directory.

## 2. The stale-token trap (SPA must never display a revoked URL)

A rotated share token stops authenticating **immediately**, so any URL the SPA
still shows carrying the old token is a dead link that looks alive. Two rules
follow:

- **The GET endpoint serves the LIVE in-memory value, never the file**
  (`TokenHandler`, `internal/desktop/share_token.go:136`) — "the SPA must never be
  handed a token the running server would reject."
- **On a failed reset, keep the previous token.** `ResetHandler`
  (`internal/desktop/share_token.go:147`) logs and returns 500 *without* replacing
  what the SPA knows; `handleResetToken`
  (`web/src/components/Layout/ShareDialog.tsx:230`) on `!r.ok || !token` sets
  `share-dialog-reset-error` (`ShareDialog.tsx:420`) — *"Reset failed (HTTP …). The
  previous link still works"* — and leaves `shareToken` untouched, so the displayed
  URL stays the one that actually authenticates. On success it swaps the token,
  rewrites every share URL, and clears the "Copied" flag (`setCopied(false)`) so a
  stale "Copied" can't invite re-sharing the revoked link.

The native **Share ▸ Reset Share Token…** menu item (`cmd/ocode-desktop/main.go:825`)
dispatches the DOM event `ocode:reset-share-token`; the dialog's listener
(`ShareDialog.tsx:267`) only **arms the inline confirm**
(`share-dialog-reset-confirm`, `ShareDialog.tsx:381`) — it never rotates on its own,
so the menu item can never be a silent revocation. The comment above the Share menu
(`cmd/ocode-desktop/main.go:804`) states the durable-token model.

## 3. `HandleAuthedDesktopRoute` vs `HandleDesktopRoute`

- `HandleDesktopRoute` (`internal/server/server.go:1648`) is **deliberately
  unauthenticated** — it exists for the one-time storage-migration call
  (`boot.go:136`, which legitimately carries the launch token in the migration
  payload).
- `HandleAuthedDesktopRoute` (`internal/server/share_token.go:80`) mounts behind the
  same `authMiddleware` every `/api` route uses. **The listener binds `0.0.0.0`**
  (`boot.go:86`) precisely so LAN/tailscale share URLs can connect — which means an
  unauthenticated desktop-mounted route is reachable from off-host. Any route that
  **reads or changes a credential** therefore MUST use the authed variant: an
  unauthenticated `GET /api/desktop/share-token` would hand the share token to any
  machine on the network, and an unauthenticated reset would let anyone revoke the
  user's links (a denial of service against every device already sharing).
  Both `ShareTokenPath` and `ShareTokenResetPath`
  (`internal/desktop/share_token.go:21`, `:22`) are mounted authed
  (`boot.go:155`, `boot.go:156`); `TestHandleAuthedDesktopRouteRequiresACredential`
  pins the 401.

**Which `checkAuth` branches accept the share token** (`internal/server/server.go:683`):

| Path | Accepts share token? | Where |
|---|---|---|
| `Authorization: Bearer` | **yes** | `server.go:686` → `tokenMatches` |
| `?token=` query (EventSource; forbidden in `--remote`) | **yes** | `server.go:693` → `tokenMatches` |
| `Sec-WebSocket-Protocol` bearer (remote mode only) | **no** — launch token only | `server.go:705` |
| HTTP Basic | **no** — launch token only | `server.go:714` |

The two "no" rows are deliberate, with comments at `server.go:701`: a remote session
never carries a durable share token (the SPA refuses to share there), so accepting
one on the WS path "would let a share link outlive the launch model it was minted
for"; Basic is launch-only because the remote token model treats a second Basic
path as a leak.

## 4. Register AFTER the saved-port fallback in `StartServer`

`StartServer` (`internal/desktop/boot.go:74`) may replace `srv` **wholesale** when
the saved port range is exhausted: the fallback does
`srv = server.New("0.0.0.0:0", …)` (`boot.go:102`). Anything mounted on the earlier
`srv` is silently dropped. The share-token block (`boot.go:150`) is therefore placed
**after** the fallback (and after `saveDebugHandle`/migration at `boot.go:133`,
`boot.go:136`), so it always lands on the srv that actually gets served. Same reason
the block is a single `if/else if/else`: store creation failure, `LoadOrCreate`
failure (no writable config dir) and success are handled there, with failure
logged as `durable share token disabled` — best-effort boot, never a boot failure;
the SPA then falls back to `authToken()` (§5).

## 5. Scope boundaries: where the share token does NOT exist

- **Plain `ocode serve` / TUI:** `SetShareToken` is never called, `shareToken == ""`,
  `tokenMatches` behaves exactly as before. The desktop-only routes are not mounted
  → `GET /api/desktop/share-token` 404s, and the SPA's fetch in
  `resolveShareInfo` (`ShareDialog.tsx:159`, gated on `isDesktopShell()`) leaves
  `token = ""`, so `tokenSuffix = tokenQuery(shareToken || authToken())`
  (`ShareDialog.tsx:192`) uses the launch token — **prior behaviour**, including
  "expires on next restart", and no reset button is offered (`shareToken` falsy).
- **Remote-workspace desktop sessions:** `StartServer` returns into
  `startRemoteServer` before any of this code runs (`boot.go:82`), so no store, no
  routes, no token.
- **Remote (SSH) sessions in the SPA:** the Share dialog refuses entirely
  (`ShareDialog.tsx:184` resolve gate; `ShareDialog.tsx:324` renders
  `share-dialog-remote-unavailable`), and the `ocode:copy-desktop-url` handler
  early-returns (`ShareDialog.tsx:276`). Test: *"keeps refusing to share in a remote
  session even with a durable token."*

## 6. Security posture (stated honestly)

- **The token is plaintext at rest** in `~/.config/opencode/desktop-share-token`,
  written `0600` (`ShareTokenStore.write`, `internal/desktop/share_token.go:112`)
  next to `desktop-port` — the path reuses `portFilePath()`
  (`internal/desktop/share_token.go:51`, defined `boot.go:191`) so the config dir
  has a single source of truth. This is **the same exposure class as the
  pre-existing `desktop-debug-handle` file** (`saveDebugHandle`, `boot.go:133`),
  which already stores the launch token in plaintext — except the share token is an
  **intentional, revocable** credential rather than debug plumbing.
- **Rotating is not an escalation.** Anyone who can call the reset endpoint already
  presented a valid credential, and a share-token holder already has *full desktop
  control* (the dialog says so: all sessions, projects, files, terminals, settings).
  Letting a share-token holder rotate the share token therefore grants no capability
  it did not already have — and `handleResetToken` runs `authedFetch` with whatever
  credential the SPA holds.
- **Scope of a leak is bounded by design:** the token authenticates API calls only
  through `checkAuth`; it is never a file-system capability on its own.

## 7. Validation format: regenerate rather than break a working link

`validShareToken` (`internal/desktop/share_token.go:126`) requires **exactly
`ShareTokenBytes*2` hex chars** (= `16*2` = 32, `internal/server/share_token.go:12`).
Validation happens **on read**: a truncated write, a partial file, or a stray
hand-edit yields `""` from `LoadOrCreate`'s parse, which logs `ignoring malformed
share token file` and **generates a fresh token** (`internal/desktop/share_token.go:72`)
— every failure mode is recoverable. The alternative — installing whatever bytes are
on disk — would replace a working link with a token nobody can authenticate as.

## Reset + rotate invariants (both tested)

- `Reset` (`internal/desktop/share_token.go:99`) = rotate in memory → persist →
  return; **the disk write happens before the token is handed to anyone**, so a
  crash can't leave memory and disk disagreeing about which link is live. On write
  failure the old token stays live and persisted (500 to the SPA, §2).
- `RotateShareToken` (`internal/server/share_token.go:44`) = generate + install,
  returns the new value; previous value stops authenticating immediately.

## Tests

- `internal/server/share_token_test.go` — `TestShareTokenIsAcceptedAsASecondCredential`,
  `TestEmptyShareTokenDoesNotAuthenticateEmptyToken`,
  `TestRotateShareTokenReplacesThePreviousValue`,
  `TestHandleAuthedDesktopRouteRequiresACredential`.
- `internal/desktop/share_token_test.go` —
  `TestShareTokenPersistsAcrossLaunches`, `TestShareTokenFileIsOwnerOnly`,
  `TestShareTokenStoreRegeneratesMalformedFile`,
  `TestShareTokenResetRotatesRevokesAndPersists`,
  **`TestStartServerWiresDurableShareToken`** (full boot → relaunch → reset
  lifecycle: same token after relaunch, revoked token 401s, launch token still 200
  after reset, unauthenticated GET is 401, share token ≠ launch token), `TestValidShareToken`.
- `web/src/components/Layout/ShareDialog.test.tsx` — *"uses the durable share token
  in place of the launch token in the desktop shell"*, *"falls back to the launch
  token and offers no reset when the desktop has no durable token"*, *"offers no
  reset outside the desktop shell"*, *"resets the share token and rewrites the
  displayed URL"*, *"cancelling the confirmation leaves the token alone"*,
  *"keeps the previous link and explains the failure when the reset does not land"*,
  *"arms the confirmation from the Share menu event without revoking anything"*,
  *"keeps refusing to share in a remote session even with a durable token"*.

## Related

- Sticky port (the other half of a durable share URL, and the origin of the
  "register after the fallback" rule): `docs/gotchas/desktop-quit-guard-and-sticky-port-fallback.md`.
- `CHANGES.md` (2026-10-01 entries).
