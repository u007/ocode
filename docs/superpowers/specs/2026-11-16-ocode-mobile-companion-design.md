# ocode Mobile Companion — Design

- **Date:** 2026-11-16
- **Status:** Revised after advisor review
- **Scope:** Mobile-first companion for controlling ocode sessions from a phone
- **Platform:** Android browser/PWA first; Trusted Web Activity later
- **Related:** `docs/superpowers/specs/2026-11-08-offline-capable-web-client-design.md`,
  `docs/concepts/web-server-project-scoping.md`,
  `docs/concepts/web-terminal-ghostty-web.md`

## 1. Summary

Build a **mobile companion**, not a phone-sized ocode desktop and not a native
agent.

The phone is a remote control for sessions that run on an ocode server. It lists
sessions, shows the live transcript, sends messages, stops turns, and answers
questions and low-risk permission asks. It does not run the agent, bash, git,
LSP, MCP, or the desktop webview.

Delivery is phased:

1. **Phase 1a:** a browser-based mobile surface served by a dedicated companion
   listener, with direct pairing, local-only session scope, project scope,
   approval policy, and audit.
2. **Phase 1b:** the same surface becomes an installable PWA with Web Push.
3. **Phase 2 (conditional):** a Trusted Web Activity wrapper for Google Play,
   only if the product moves to a single hosted origin and real use justifies
   store distribution.
4. **Later:** a native shell or hosted relay only if validation demands it.

The primary push design is **Web Push with per-server VAPID keys**, not FCM.
FCM cannot serve a Play-distributed app whose users each run their own server:
`google-services.json` binds the installed app to one Firebase project, and a
user's server cannot send to it without that project's credentials. Web Push
keeps each ocode server self-contained.

## 2. Goals

- Let the user see running and waiting sessions from a phone.
- Let the user watch live output with no manual refresh.
- Let the user answer questions and low-risk permission asks from the phone.
- Let the user send a message, stop a turn, retry, and queue a message.
- Notify the user when a session needs attention while the app is closed.
- Make pairing and revocation understandable without editing config files.
- Keep the desktop and TUI unchanged in behavior.
- Keep the public surface small enough to secure and test.

## 3. Non-goals

- Running the ocode agent on Android.
- A phone file tree, code editor, terminal, git UI, MCP manager, browser, voice,
  or artifact manager.
- FCM, a Firebase project, or a native Kotlin service in v1.
- A hosted relay, ocode account system, team sharing, or multi-tenant hosting.
- Multi-server support in one app. One companion origin is one server.
- Offline transcript cache. The app shell may be cached; session data is not.
- iOS in v1.
- Turn-finished or turn-failed push. Push is for asks and questions only.

## 4. Decisions

| Decision | Choice | Why |
|---|---|---|
| Product shape | Remote companion client | The agent needs a desktop/POSIX host. Android cannot supply bash, git, PTY, LSP, or MCP servers. |
| Mobile surface | Separate mobile entry, not a responsive desktop SPA | The desktop UI is dense and would force desktop-only code into the phone. |
| Server origin | Mobile entry at the companion listener root | Avoids CORS, avoids putting a credential in a URL, and keeps one SSE origin. |
| Listener split | Second loopback listener in the same process | The full API and launch token stay off the public surface. |
| Auth | Pairing code → HttpOnly device cookie | The browser stores the cookie; page JS never sees the secret. No login ticket or JS token. |
| Device privilege | Scoped, local-only allowlist | A device must not reach files, git, terminal, config, MCP, or remote SSH hosts. |
| High-risk approvals | Desktop-only in 1a; WebAuthn step-up in 1b | Sending messages is one thing; approving writes/bash from a stolen phone is another. |
| Push | Web Push + VAPID per server | Self-contained, no central Firebase project, no push relay. |
| Packaging v1 | Browser, then installable PWA | Fastest path to real use. |
| Packaging later | TWA for Play, single hosted origin only | A TWA has a fixed start URL, so it cannot serve per-user self-hosted origins. |
| Reachability | User-operated HTTPS tunnel | No relay in v1. Tailscale Serve private is the default recipe. |
| Desktop/CLI impact | Additive | Existing launch-token, remote-SSH, and desktop flows keep working. |

## 5. Alternatives considered

### 5.1 Make the existing desktop SPA responsive

Rejected. The SPA assumes a mouse, wide panes, a file tree, a terminal, and
desktop-only components. A mobile-first entry is smaller and clearer.

### 5.2 Wails Android shell with FCM

Rejected for v1. FCM is structurally incompatible with per-user servers and a
single Play-distributed app: the app's Firebase project is fixed at build time,
so every user's server would need that project's credentials. A Wails shell also
needs custom Kotlin FCM code and carries experimental-platform and Play
"webview wrapper" risk. Wails remains a later option only with a push relay or
no background push.

### 5.3 Put the mobile UI in a native shell and proxy to the server

Rejected for v1. A local proxy inside the app needs a loopback listener,
cleartext or local TLS handling, and a second origin policy. Serving the mobile
entry from the companion listener keeps one origin, one cookie, and one SSE
stream.

