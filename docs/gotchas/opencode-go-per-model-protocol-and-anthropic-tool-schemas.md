---
type: Gotcha
title: opencode-go per-model protocol routing & Anthropic tool schema flatness
description: 'Retry policy: 500, thinking-mode 400, and non-standard 529 "Endpoint is unavailable" now retried (2026-09-29); opencode-go per-model protocol routing & Anthropic flat tool schemas'
tags: [gotcha, opencode-go, anthropic, tool-schema, models-registry, retry]
timestamp: 2026-09-29T07:16:08Z
---
# opencode-go per-model protocol routing & Anthropic tool schema flatness

**Type:** Gotcha  
**Description:** Two ocode bugs fixed 2026-09-17: opencode-go per-model routing by provider.npm, and flat tool Definition() shape required by chatAnthropic  
**Tags:** gotcha, opencode-go, anthropic, tool-schema, models-registry  

---

## opencode-go is per-model routed, not per-provider

The ocode transport for `opencode-go` routes each model to a different upstream protocol based on the model's `provider.npm` annotation in the models.dev registry. The canonical source of truth is models.dev's `provider.npm` field — upstream OpenCode itself routes on `api.package` in `packages/core/src/session/runner/model.ts`.

### Protocol map (verified 2026-09-17)

| `provider.npm` value | Upstream route | Models served |
|----------------------|----------------|---------------|
| `@ai-sdk/anthropic` | `/zen/go/v1/messages` (Anthropic Messages API) | minimax-m2.5/m2.7/m3, qwen3.8-flash, union-alpha |
| `@ai-sdk/openai` | Responses API | gpt-5.6-luna, grok-4.5/4.6, muse-spark-* |
| `null` | `/v1/chat/completions` (OpenAI-compatible) | All others |

### How ocode resolves it

1. `modelEntry` in `internal/agent/models_registry.go` captures the `provider.npm` value from the registry snapshot.
2. `ModelAPIPackageFromRegistry` reads it non-blocking at request time.
3. `usesAnthropicMessagesAPI` in `internal/agent/client.go` now checks the npm value (not just the model name prefix).
4. `opencodeAnthropicMessagesModel("union-alpha")` is the fallback when the registry is stale or unavailable.

The `opencode` (zen) provider intentionally keeps its historical `minimax-` prefix-only rule — the claude/gemini protocol switch is unverified without a zen credential.

### The bug that bit us

Previously `usesAnthropicMessagesAPI` only matched the `minimax-` model name prefix. `opencode-go/union-alpha` therefore fell through to `/v1/chat/completions`, and the proxy returned HTTP 500 `Internal server error`.

### Takeaway

Adding a new `opencode-go` model that only serves `/messages` requires **no ocode change** once models.dev annotates it with the correct `provider.npm`. But do not rely on the raw endpoint accepting `chat/completions` for Anthropic-protocol model IDs — it will 500.

---

## Built-in tool `Definition()` must return the flat shape

Built-in tools must return `{name, description, parameters}` from `Definition()`. Some providers (OpenAI, Google) normalize nested shapes internally, but `chatAnthropic` does not — it reads `t["name"]` and `t["parameters"]` directly off the tool definition object.

### The bug

`PreviewOpenTool.Definition()` returned the nested OpenAI form `{type:function, function:{name, description, parameters}}`. Because `openAITools` and `googleTools` normalize both forms, this was invisible on those providers. But `chatAnthropic` skipped the nesting and read the top-level keys, so `preview_open` arrived at every Anthropic-protocol route (minimax-m3, union-alpha, and native `anthropic`) with an empty `name` and nil `input_schema`. The request was rejected:

- **opencode-go/minimax-m3**: `tools[N] must have a string "name" and an object "input_schema"`
- **minimax-m3 direct**: `function name is empty (2013)`

This silently broke native `anthropic` too — any session using `preview_open` on an Anthropic-protocol provider hit the same 400.

### Guard

`TestBuiltinToolDefinitionsAreFlat` in `internal/tool/tool_test.go` now asserts every built-in tool's `Definition()` returns a flat shape with a non-empty `name` and object `parameters`. Any future tool that returns a nested shape will fail the test suite.

### Takeaway

When adding a new built-in tool, always return `{name, description, parameters}` from `Definition()`. Do not assume provider-side normalization covers Anthropic-protocol routes — it does not.

---

## 2026-09-18 — HTTP 500 now retried (transient server error)

