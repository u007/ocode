# ocode Mobile Companion — Design

- **Date:** 2026-11-16
- **Status:** Draft for review
- **Scope:** Mobile-first companion for controlling ocode sessions from a phone
- **Platform:** Android first; iOS deferred
- **Related:** `docs/superpowers/specs/2026-11-08-offline-capable-web-client-design.md`,
  `docs/concepts/web-server-project-scoping.md`,
  `docs/concepts/web-terminal-ghostty-web.md`

## 1. Summary

Build a **mobile companion**, not a phone-sized ocode desktop.

The phone is a remote control for sessions that run on an ocode server. It lists
sessions, shows the live transcript, sends messages, stops turns, and answers
permission and question asks. It does not run the agent, bash, git, LSP, MCP, or
the desktop webview.

Delivery has four pieces:

1. A mobile-first web surface served at the root of a dedicated companion
   listener on the existing ocode server.
2. A `--companion` listener that serves the mobile surface, requires durable
   device auth, and is safe to expose through a user-operated HTTPS tunnel.
3. A thin Wails v3 Android shell that opens the companion URL and adds setup,
   deep links, and background push.
4. FCM push for permission asks and questions.

This is deliberately **not** a hosted relay in v1. "From anywhere" means the
user's server is reachable over HTTPS through a tunnel or reverse proxy they
operate. A zero-config ocode-hosted relay is a separate product with its own
backend, identity, and cost model.

## 2. Goals

- Let the user see running and waiting sessions from a phone.
- Let the user watch live output with no manual refresh.
- Let the user answer a permission ask or a question from the phone.
- Let the user send a message, stop a turn, retry, and queue a message.
- Notify the user when a session needs attention while the app is closed.
- Make pairing and revocation understandable without editing config files.
- Keep the desktop and TUI unchanged in behavior.

## 3. Non-goals

- Running the ocode agent on Android.
- A phone file tree, code editor, terminal, git UI, MCP manager, browser, voice,
  or artifact manager.
- Hosted relay, ocode account system, team sharing, or multi-tenant hosting.
- iOS in v1.
- Web Push or a Trusted Web Activity as the primary packaging.
- Changing the desktop SPA layout or the TUI.

## 4. Decisions

| Decision | Choice | Why |
|---|---|---|
| Product shape | Remote companion client | The agent needs a desktop/POSIX host. Android cannot supply bash, git, PTY, LSP, or MCP servers. |
| Mobile surface | Separate mobile entry, not a responsive desktop SPA | The desktop UI is dense and would force desktop-only code into the phone. A separate entry keeps both surfaces focused. |
| Server origin | Mobile entry at the companion listener root | Avoids CORS changes and avoids putting the bearer token in a URL or cross-origin storage. |
| Auth | One-time pairing code → durable device cookie | The desktop's per-launch token rotates and is unsuitable for a phone. A device credential is revocable and survives restarts. |
| Device privilege | Scoped companion allowlist | A stolen phone credential must not reach files, git, terminal, config, or MCP. |
| Android shell | Wails v3 thin shell | The user asked for Wails, and it gives a Go-owned native shell plus a WebView. |
| Push | FCM from the ocode server | Wails v3.1.0 has no Android notification backend; background delivery needs a native Android service. |
| Reachability v1 | User-operated HTTPS tunnel | Reuses the existing server and auth. A hosted relay is a larger, separate system. |
| Desktop/CLI impact | Additive | Existing launch-token, remote-SSH, and desktop flows keep working. |

## 5. Alternatives considered

### 5.1 Make the existing desktop SPA responsive

Rejected. The SPA assumes a mouse, wide panes, a file tree, a terminal, and
desktop-only components. Hiding them on small screens would leave a compromised
desktop UI and a large regression surface. A mobile-first entry is smaller and
clearer.

### 5.2 Put the mobile UI in the Wails app and proxy to the server

Rejected for v1. A local proxy inside the Android app would need a loopback
listener, cleartext or local TLS handling, and a second origin policy. Serving
the mobile entry from the companion listener keeps one origin, one cookie, and
one SSE stream. The shell still owns setup and push.