### 5.4 Use the existing `--remote` SSH workspace flow

Rejected. `internal/remote` is an SSH workspace client with host keys, a remote
agent server, and a reverse proxy. It assumes a desktop peer and does not solve
phone pairing, app distribution, or push.

### 5.5 Use Tailscale or another VPN as the only transport

Partially accepted. Tailscale Serve is the default private recipe; Tailscale
Funnel, Cloudflare Tunnel, and Caddy are supported public recipes. The app
accepts any HTTPS base URL.

### 5.6 Use a Trusted Web Activity and Web Push instead of Wails

Accepted only for a single hosted origin. A TWA wraps the same PWA, uses the
same Web Push subscription, and avoids a second UI and a custom native push
path. It cannot serve per-user self-hosted origins because its start URL is
fixed at build time. Play distribution is therefore deferred until the product
has a hosted origin or a native shell with a server URL setup.

## 6. Architecture

```
┌──────────────────────────┐        HTTPS (public or tailnet)   ┌─────────────────────┐
│ Android Chrome / PWA     │  ────────────────────────────────► │ User-operated tunnel │
│  ├─ mobile entry         │                                    │ Tailscale Serve /    │
│  ├─ service worker       │                                    │ Funnel / Cloudflare  │
│  └─ Web Push subscription│                                    └──────────┬──────────┘
└──────────┬───────────────┘                                               │ loopback
           │ browser push endpoint                                          ▼
           │ (Chrome/Firefox push service)                    ┌─────────────────────────┐
           ▼                                                  │ ocode process           │
┌──────────────────────────┐        outbound HTTPS             │  companion listener     │
│ Browser push service     │ ◄──────────────────────────────── │   ├─ mobile entry at /  │
│ FCM / Mozilla / Apple    │        (Web Push + VAPID)          │   ├─ scoped /api        │
└──────────────────────────┘                                   │   ├─ pair / push        │
                                                               │  main listener          │
                                                               │   ├─ desktop SPA        │
                                                               │   └─ full API           │
                                                               │  shared sessions/store  │
                                                               └─────────────────────────┘
```

The ocode server sends Web Push directly to the browser vendor's push endpoint,
signed with its own VAPID key. "FCM" in the diagram is Chrome's browser push
transport, not a Firebase app dependency: ocode never holds Firebase
credentials. There is no ocode relay.

### 6.1 Components

| Component | Responsibility | Depends on |
|---|---|---|
| Mobile entry | Sessions, transcript, asks, composer, settings | Existing ocode API, SSE, shared types |
| Companion listener | Serve the mobile entry, device auth, scoped routes, push, audit | `internal/server`, `internal/paths`, file locking |
| Companion device store | Pairing codes, device records, project scope, push subscriptions | `GlobalDataDir`, file locking, atomic writes |
| Ask journal | Durable pending asks, resolution state, push dedupe | `GlobalDataDir`, file locking |
| Web Push sender | VAPID signing, encryption, delivery, clear messages | VAPID key file, device subscriptions |
| Mobile service worker | Push display, notification click, app-shell cache | Mobile build output |
| PWA manifest | Installability, standalone display, icons | Mobile build output |
| TWA wrapper (later) | Play distribution of the same PWA | Digital Asset Links, Play signing |

### 6.2 Request and stream flow

1. The phone opens the companion listener root over HTTPS.
2. The page pairs with a code or authenticates with the device cookie.
3. The page loads allowed projects and their sessions.
4. The page opens `/api/events` with the same cookie.
5. The user reads the transcript, sends a message, stops a turn, or answers an
   ask.
6. The server writes the action through the same path a desktop client uses.
7. When an ask is created and the device has a push subscription, the server
   writes the ask journal and sends a Web Push message.
8. Tapping the notification opens `/session/<id>?ask=<id>`.
9. Resolving the ask anywhere sends a clear push that closes the notification.

The mobile surface never receives a second, parallel agent path. It is another
client of the same server.

## 7. Mobile web surface

### 7.1 Build shape

- Add `web/mobile.html` and `web/src/mobile/` as a second Vite entry. Add it to
  `build.rollupOptions.input` in `web/vite.config.ts`; Vite does not pick up a
  second root HTML file automatically.
- Reuse `web/src/api/client.ts`, `web/src/api/types.ts`, markdown rendering, and
  the existing fetch-based SSE parser (`readSSEStream`).
- Do not import the desktop `App`, sidebar, terminal, editor, git, browser, or
  service-worker registration path.
- The companion listener serves `mobile.html` as its `/` document. Assets stay
  at `/assets/*` on the same origin.
- `mobile.html` uses `<base href="/">` so the shared `/assets/*` resolve at the
  companion origin root.
- Phase 1a build output adds `mobile.html` plus hashed assets. Phase 1b adds
  `mobile-sw.js` and `mobile-manifest.webmanifest`.
- `web/embed.go` already embeds all of `dist/`.

### 7.2 Screens

