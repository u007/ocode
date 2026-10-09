---
type: Design
title: Web/Desktop "Connectors" settings — TUI /connect parity — design
description: 'Design spec for web/desktop Connectors settings (TUI /connect parity): shared auth method catalog, credential-version invalidation, OAuth completion modes (auto/manual), security rules, and the as-built 2026-10-02 connect-flow fixes (committing gate, atomic beginInput, cancel-discards-credential, tail-only masking, 409 input authority).'
tags:
  - design
  - connectors
  - oauth
  - security
  - web
timestamp: 2026-10-02T02:51:21Z
---
# Web/Desktop "Connectors" settings — TUI `/connect` parity — design

Date: 2026-10-01
Status: proposed (server half committed 2026-10-01; §8 records the 2026-10-02 as-built fixes)

## Problem

TUI `/connect` (`internal/tui/connect.go`) is the only way to manage provider
credentials without hand-editing `~/.local/share/opencode/auth.json`. The web and
desktop Settings panels have nothing: `web/src/components/Settings/` has 38 form
components and not one for base credentials. `ProfilesManager.tsx` manages
per-profile overlays (`auth.profiles.json`), a different layer entirely.

The server half already exists and passes: `internal/server/handler_connect.go`
(1010 lines as of 2026-10-02) with 8 routes registered in `registerRoutes()`, and
`handler_connect_test.go` (16 test functions, plus `handler_connect_manual_test.go`
(4) and `handler_connect_cancel_test.go` (12), all green;
`go build ./internal/server/` clean). The web half did not exist at design time;
it landed with plan Phase 1 on 2026-10-01 (§5). This spec covers the web half
plus the four design decisions the existing server code forces us to confront.

### Provenance — resolved (2026-10-01/02)

When this spec was written, `handler_connect.go` and its test were **untracked
(`??`) and not written by the session that wrote this spec** (file mtimes
`14:32:27`/`14:35:59` against a session start of `16:15:47`, ~8 sessions writing
concurrently that afternoon). The risk — building on a file another agent might
still rewrite, with no git history to recover from — was closed by committing
the backend in `d81a7e58` (2026-10-01). The 2026-10-02 fixes in §8 sit on top of
that commit. The standing rule remains: re-run
`go test ./internal/server/ -run Connect` immediately before starting, because a
concurrent session may change the numbers above.

## Scope

Full TUI parity, base store (`auth.json`), full provider catalog. Phased — see
`docs/superpowers/plans/2026-10-01-web-connector-settings.md`.

## 1. OAuth completion mode depends on where the server runs

The browser never binds a port. The **server** binds the OAuth loopback listener
(`auth.StartOpenAIOAuth` listens on `127.0.0.1:1455`; Google on `:8080`),
browser navigates to the provider, and the provider redirects the *browser* to
`localhost`. That reaches ocode only when the browser's machine is the server's
machine.

| Server deployment | Browser location | `local-callback` works? |
|---|---|---|
| Desktop app | same machine | yes |
| `ocode serve` local, browser on same machine | same machine | yes |
| `ocode serve --remote` (SSH/WSL) | laptop | **no** — browser's `localhost` is the laptop |
| local server, browser on a phone/2nd device | other machine | **no** |

**Decision: one flow, two completion modes.** A `local-callback` flow is
registered with a `mode` chosen at start time:

- `auto` — the server owns the loopback listener (current behaviour). Offered
  only when `!remoteMode` and the request carries no `host=`.
- `manual` — the server does NOT bind; it returns the provider auth URL plus the
  PKCE verifier/state held server-side, and the user pastes the full redirect URL
  back through `POST /flows/{id}/input`. This is exactly the shape the TUI's
  Anthropic paste-code flow already uses, so it is proven, not invented.

`device-code` (Copilot), the Cloudflare account-id/gateway prompts, and plugin
`AuthMethod.Run` flows are host-agnostic and always work; they keep a single mode.

The start response must state which mode it chose and, for `manual`, carry the
residual caveat. The existing `note` field is the right vehicle.