### 5.3 Use the existing `--remote` SSH workspace flow

Rejected. `internal/remote` is an SSH workspace client with host keys, a remote
agent server, and a reverse proxy. It assumes a desktop peer and does not solve
phone pairing, app distribution, or push.

### 5.4 Use Tailscale or another VPN as the only transport

Partially accepted. Tailscale is a supported tunnel recipe, not a hard
requirement. The app accepts any HTTPS base URL. This keeps the door open for
Cloudflare Tunnel, Caddy, or a future relay without changing the client.

### 5.5 Use a Trusted Web Activity and Web Push instead of Wails

Deferred. A TWA plus Web Push would avoid custom FCM code and is the natural
fallback if Google Play rejects a WebView-centric app. It is not the primary
because the user asked for Wails, and a Wails shell leaves room for native
setup, deep links, and future Android integrations.

## 6. Architecture

```
┌──────────────────────────┐        HTTPS (public)        ┌─────────────────────────┐
│ Android phone            │  ──────────────────────────► │ User-operated tunnel    │
│                          │                              │ Tailscale Funnel /      │
│ Wails v3 shell           │                              │ Cloudflare Tunnel /     │
│  ├─ setup page (local)   │                              │ reverse proxy           │
│  ├─ WebView → companion  │                              └────────────┬────────────┘
│  └─ FCM service          │                                           │ loopback
└──────────┬───────────────┘                                           ▼
           │ FCM registration                             ┌─────────────────────────┐
           │                                              │ ocode process           │
           ▼                                              │  companion listener     │
┌──────────────────────────┐        outbound HTTPS        │   ├─ mobile SPA at /    │
│ Firebase Cloud Messaging │ ◄─────────────────────────── │   ├─ scoped /api        │
└──────────────────────────┘        (HTTP v1)              │   ├─ pair / push        │
                                                           │  main listener          │
                                                           │   ├─ desktop SPA        │
                                                           │   └─ full API           │
                                                           │  shared sessions/store  │
                                                           └─────────────────────────┘
```

### 6.1 Components

| Component | Responsibility | Depends on |
|---|---|---|
| Mobile web entry | Sessions, transcript, asks, composer, settings | Existing ocode API, SSE, shared types |
| Companion listener | Serve the mobile entry, device auth, scoped routes, push dispatch | `internal/server`, `internal/paths`, `internal/auth` style stores |
| Companion device store | Pairing codes, device tokens, push tokens, revocation | `GlobalDataDir`, file locking, atomic writes |
| Wails Android shell | Setup, server list, WebView, deep links, FCM | Wails v3 Android template, Firebase SDK |
| Push sender | Send FCM HTTP v1 messages for asks and questions | FCM service account, device store |

### 6.2 Request and stream flow

1. The app opens the companion listener's root URL.
2. The mobile page authenticates through the device cookie.
3. The page loads sessions through the existing JSON API.
4. The page opens the existing `/api/events` SSE stream with the same cookie.
5. The user answers an ask through the existing resolve/answer endpoint.
6. The server writes the answer through the same path a desktop client uses.
7. When an ask is created and the device is registered, the server sends an
   FCM data message.
8. Tapping the notification opens `/session/<id>?ask=<id>` on that origin.

The mobile surface never receives a second, parallel agent path. It is another
client of the same server.

## 7. Mobile web surface

### 7.1 Build shape

- Add `web/mobile.html` and `web/src/mobile/` as a second Vite entry. Add it to
  `build.rollupOptions.input` in `web/vite.config.ts`; Vite does not pick up a
  second root HTML file automatically.
- Reuse `web/src/api/client.ts`, `web/src/api/types.ts`, markdown rendering, and
  the existing SSE parser (`readSSEStream`).
- Do not import the desktop `App`, sidebar, terminal, editor, git, or browser
  components.
- The companion listener serves `mobile.html` as its `/` document. Assets stay
  at `/assets/*` on the same origin.
- `mobile.html` uses `<base href="/">` so the shared `/assets/*` resolve at
  the companion origin root.
