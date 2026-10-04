---
type: Plan
title: Web/Desktop Connectors settings — implementation plan
description: 'Implementation plan for web/desktop Connectors settings (TUI /connect parity): shared method catalog, credential-version invalidation, client methods/forms, OAuth flow wiring; records the 2026-10-02 connect-flow correctness fixes (commit gate, beginInput single authority, cancel-drops-credential, tail-only masking). Phase 0–1 done; Phase 2 partial.'
tags:
  - plan
  - connectors
  - oauth
  - web
timestamp: 2026-10-01T18:14:06Z
---
# Web/Desktop Connectors settings — implementation plan

Spec: `docs/superpowers/specs/2026-10-01-web-connector-settings-design.md`

Server half was already built by another session (`internal/server/handler_connect.go`)
and is green. **Phase 0 and ALL of Phase 1 are DONE** (2026-10-01). Phase 2
(OAuth flows) is the remaining work.

A concurrent session is building the same feature at the same time, and also
edits this file. Treat every file it owns as read-only until it lands: as of
2026-10-01 it has written `handler_connect.go`, the connector methods in
`client.ts`, `ConnectorsForm.tsx` and both web test files, and
`internal/auth/google.go` / `openai_oauth.go`. Items marked `[x]` with a
concurrent-session note were verified, not written, by this session:
`npx vitest run src/components/Settings/` (17 files / 98 tests),
`npm run typecheck`, and `npx vite build` all green.

**2026-10-02 — connect-flow correctness fixes landed** (details in spec §8): the
commit gate (`connectFlowCommitting` — a cancelled flow's credential is
discarded; cancel is refused 409 while committing), `beginInput` as the single
input authority (409, not 400, for a state that will not accept input), cancel
actually cancelling the Anthropic/Google/manual-OpenAI exchanges
(`runConnectExchange` runs their context-free exchanges under a cancellable
wait), and tail-only credential masking (≥16-char keys show their last 4;
shorter masked whole). Tests: `handler_connect_cancel_test.go` (12).

## Phase 0 — gate, then shared catalog (DRY)

0. ~~**GATE: settle `handler_connect.go` provenance.**~~ Approved 2026-10-01. A
   baseline copy is in `/tmp/connect-baseline/` for this session only; the file
   was untracked then and **needs a commit before it can be relied on** —
   resolved: committed in `d81a7e58` (2026-10-01).
1. [x] `internal/auth/methods.go`: `Method` (`ID`, `Label`, `Kind`) +
   `MethodsFor(*Provider)`. `Status` was already shared in `providers.go`, so
   only the methods needed extracting.
2. [x] Both callers converted. The TUI keeps its `connectMethod` shape and
   appends `cancel`; the server adapts to the wire shape.
3. [x] Parity tests: `internal/auth/methods_test.go` (catalog contract),
   `internal/server/handler_connect_methods_parity_test.go` and
   `internal/tui/connect_methods_parity_test.go` (each adapter vs the catalog,
   every provider, stored and unstored). All three mutation-verified.
4. [x] `auth.Status` already fed the server list payload and the TUI row
   rendering, so no change was needed; covered by the existing
   `TestConnectList*` tests.

## Phase 1 — status + API keys (covers most providers)

