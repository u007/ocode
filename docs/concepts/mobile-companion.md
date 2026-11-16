---
type: Concept
title: ocode Mobile Companion — design draft (remote phone client)
description: 'Draft, NOT implemented: a mockup-free design summary for a mobile-first companion — a second loopback listener in the same ocode process, pairing-code device cookie, local-only scoped API and SSE, low-risk-only approvals, durable ask journal, and Web Push/VAPID instead of FCM.'
resource: docs/superpowers/specs/2026-11-16-ocode-mobile-companion-design.md
tags:
  - server
  - mobile
  - companion
  - auth
  - push
timestamp: 2026-11-16T08:24:10Z
status: draft
---
# ocode Mobile Companion — design draft

> **Status: design draft, not implemented, pending user approval.** The
> normative document is the
> [mobile companion design spec](../superpowers/specs/2026-11-16-ocode-mobile-companion-design.md)
> (commit `f3b3f3c5`, spec only). This page is a short map of its load-bearing
> decisions so the design is discoverable from the bundle; if the two disagree,
> the spec wins. Every route, store and file name below is a proposal, not
> landed behavior.

## Shape

The phone is a **remote control**, not a second agent host. No agent runs on
Android: the companion lists sessions, streams the transcript, sends messages,
stops turns, and answers questions and **low-risk** permission asks. It gets no
file tree, editor, terminal, git UI, MCP manager, artifact manager, or browser.

One companion origin is one server. Multi-server support, a hosted relay, an
account system, team sharing, offline transcript cache, and iOS are out of
scope for v1. The desktop SPA and TUI are unchanged when companion mode is off.

## The listener split is load-bearing

- A **second `http.Server` in the same ocode process**, backed by the same
  `Server` and session state. Loopback only, default `127.0.0.1:4097`,
  configurable. Started by `ocode serve --companion` or Desktop Settings → Mobile.
- It serves only the mobile entry at `/`, `/assets/*`, `/api/health`, the
  scoped companion API, and pairing/push endpoints. It never serves the desktop
  `index.html`, the full API, LAN/Tailscale sharing, or the local-cert flow, and
  the desktop launch token is not valid there.
- The **main listener keeps the desktop SPA, the full API, and the launch
  token**. A tunnel is pointed at the companion port, never the main port —
  otherwise the desktop UI and full API sit behind one token.
- The tunnel must expose the **origin root**. A path-prefixed mount (for
  example `/rc/`) breaks the PWA scope, asset paths, and Digital Asset Links.

## Pairing and device credential

- A short-lived pairing code (10 chars, Crockford base32, 10 minutes, single
  use, hashed at rest, memory only) is exchanged for an `HttpOnly`, `Secure`,
  `SameSite=Lax`, `__Host-ocode_companion` cookie.
- **No login ticket, no token in a URL, no token in `localStorage`.** Page JS
  never sees the secret. Cookie-authenticated mutations require an exact
  `Origin` match; a missing `Origin` is rejected.
- The device credential is 32 random bytes, stored as a SHA-256 hash in
  `<GlobalDataDir>/companion/devices.json` with the shared lock plus
  atomic-write pattern. Revocation takes effect on the next request.
- Auth grows a **principal** (`launch` / `device` / `anonymous`) attached by
  `authMiddleware`; existing handlers keep their behavior.

## Scope is deny-by-default

The companion API allowlist matches **method + path + query**. It always
refuses:

- any request carrying `host` or `hosts`;
- any `pulse_…` session and any `yolo` session;
- any session or project outside the device's per-device project scope;
- files, git, terminal, config, models, MCP, artifacts, browser, and desktop
  control.

A session id alone is not enough: session-scoped routes resolve the session's
owning project through the session registry, never `h.workDir` (see
[Web/Desktop Server Project Scoping](web-server-project-scoping.md)). Device
scope is a subset of `allowedProjectRoots()` and never includes remote paths.

`GET /api/events` is local only: the remote-event relay is not enabled for a
companion request, and the stream is filtered by event family and by the
device's project scope. An event with no project identifier is dropped, not
guessed. A new event the phone needs still has to be classified in **both**
`sessionScopedEvents` and the replayable `liveFrameEvents` set — see the SSE
rules in
[Web/Desktop Server Locking and Liveness Rules](web-server-locking-and-liveness-rules.md).