- Build output adds `mobile.html` plus hashed assets. `web/embed.go` already
  embeds all of `dist/`.

### 7.2 Screens

| Screen | Contents |
|---|---|
| Servers | Saved server URLs, connection status, add server, sign out |
| Sessions | All sessions grouped by project; filters All / Waiting / Running; unread and running badges |
| Session | Transcript, live streaming state, permission ask, question card, composer, Stop, Retry, Queue |
| Settings | Notification toggles, device name, server version, revoke this device |

### 7.3 Mobile behavior

- Bottom navigation: Sessions and Settings. A session is a full-screen route.
- Large tap targets, safe-area insets, keyboard-aware composer, no hover-only
  actions.
- The transcript renders user, assistant, thinking, and tool-result blocks in a
  compact form. Tool arguments are collapsed by default.
- Permission and question cards are pinned above the composer until resolved.
- A "Waiting" filter is the default when at least one ask is pending.
- Write actions are disabled with a banner when the server is unreachable. The
  last session list and last transcript snapshot remain readable from
  IndexedDB.

### 7.4 Offline policy

- The mobile entry does **not** register the existing desktop service worker in
  v1. The native shell's setup page is local; the session UI is network-first.
- The mobile app writes the last session summary and transcript to IndexedDB,
  scoped by server origin and session id.
- Offline reads are read-only. Sending, stopping, and answering show a clear
  "server unreachable" error and keep the draft.
- A companion-scoped service worker can be added later without changing the
  API or the native shell. It is not required for the first working version.

## 8. Companion server mode

### 8.1 A second listener, not a replacement server

Add a **companion listener**. It is a second `http.Server` in the same ocode
process, backed by the same `Server` and the same session state.

- `ocode serve --companion` starts it. The desktop app can also enable it from
  Settings → Mobile.
- In the desktop app, Settings → Mobile shows the companion port, lets the user
  record the public URL, and issues a pairing code. On a headless host,
  `ocode companion pair` prints the code.
- It binds `127.0.0.1:<companion-port>` only. The default port is 4097 and it
  is configurable. The public TLS endpoint is the tunnel's job.
- It serves only the mobile entry at `/`, `/api/health`, the scoped companion
  API, and the companion pairing/push endpoints.
- It never serves the desktop `index.html`, the full desktop API, LAN sharing,
  Tailscale share, or the desktop local-cert flow.
- It accepts device credentials, pairing codes, and login tickets only. The
  launch token is not valid there.
- The main listener keeps the desktop SPA, the full API, and the launch token.
  The tunnel is pointed at the companion port, never at the main port.

This separation is load-bearing: exposing the main listener through a tunnel
would put the desktop UI and the full API behind a single rotating token. The
companion listener is the only public surface, and it is scoped by design.

The mode is additive. `--remote` keeps its current SSH semantics.

### 8.2 Route serving

`internal/server/web.go` currently serves one SPA entry on the main listener.
The companion listener gets its own mux:

- `/` and `/session/*` serve `mobile.html`.
- `/assets/*` serves the shared hashed build assets.
- `/api/health` is unauthenticated and returns only the version and
  `companion_api: 1`.
- `/api/companion/pair` accepts a pairing code.
- `/api/companion/session-ticket` mints a short-lived WebView login ticket for
  an already-paired device.
- `/api/companion/session` exchanges a login ticket for the device cookie.
- `/api/companion/push` registers or removes an FCM token.
- `DELETE /api/companion/device` revokes only the calling device and clears the
  cookie.
- Scoped `/api/*` routes are served as listed in 8.3.
- Everything else returns a JSON 404 or 403. There is no desktop SPA fallback.
- Cache headers are unchanged: hashed `/assets/*` immutable, documents
  no-cache.

### 8.3 Scope enforcement

On the companion listener, a device principal is allowed only these areas:

- `GET /api/health` (also reports the server version)
- `GET /api/events`
- `GET /api/sessions`, `GET /api/sessions/{id}`, `GET /api/sessions/{id}/state`,
  `GET /api/sessions/{id}/status`
