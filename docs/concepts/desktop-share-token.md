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
  - wails
  - persistence
  - security
timestamp: 2026-10-06T12:26:58Z
---
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
  - wails
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

`checkAuth` (`internal/server/share_token.go:80`) is the single gate: it runs
**before** any route handler, so a request with neither credential is rejected
regardless of path. The share-token branch is reached only after the launch-token
comparison fails, and only when `shareToken != ""` — an empty share token can never
match, which is what makes "no share token installed" equivalent to "no second
credential".

`LoadOrCreate` (`internal/desktop/share_token.go:62`) reads the persisted token at
boot. The file is `0600`, written once, and only replaced by an explicit reset —
never by a relaunch. This is what makes a shared URL survive a restart: the token
on disk is the same one that was embedded in the link.

## 2. Reset = rotate, not revoke-in-place

The reset flow is a **rotation**, not a deletion:

1. SPA dispatches `ocode:reset-share-token` (Share menu → "Reset Share Token…").
2. `ShareDialog` (`web/src/components/Layout/ShareDialog.tsx:267`) **arms an inline
   confirmation** (`share-dialog-reset-confirm`) — it never rotates on its own.
3. On confirm, SPA POSTs `/api/desktop/share-token/reset`.
4. `RotateShareToken` (`internal/server/share_token.go:44`) generates a fresh
   token, installs it in memory, and persists it to disk.
5. The SPA updates its displayed URL with the new token; the old token stops
   authenticating **immediately** (the in-memory value is already replaced).

The old token is not "deleted" — it simply no longer matches. There is no
revocation list; the single in-memory value is the only state. This is why a
rotated token stops working at once: `checkAuth` compares against the new value,
and the old one fails the byte-equality check.

## 3. Auth scoping: what the share token does NOT do

- **It is not a file-system capability.** The token authenticates HTTP requests to
  the ocode server API. It cannot read or write files on the host except through
  the API's own tools (which have their own permission gates).
- **It is not a tailscale credential.** The share token is embedded in the URL's
  query string; tailscale's own auth (if enabled) is a separate layer.
- **It is not scoped to a single session.** A share-token holder can access every
  session, project, file, terminal, and setting the server exposes — the dialog
  says so explicitly ("Share Entire Desktop"). This is a deliberate product
  decision, not an oversight: the use case is "let another device control this
  desktop", not "share one conversation".

## 4. Persistence and the desktop-only gate

The share token exists **only in the desktop shell**. A plain `ocode serve` (headless
or TUI) never calls `SetShareToken`, so `shareToken == ""` and the second branch
of `tokenMatches` is inert. The desktop shell calls it during `StartServer`
(`internal/desktop/boot.go:150`), after the server is constructed but before it
starts listening.

The token file lives at `~/.config/opencode/desktop-share-token` (or
`$XDG_CONFIG_HOME/opencode/desktop-share-token` on Linux). The directory is the
same one that stores `desktop-port` and `desktop-debug-handle` — all three are
desktop-shell state that must survive a restart.

`LoadOrCreate` is called once at boot. If the file is missing (first run), it
generates a token and writes it. If the file is corrupt or unreadable, it logs a
warning and generates a fresh token — a malformed token file never prevents the
server from starting.

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

## Share dialog UI: status + start/stop (2026-10-06)

The Share dialog (`ShareDialog.tsx`) now shows a **Running/Not-sharing badge** with
the exposure kind (`funnel` = public internet, `serve` = tailnet-only), and
**Start/Stop** buttons. Opening the dialog only READS status
(`loadShareState` at `ShareDialog.tsx:117`); it never starts an exposure. Only the
explicit "Copy Desktop URL" menu action starts a share when stopped. The old
`GET /api/tailscale-url` endpoint (which started a funnel-first exposure on read)
has been replaced by `GET /api/tailscale-share` (side-effect-free status),
`POST /api/tailscale-share/start`, and `POST /api/tailscale-share/stop`. See
`concepts/auto-share-on-start.md` for the full endpoint documentation.

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