`isServerUnavailableError` in `internal/agent/client.go` now treats HTTP 500 (`StatusInternalServerError`) the same as 502/503/504 (and, since 2026-09-29, the non-standard 529): retried with the standard budget (`llmMaxRetries` = 3 attempts, `(attempt+1) × llmRetryBaseDelay` backoff). Previously 500 failed fast after a single attempt.

**Rationale.** Providers (opencode-go in particular) return a generic 500 "Internal server error" for transient upstream faults; hard-failing the turn on the first one is worse than retrying. Retrying a *deterministic* 500 (like the protocol-routing 500 described above) is harmless — it just spends the retry budget and surfaces the same error.

**Caveat — routing 500 vs transient 500 are indistinguishable from the status code alone.** A 500 with the Anthropic envelope body (`{"type":"error","error":{"type":"invalid_request_error",...}}`) from a mis-routed opencode-go model is deterministic: the upstream knows the model ID but was posted to the wrong endpoint. Retrying it three times will not make it succeed — the routing fix in `usesAnthropicMessagesAPI` remains the load-bearing correction. A persistent 500 that does not resolve after retries therefore still points at a routing or protocol mismatch, not a transient fault.

The delta-emitted gate is unchanged: a 500 after partial streamed deltas still does not retry (duplicate-transcript protection), except for empty-response errors.

**Tests:** `TestChatRetriesTransientServerStatusCodes` (now includes 500), `TestChat500UsesUsualMaxRetries` (pins 4 attempts total on persistent 500), `TestProviderStatusErrorClassification` (500 ⇒ server-unavailable true), `TestStatusErrorBodyTextDoesNotCauseRetry` (switched to 400).

---

## 2026-09-22 — thinking-mode 400 (`reasoning_content` must be passed back) is now retried

A thinking-mode conversation that omits the assistant `reasoning_content` to echo back now gets retried instead of hard-failing. The opencode-go gateway returns this HTTP 400:

```json
{"error":{"param":null,"type":"invalid_request_error","code":"invalid_request_error",
"message":"Upstream request failed: [invalid_request_error] The `reasoning_content` in the thinking mode must be passed back to the API."}}
```

Previously this hard-failed the turn as "llm request failed after 1 attempt(s)" because 400 was not in `{429, 500, 502, 503, 504}` (the retryable set has since gained the non-standard 529, added 2026-09-29; 400 remains outside it). 400 remains non-retryable by default — see the narrow exemption below.

### New helper — `isRetryableThinkingModeRequestError(err)` (`internal/agent/client.go`)

Returns `true` **only** when `err` is a typed `*providerStatusError` with **all three** conditions met:

1. `Code == http.StatusBadRequest` (400), and
2. the body contains the field name `reasoning_content`, and
3. the body contains the phrasing `passed back` or `thinking mode` (case-insensitive).

This is a deliberately narrow exemption. A 400 is otherwise non-retryable (see `TestStatusErrorBodyTextDoesNotCauseRetry` — a 400 body mentioning "timeout" or "eof" still fails fast). Requiring **both** the field name **and** the thinking-mode phrasing keeps unrelated malformed-request 400s (including bodies that merely name `reasoning_content`) failing fast. The gateway re-validates conversation state on each attempt, so a retry can re-establish the reasoning continuity the first attempt missed.

### Shared policy — `isRetryableLLMError` (`internal/agent/client.go`)

`isRetryableLLMError` is now the **single** retryability policy used by both retry loops:

```
isRetryableLLMError = isRateLimitError || isServerUnavailableError || isRetryableLLMClientError || isRetryableThinkingModeRequestError
```

Callers: the main `ChatWithContext` retry loop (`client.go` ~:808) and the auto-permission judge's outer retry loop (`agent.go` ~:4139). The two call sites no longer duplicate the boolean expression. Callers still check `isRateLimitError` separately to select the 429 budget/delay.

**The `deltaEmitted` anti-duplication gate is unaffected.** A 400 arrives before any streamed deltas (it is a pre-stream rejection), so `deltaEmitted` is `false` and the gate never interferes.

### Budget and delay

Non-429 path: `llmMaxRetries` = 3 retries → 4 attempts total, delay `(attempt+1) × llmRetryBaseDelay` (500 ms base, zeroed in tests).

### Tests (`internal/agent/client_test.go`, all mutation-verified by flipping the status-code gate to 418 so they fail)