- `POST /api/sessions/{id}/message`, `/cancel`, `/retry`, `/queue`
- `POST /api/permissions/resolve`, `POST /api/questions`,
  `POST /api/questions/cancel`
- `GET /api/projects` and `GET /api/projects/current` (read-only labels)
- Companion push registration and sign-out

Everything else returns `403` for a device principal, including files, git,
terminal, config, models, MCP, artifacts, browser, and desktop control. The
allowlist lives in one function so a new route cannot silently widen phone
access.

The companion listener's `/api/events` stream is filtered to the event families
the mobile UI consumes: session updates, messages, turns, permission asks,
questions, and run state. Artifact, file, git, terminal, browser, and config
events are dropped before they reach the phone.

Device management lives on the **main listener** and requires the launch
token: `POST /api/companion/pair/start`, `GET /api/companion/devices`, and
`DELETE /api/companion/devices/{id}`. The desktop Settings → Mobile pane calls
these. The phone never receives the launch token.

The **companion listener** has only `POST /api/companion/pair` (code exchange,
rate-limited), `POST /api/companion/session-ticket` (device bearer),
`POST /api/companion/session` (login ticket), `PUT /api/companion/push`,
`DELETE /api/companion/push`, and `DELETE /api/companion/device` (self-revoke
only). Pairing, cookie exchange, push, and self-revoke use a code, a ticket, or
a device credential, never the launch token.

## 9. Pairing and authentication

### 9.1 Pairing flow

1. The user opens Settings → Mobile on the desktop (or runs the companion pair
   command on a headless host) and requests a pairing code. The desktop calls
   `POST /api/companion/pair/start` on the main listener with the launch token.
2. The server returns a short-lived code bound to that server instance. The
   companion listener shares the same code store.
3. The user enters the companion URL and code in the Android setup screen.
4. The setup screen calls a Go pairing binding. The Go shell posts the code to
   `/api/companion/pair` on the companion listener over HTTPS, outside the
   WebView and therefore outside CORS.
5. The server returns the device token once plus a one-time WebView login
   ticket. The Go shell stores the device token in app-private storage and
   navigates the WebView to `https://<server>/#login=<ticket>`.
6. The mobile page reads the fragment, posts the ticket to
   `/api/companion/session`, receives the `__Host-ocode_companion` cookie, and
   clears the fragment.
7. The web page uses the cookie. The native shell keeps the device token and
   uses it as a bearer credential for push registration and for minting new
   login tickets.

The pairing code is never sent in a query string. The login ticket is sent in
the URL fragment, which browsers do not send to the server, and is single-use
for 60 seconds, so it does not reach server access logs or proxy logs.

### 9.2 Pairing code rules

- 10 characters, Crockford base32, generated from a cryptographic random source.
- Expires after 10 minutes.
- Single use.
- At most 5 failed attempts per minute per client IP.
- Hash at rest; the plaintext code is shown once.
- Bound to the server instance id, so a code from one server cannot pair with
  another.

### 9.2.1 WebView login ticket

- 32 random bytes, base64url encoded.
- Bound to one device id.
- Expires after 60 seconds.
- Single use.
- Hashed at rest; the plaintext value exists only in the fragment navigation.
- Exchanged at `/api/companion/session` for the device cookie.
- A new ticket can be minted only with the device bearer token at
  `/api/companion/session-ticket`.

### 9.3 Device token rules

- 32 random bytes, base64url encoded.
- Stored as a SHA-256 hash in the device store.
- Fields: device id, display name, platform, created at, last seen, revoked,
  push tokens.
- Cookie: `__Host-ocode_companion`, `HttpOnly`, `Secure`, `SameSite=Lax`,
  `Path=/`. The `__Host-` prefix prevents a sibling subdomain from setting it.
  The server sets it from `/api/companion/session` after a login-ticket
  exchange.
- A debug build testing plain `http://localhost` may use a non-prefixed,
  non-Secure cookie. Production never relaxes the `__Host-` rule.
