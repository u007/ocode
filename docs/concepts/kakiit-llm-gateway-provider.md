---
type: Concept
title: kakiit LLM Gateway Provider
description: The `kakiit` provider — auth and base-URL override chain, live-only /v1/models catalog (no models.dev), per-model protocol routing with the refusing-to-guess rule, the sticky session header plus the SINGLE-USE per-turn request id, and the 402 top-up / 409 duplicate-id mappings (which fire on all three transports and reach the web/desktop banner); includes the provider registration sites that adding a provider must touch.
resource: internal/agent/client_kakiit.go
tags:
  - provider
  - kakiit
  - llm
  - gateway
  - models-registry
  - auth
timestamp: 2026-11-27T16:04:13Z
---
# kakiit LLM Gateway Provider

ocode ships a first-class provider for the **kakiit LLM gateway** — the
multi-tenant LLM service at `https://hub.mercstudio.com/api/llm` (kakiit repo,
`src/server/llm/`, staged M1–M4). Provider id `kakiit`, model ids
`kakiit/<model>`. It is OpenAI/Anthropic-compatible **per model**, not per
provider: the gateway declares each model's upstream protocol in a vendor
extension, and ocode routes each request accordingly through the *existing*
dispatch — no forked transport.

Committed in `8ab8d978` ("feat(agent): kakiit LLM gateway provider",
2026-11-20); the single-use request-id contract and its 409 mapping landed in
`e607214c` (gateway side `92f3b276`), and `590971e6` made that mapping fire on
all three transports and reach the web/desktop banner. Everything in this doc is verified
against the current tree; all behaviour is pinned by httptest stubs, and **no
live end-to-end call has been made yet** (the gateway is not deployed — see
"Deferred / open").

## Setup and credential

- **Connect:** `ocode /connect kakiit` → stores an auth.json `kakiit` entry
  (API-key credential). Env fallback: `KAKIIT_API_KEY`
  (the kakiit entry in the `Providers` slice, `internal/auth/providers.go` — and the
  providers map `internal/agent/client.go:4616`). Key resolution is the
  standard order every keyed provider follows: env var wins over auth.json
  (pinned by `TestKakiitAPIKeyResolutionPrecedence`).
- **Keyless is a typed refusal, not a 401 later.** kakiit is deliberately NOT
  in `keyOptionalProviders` (`internal/agent/client.go:4548-4553` — only
  opencode/opencode-go/lmstudio/local), so `NewClient` returns nil when no
  credential exists (`client.go:5029-5049`, pinned by
  `TestKakiitNoCredentialYieldsClearTypedError`). A keyless client would 401
  on its first request; callers (model switch, `ResolveSmallModel`,
  compaction client) skip instead.
- **Base URL.** Default `KakiitDefaultBaseURL` =
  `https://hub.mercstudio.com/api/llm` (`internal/agent/client_kakiit.go:25`).
  `KakiitBaseURL()` (`client_kakiit.go:233-241`) resolves:
  `KAKIIT_BASE_URL` env > `kakiit_base_url` (ocodeconfig.json,
  `internal/config/ocodeconfig.go:1265-1270`) > default.
