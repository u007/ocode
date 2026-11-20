---
type: Concept
title: kakiit LLM Gateway Provider
description: The `kakiit` provider — auth and base-URL override chain, live-only /v1/models catalog (no models.dev), per-model protocol routing with the refusing-to-guess rule, sticky session + per-turn request-id ledger idempotency, and the 402 top-up mapping; includes the provider registration sites that adding a provider must touch.
resource: internal/agent/client_kakiit.go
tags:
  - provider
  - kakiit
  - llm
  - gateway
  - models-registry
  - auth
timestamp: 2026-11-20T00:30:00+08:00
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
2026-11-20). Everything in this doc is verified against that commit's tree;
all behaviour is pinned by httptest stubs, and **no live end-to-end call has
been made yet** (the gateway is not deployed — see "Deferred / open").

## Setup and credential

- **Connect:** `ocode /connect kakiit` → stores an auth.json `kakiit` entry
  (API-key credential). Env fallback: `KAKIIT_API_KEY`
  (`internal/auth/providers.go:159` — the `Providers` slice entry — and the
  providers map `internal/agent/client.go:4613`). Key resolution is the
  standard order every keyed provider follows: env var wins over auth.json
  (pinned by `TestKakiitAPIKeyResolutionPrecedence`).
- **Keyless is a typed refusal, not a 401 later.** kakiit is deliberately NOT
  in `keyOptionalProviders` (`internal/agent/client.go:4545-4550` — only
  opencode/opencode-go/lmstudio/local), so `NewClient` returns nil when no
  credential exists (`client.go:5031-5045`, pinned by
  `TestKakiitNoCredentialYieldsClearTypedError`). A keyless client would 401
  on its first request; callers (model switch, `ResolveSmallModel`,
  compaction client) skip instead.
- **Base URL.** Default `KakiitDefaultBaseURL` =
  `https://hub.mercstudio.com/api/llm` (`internal/agent/client_kakiit.go:25`).
  `KakiitBaseURL()` (`client_kakiit.go:194-202`) resolves:
  `KAKIIT_BASE_URL` env > `kakiit_base_url` (ocodeconfig.json,
  `internal/config/ocodeconfig.go:1262-1267`) > default.
- **The two base-URL paths disagree on where the auth.json per-credential
  override sits — deliberately.**
  - *Chat* (`NewClientWithProfile`, `client.go:4919-4927`): the standard
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
(`models_registry.go:1267-1280`).

- **Source:** `GET {BaseURL}/v1/models` with the bearer key. Entries carry the
  vendor extensions ocode consumes:
  - `x_kakiit_protocol` — `anthropic-messages` | `openai-responses` |
    `openai-chat` (wire literals; never rename —
    `models_registry_kakiit.go:294-298`)
  - `x_kakiit_free` — free models report zero price
  - `context_length`, `pricing{prompt,completion}` (USD per 1M, string, the
    OpenRouter/models.dev convention) — parsed into
    `pricing.ModelPricing` (`models_registry_kakiit.go:29-57`)
- **Cache:** one in-process cache (`kakiitLiveData`), 5-minute TTL
  (`kakiitCacheTTL`, same value as the models.dev/OpenRouter caches). On the
  TUI main loop only a still-fresh cache is consulted (`refresh=false`), so
  the picker never blocks on the gateway; `PreloadKakiitModels()`
  (`models_registry_kakiit.go:283-285`, via `crashguard.Go`) warms it at TUI
  Init (`internal/tui/model.go:3127`) and server Serve
  (`internal/server/server.go:2474`).
- **Failure behaviour is split by cause:**
  - *Fetch failure / malformed body / empty list* → nil list + one debug line;
    the picker section is simply empty (`TestKakiitFetchFailureYieldsEmptyListAndDebugLineNotStaleFallback`).
    Never a stale or models.dev fallback — a wrong list is worse than none.
  - *401/403* → recorded in `KakiitModelsAuthError()`
    (`models_registry_kakiit.go:209-217`) so a rejected key reads as "your key
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
(`ChatWithContext`, `internal/agent/client.go:881-889`):

| `x_kakiit_protocol` | Transport | Endpoint (via `apiRoot()`) |
|---|---|---|
| `anthropic-messages` | `chatAnthropic` (via `usesAnthropicMessagesAPI`, `client.go:763-778`) | `{BaseURL}/v1/messages` |
| `openai-responses` | `chatOpenAIResponses` (guard-first branch in `chatOpenAI`, `client.go:1479-1493`) | `{BaseURL}/v1/responses` |
| `openai-chat` | `chatOpenAI` fall-through | `{BaseURL}/v1/chat/completions` |

- **`apiRoot()` inserts the `/v1` segment for kakiit**
  (`client_kakiit.go:131-142`): the declared BaseURL is the gateway ROOT
  (…/api/llm), unlike `api.openai.com/v1`-style providers whose map entry
  already carries the version. All three transports append their suffix to
  `apiRoot()` (`client.go:1510, 3821, 3964`), so one helper keeps them
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
  `client.go:416-421`). Behaviour for opencode* providers is byte-identical;
  the gateway spec requires the same sticky header so session affinity (and
  upstream prompt-cache affinity) survives the gateway. All five transport
  call sites stamp it (`client.go:1560, 1667, 3356, 4042, 5338`).