- Bearer device tokens are accepted for non-browser clients. The WebView uses
  the cookie; the native shell uses the bearer token for push registration.
- Revocation takes effect on the next request. There is no long-lived session
  cache in front of the store.

### 9.4 Store and locking

- Path: `<GlobalDataDir>/companion/devices.json`.
- Use the same file-lock plus atomic-write pattern as other shared stores.
- Pairing codes and login tickets live in memory only. A process restart
  invalidates every outstanding code and ticket.
- Never write the plaintext device token, pairing code, or login ticket to
  disk.
- Keep the store small; it holds one record per paired device.

### 9.5 Authentication changes

`internal/server/server.go` currently returns a boolean from `checkAuth` and
compares one token. Extend it with a principal:

- `launch` for the existing per-launch token.
- `device` for a companion device.
- `anonymous` when auth is not configured locally.

`authMiddleware` attaches the principal to the request context. Companion
scope checks use that principal. Existing handlers keep their current behavior
and do not need to know about principals.

For cookie-authenticated mutations, require the request `Origin` to match the
server's own origin. `SameSite=Lax` is the primary guard; the origin check is
the second.

## 10. Push notifications

### 10.1 What triggers a push

| Event | Push | Default |
|---|---|---|
| Permission ask created | Yes | On |
| Question asked | Yes | On |
| Turn finished | Optional | Off |
| Turn failed | Yes | On |

A push is sent only to devices registered for that server. Deduplicate per
device and ask id. Send at most one push per ask. Never include transcript
text, tool arguments, file paths, or tokens in the payload.

### 10.2 Server side

- Store the FCM token per device.
- Send through FCM HTTP v1 with a service account configured by
  `OCODE_FCM_CREDENTIALS` (path to a service-account JSON file) or an explicit
  config key.
- If credentials are unset, log once and skip push. The server must still work.
- On FCM `UNREGISTERED` or `INVALID_ARGUMENT`, remove that token.
- Push dispatch is best-effort and never blocks the agent turn or the ask.

### 10.3 Android side

- Add the Firebase Gradle plugin and `google-services.json` to the Android host.
- Create a notification channel for asks, questions, and failures.
- Request `POST_NOTIFICATIONS` at pair time on Android 13+.
- The FCM service writes the token to app-private storage. The Go shell reads
  it on launch and posts it to `/api/companion/push` with the stored device
  bearer token. It never reads the WebView cookie.
- A notification tap opens the saved server URL at
  `/session/<id>?ask=<id>`.
- Validate the server URL from the push payload against the saved server list.
  Never open an arbitrary URL from a push.

### 10.4 Why not Web Push

Web Push works for a PWA but is not supported by an Android WebView. Using it
would force a TWA and a different packaging path. That remains the fallback if
Play policy rejects the Wails shell.

## 11. Android shell

### 11.1 Shape

- New `cmd/ocode-mobile` Wails v3 application.
- One window. Android is fullscreen; menus, tray, and multiple windows are out.
- The shell does not start `internal/server`, does not import the desktop
  native services, and does not link the bash/PTY/LSP/MCP toolchain.
- The shell has two local responsibilities: server setup and push/deep-link
  handling.

### 11.2 Setup and navigation

- On first launch, show a local setup page from the Wails asset server.
- Setup asks for a server URL and a pairing code.
- The Go side saves the server list in an app-private file.
- The Go side pairs with the server and stores the device token in app-private
  storage.
- After pairing, navigate the WebView to `https://<server>/#login=<ticket>`.
  The page exchanges the ticket for the device cookie.
- On later launches, the Go side mints a fresh login ticket with the stored
  device token and navigates through the fragment exchange. This also repairs a
  cleared cookie. The user does not re-pair.
- A server switch loads that server's own origin, cookie, and stored token.

- Setup accepts `https://` only. `http://localhost` and `http://127.0.0.1` are
  allowed only in debug builds.
- The URL is normalized to an origin. Userinfo, paths, queries, and fragments
  are rejected.

The setup page is local; the session UI is remote. Wails bindings are only
needed on the setup page. The remote page uses the existing HTTP API.