Requires: `auth` gains non-binding variants of `StartOpenAIOAuth` /
`StartGoogleOAuth` that expose the auth URL instead of a listener.

## 2. Extract the method catalog — do not copy `connect.go`

`internal/tui/connect.go:251` (`buildMethods`) and
`internal/server/handler_connect.go` (`connectMethodsFor`) are near-identical
duplicates: same API-key-first ordering, same `providerplugin.Get` branch, same
`OAuthFlow` switch, same Grok subscription special case, same comments. They will
drift.

**Decision: one shared implementation in `internal/auth`.** A `MethodsFor(*Provider)`
returning `[]Method{ID, Label, Kind}` where `Kind` is `apikey | oauth | plugin |
remove` and is *derived once*, not re-derived per caller. The TUI appends its
extra `cancel` entry locally; the server does not. The TUI keeps its
`connectMethod` shape by converting at the boundary.

`Status(provider)` and the flow-step signatures move with it, so the TUI's
✓/✗ + detail rendering and the server's list payload cannot disagree.

## 3. A new key does NOT reach live agents today — this is a bug to fix

`NewClientWithProfile` (`internal/agent/client.go:4590`) resolves the credential
**at client construction**: `auth.Get(provider)` at :4685,
`auth.ResolveKeyForProfile` at :4808/:4828. An agent constructed before a
connector write keeps the old key until it is rebuilt.

There is a next-turn rebuild path — `reconcileProfileAgent`
(`internal/server/agent_session.go:657-698`) compares `as.credVersion` against
`auth.CredentialVersion()` and rebuilds on a mismatch. Its comment
(:678-680) states the intent outright: *"The credential version is global, not
per-profile: an in-place edit must invalidate the cached client."*

**The intent was not met for the base store (design-time state; fixed —
as-built note below).** `auth.Set` and `auth.Remove`
(`internal/auth/store.go:373`/`:393`) mutate the cache and call `persistLocked`,
and at the time **never bumped `profileVersion`** — that counter lives in
`internal/auth/profile_store.go` and was bumped only by profile mutations. So a
write through `PUT /api/auth/connect/{provider}` left every live session on the
old key until some unrelated model or profile change happened to rebuild it.

**Decision: one credential version, bumped by both stores.** Add a base-store
counter bumped from `persistLocked` (the single choke point every base write
passes through) and have the reconcile check compare a single
`auth.CredentialVersion()`. `ProfileCredentialVersion()` is `internal/`-only with
8 references, so fold it in rather than adding a second counter callers must
remember to read. Fire `OnCredentialsSaved` as today — it is what
`internal/sync` already watches, and one path is better than two.

**As-built (2026-10-01, plan item 5).** Implemented with one correction to the
decision above: the bump lives directly in `Set`/`Remove`
(`internal/auth/store.go:388` / `:407`), NOT in `persistLocked` — that also runs
on the seed-on-load path that materialises an empty `auth.json` without changing
a credential, which would invalidate every agent for a no-op write (14 tests
failed until it moved). The reconcile check now compares the single
`auth.CredentialVersion()` (`internal/server/agent_session.go:681`);
`ProfileCredentialVersion()` remains as a delegating alias.

Prompt-cache impact is nil: rebuilding an `LLMClient` touches neither the `tools`
array nor the cached system prefix, so `docs/concepts/prompt-cache-stability.md`
is unaffected. Rebuild happens on the **next turn**, never mid-turn — the same
contract as a model switch.

## 4. Security rules

- **No endpoint returns a secret.** `handleConnectList` masks; pinned by
  `TestConnectListMasksStoredKeys`. Since 2026-10-02 the mask shows **only the
  trailing 4 characters, and only for keys ≥ 16 chars** — anything shorter is
  masked whole (`maskConnectCredential`); the old 4+4 window rendered a 9-char
  key as `1234••••6789`, disclosing 8 of 9 characters. Pinned additionally by
  `TestMaskConnectCredentialNeverRevealsTheHead`. `GET /flows/{id}` returns
  `f.snapshot()`, an explicit `map[string]interface{}` — the PKCE verifier and
  OAuth state are unexported struct fields, so they cannot leak by construction.
  **Pin both.**