- **`X-Request-Id` is minted once per turn** — a `req_`-prefixed UUIDv7
  (`kakiitTurnRequestID`, `client_kakiit.go:144-149`; `newUUIDv7` implemented
  locally, `client_kakiit.go:155-188`, rather than promoting google/uuid from
  an indirect dependency) — and parked on the context in `ChatWithContext`
  BEFORE the attempt loop (`client.go:840-845`). Every retry inside the turn
  reuses it; the next turn mints a new one (pinned by
  `TestKakiitRequestIDStableAcrossRetriesUniqueAcrossTurns`).
- **Why:** the gateway persists the request id on every wallet-ledger row it
  touches; a retried request converges to one settle instead of
  double-debiting (`setKakiitHeaders` doc comment,
  `client_kakiit.go:66-80`). `setKakiitHeaders` is stamped at the three
  transport sites (`client.go:1562, 3358, 4044`).
- kakiit introduces **no new session-keyed state**: it reuses the agent's
  conversation id, so the `/reset-id` re-key contract applies unchanged — see
  [concepts/session-rekey-reset-id.md](/concepts/session-rekey-reset-id.md).

## 402 mapping (insufficient credits)

The gateway returns HTTP 402 with an OpenAI-shaped envelope whose code is
`llm_insufficient_credits` for an uncovered wallet hold.

- **Classification is status-code + typed code, never prose**
  (`kakiitUserFacingError`, `client_kakiit.go:100-115`): a body that merely
  mentions credits on a non-402 status stays generic, and a 402 WITHOUT the
  `llm_insufficient_credits` code stays generic too (the gateway may grow
  other payment errors whose remedy is different). Pinned by
  `TestKakiit402ClassificationIsByStatusAndCodeNotProse`.
- **User-facing line** (verbatim, `KakiitTopUpMessage`,
  `client_kakiit.go:85`): *Insufficient kakiit credits — top up at
  hub.mercstudio.com (or ask your site admin)*. Surfaced through
  `UserFacingLLMError` (`client_kakiit.go:121-129`), consumed by the TUI
  `streamDoneMsg` error line (`internal/tui/model.go:5618`) and
  `Agent.StepLimitHitDetail` (`internal/agent/agent.go:6584-6592`) — the
  web/desktop error banner rides the latter.
- **No auto-retry:** 402 is outside the retryable set — typed status errors
  retry only via the 429 / 500-504-529 arms (`isRateLimitError`,
  `isServerUnavailableError`, `internal/agent/client.go:1080-1133`), and
  `isRetryableLLMClientError` excludes typed status errors outright. The turn
  fails once with the top-up line. Pinned by `TestKakiit402IsNotRetried`.

## Registration sites (adding a provider like this one)

No playbook doc existed for registering a new provider; these are the sites
commit `8ab8d978` touched, in the order that matters:

1. **Providers map** (`internal/agent/client.go:4613`): `id → {envKey,
   baseURL}`. `NewClient`/`NewClientWithProfile` read env + auth.json from it.
2. **`auth.Providers`** (`internal/auth/providers.go:159`): the `/connect`
   dialog list — and note the **positional pin**: kakiit had to be appended
   LAST (index 30) because `internal/tui/connect_filter_test.go:79` pins
   `codex` at index 29 for the "OPENAI" fuzzy-match assertion. Adding a
   provider mid-slice breaks that test.
3. **`keyOptionalProviders` non-membership** (`client.go:4545-4550`): decide
   keyless-allowed vs typed refusal; kakiit is refused (see Setup).
4. **Preload sites**: TUI `Init` (`internal/tui/model.go:3127`) and server
   `Serve` (`internal/server/server.go:2474`) call `PreloadKakiitModels()`.
5. **Registry branches**: `providerModelsFromRegistry` /
   `allProviderModelsFromRegistry` (`internal/agent/models_registry.go:1412`,
   `:1267`) — live-only here, snapshot-fallback for models.dev providers.
6. **Optional env/config overrides**: `KakiitBaseURL()` +
   `kakiit_base_url` config field (`internal/config/ocodeconfig.go:1262`) and
   the `NewClientWithProfile` override branch (`client.go:4919-4927`).

Tests live in `internal/agent/{client_kakiit_test.go,
client_kakiit_protocol_test.go, client_kakiit_402_test.go,
models_registry_kakiit_test.go}` — 23 test functions (25 with subtests), all
green, verified by `go test ./internal/agent/ -run Kakiit`.

## Operator pointer and deferred items

- **The gateway side lives in the kakiit repo** (`/Users/james/www/kakiit`):
  design `docs/superpowers/specs/2026-11-19-kakiit-llm-gateway-design.md`,
  staged plans `docs/superpowers/plans/2026-11-19-kakiit-llm-gateway/`
  (M1 money path, M2 protocols, M3 billing/reporting, M4 this provider).
  Wallet top-ups and per-model admin access are configured there, not here.
- **Deferred / open (ocode TODO.md:7229-7244):**
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