| Screen | Contents |
|---|---|
| Pair | Server URL (prefilled by the link), pairing code, error states |
| Sessions | Allowed projects; sessions grouped by project; All / Waiting / Running filters |
| Session | Transcript, live streaming state, question card, permission card, composer, Stop, Retry, Queue |
| Settings | Device name, read-only project scope, server version, revoke this device; notification and WebAuthn settings appear in 1b |

No Servers screen. One installed PWA is one companion origin and one server.
Multi-server is deferred.

### 7.3 Mobile behavior

- Sessions are a full-screen route; Settings is a second tab.
- Large tap targets, safe-area insets, keyboard-aware composer, no hover-only
  actions.
- The transcript renders user, assistant, thinking, and tool-result blocks in a
  compact form. Tool arguments are collapsed by default.
- Permission and question cards are pinned above the composer until resolved.
- A "Waiting" filter is the default when at least one ask is pending.
- A high-risk permission card shows "Approve on desktop" and cannot be resolved
  from the phone in Phase 1a. In Phase 1b it offers a WebAuthn step-up.
- Write actions are disabled with a banner when the server is unreachable.

### 7.4 Installability and service worker

- Phase 1a is a normal browser page. No manifest, no service worker.
- Phase 1b adds `mobile-manifest.webmanifest`:
  - `name` / `short_name`, icons, `display: standalone`, `start_url: /`,
    `scope: /`, `theme_color`, `background_color`.
  - `mobile.html` links it with `<link rel="manifest"
    href="/mobile-manifest.webmanifest">` and sets `theme-color`.
- Phase 1b adds `mobile-sw.js` at `/mobile-sw.js`, scope `/`.
  - It precaches the mobile shell and hashed assets using a **mobile-specific**
    precache manifest. Do not reuse the desktop `sw.js` manifest, which lists
    the desktop `index.html`.
  - A second build step generates `mobile-sw.js` with that manifest; the
    existing desktop plugin must not rewrite it.
  - It handles `push` and `notificationclick`.
  - It never caches `/api/*`. Authenticated responses never enter a shared
    cache.
- The desktop origin keeps its existing `sw.js` and its own scope. The two
  service workers live on different origins and never share a cache.

## 8. Companion listener

### 8.1 A second listener, not a replacement server

Add a **companion listener**. It is a second `http.Server` in the same ocode
process, backed by the same `Server` and session state.

- `ocode serve --companion` starts it. The desktop app can also enable it from
  Settings → Mobile.
- In the desktop app, Settings → Mobile shows the companion port, lets the user
  record the public URL, issues a pairing code, and manages devices.
- On a headless host, `ocode companion pair` prints a code.
- It binds `127.0.0.1:<companion-port>` only. The default port is 4097 and it is
  configurable. The public TLS endpoint is the tunnel's job.
- It serves only the mobile entry and its static assets at `/` and
  `/assets/*`, `/api/health`, the scoped companion API, and the pairing/push
  endpoints.
- It never serves the desktop `index.html`, the full desktop API, LAN sharing,
  Tailscale share, or the desktop local-cert flow.
- It accepts device cookies, pairing codes, Web Push subscription
  registrations, and WebAuthn registration/assertion calls only. The launch
  token is not valid there.
- The main listener keeps the desktop SPA, the full API, and the launch token.
  The tunnel is pointed at the companion port, never at the main port.

This separation is load-bearing: exposing the main listener through a tunnel
would put the desktop UI and the full API behind a single rotating token.

The mode is additive. `--remote` keeps its current SSH semantics.

### 8.2 Route serving

`internal/server/web.go` currently serves one SPA entry on the main listener.
The companion listener gets its own mux:

- `/` and `/session/*` serve `mobile.html`.
- `/assets/*` serves the shared hashed build assets.
- `/mobile-sw.js`, `/mobile-manifest.webmanifest`, and icons serve the PWA
  files (Phase 1b).
- `/.well-known/assetlinks.json` serves the Digital Asset Links for the TWA
  (Phase 2).
- `/api/health` is unauthenticated and returns only the version and
  `companion_api: 1`.
- `/api/companion/pair` accepts a pairing code and sets the device cookie.
- `/api/companion/me` returns the calling device's name and project scope.
- `GET /api/companion/push/key` returns the VAPID public key.
- `PUT /api/companion/push` and `DELETE /api/companion/push` manage Web Push
  subscriptions.
- `DELETE /api/companion/device` revokes only the calling device.
- Phase 1b WebAuthn: registration and assertion endpoints under
  `/api/companion/webauthn/`.
- Scoped `/api/*` routes are served as listed in 8.3.
- Everything else returns a JSON 404 or 403. There is no desktop SPA fallback.
- Cache headers: hashed `/assets/*` immutable; documents, the manifest, and the
  service worker no-cache.

### 8.3 Scope enforcement

On the companion listener, a device principal is allowed only these areas:

- `GET /api/health`
- `GET /api/events` (local only)
- `GET /api/projects` (filtered to the device's allowed projects)
- `GET /api/projects/sessions?path=<local allowed project>` (no `host`)
- `GET /api/sessions/{id}`, `/state`, `/status`
- `POST /api/sessions/{id}/message`, `/cancel`, `/retry`, `/queue`
- `POST /api/permissions/resolve` (subject to the approval policy)
- `POST /api/questions`, `POST /api/questions/cancel`
- `GET /api/companion/me`, `GET /api/companion/push/key`,
  `PUT /api/companion/push`, `DELETE /api/companion/push`
- `DELETE /api/companion/device`

The allowlist matches **method + path + query**. It is deny-by-default. These
are always refused:

- Any request with `host` or `hosts` query parameters.
- Any `pulse_...` session.
- Any session in `yolo` permission mode.
- Any session or project outside the device's project scope. Session-scoped
  routes resolve the session's owning project and check it; a session id alone
  is not enough.
- Files, git, terminal, config, models, MCP, artifacts, browser, and desktop
  control.

Remote SSH host sessions are out of scope. To control sessions on another
machine, run that machine's own companion listener and open its URL in the
browser; switching between companion origins is a manual action in v1.

Question and permission resolution bodies carry a session or ask id. The
handler checks that id's owning project against the device scope before
resolving; a path-only check is insufficient.

`/api/events` is called without `hosts`. The remote-event relay in
`internal/server/handler_events.go` is never enabled for a companion request.
The SSE stream is filtered to the event families the mobile UI consumes:
session updates, messages, turns, permission asks, questions, and run state.
It is also filtered to the device's project scope. Artifact, file, git,
terminal, browser, and config events are dropped before they reach the phone.
An event with no project identifier is dropped rather than guessed.

Device management lives on the **main listener** and requires the launch token:
`POST /api/companion/pair/start`, `GET /api/companion/devices`,
`PATCH /api/companion/devices/{id}` (project scope, name),
`DELETE /api/companion/devices/{id}`, and `GET /api/companion/audit`.

## 9. Pairing and authentication

### 9.1 Pairing flow

1. The user opens Settings → Mobile on the desktop (or runs `ocode companion
   pair` on a headless host) and requests a pairing code.
2. The desktop calls `POST /api/companion/pair/start` on the main listener with
   the launch token. The code is short-lived, and the companion listener shares
   the same in-memory code store.
3. The phone opens the companion URL and enters the code.
4. The page posts the code to `/api/companion/pair` on the companion listener.
5. The server verifies the code and sets `__Host-ocode_companion` as an
   HttpOnly, Secure, SameSite=Lax cookie. The response contains only the device
   name and id.
6. The browser stores the cookie. Page JavaScript never sees the secret.
7. On later visits the cookie authenticates directly. If it is missing or
   expired, the user pairs again.

There is no login ticket, no token in a URL, and no token in `localStorage`.
The pairing code is sent in a POST body over HTTPS.

### 9.2 Pairing code rules

- 10 characters, Crockford base32, generated from a cryptographic random source.
- Expires after 10 minutes.
- Single use.
- At most 5 failed attempts per minute per client IP.
- After 5 failed attempts for one code, invalidate that code globally.
- Hash at rest; the plaintext code is shown once.
- Bound to the server instance id, so a code from one server cannot pair with
  another.
- Pairing codes live in memory only. A process restart invalidates every
  outstanding code.

### 9.3 Device credential rules

- 32 random bytes, base64url encoded.
- Stored as a SHA-256 hash in the device store.
- Fields: device id, display name, platform, created at, last seen, revoked,
  project scope, push subscriptions.
- Project scope: empty means every local project allowed by
  `allowedProjectRoots()`; a non-empty list is an explicit allowlist of saved
  local project paths. Remote project paths are never allowed.
- Cookie: `__Host-ocode_companion`, `HttpOnly`, `Secure`, `SameSite=Lax`,
  `Path=/`. The server sets it from `/api/companion/pair`.
- A debug build testing plain `http://localhost` may use a non-prefixed,
  non-Secure cookie. Production never relaxes the `__Host-` rule.
- Revocation takes effect on the next request. There is no long-lived session
  cache in front of the store.
- Self-revoke (`DELETE /api/companion/device`) removes only the calling device
  and expires the cookie.

### 9.4 Store and locking

- Devices: `<GlobalDataDir>/companion/devices.json`.
- Pairing codes: memory only.
- Use the same file-lock plus atomic-write pattern as other shared stores.
- Never write the plaintext device secret or pairing code to disk.
- Keep the store small; one record per paired device.

### 9.5 Authentication changes

`internal/server/server.go` currently returns a boolean from `checkAuth` and
compares one token. Extend it with a principal:

- `launch` for the existing per-launch token.
- `device` for a companion device.
- `anonymous` when auth is not configured locally.

`authMiddleware` attaches the principal to the request context. Companion
scope checks use that principal. Existing handlers keep their current behavior
and do not need to know about principals.

Cookie-authenticated mutations require the request `Origin` to match the
companion origin exactly. A missing `Origin` header is rejected. `SameSite=Lax`
is the primary guard; the origin check is the second.

## 10. Permission and approval policy

### 10.1 Risk classes

- **Low risk:** read-only tools that do not write files or execute code:
  `read`, `grep`, `glob`, `list`, `todoread`, read-only LSP operations.
- **High risk:** everything that mutates or executes: `bash`, `write`, `edit`,
  `multi_edit`, `apply_patch`, `delete`, git mutations, `task`, `browser_run`,
  all MCP tools, and every unknown tool.
- A question is not a permission ask. The phone may answer it, with an audit
  record.

The server classifies an ask by the tool name and any existing risk hint. If the
classification is unknown, it is high risk. Ask payloads include the server's
`risk` value; the client never infers risk itself.

### 10.2 Phase 1a policy

- The phone may resolve **low-risk** permission asks.
- The phone may not resolve high-risk asks. The card says "Approve on desktop"
  and the resolve endpoint returns `403 high_risk_requires_desktop`.
- The phone may always send a message, stop a turn, retry, queue, and answer
  questions.
- Sessions in `yolo` mode are **not exposed** on the companion listener. They
  are hidden from the session list; a session that switches to yolo while open
  shows "Desktop only" and becomes read-only. This is what stops a stolen phone
  from directing an agent that can act without asking.
- The phone cannot change a session's permission mode.
- Every phone action is audited.

### 10.3 Phase 1b policy

- High-risk approvals require a WebAuthn platform-authenticator assertion
  (fingerprint or face unlock) bound to the device.
- Registration happens after pairing, from Settings → Security.
- If no authenticator is available or the assertion fails, the ask stays
  desktop-only.
- WebAuthn challenges are single-use, short-lived, and bound to the ask id and
  device id.

### 10.4 Audit log

- Path: `<GlobalDataDir>/companion/audit.jsonl`, append-only under a file
  lock.
- Fields: timestamp, device id, device name, action, session id, ask id, tool,
  risk, decision, method (`cookie`/`webauthn`), client IP.
- Retain 30 days or 10,000 entries, whichever comes first.
- `GET /api/companion/audit` on the main listener (launch token) returns recent
  entries. Desktop Settings → Mobile shows them.
- A phone approval emits a desktop event and a native desktop notification.

### 10.5 Security boundary

A paired phone is a **trusted control surface**, not a security boundary against
a compromised unlocked phone. It can read transcripts and direct the agent
within the agent's permission mode. The design reduces blast radius by denying
the API surfaces the phone does not need and by gating high-risk approvals. It
does not prevent a stolen phone from sending a message or reading a transcript.
Revoke the device on loss.

## 11. Push notifications

### 11.1 What triggers a push

| Event | Push | Default |
|---|---|---|
| Permission ask created | Yes | On |
| Question asked | Yes | On |
| Turn finished | No | Out of scope |
| Turn failed | No | Out of scope |

A push is sent only to devices registered for that server. Deduplicate per
device and ask id. Send at most one push per ask. Never include transcript text,
tool arguments, file paths, or tokens in the payload. Phase 1a has no background
push; its only notification is the in-app ask card.

### 11.2 Web Push and VAPID

- On first Web Push use (Phase 1b), generate a VAPID key pair. Store the private
  key in `<GlobalDataDir>/companion/vapid.json` with mode 0600.
- Serve the public key to the authenticated page.
- The page subscribes with `PushManager.subscribe({ userVisibleOnly: true,
  applicationServerKey })` and posts the subscription (endpoint, p256dh, auth,
  expiration) to `PUT /api/companion/push`.
- The server sends with a maintained Go Web Push implementation using
  `aes128gcm` encryption and the VAPID private key.
- `TTL: 600` for asks; `Urgency: high` for ask notifications; `normal` for
  clear messages.
- If VAPID keys are lost, all subscriptions are invalid and devices must
  re-subscribe. The keys are part of the server's persistent state.

### 11.3 Payload and notification behavior

- Ask payload: `{type: "ask", ask_id, session_id, tag, title, body, url}`.
- Clear payload: `{type: "clear", ask_id, tag}`.
- `tag` is `ask:<ask_id>`. The service worker uses it so a repeated push
  replaces the notification instead of stacking.
- `title` is generic: "ocode needs permission" or "ocode has a question".
  `body` is generic and contains no tool arguments or transcript content. For a
  high-risk ask in Phase 1a, the body says "Approve on desktop".
- `url` is `/session/<id>?ask=<id>`. The service worker validates that the URL
  is same-origin before opening it.
- `notificationclick` focuses an existing client at that URL or opens a new one.
- On resolve, the server sends a clear push. The service worker closes every
  notification with the matching tag.
- If a subscription returns `404` or `410`, delete it.
- The app re-subscribes on launch when notification permission is already
  granted.

### 11.4 Service worker

- `mobile-sw.js` handles `push`, `notificationclick`, and `pushsubscriptionchange`.
- It precaches the mobile shell and hashed assets; it never caches `/api/*`.
- It is served with `Cache-Control: no-cache` so updates are picked up.
- The PWA manifest and service worker are added in Phase 1b. Phase 1a uses no
  service worker.

## 12. Durability and restart behavior

### 12.1 Ask journal

- Path: `<GlobalDataDir>/companion/asks.json`.
- Key: ask id. Fields: session id, project root, kind (permission/question),
  tool, risk, state (pending/resolved/expired), created at, resolved at,
  resolved by, resolved method, notified devices, notification tag.
- Write the journal before sending a push. Update it on resolution. If the
  journal write fails, log it and skip the push; never notify for an ask the
  server cannot recover after restart.
- On startup, rehydrate pending asks and reconcile with live session state.
  An ask whose turn or tool call no longer exists is marked `expired` and a
  clear push is sent.
- Push dedupe uses the `notified` map, so a restart does not resend an ask.

### 12.2 Server restart and host sleep

- Device records, VAPID keys, subscriptions, and the ask journal are durable.
- The app reconnects and resyncs via `GET /api/sessions/{id}/state` and
  `/api/events`.
- A sleeping host is an operator problem, not a code problem. The app shows an
  offline state and retries. The documented deployment expectation is a host
  that stays reachable while the user expects notifications. Wake-on-LAN and a
  relay are out of scope.
- No API response or transcript is cached in the service worker, so a stale
  server view cannot be mistaken for live state.

## 13. Android distribution

### 13.1 Phase 1a/1b: browser and PWA

- The user opens the companion URL in Chrome on Android.
- Phase 1b adds "Add to Home Screen" / install prompt; the app opens standalone.
- Web Push works through the browser vendor's push service. No Firebase project
  and no ocode relay.
- Cookies persist in the browser profile, including the installed PWA.
- Revoking the device or clearing browser data requires re-pairing.

### 13.2 Phase 2 (conditional): Trusted Web Activity for Play

A TWA has a **fixed start URL**. It cannot open each user's self-hosted
companion origin. A single Play app therefore fits only a single hosted origin,
not the self-hosted v1 product. Play distribution is deferred until one of these
is true:

- The product moves to a hosted, multi-tenant origin, or
- A native setup shell opens the user's server in Chrome Custom Tabs, not a
  WebView, so Web Push stays in Chrome.

If that happens:

- Wrap the PWA in a TWA with the Play app package `com.mercstudio.ocode`.
- Serve `/.well-known/assetlinks.json` from the hosted origin with the Play
  signing certificate fingerprints.
- The TWA shares the Chrome profile and cookie jar; the same device cookie
  works.
- Use Play App Signing and an AAB. Internal testing first.
- Play requires a public HTTPS origin. Tailscale Serve (private tailnet) is not
  enough; use Funnel, Cloudflare Tunnel, or Caddy.
- The TWA is a distribution shell, not a second UI. No Go, Kotlin, or FCM code.

### 13.3 Native shell later

A Wails or other native shell is deferred. A WebView cannot use Web Push, so a
WebView-based shell needs FCM plus a push relay, or per-user sideloaded builds
with their own Firebase project. A native setup shell that opens the server in
Chrome Custom Tabs keeps Web Push and avoids a relay, but adds little over the
PWA. Neither is v1.

## 14. Reachability

### 14.1 Default: private tailnet

- Tailscale Serve to `127.0.0.1:<companion-port>` is the default recipe.
- The phone must be on the tailnet.
- This is the lowest-risk way to use the companion before public exposure.

### 14.2 Public: user-operated HTTPS endpoint

Supported recipes:

- Tailscale Funnel to `127.0.0.1:<companion-port>`.
- Cloudflare Tunnel to `127.0.0.1:<companion-port>`.
- Caddy or Nginx with a public certificate in front of the companion port.

The tunnel must expose the companion listener at the **origin root**. Path-
prefixed mounts (for example `/rc/`) are not supported in v1: they break the
PWA scope, the asset paths, and Digital Asset Links.

The companion listener binds loopback only. The main listener is never the
tunnel target. The tunnel terminates TLS. The server trusts `X-Forwarded-For`
only when the immediate peer is loopback; otherwise it uses the remote address.
If a tunnel hides client IPs, all clients share one rate-limit bucket; the
global pairing-code lockout still applies.

Funnel/public exposure is documented as higher risk than Serve. The user is
responsible for the tunnel and its access policy.

### 14.3 Why not a relay now

A relay needs account identity, device pairing across tenants, a persistent
outbound connection from every server, message routing, push fan-out, and a new
privacy/security review. It is a separate product. Web Push removes the need
for a relay in v1.

### 14.4 Explicitly unsupported

- Binding the companion listener directly to a public interface.
- Exposing the main listener through the tunnel.
- Plain HTTP on a public address.
- `?token=` authentication.
- Relying on the desktop self-signed certificate for a public origin.
- Remote SSH host routing from the companion listener (`host`/`hosts`).

## 15. Error handling

| Condition | Behavior |
|---|---|
| Server unreachable | Show a reconnect screen; keep the current draft in memory; writes retry manually. |
| 401 with missing or expired cookie | Show "Pair this device again"; expire the cookie. |
| 401 with a revoked device | Show "Device revoked"; keep the URL; require a new pairing code. |
| 403 scope denial | Show an explicit "This build tried an unsupported action" error; never retry silently. |
| 403 high-risk approval | Show "Approve on desktop"; keep the ask pending. |
| Remote host parameter | Reject with 400; the mobile UI never sends it. |
| Project outside device scope | Hide the project; reject its session ids with 403. |
| Session in `yolo` mode | Hide it; reject its session ids with 403; an already-open session becomes read-only with "Desktop only". |
| SSE disconnect | Reconnect with capped backoff; resync session state. |
| Pair code expired or used | Clear the field; ask for a new code. |
| Pair code lockout | Show a wait message; the code is invalid. |
| Push subscription expired | Remove it server-side; the app re-subscribes on launch. |
| Push delivery failure | Best effort; the ask remains visible in-app. |
| Notification resolved elsewhere | Clear push closes the notification by tag. |
| Server restart | Rehydrate pending asks; expire stale ones; clear their notifications. |
| API version mismatch | Show the server version and an "update app" message; block writes until compatible. |

## 16. Security

- The launch token never reaches the phone or the companion listener.
- No credential appears in a URL, a push payload, a log line, or `localStorage`.
- Device cookies are `HttpOnly`, `Secure`, `SameSite=Lax`, and `__Host-`
  prefixed. Page JavaScript never sees the secret.
- Device secrets are hashed at rest and revoked on the next request.
- Pairing is rate-limited, single-use, short-lived, hashed at rest, and
  globally locked out after repeated failures for one code.
- Cookie-authenticated writes require an exact `Origin`; a missing `Origin` is
  rejected.
- The companion API is local-only and deny-by-default on method + path + query.
- Device project scope restricts both the project list and session ids.
- High-risk approvals are desktop-only in 1a and WebAuthn-gated in 1b.
- Every phone action is audited; phone approvals raise a desktop alert.
- Push payloads carry identifiers only, never content.
- VAPID private keys are stored with 0600 permissions.
- The service worker never caches `/api/*`.
- The TWA shares the browser cookie jar; no second credential store.

Threats explicitly reduced:

- A stolen phone cannot call file, git, terminal, config, MCP, or remote-host
  endpoints.
- A stolen phone cannot approve high-risk writes or bash in Phase 1a, and needs
  a biometric assertion in Phase 1b.
- A leaked URL or proxy log does not contain a reusable credential.
- A malicious page cannot reuse the cookie cross-site because of `SameSite`
  plus the exact-origin check.

Threats not solved:

- A compromised unlocked phone can read the transcript and send messages that
  direct the agent within its permission mode. Revocation is the response.
- A compromised server is out of scope.
- Web Push delivery is best effort, not guaranteed.

## 17. Testing

### 17.1 Go

- Device store: create, match, revoke, hash, atomic write, concurrent access.
- Pairing: expiry, single use, wrong code, per-IP rate limit, global code
  lockout, server binding.
- Auth: cookie, missing cookie, revoked device, missing `Origin`, wrong
  `Origin`, self-revoke only.
- Scope: method + path + query allowlist; allowed routes succeed; `host`/
  `hosts` rejected; `pulse_` rejected; `yolo` sessions rejected; remote host
  events never relayed.
- Project scope: project list filtered; out-of-scope session id 403.
- Approval policy: low-risk allowed; high-risk 403; unknown tool high risk;
  WebAuthn assertion required in 1b.
- Audit: every action appended; retention; main-listener read endpoint.
- Ask journal: write-before-push, dedupe, restart rehydrate, stale expiry,
  clear push.
- Web Push: VAPID signing with a fake sender; `404`/`410` subscription removal;
  payload contains no content.
- Versioning: `companion_api: 1` present; mismatch behavior.
- Listener split: companion binds loopback, serves the mobile entry at `/`, no
  desktop fallback; main listener still serves desktop; `?token=` refused.

### 17.2 Web

- Pair flow: code entry, cookie success, expired code, lockout, revoked device.
- Sessions: project list, grouped sessions, filters, empty/loading/error.
- Session: streaming, reconnect, question card, low-risk ask, high-risk
  "Approve on desktop", composer, stop, retry, queue.
- Audit/settings: device name, project scope, notification enable, revoke.
- Version mismatch: writes blocked, update message.
- No desktop-only imports in the mobile entry.
- Phase 1b: manifest fields, service worker registration, push display, tag
  replacement, notification click, clear by tag, no `/api/*` caching.

### 17.3 Integration and manual

- Start `ocode serve --companion` with a temp HOME and a temp project.
- Pair a test client with a cookie jar; list projects and sessions; answer a
  low-risk ask; verify a high-risk ask is rejected.
- Revoke the device; verify the next request is 401.
- Restart the server; verify a pending ask is rehydrated and a stale ask is
  expired with a clear push.
- Test a denied `host`/`hosts` request and a denied `pulse_` session.
- Phase 1b: install the PWA in Chrome on Android; subscribe to push; trigger an
  ask; verify notification while the app is closed; resolve elsewhere; verify
  clear.
- If Phase 2 is triggered, run the TWA in Play internal testing before
  production.

## 18. Rollout

| Phase | Deliverable | Exit condition |
|---|---|---|
| 1a | Companion listener, direct pairing cookie, local-only scoped API, project scope, approval policy, audit, minimal mobile browser screens, Tailscale Serve | Answer a low-risk ask from the phone on the tailnet |
| 1b | PWA manifest, service worker, Web Push/VAPID, notification clear/dedupe, WebAuthn step-up | Notification arrives with the app closed; high-risk step-up works |
| 2 (conditional) | Hosted single origin, TWA, Digital Asset Links, AAB, Play internal testing | A hosted origin exists; internal track installs and passes review |
| 3 | Optional native shell or relay, only if validated | A demonstrated need Web Push/TWA cannot meet |

Phase 1a is independently useful and is the test surface for the rest. Phase 2
does not start for the self-hosted product; the PWA is installed directly.

## 19. Success criteria

- A new phone pairs in under one minute with a code from the desktop.
- A running session streams to the phone without a manual refresh.
- A low-risk ask can be answered from the phone and the agent continues.
- A high-risk ask cannot be approved from the phone in 1a.
- A push arrives for an ask while the app is closed (1b).
- Resolving an ask elsewhere clears its notification.
- A revoked device is denied on its next request.
- No reusable credential appears in a URL, log, push payload, or browser
  storage.
- The desktop and TUI behave exactly as before when companion mode is off.
- The phone works from the tailnet and, when deliberately exposed, through a
  public tunnel.

## 20. Deferred

- Wails or any native shell.
- FCM app integration and any Firebase project. (Chrome's Web Push transport
  is still used by the browser; ocode holds no Firebase credentials.)
- Hosted ocode relay and account system.
- Multi-server support in one app.
- Offline transcript cache.
- Turn-finished and turn-failed push.
- iOS.
- Voice input and speech output.
- File attachments and artifact previews.
- Terminal, file tree, git, MCP, and settings management from the phone.
- Team or shared-device access.
- Remote SSH host routing from the companion listener.

## 21. Review resolution

| Review finding | Resolution |
|---|---|
| FCM cannot serve a Play app with per-user servers | Replaced FCM with Web Push + per-server VAPID; FCM deferred. |
| Phone approval is agent-level capability | Added risk classes, desktop-only high-risk approvals in 1a, WebAuthn step-up in 1b, hiding yolo sessions, and an audit log. |
| Allowlist misses `host`/`hosts` | Companion listener is local-only; `host`/`hosts` and `pulse_` are rejected; scope matches method + path + query. |
| Login ticket is Phase 2 machinery | Removed the login ticket; direct pairing sets the HttpOnly cookie. |
| X-Forwarded-For is spoofable | Trust it only from loopback; add a global lockout per pairing code. |
| Missing `Origin` on cookie mutations | Missing or mismatched `Origin` is rejected. |
| Wails + Kotlin FCM is the riskiest path | Wails removed from v1; PWA now, TWA for Play, native shell deferred. |
| A single Play app cannot serve per-user self-hosted origins | TWA fixed start URL; Play deferred until a hosted origin exists, or a native server-URL shell. |
| Server host sleep/restart | Added the durable ask journal, restart rehydration, stale expiry, and reconnect behavior; host uptime is a documented operator requirement. |
| Notification clearing and dedupe | Added tags, clear pushes, durable `notified` state, and service-worker tag closing. |
| Device sees every session | Added per-device project scope, enforced on project list and session ids. |
| API versioning is only a number | Added `companion_api`, mismatch behavior, and a compatibility test. |
| Server list while multi-server is deferred | Removed the Servers screen; one PWA install is one server. |
| Offline IndexedDB | Removed offline transcript cache; the app shell may be cached, session data is not. |
| Turn-finished push | Removed; push is asks and questions only. |

## 22. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Web Push support varies by browser | No background push on some browsers | Target Chrome/Edge/Firefox; iOS later; foreground in-app notifications always work |
| PWA install is browser-dependent | Inconsistent install UX | Clear install prompt; TWA later only for a hosted origin |
| TWA Play review | Store rejection | Digital Asset Links, real functionality (push, pairing, audit), internal testing first |
| TWA fixed start URL | Cannot serve per-user servers | Keep Play conditional on a hosted origin; otherwise install the PWA directly |
| Host sleep or restart | Missed notifications, stale asks | Durable ask journal, reconnect/resync, documented uptime requirement |
| VAPID key loss | All push subscriptions invalid | Treat VAPID keys as durable server state; re-subscribe after rotation |
| Phone approval risk | Agent-level actions from a stolen phone | High-risk gating, audit, desktop alert, revocation |
| Scope drift | New route widens phone access | One method+path+query allowlist plus deny-by-default tests |
| Remote-host query leak | Phone reaches SSH hosts | Reject `host`/`hosts`; local-only tests |
| Service worker staleness | Old mobile shell after update | No-cache SW, build-hash cache names, update check |
| Web Push delivery is best effort | Missing notification | Ask remains visible in-app; no guarantee claimed |

## 23. Approval

This revision resolves the advisor review. The next step after approval is an
implementation plan, not code.