- `TestIsRetryableThinkingModeRequestError` — unit matrix including negatives: phrase without field name, field name without phrase, non-400 status, untyped `error` (not `*providerStatusError`), nil.
- `TestChatRetriesThinkingModeReasoningContent400` — 400 then 200 success, exactly 2 calls.
- `TestChatThinkingMode400UsesUsualMaxRetries` — persistent 400 → `llmMaxRetries + 1` attempts.
- `TestChatGenericInvalidRequest400DoesNotRetry` — an unrelated 400 (`invalid_request_error` without the reasoning_content/thinking-mode phrasing) → 1 call, fails fast.
- `TestStatusErrorBodyTextDoesNotCauseRetry` (existing) — a 400 body with "timeout"/"eof" wording still does not retry; green.

---

## 2026-09-29 — HTTP 529 "Endpoint is unavailable" now retried

**Reported symptom.** A turn against an opencode-go model hard-failed on the very first response with `llm request failed after 1 attempt(s)` even though the gateway's 529 is a transient "upstream momentarily overloaded" signal. The exact opencode-go gateway body:

```json
{"error":{"type":"server_error","message":"Upstream request failed: Endpoint is unavailable."}}
```

529 is the non-standard "Endpoint is unavailable" / overloaded code returned by the opencode-go gateway and used by Anthropic as `overloaded_error`.

**Root cause.** 529 is non-standard, so `net/http` has no `http.Status*` constant for it, and nothing in the classifier matched it. Critically, `isRetryableLLMClientError` **deliberately returns `false` for every typed `*providerStatusError`** (its first check) so that typed status errors are classified **by code only, never by body text** — which leaves `isServerUnavailableError`'s status-code switch as the one and only place a 529 could ever become retryable. With 529 missing from that switch, the turn hard-failed.

**Fix (classifier only).** In `internal/agent/client.go`:

- `const statusEndpointUnavailable = 529` declared next to `isServerUnavailableError`, commented with the opencode-go 529 body and the Anthropic `overloaded_error` equivalence;
- the classifier switch is now `case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, statusEndpointUnavailable`.

No retry-loop change, no body-text heuristic, budget/delay unchanged (non-429 path: `llmMaxRetries` = 3 retries → 4 attempts total). Both retry loops get it through the shared policy — the main `ChatWithContext` retry loop (`client.go` ~:850) and the auto-permission judge loop in `internal/agent/agent.go` (~:4269), both calling `isRetryableLLMError` → `isServerUnavailableError`. All 7 non-200 provider paths in `client.go` build their error with the generic `newProviderStatusError`, so this one classifier change covers every provider.

**Tests** (`internal/agent/client_test.go`, all verified failing against the pre-fix classifier by temporary revert, then green):

- `TestChatRetries529EndpointUnavailable` — 529 (with the exact opencode-go body) then 200 → success in exactly 2 calls; pre-fix this surfaced as `llm request failed after 1 attempt(s): opencode-go error (529): {...}`.
- `TestChat529UsesUsualMaxRetries` — persistent 529 → the usual `llmMaxRetries + 1` attempts (not a fast fail), with the standard error format preserved.
- `providerStatusCodeName` (new test helper) — readable subtest names per status code, falling back to the bare number; needed because `http.StatusText(529)` returns `""`.
- `TestChatRetriesTransientServerStatusCodes` — extended to include 529 alongside 500/502/503/504.
- `TestProviderStatusErrorClassification` — gains a 529 positive case (`isServerUnavailableError` ⇒ true) plus a 501 Not Implemented negative case (a genuine permanent 5xx that must stay non-retryable).

---

**Cite:** `internal/agent/client.go` (`isServerUnavailableError`, `statusEndpointUnavailable`, `isRetryableThinkingModeRequestError`, `isRetryableLLMError`, `ChatWithContext` retry loop), `internal/agent/agent.go` (auto-permission judge outer retry loop), `internal/agent/client_test.go` (`TestIsRetryableThinkingModeRequestError`, `TestChatRetriesThinkingModeReasoningContent400`, `TestChatThinkingMode400UsesUsualMaxRetries`, `TestChatGenericInvalidRequest400DoesNotRetry`, `TestStatusErrorBodyTextDoesNotCauseRetry`, `TestChatRetriesTransientServerStatusCodes`, `TestChat500UsesUsualMaxRetries`, `TestChatRetries529EndpointUnavailable`, `TestChat529UsesUsualMaxRetries`, `TestProviderStatusErrorClassification`), `internal/agent/models_registry.go` (`modelEntry`, `ModelAPIPackageFromRegistry`), `internal/tool/preview.go`, `internal/tool/tool_test.go`, CHANGES.md entry "2026-09-17 — Union Alpha (opencode-go) works; Anthropic tool schemas fixed"