- **Middleware:** `s.authMiddleware` for all 8 routes, deliberately: these mutate
  a machine-global credential file, not project-scoped state. Never
  `healthMiddleware`. Wrapper choice is a documented decision per
  `concepts/web-server-locking-and-liveness-rules.md`.
- **Machine-global, not project-scoped.** No `?project=`, no
  `allowedProjectRoots()` validation, never read `h.workDir`, never
  `os.Getwd()` — per `concepts/web-server-project-scoping.md`. These routes are
  about the machine's credentials, not a project's files.
- **Never hold `h.mu` across a flow probe or the "test key" call.** Already
  satisfied: `handler_connect.go` contains zero `h.mu` references, and
  `handleConnectTest` runs `testCredentialFn` (network) with no lock held.
  Keep it that way when the OAuth mode logic lands.
- **Write serialisation is already correct.** `auth.Set` / `auth.Remove`
  (`internal/auth/store.go:373`/`:393`) take `storeMu` and `persistLocked` writes
  the whole file under that lock, so the read-modify-write is atomic. No new lock
  needed.
- **Per-host credentials, never a local fallback.** A remote project's agent runs
  on the host, so its key must come from the host's `auth.json`. `/api/remote/{host}`
  proxies to the host's `serve --remote`, where this same handler runs against the
  host's store — so the correct answer is to require `host` threading, and to
  refuse (never silently fall back) rather than answer from local state.

## 5. What exists vs what is missing

Server, done and green: `GET /api/auth/connect`,
`PUT|DELETE /api/auth/connect/{provider}`,
`POST /api/auth/connect/{provider}/test`,
`POST /api/auth/connect/{provider}/oauth/start`,
`GET /api/auth/connect/flows/{flowId}`,
`POST /api/auth/connect/flows/{flowId}/input`,
`DELETE /api/auth/connect/flows/{flowId}`.

Web, landed 2026-10-01 (plan Phase 1): client methods for all 8 routes
(`listConnectProviders`, `setConnectCredential`, `removeConnectCredential`,
`testConnectCredential`, `startConnectFlow`, `getConnectFlow`,
`submitConnectFlowInput`, `cancelConnectFlow`), `ConnectorsForm.tsx`, and the
`SettingsPanel` section — `connectors` sits immediately before `profiles`
(`SettingsPanel.tsx:78`) — base creds are the layer beneath profile overlays. The
legacy `api.connectProvider` (`client.ts:2885`) is kept deliberately: it targets
a different, older Server-level route.

2026-10-09: the web surfaces for the remaining kinds landed. `ConnectFlowPanel` renders
`cookies` (Grok `auth_token`/`ct0` fields), `device-code` (the user code and
verification URI), and `plugin` (instructions, then polling). `ConnectorsForm` sends
Cloudflare's `accountId` (Workers) and `baseUrl` (AI Gateway) with the key. Google
manual completion is still not started.

## 6. Test obligations

Per endpoint in `handler_connect_test.go`. Plus, new: TUI/server method-catalog
parity; no-secret-leak on the flow-status payload; web flow lifecycle for all
five flow kinds; `host` threading including the remote refusal; and a base-store
credential write invalidating a live agent (the §3 bug). 2026-10-02: the
cancel/commit lifecycle, the input double-submit race, and the mask-head rule
(`handler_connect_cancel_test.go`, 12 tests).

## 7. Documentation obligations

Checked against the bundle (2026-10-01). No existing page contradicts this design,
and three gaps are ours to close in the new `concepts/` page (via the `context`
agent):

- **No page forbids returning raw keys.** The masked-key contract has to be
  stated in the new page; nothing today carries it.