## Approval policy

- The **server** classifies risk from the tool name and any existing risk hint;
  the client never infers it. Low risk is read-only tools; high risk is
  anything that mutates or executes, plus every unknown tool.
- **Phase 1a:** the phone may resolve low-risk asks and answer questions. A
  high-risk resolve returns `403 high_risk_requires_desktop` and the card says
  "Approve on desktop". `yolo` sessions are hidden; one that switches while open
  becomes read-only.
- **Phase 1b:** high-risk approvals need a WebAuthn platform-authenticator
  assertion bound to the device and the ask id.
- Every phone action is appended to `<GlobalDataDir>/companion/audit.jsonl`
  under a file lock (30 days or 10,000 entries).
- A paired phone is a **trusted control surface, not a boundary** against a
  compromised unlocked phone: it can read a transcript and send a message.
  Revocation is the mitigation.

## Push and durability

- **Web Push with per-server VAPID keys** (`companion/vapid.json`, mode 0600),
  `aes128gcm`; no FCM, no Firebase project, no ocode relay. Wails + FCM is
  rejected for v1 because FCM cannot target a Play app from each user's own
  server, and a WebView cannot use Web Push.
- Push fires for asks and questions only, never for turn-finished or
  turn-failed. Payloads are generic (no transcript, tool arguments, paths, or
  tokens), tagged `ask:<id>` so a repeat replaces rather than stacks, deduped
  per device and ask, and cleared by a `clear` push when resolved elsewhere.
- The durable ask journal (`companion/asks.json`) is written **before** the
  push; on restart pending asks are rehydrated and reconciled with live session
  state, and asks whose turn/tool call no longer exists expire. Push dedupe
  survives a restart through the journal's `notified` map.
- Phase 1a has no background push; its only notification is the in-app ask card.

## Distribution and reachability

- Phase 1a is a browser surface; Phase 1b is the installable PWA (service worker
  caches the shell and hashed assets, never `/api/*`).
- A Trusted Web Activity for Play is conditional on a **single hosted origin**
  because a TWA's start URL is fixed at build time; it cannot serve each user's
  self-hosted companion origin.
- Tailscale Serve to the companion port is the default private recipe;
  Tailscale Funnel, Cloudflare Tunnel, or Caddy are the public recipes. Binding
  the companion to a public interface, exposing the main listener, plain HTTP on
  a public address, `?token=` auth, and `host`/`hosts` routing are explicitly
  unsupported.

## Repo invariants this design must keep

The companion is a new surface on existing server state, so these rules are
scope, not optional:

- **`Handler.mu` stays a map lock.** Ask-journal writes, Web Push sends, and
  audit appends happen outside it; lock order is `agentSession.mu` → `h.mu`
  ([Web/Desktop Server Locking and Liveness Rules](web-server-locking-and-liveness-rules.md)).
- **A pending ask stays recoverable from live state.** The journal reconciles
  with `livePendingAsks` / `pending_asks`; resolution goes through the existing
  permission and question handlers, with no parallel answer path, and no
  re-`Step()` until every ask in the trailing tool round is resolved.
- **Project roots are per session, not per process**
  ([Web/Desktop Server Project Scoping](web-server-project-scoping.md)).
- **Storage and logs use the existing paths.** Companion stores live under
  `<GlobalDataDir>/companion/` with file locks and atomic writes; logs resolve
  through `paths.LogsDir()`.
- **Deployment config is not created by the spec.** VAPID key path and any
  companion port override belong in `.env.example` and the deployment
  environment, added by the implementation plan.

## Verification status

The spec verified Web Push delivery, VAPID, and the TWA fixed-start-URL
constraint against primary sources (§21.1). **Not verified:** Android WebView
Push API support (the design does not depend on it), Play review of a TWA or
Custom Tabs shell (Phase 2 must use internal testing), and the exact Go Web Push
library. No implementation exists yet; this page should move out of `draft` only
when Phase 1a lands and its behavior is verified against the code.
