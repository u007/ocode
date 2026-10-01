# Web/Desktop "Connectors" settings — TUI `/connect` parity — design

Date: 2026-10-01
Status: proposed (server half already implemented, uncommitted)

## Problem

TUI `/connect` (`internal/tui/connect.go`) is the only way to manage provider
credentials without hand-editing `~/.local/share/opencode/auth.json`. The web and
desktop Settings panels have nothing: `web/src/components/Settings/` has 38 form
components and not one for base credentials. `ProfilesManager.tsx` manages
per-profile overlays (`auth.profiles.json`), a different layer entirely.

The server half already exists and passes: `internal/server/handler_connect.go`
(782 lines) with 8 routes registered in `registerRoutes()`, and
`handler_connect_test.go` (16 test functions / 31 including subtests, all green;
`go build ./internal/server/` clean). The web half does not exist. This spec
covers the web half plus the four design decisions the existing server code
forces us to confront.

### Provenance — READ BEFORE BUILDING ON IT

`handler_connect.go` and its test are **untracked (`??`) and were not written by
the session that wrote this spec.** File mtimes are `14:32:27` / `14:35:59`;
this spec's session started `16:15:47`, and the project's session directory
shows ~8 other sessions writing concurrently this afternoon (the nearest,
`ses_2026-10-01-125917-851d50cc`, last wrote `14:30:18` — two minutes before the
file appeared).

So this is another agent's in-flight work, and untracked means **no git history
to recover from**. Before implementing Phase 1+:

1. Confirm with the user that `handler_connect.go` is intended to land, or
   coordinate with the owning session. Do not silently build a web client on top
   of a file another agent may still be rewriting.
2. If it is landing, commit it first. A committed 782-line backend with 16 tests
   is a safe foundation; an untracked one is not.
3. Re-run `go test ./internal/server/ -run Connect` immediately before starting —
   the numbers above were measured at 2026-10-01 ~16:30 and a concurrent session
   may change them.

## Scope

Full TUI parity, base store (`auth.json`), full provider catalog. Phased — see
`docs/superpowers/plans/2026-10-01-web-connector-settings.md`.

## 1. OAuth completion mode depends on where the server runs

The browser never binds a port. The **server** binds the OAuth loopback listener
(`auth.StartOpenAIOAuth` listens on `127.0.0.1:1455`; Google on `:8080`), the
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

`NewClientWithProfile` (`internal/agent/client.go:4559`) resolves the credential
**at client construction**: `auth.Get(provider)` at :4685,
`auth.ResolveKeyForProfile` at :4808/:4828. An agent constructed before a
connector write keeps the old key until it is rebuilt.

There is a next-turn rebuild path — `reconcileProfileAgent`
(`internal/server/agent_session.go:675-698`) compares `as.credVersion` against
`auth.ProfileCredentialVersion()` and rebuilds on a mismatch. Its comment
(:678-680) states the intent outright: *"The credential version is global, not
per-profile: an in-place edit must invalidate the cached client."*

**The intent is not met for the base store.** `auth.Set` and `auth.Remove`
(`internal/auth/store.go:372`/`:384`) mutate the cache and call `persistLocked`,
and **never bump `profileVersion`** — that counter lives in
`internal/auth/profile_store.go` and is bumped only by profile mutations. So a
write through `PUT /api/auth/connect/{provider}` leaves every live session on the
old key until some unrelated model or profile change happens to rebuild it.

**Decision: one credential version, bumped by both stores.** Add a base-store
counter bumped from `persistLocked` (the single choke point every base write
passes through) and have the reconcile check compare a single
`auth.CredentialVersion()`. `ProfileCredentialVersion()` is `internal/`-only with
8 references, so fold it in rather than adding a second counter callers must
remember to read. Fire `OnCredentialsSaved` as today — it is what
`internal/sync` already watches, and one path is better than two.

Prompt-cache impact is nil: rebuilding an `LLMClient` touches neither the `tools`
array nor the cached system prefix, so `docs/concepts/prompt-cache-stability.md`
is unaffected. Rebuild happens on the **next turn**, never mid-turn — the same
contract as a model switch.

## 4. Security rules

- **No endpoint returns a secret.** `handleConnectList` masks; pinned by
  `TestConnectListMasksStoredKeys`. `GET /flows/{id}` returns `f.snapshot()`, an
  explicit `map[string]interface{}` — the PKCE verifier and OAuth state are
  unexported struct fields, so they cannot leak by construction. **Pin both.**
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
  (`internal/auth/store.go:372`/`:384`) take `storeMu` and `persistLocked` writes
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

Web, missing: no client methods for any of the 8 (the `api.connectProvider` at
`client.ts:2828` targets a different, older Server-level route), no
`ConnectorsForm.tsx`, no `SettingsPanel` section. `connectors` goes immediately
before `profiles` (`SettingsPanel.tsx:76`) — base creds are the layer beneath
profile overlays.

## 6. Test obligations

Per endpoint in `handler_connect_test.go`. Plus, new: TUI/server method-catalog
parity; no-secret-leak on the flow-status payload; web flow lifecycle for all
five flow kinds; `host` threading including the remote refusal; and a base-store
credential write invalidating a live agent (the §3 bug).

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