5. [x] **Base-credential invalidation fixed, test-first** (spec §3). Failing
   tests first (`internal/auth/credential_version_test.go`,
   `internal/server/agent_session_base_cred_test.go`), then the fix: one
   `auth.CredentialVersion()` covering both stores, with
   `ProfileCredentialVersion()` kept as a delegating legacy name. Both test
   files re-verified by reverting the bump — they fail with an explicit
   "resident agent was not rebuilt" message, so the guard is real rather than
   incidental.

   Two corrections found by running the FULL `internal/server` suite, not the
   focused tests:
   - **The bump belongs in `Set`/`Remove`, not `persistLocked`.** The plan
     called `persistLocked` the single choke point; it is not, because it also
     runs on the seed-on-load path that materialises an empty `auth.json`
     without changing a credential. Bumping there invalidated every agent in
     the process for a no-op write — 14 unrelated tests failed until it moved.
   - **Test sessions must snapshot the version.** `agentSession.credVersion` is
     load-bearing and a hand-built `&agentSession{...}` leaves it 0, which
     `reconcileProfileAgent` reads as "credentials changed". Once base writes
     moved the counter, any earlier credential write anywhere in the binary made
     those sessions spuriously rebuild — and a `fake-model` session cannot
     survive a rebuild. `newTestSession` now snapshots it, plus the four
     literals that bypass the helper.

   LESSON worth carrying: the focused tests passed while the full suite had 14
   real regressions. The decisive check was comparing FAIL SETS against a run
   with the change reverted (`comm` on sorted `--- FAIL` lines), because both
   directions of that comparison are cheap and it is the only way to separate
   "I broke it" from "this suite is flaky at HEAD".