- **`concepts/data-storage-layout.md` lists `auth.json` under `GlobalDataDir()`
  and mentions `auth.profiles.json` separately.** The new page must state the
  boundary explicitly so Connectors (base) and Profiles (overlays) are not blurred
  — the same split as `gotchas/profile-switch-window-id-divergence.md`.
- **No page covers OAuth flow endpoints or TUI `/connect` parity.** The new page
  is the first.

## 8. As-built: 2026-10-02 security/correctness fixes

Three defects in the shipped server half were fixed and regression-tested on
2026-10-02. All three change wire-visible behaviour, so clients and tests must
match the descriptions below, not the original design.

### 8.1 Cancelling no longer persists, and commit is exclusive

- New flow state `connectFlowCommitting`. `completeConnectFlow` now takes the
  flow's `ctx` and refuses to persist unless `ctx.Err() == nil` AND
  `f.beginCommit()` wins — which only succeeds from `running` or
  `waiting_browser`. A cancelled or already-finished flow loses the claim and its
  credential is DISCARDED; `waiting_input` is deliberately not claimable (no
  exchange was ever started there).
- Rationale: cancelling the Anthropic, Google and manual-OpenAI flows was a
  complete no-op (no cancel func existed) yet `completeConnectFlow` still called
  `auth.Set` — a cancelled connect still persisted its credential.
- New helpers: `beginInput(cancel)` — an atomic waiting_input→running
  compare-and-set that installs the cancel func under the same lock, replacing a
  TOCTOU pair of separate lock acquisitions — plus `setCancel`,
  `takeCancel` (single-use), `markCancelled` (never overwrites a terminal state),
  and `runConnectExchange(ctx, f, providerID, exchange)` for the Anthropic /
  Google / manual-OpenAI exchanges, which take NO context: it runs the exchange
  on one goroutine and the wait on a second, dropping the credential on cancel.
- `handleConnectFlowCancel` returns **409** while `committing` ("this flow is
  saving its credential and can no longer be cancelled").
- Pinned by `handler_connect_cancel_test.go` (12 tests), including
  `TestConnectFlowCancelDiscardsAnthropicCredential`,
  `TestConnectFlowCancelDiscardsManualOpenAICredential`,
  `TestConnectFlowCancelDiscardsGoogleCredential`,
  `TestConnectFlowCancelRefusedOnceCommitting`,
  `TestBeginCommitOnlyFromExchangeBackedStates`,
  `TestConnectFlowCancelRacesInputHandler`, and
  `TestCompleteConnectFlowRefusesCancelledFlowWithLiveContext`.

### 8.2 Masked key never reveals the head

`maskConnectCredential` shows only the trailing 4 characters, and only when
`len(key) >= 16`; anything shorter is masked whole. The old 4+4 window on a
9-character key rendered `1234••••6789`, disclosing 8 of 9 characters. Pinned by
`TestMaskConnectCredentialNeverRevealsTheHead` and the updated
`TestConnectListMasksStoredKeys` (expects `••••••••••••mnop` for a 16-char key).

### 8.3 Input endpoint: `beginInput` is the single authority

`handleConnectFlowInput` no longer has an up-front `isWaitingInput()` check —
`beginInput`'s compare-and-set is the authority for every branch. Consequences:

- Pasting input into a flow whose state does not accept it now yields **409
  Conflict** from `beginInput` rather than 400 Bad Request; the loser of a
  double-submit is told `flow is <state>, it already started` and never starts a
  second exchange (`TestConnectFlowInputDoubleSubmitStartsOneExchange`).
- A device-code flow (which has no pasted-input branch) reports **400 "this flow
  does not accept pasted input"** from the default case.
- The old up-front check was a SEPARATE lock acquisition from the
  `setState(running)` that followed, so two concurrent POSTs (a double-click or a
  retried client) both passed it and both started an exchange; `beginInput` also
  removes the unlocked cancel-func write that raced the cancel handler.

`handler_connect.go` grew from 782 to 1010 lines across these fixes; the route
list in §5 and the masking contract in §4 are the wire-level contracts to keep.