- **The two base-URL paths disagree on where the auth.json per-credential
  override sits — deliberately.**
  - *Chat* (`NewClientWithProfile`, `client.go:4922-4930`): the standard
    per-credential override is applied first, then the kakiit branch overrides
    it when env/config is set — so env/config **win** over auth.json for chat.
    (Same shape as LM Studio's `LMSTUDIO_BASE_URL`.)
  - *Models fetch* (`kakiitModelsURL`, `models_registry_kakiit.go:156-164`):
    the per-credential override is applied **after** env/config, so auth.json
    **wins** for `/v1/models`. A credential that pins its own gateway root
    lists models from that root even when a dev env var points elsewhere.
  - Do not "fix" this asymmetry: the chat path mirrors every other
    provider's env-override pattern, and the models path keeps a stored
    credential authoritative for its own catalog.
- `.env.example` documents both vars (`.env.example:46-47`).

## Live-only model catalog

kakiit is **deliberately absent from models.dev and the embedded snapshot**:
the gateway owns the catalog, and prices/protocols can change server-side at
any moment. `providerModelsFromRegistry("kakiit", …)` therefore returns the
live list and never falls back to the snapshot
(`internal/agent/models_registry.go:1412-1418`); `allProviderModelsFromRegistry`
merges the live `kakiit/` ids into the picker list
(`models_registry.go:1267-1282`).

- **Source:** `GET {BaseURL}/v1/models` with the bearer key. Entries carry the
  vendor extensions ocode consumes:
  - `x_kakiit_protocol` — `anthropic-messages` | `openai-responses` |
    `openai-chat` (wire literals; never rename —
    `models_registry_kakiit.go:294-298`)
  - `x_kakiit_free` — free models report zero price
  - `context_length`, `pricing{prompt,completion}` (USD per 1M, string, the
    OpenRouter/models.dev convention) — parsed into
    `pricing.ModelPricing` (`models_registry_kakiit.go:37-57`)
- **Cache:** one in-process cache (`kakiitLiveData`), 5-minute TTL
  (`kakiitCacheTTL`, same value as the models.dev/OpenRouter caches). On the
  TUI main loop only a still-fresh cache is consulted (`refresh=false`), so
  the picker never blocks on the gateway; `PreloadKakiitModels()`
  (`models_registry_kakiit.go:283-285`, via `crashguard.Go`) warms it at TUI
  Init (`internal/tui/model.go:3146`) and server Serve
  (`internal/server/server.go:2532`).
- **Failure behaviour is split by cause:**
  - *Fetch failure / malformed body / empty list* → nil list + one debug line;
    the picker section is simply empty (`TestKakiitFetchFailureYieldsEmptyListAndDebugLineNotStaleFallback`).
    Never a stale or models.dev fallback — a wrong list is worse than none.
  - *401/403* → recorded in `KakiitModelsAuthError()`
    (`models_registry_kakiit.go:74-78`) so a rejected key reads as "your key
    is bad", not as "no models" (`TestKakiitRevokedKeyYieldsAuthErrorNotSilentEmptyList`).
  - *No credential configured* → no fetch at all (nothing to list).
- **Exported, currently test-only surfaces** (for the deferred picker chrome,
  see below): `KakiitModelCost` / `KakiitModelWindow` (full-id pricing/window
  accessors; free ⇒ zero price), `KakiitModelsLoaded`,
  `KakiitModelsAuthError`. No non-test caller yet — `ModelCost`'s models.dev
  path does not cover kakiit, so any future ledger/gauge consumer must call
  the kakiit accessors.

## Per-model protocol routing

The gateway serves three upstream protocols and declares which one each model
speaks via `x_kakiit_protocol`. ocode routes through the **existing dispatch**
(`ChatWithContext`, `internal/agent/client.go:884-892`):

| `x_kakiit_protocol` | Transport | Endpoint (via `apiRoot()`) |
|---|---|---|
| `anthropic-messages` | `chatAnthropic` (via `usesAnthropicMessagesAPI`, `client.go:763-796`) | `{BaseURL}/v1/messages` |
| `openai-responses` | `chatOpenAIResponses` (guard-first branch in `chatOpenAI`, `client.go:1482-1495`) | `{BaseURL}/v1/responses` |
| `openai-chat` | `chatOpenAI` fall-through | `{BaseURL}/v1/chat/completions` |

- **`apiRoot()` inserts the `/v1` segment for kakiit**
  (`client_kakiit.go:176-181`): the declared BaseURL is the gateway ROOT
  (…/api/llm), unlike `api.openai.com/v1`-style providers whose map entry
  already carries the version. All three transports append their suffix to
  `apiRoot()` (`client.go:1513, 3824, 3967`), so one helper keeps them
  consistent.
- **Refusing to guess.** A model absent from the cache (fetch never ran,
  failed, or an id typo) or carrying a missing/unknown protocol is a **hard
  typed error before any request is sent**:
  `kakiit model <id> reports no protocol; refusing to guess (run a model from
  /v1/models, or re-fetch after a gateway outage)`
  (`kakiitProtocolForModel`, `models_registry_kakiit.go:306-317`). The guard
  in `chatOpenAI` runs FIRST, so a bad model can never silently default to
  chat/completions. This is the same failure class as the opencode-go
  per-model-protocol bug — an Anthropic-protocol model posted to
  /chat/completions 500s deterministically, and a Responses model there
  streams broken tool calls. See
  [gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md).
- The Anthropic path sends the **flat tool `Definition()` shape** — tools as
  `{name, description, input_schema}`, never the nested OpenAI
  `function{parameters}` form (pinned by
  `TestKakiitAnthropicModelRoutesToMessagesPathWithAnthropicToolShape`), and
  the Anthropic cache breakpoints survive the path unreordered
  (`TestKakiitCacheBreakpointsSurviveAnthropicPathUnreordered` — see
  [concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md)).

## Sticky session header and per-turn request id

- **`X-Opencode-Session` rides kakiit requests** via the existing
  `setOpencodeSessionHeader` gate, which now admits kakiit
  (`!c.isOpencodeProvider() && !c.isKakiitProvider()`,
  `client.go:416-423`). Behaviour for opencode* providers is byte-identical;
  the gateway spec requires the same sticky header so session affinity (and
  upstream prompt-cache affinity) survives the gateway. All five transport
  call sites stamp it (`client.go:1563, 1670, 3359, 4045, 5341`).
- **`X-Request-Id` is minted once per turn and is SINGLE-USE at the
  gateway.** A `req_`-prefixed UUIDv7 (`kakiitTurnRequestID`,
  `client_kakiit.go:186-188`; `newUUIDv7` implemented locally,
  `client_kakiit.go:194-227`, rather than promoting google/uuid from an
  indirect dependency) is parked on the context in `ChatWithContext` BEFORE
  the attempt loop (`client.go:840-848`), so every retry inside one turn
  reuses it; the next turn mints a new one (pinned by
  `TestKakiitRequestIDStableAcrossRetriesUniqueAcrossTurns`).
- **That reuse is fail-closed, not idempotent.** The id is the gateway's
  wallet-ledger correlation key, and the gateway now refuses ANY id that
  already has a hold: a request still in flight, one already settled/released,
  and one owned by another tenant all answer `409 duplicate_request_id`. A
  same-turn retry that reaches the gateway after the first attempt was
  accepted therefore fails the turn with the duplicate-id message (below)
  instead of silently billing a second upstream call. `setKakiitHeaders` is
  stamped at the three transport sites (`client.go:1565, 3361, 4047`); its
  doc comment records the consequence for retries
  (`client_kakiit.go:83-98`).
- **Why single-use:** the gateway keeps the id on every ledger row it touches
  (`placeHold` in the kakiit repo's `src/server/llm/wallet.ts`). Returning an
  existing hold as an "idempotent retry" was a double-spend — the caller got
  one charge but a second billed upstream call — so `placeHold` now throws
  `RequestInFlightError` while in flight, `RequestAlreadyFinalizedError` once
  settled/released, and `DuplicateRequestIdError` when another tenant owns the
  id, and `src/server/llm/proxy.ts` maps all three to `409
  duplicate_request_id`. A proxy cannot replay a response, so there is no safe
  retry that reuses an id: the remedy is a NEW attempt (a fresh turn mints a
  fresh id).
- kakiit introduces **no new session-keyed state**: it reuses the agent's
  conversation id, so the `/reset-id` re-key contract applies unchanged — see
  [concepts/session-rekey-reset-id.md](/concepts/session-rekey-reset-id.md).

## 402 mapping (insufficient credits)

The gateway returns HTTP 402 with an OpenAI-shaped envelope whose code is
`llm_insufficient_credits` for an uncovered wallet hold.

- **Classification is status-code + typed code, never prose**
  (`kakiitUserFacingError`, `client_kakiit.go:121-154`, the same classifier
  the 409 mapping below uses): a body that merely mentions credits on a
  non-402 status stays generic, and a 402 WITHOUT the
  `llm_insufficient_credits` code stays generic too (the gateway may grow
  other payment errors whose remedy is different). Pinned by
  `TestKakiit402ClassificationIsByStatusAndCodeNotProse`.
- **All three transports label the error `kakiit`, so the mapping fires on
  every one.** The classifier keys on `providerStatusError.Provider ==
  "kakiit"`, but the two native transports used to label a typed status error
  with their PROTOCOL (`anthropic`, `openai responses`), so a gateway 402/409
  on those routes silently stayed generic. `statusProviderLabel(fallback)`
  (`client_kakiit.go:63-75`) returns `kakiit` for a kakiit client and the
  protocol label for every other provider; the Responses and Anthropic
  transports now pass their label through it (`client.go:3402`,
  `client.go:4073`), and chat/completions already used `c.Provider`
  (`client.go:1578`). Pinned by `TestKakiit402MapsOnAllThreeProtocols` and
  `TestNonKakiitProvidersKeepProtocolLabels`.
- **User-facing line** (verbatim, `KakiitTopUpMessage`,
  `client_kakiit.go:103`): *Insufficient kakiit credits — top up at
  hub.mercstudio.com (or ask your site admin)*. Surfaced through
  `UserFacingLLMError` (`client_kakiit.go:160-168`), consumed by the TUI
  `streamDoneMsg` error line (`internal/tui/model.go:5649`), by
  `Agent.StepLimitHitDetail` (`internal/agent/agent.go:6788`), and by the
  web/desktop server's `publishTurnError`
  (`internal/server/agent_session.go:1184`), which publishes
  `agent.UserFacingLLMError(err)` as the `turn_error` payload's `error` field
  (`agent_session.go:1200`) — so the browser/desktop banner shows the mapped
  line, not the raw JSON envelope.
- **No auto-retry:** 402 is outside the retryable set — typed status errors
  retry only via the 429 / 500-504-529 arms (`isRateLimitError`,
  `internal/agent/client.go:1083-1093`; `isServerUnavailableError`,
  `client.go:1127-1136`), and `isRetryableLLMClientError` excludes typed
  status errors outright. The turn fails once with the top-up line. Pinned by
  `TestKakiit402IsNotRetried`.

## 409 mapping (reused request id)

The gateway answers `409` with an OpenAI-shaped envelope whose code is
`duplicate_request_id` for ANY `X-Request-Id` that already has a wallet hold
(see "Sticky session header and per-turn request id").

- **Same status-then-code classification.** The 409 arm of
  `kakiitUserFacingError` (`client_kakiit.go:121-154`) requires the envelope
  code `duplicate_request_id`; an unrelated 409 stays generic, so a conflict
  with a different cause never tells the user their request "already ran".
  Pinned by `TestKakiit409OtherConflictsStayGeneric`.
- **The `kakiit` provider label is what makes it fire on every transport**,
  the same `statusProviderLabel` change described under 402: without it the
  Responses/Anthropic 409s carried the protocol name and stayed generic.
  Pinned by `TestKakiit409MapsOnAllThreeProtocols`.
- **User-facing line** (verbatim, `KakiitDuplicateRequestMessage`,
  `client_kakiit.go:119`): *kakiit: this request already ran (or is still
  running) — send it again to start a new attempt*. It rides the same
  `UserFacingLLMError` (`client_kakiit.go:160-168`) path as the top-up line:
  the TUI `streamDoneMsg` error line (`internal/tui/model.go:5649`),
  `Agent.StepLimitHitDetail` (`internal/agent/agent.go:6788`), and the
  web/desktop `publishTurnError` banner
  (`internal/server/agent_session.go:1184`, `agent.UserFacingLLMError(err)` at
  `:1200`).
- **When it appears:** only after the gateway has accepted an attempt carrying
  the turn's id — a retry inside that same turn, or an ambiguous failure the
  caller retried — because the id is single-use. A fresh turn mints a fresh
  id, so sending the message again is the remedy; resending the same request
  under the same id is not.
- **No auto-retry:** like 402, 409 is outside the retryable set (the typed
  status arms are 429 and 500/502/503/504/529 only,
  `client.go:1083-1093, 1127-1136`), and `isRetryableLLMClientError` excludes
  typed status errors. The turn fails once with the duplicate-id line. Pinned
  by `TestKakiit409DuplicateRequestIDMapsToClearMessage`.

## Registration sites (adding a provider like this one)

No playbook doc existed for registering a new provider; these are the sites
commit `8ab8d978` touched, in the order that matters:

1. **Providers map** (`internal/agent/client.go:4616`): `id → {envKey,
   baseURL}`. `NewClient`/`NewClientWithProfile` read env + auth.json from it.
2. **`auth.Providers`** (`internal/auth/providers.go`, the `Providers` slice): the `/connect`
   dialog list — and note the **positional pin**: kakiit had to be appended
   LAST (index 30) because `internal/tui/connect_filter_test.go:79` pins
   `codex` at index 29 for the "OPENAI" fuzzy-match assertion. Adding a
   provider mid-slice breaks that test.
3. **`keyOptionalProviders` non-membership** (`client.go:4548-4553`): decide
   keyless-allowed vs typed refusal; kakiit is refused (see Setup).
4. **Preload sites**: TUI `Init` (`internal/tui/model.go:3146`) and server
   `Serve` (`internal/server/server.go:2532`) call `PreloadKakiitModels()`.
5. **Registry branches**: `providerModelsFromRegistry` /
   `allProviderModelsFromRegistry` (`internal/agent/models_registry.go:1412`,
   `:1267`) — live-only here, snapshot-fallback for models.dev providers.
6. **Optional env/config overrides**: `KakiitBaseURL()` +
   `kakiit_base_url` config field (`internal/config/ocodeconfig.go:1265`) and
   the `NewClientWithProfile` override branch (`client.go:4922-4930`).

Tests live in `internal/agent/{client_kakiit_test.go,
client_kakiit_protocol_test.go, client_kakiit_protocol_errors_test.go,
client_kakiit_402_test.go, client_kakiit_409_test.go,
models_registry_kakiit_test.go}` — 28 test functions, runnable as
`go test ./internal/agent/ -run Kakiit`. The 402 and 409 cases go through the
real `ChatWithContext` path with a stubbed transport (`chatErrorFromStub`),
not a hand-built error value; `client_kakiit_protocol_errors_test.go` pins the
per-transport provider label.

## Operator pointer and deferred items

- **The gateway side lives in the kakiit repo** (`/Users/james/www/kakiit`):
  design `docs/superpowers/specs/2026-11-19-kakiit-llm-gateway-design.md`,
  staged plans `docs/superpowers/plans/2026-11-19-kakiit-llm-gateway/`
  (M1 money path, M2 protocols, M3 billing/reporting, M4 this provider).
  Wallet top-ups and per-model admin access are configured there, not here.
- **Deferred / open (ocode TODO.md:7257-7261):**
  - Web SPA model-picker section chrome (a `kakiit/` provider label + free
    badge in `web/src/components/Layout/ModelDialog.tsx`): the files carried
    another session's uncommitted work at implementation time. The generic
    picker already renders `kakiit/<model>` rows from the registry list, so
    models are selectable; only the dedicated section header/free-badge
    chrome is missing.
  - **Live end-to-end smoke once the gateway deploys** — everything here is
    httptest-stubbed; `hub.mercstudio.com/api/llm` is not deployed yet and no
    real key exists.
  - `previous_response_id` unsupported in v1 (stateless full-echo already
    matches), and `/v1/models` refresh cadence beyond the 5-min TTL.

## Related

- [gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md](/gotchas/opencode-go-per-model-protocol-and-anthropic-tool-schemas.md) — the per-model routing failure class kakiit's guard prevents
- [concepts/prompt-cache-stability.md](/concepts/prompt-cache-stability.md) — why the sticky session id and byte-stable transports matter
- [concepts/session-rekey-reset-id.md](/concepts/session-rekey-reset-id.md) — the conversation-id contract kakiit inherits without adding session-keyed state
- [concepts/backend-sync-url-split.md](/concepts/backend-sync-url-split.md) — the *other* hub URL (`sync_url`) is unrelated to `kakiit_base_url`