### 11.3 Android build

- Use the Wails v3 Android template with API 35 and the SDK/NDK versions the
  Wails doctor requires.
- Add Firebase dependencies to the generated Gradle project.
- Package a debug APK for device testing and an AAB for Play.
- Keep `google-services.json` out of the public repo; CI injects it.
- Sign with an upload key and use Play App Signing.

### 11.4 Play Store risk

A WebView-centric app can be rejected for minimum functionality. The shell
includes native setup, secure server storage, deep links, and push, so it is
more than a bare browser. If Play still rejects it, fall back to a TWA around
the same mobile web surface and use Web Push. That fallback adds a web app
manifest and a companion-scoped service worker; the API and screen design stay
the same.

## 12. Reachability from anywhere

### 12.1 v1: user-operated HTTPS endpoint

Supported recipes:

- Tailscale Funnel to `127.0.0.1:<companion-port>`.
- Cloudflare Tunnel to `127.0.0.1:<companion-port>`.
- Caddy or Nginx with a public certificate in front of
  `127.0.0.1:<companion-port>`.

The companion listener binds loopback only. The main listener is never the
tunnel target. The tunnel terminates TLS. The server trusts `X-Forwarded-For`
only when the immediate peer is loopback, so rate limiting sees the real client
IP.

### 12.2 Why not a relay now

A relay needs account identity, device pairing across tenants, a persistent
outbound connection from every server, message routing, push fan-out, and a
new privacy/security review. It is a separate product. The mobile client and
the device-auth model are designed so a relay can be added later without
changing the mobile UI.

### 12.3 Explicitly unsupported

- Binding the companion listener directly to a public interface.
- Exposing the main listener through the tunnel.
- Plain HTTP on a public address.
- `?token=` authentication.
- Relying on the desktop self-signed certificate for a public origin.

## 13. Error handling

| Condition | Behavior |
|---|---|
| Server unreachable | Show the cached session list and transcript in read-only mode; writes show a retryable error. |
| 401 with a valid device token | The shell mints a new login ticket and reopens the page; the user is not interrupted. |
| 401 with a revoked device | Show "Pair this device again"; keep the server URL. |
| Login ticket expired or reused | Mint a new ticket; if minting fails, re-pair. |
| 403 scope denial | Show an explicit "This build tried an unsupported action" error; never retry silently. |
| SSE disconnect | Reconnect with capped backoff; resync from `GET /api/sessions/{id}/state` and the live-frame fields. |
| Pair code expired or used | Clear the field, ask the user to generate a new code. |
| Pair code wrong too often | Rate-limit and show a wait message. |
| Push token rejected | Remove it server-side; the app re-registers on next launch. |
| FCM not configured | No push; the app still works in the foreground. |
| Push payload has an unknown server | Ignore it; do not navigate. |
| Stale app vs server | Show the server version and a "update app" hint when the companion API version is unsupported. |

## 14. Security

- The launch token never reaches the phone or the companion listener.
- The server token is never placed in a URL, a push payload, a log line, or
  `localStorage`.
- Device cookies are `HttpOnly`, `Secure`, `SameSite=Lax`, and `__Host-`
  prefixed.
- Device tokens are hashed at rest and revoked on the next request.
- The companion scope allowlist is deny-by-default for device principals.
- Pairing is rate-limited, single-use, short-lived, and hashed at rest.
- The companion listener uses the existing per-IP rate limiter, with stricter
  limits on pairing and login-ticket endpoints.
- Cookie-authenticated writes require a matching `Origin`.
- Push payloads carry identifiers only, never content.
- The Android shell stores the device credential in app-private storage. The
  WebView receives only a short-lived login ticket, exchanged for an HttpOnly
  cookie. Page JavaScript never sees the device token.
- The app validates deep-link server URLs against the saved server list.

Threats explicitly reduced:

- A stolen phone cannot read files, open a terminal, change config, or reach
  MCP because the device role is scoped.
- A leaked URL or proxy log does not contain a reusable credential.
- A malicious page cannot reuse the cookie cross-site because of `SameSite`
  plus the origin check.