6. [x] `web/src/api/client.ts`: methods for the 8 routes, every one taking a trailing
   `host`. (`listConnectProviders`, `setConnectCredential`, `removeConnectCredential`,
   `testConnectCredential`, `startConnectFlow`, `getConnectFlow`,
   `submitConnectFlowInput`, `cancelConnectFlow`, plus the `Connect*` wire types. The
   legacy `connectProvider` is kept and PINNED by a test — it targets a different,
   older route (`POST /api/auth/connect`). Keep the old `connectProvider` (different route) until its caller moves.
7. [x] `web/src/components/Settings/ConnectorsForm.tsx`: provider list with live
   status, filter/search (the catalog is large), method picker, API-key entry,
   remove, and a `Test` action. Gate remove behind `common/ConfirmDialog` —
   native `confirm()` silently returns false in the desktop webview
   (`skills/ocode-web` item 37).
8. [x] `SettingsPanel.tsx`: `{ id: "connectors" }` immediately before `profiles`;
   render `ConnectorsForm` — with NO `host`, because settings are a global surface
   by convention. The decision is pinned by a test and documented at the prop, so
   it is not "fixed" later by someone reading the prop as unused.
9. [x] Tests: form + `SettingsPanel` section; `host` threading; remove-confirm; and
   a base-store write invalidating a live agent on its next turn (the last one is
   task 5's Go test). 39 new web tests across three files, all mutation-verified.
10. [x] `skills/ocode-web/SKILL.md` file map + item 59; `CHANGES.md` entry.

   **Two of MY OWN tests were wrong before the code was.** The client test asserted
   `fetch` received three arguments, which measures `fetchJSON`'s internals, not
   whether `host` was threaded — it now asserts the remote-proxy prefix appears
   with a host AND is absent without one, which is the observable contract. The
   form test called `.getByTestId` on a raw element instead of `within(...)`.
   Worth noting because both "failed" against correct code, and one of them would
   have been easy to "fix" by changing the implementation to satisfy nonsense.

   A third mutation attempt also misfired: `host ? host : undefined` is an
   EQUIVALENT mutant, so its survival said nothing about coverage. Re-running it
   as `host ?? "stale-host"` caught it immediately.

## Phase 2 — OAuth, one flow at a time

11. [x] Anthropic paste-code — already shipped in the server handler.
12. [~] **PARTIAL — the auth layer of manual completion is done; the server
    wiring is blocked on file ownership.** `internal/auth/openai_oauth_manual.go`
    (new file, this session) provides `OpenAIManualFlow`,
    `StartOpenAIOAuthManual()` (builds the authorize URL, binds NO port) and
    `ExchangeOpenAIManual(f, pasted)`; TDD'd in
    `openai_oauth_manual_test.go` and mutation-verified (removing the state check
    is caught). A bare code is REFUSED, not accepted: it carries no state, so it
    cannot be validated, and accepting it would let an attacker-supplied code be
    redeemed on the user's machine. One test was corrected mid-implementation —
    it originally asserted the opposite, and the implementation was right.

    **Server wiring: DONE** (2026-10-01, after `handler_connect.go` had been
    stable for ~3.75h). `startOpenAIConnectFlow` now takes a `mode`; the client
    sends it, because only the client knows whether the browser shares the
    server's host — `remoteMode` lives on `*Server` and these routes are
    `s.handler.*`, so the handler cannot read it. An absent or unrecognised
    `mode` is `auto`, so existing behaviour is unchanged.
    `handleConnectFlowInput` gained a `connectFlowLocalCallback` case, and
    `api.startConnectFlow` takes a trailing `mode` argument. Tests:
    `internal/server/handler_connect_manual_test.go` (4; mutation-verified —
    ignoring `mode` fails 3 of them).

    Two things the implementation settled:
    - **The auto/manual separation is enforced by flow STATE, not by a guard.**
      An auto flow is `waiting_browser`, so a `local-callback` + `waiting_input`
      flow can only be a manual one. (Originally this note relied on "the
      `isWaitingInput()` check above the switch already rejects a paste" —
      **2026-10-02: that up-front check is gone.** `beginInput`'s
      waiting_input→running compare-and-set is now the single authority: a paste
      into a state that will not accept it gets **409** from `beginInput`, and an
      auto flow stays `waiting_browser`, so it can never pass the CAS.) My first
      version added a defensive `f.openaiManual.State == ""` check; the mutation
      removing it SURVIVED, and on inspection it was provably unreachable (only
      the manual starter creates that combination, and it always sets State) — an
      equivalent mutant, not a coverage gap. Deleted, with the invariant written
      down in its place.
    - **The auto start response now reports `state`.** It did not, so a client
      could not branch on the completion mode without a second poll; the manual
      branch always did. Symmetric now.

    Google manual mode is still not started; it additionally needs user-supplied
    client credentials. Do not hold `h.mu` while deciding or while the flow runs
    — the file has zero `h.mu` references and that is the invariant.
13. OpenAI then Google `local-callback` in the web UI, both modes.
14. Copilot device code (host-agnostic, single mode) with polling.
15. Flow UI: one component per `connectFlowKind`, driven by
    `GET /flows/{flowId}` polling + `POST /flows/{id}/input` + `DELETE` cancel.
16. Tests: flow lifecycle per kind; the `auto`/`manual` selection incl. the remote
    case; cancel. — **cancel: DONE 2026-10-02** (`handler_connect_cancel_test.go`,
    12 tests: cancel discards the credential for all three context-free
    exchanges, 409 while committing, double-submit starts exactly one exchange,
    the mask never reveals a key's head).
17. Test: `GET /flows/{id}` never returns the PKCE verifier or OAuth state.

## Phase 3 — the odd providers

18. Grok x.com cookies (UI collects the cookies, posts to `flows/{id}/input`).
19. Cloudflare account-id / gateway prompts.
20. Generic plugin `AuthMethod.Run` dispatch rendering.

## Phase 4 — cross-cutting

21. Remote refusal: a `host=`, `remoteMode` request must never answer from local
    `auth.json`. Test the refusal.
22. Docs: a `concepts/` page for the Connectors section (via the `context`
    agent), stating the masked-key contract and the base-vs-profile store
    boundary — neither is documented anywhere today (spec §7).

## Notes

- Every client method takes a trailing `host`. Any `toHaveBeenCalledWith`
  assertion on one must include the explicit `undefined` — vitest distinguishes
  two args from three-with-undefined.
- Never render a stored key. Status and masked values only (since 2026-10-02:
  the mask is tail-4-only for keys ≥ 16 chars; shorter keys are masked whole).
- `docs/` is bundle-owned: route the concept page through the `context` agent;
  this plan and the spec are plain files and may be edited directly.