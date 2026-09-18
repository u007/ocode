---
type: Gotcha
title: opencode-go per-model protocol routing & Anthropic tool schema flatness
description: 'Two ocode bugs fixed 2026-09-17: opencode-go per-model routing by provider.npm, flat tool Definition() shape for chatAnthropic; 2026-09-18: HTTP 500 retried as transient with routing caveat.'
tags: []
timestamp: 2026-09-18T12:22:29Z
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

`isServerUnavailableError` in `internal/agent/client.go` now treats HTTP 500 (`StatusInternalServerError`) the same as 502/503/504: retried with the standard budget (`llmMaxRetries` = 3 attempts, `(attempt+1) × llmRetryBaseDelay` backoff). Previously 500 failed fast after a single attempt.

**Rationale.** Providers (opencode-go in particular) return a generic 500 "Internal server error" for transient upstream faults; hard-failing the turn on the first one is worse than retrying. Retrying a *deterministic* 500 (like the protocol-routing 500 described above) is harmless — it just spends the retry budget and surfaces the same error.

**Caveat — routing 500 vs transient 500 are indistinguishable from the status code alone.** A 500 with the Anthropic envelope body (`{"type":"error","error":{"type":"invalid_request_error",...}}`) from a mis-routed opencode-go model is deterministic: the upstream knows the model ID but was posted to the wrong endpoint. Retrying it three times will not make it succeed — the routing fix in `usesAnthropicMessagesAPI` remains the load-bearing correction. A persistent 500 that does not resolve after retries therefore still points at a routing or protocol mismatch, not a transient fault.

The delta-emitted gate is unchanged: a 500 after partial streamed deltas still does not retry (duplicate-transcript protection), except for empty-response errors.

**Tests:** `TestChatRetriesTransientServerStatusCodes` (now includes 500), `TestChat500UsesUsualMaxRetries` (pins 4 attempts total on persistent 500), `TestProviderStatusErrorClassification` (500 ⇒ server-unavailable true), `TestStatusErrorBodyTextDoesNotCauseRetry` (switched to 400).

---

**Cite:** `internal/agent/client.go` (`usesAnthropicMessagesAPI`, `chatAnthropic`, `isServerUnavailableError`), `internal/agent/models_registry.go` (`modelEntry`, `ModelAPIPackageFromRegistry`), `internal/tool/preview.go`, `internal/tool/tool_test.go`, CHANGES.md entry "2026-09-17 — Union Alpha (opencode-go) works; Anthropic tool schemas fixed", `internal/agent/agent.go` (`ChatWithContext` retry loop, auto-permission judge loop)