## 15. Testing

### 15.1 Go

- Device store: create, match, revoke, hash, atomic write, concurrent access.
- Pairing: expiry, single use, wrong code, rate limit, server binding.
- Login ticket: expiry, single use, wrong device, reuse, cookie set, ticket
  minting with a revoked device.
- Auth: cookie, bearer device token, launch token, revoked device, missing
  cookie, wrong origin, self-revoke only.
- Scope: every allowed route succeeds for a device; representative denied
  routes return 403.
- SSE filter: companion event families pass; artifact, file, git, terminal,
  browser, and config events do not.
- Push: dedupe, no credentials, FCM rejection removes the token, payload has no
  secrets.
- Listener split: companion binds loopback, serves the mobile entry at `/`,
  and has no desktop fallback; the main listener still serves the desktop `/`
  and the full API; `?token=` is refused on the companion listener.

### 15.2 Web

- Mobile sessions list: empty, loading, running, waiting, offline cache.
- Session view: streaming, reconnect, ask card, question card, composer,
  stop, retry, queue.
- Pair flow: setup form, Go pairing binding, login-ticket exchange, cookie set,
  expired code, wrong code, and unreachable server states.
- SSE: existing parser tests plus a mobile reducer test.
- No import of desktop-only components in the mobile entry.

### 15.3 Integration and manual

- Start `ocode serve --companion` with a temp HOME and a temp project.
- Pair a test client, exchange the login ticket for a cookie, list sessions,
  answer an ask, revoke the device, and verify the next request is 401.
- Run the Wails Android build and install on an emulator.
- Test a real device over a tunnel from cellular.
- Test FCM with a test Firebase project: app killed, ask created, notification
  arrives, tap opens the right session.
- Run the Play internal testing track before production.

## 16. Rollout

| Phase | Deliverable | Exit condition |
|---|---|---|
| 1 | Companion listener, mobile entry, pairing, device auth, scoped routes | Phone browser can pair and control a session over a tunnel |
| 2 | Wails Android shell, setup, server list, deep links | APK installs and opens the companion root on a device |
| 3 | FCM registration and push dispatch | A killed app receives a permission-ask notification |
| 4 | Play Store AAB, signing, listing, policy review | Internal testing track installs and passes review |

Phase 1 is independently useful and is the test surface for the rest.

## 17. Success criteria

- A new phone pairs in under one minute with a code from the desktop.
- A running session streams to the phone without a manual refresh.
- A permission ask can be answered from the phone and the agent continues.
- A push arrives for an ask while the app is closed.
- A revoked device is denied on its next request.
- No reusable credential appears in a URL, log, push payload, or browser
  storage.
- The desktop and TUI behave exactly as before when companion mode is off.
- The app works from cellular through a supported tunnel recipe.

## 18. Deferred

- Hosted ocode relay and account system.
- iOS shell.
- Web Push and TWA packaging (kept as the Play policy fallback).
- Multiple simultaneous servers in one session view.
- Voice input and speech output.
- File attachments and artifact previews.
- Terminal, file tree, git, MCP, and settings management from the phone.
- Team or shared-device access.

## 19. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Wails Android is experimental | Build or runtime breakage across Wails releases | Pin the Wails version, keep the shell thin, test on a real device each release |
| No Android notification backend in Wails 3.1.0 | Background push needs custom native code | Treat FCM as a separate component; keep it behind a small interface |
| Play rejects a WebView-centric app | No store distribution | Add native setup/push; keep the TWA + Web Push fallback |
| Public exposure of the server | Account or data compromise | Tunnel-only, device auth, scope allowlist, rate limits, no query token |
| Tunnel setup is user work | "From anywhere" is not zero-config | Document three recipes; keep the relay as a future product |
| Companion scope drifts | A new route widens phone access | One allowlist function plus a deny-by-default test |
| SSE or push duplicates | Repeated notifications or repeated answers | Dedupe by ask id and device; idempotent answer path |

## 20. Approval

This design is ready for review. The next step after approval is an
implementation plan, not code.
