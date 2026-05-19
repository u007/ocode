# Design: Provider-Native Prompt Caching

**Date:** 2026-05-19
**Status:** Draft

## Problem

LLM API calls repeat the same system prompts, AGENTS.md, CLAUDE.md, tool definitions, and conversation context on every request. Providers offer native prompt caching to reduce costs and latency for repeated content, but ocode doesn't use it.

## Solution

Add provider-native cache markers to chat requests. Zero config — always on when the provider supports it.

## Provider Support

### Anthropic (primary — only provider needing explicit cache markers)
- **Mechanism:** `cache_control: {type: "ephemeral"}` on message elements
- **Limit:** 4 cache breakpoints per request
- **What to cache (in order, using all 4 breakpoints):**
  1. **System prompt** — AGENTS.md, CLAUDE.md, instructions
  2. **Tool definitions** — identical on every turn, high token count
  3. **First user message** — often contains file/context content
  4. **Last assistant message** — in multi-turn, the previous response repeats
- **Implementation:**
  - Convert `system` from string to `[]interface{}` with `cache_control` on last element
  - Append `cache_control` to last tool definition in the tools array
  - Append `cache_control` to last element of first user message content array
  - Auto-invalidation: Anthropic invalidates cache when content changes — safe to always-on

### OpenAI (Chat Completions + Responses API)
- **No implementation needed.** OpenAI prompt caching is automatic — no flag required.
- Requests sharing a common prefix are cached transparently by OpenAI's infrastructure.
- `store: true` is for response storage, NOT prompt caching. Remove from scope.

### OpenAI-compatible (Google, Z.AI, Alibaba, DeepSeek, etc.)
- **No implementation needed.** Most proxy through OpenAI-compatible endpoints.
- Caching behavior depends on the upstream provider. No portable flag exists.

### Copilot
- **No implementation needed.** Uses GitHub's proxied OpenAI endpoint.
- Caching is automatic if GitHub supports it; no flag to send.

## Changes

### `internal/agent/client.go`

1. **`chatAnthropic`** (~40 lines changed):
   - Convert `system` string to array format with cache_control:
     ```go
     "system": []interface{}{
         map[string]interface{}{
             "type": "text",
             "text": system,
             "cache_control": map[string]interface{}{"type": "ephemeral"},
         },
     }
     ```
   - Add `cache_control` to last tool definition in the tools array:
     ```go
     tools[n-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
     ```
   - Add `cache_control` to last element of first user message content array (if content is array)
   - Only add cache markers when system/tools/content is non-empty
   - Guard: if system is empty string, skip system cache marker

2. **`chatOpenAI`** — no changes needed (automatic caching)

3. **`chatOpenAIResponses`** — no changes needed (automatic caching)

4. **`chatCopilot`** — no changes needed (automatic caching)

### Streaming (future)
- `StreamingLLMClient` interface exists in `llm_contract.go` but has no implementation yet.
- When streaming is implemented, cache markers apply identically to streaming requests.

## Risk Assessment

- **Low risk:** Cache markers are ignored by providers that don't support them
- **No breaking changes:** Existing behavior preserved, caching is additive
- **Cost impact:** 50-80% savings on cached tokens (system prompt + tools are ~40-60% of typical request)
- **Auto-invalidation:** Anthropic invalidates cache when content changes — no stale cache risk
- **No config debt:** Always-on means no user-facing configuration to maintain

## Testing

- Manual testing with Anthropic provider
- Verify cache hit headers in responses: `anthropic-cache-control: hit` or `miss`
- No unit test changes needed (HTTP-level behavior)
- Verify no errors from non-Anthropic providers (they should be unaffected)

## Gaps (out of scope)

- **Google Vertex AI cachedContent** — Google has a separate caching API (`cachedContent` resource). Not usable via OpenAI-compatible endpoint. Would require a dedicated Google client.
- **OpenRouter caching** — OpenRouter proxies multiple providers. Cache behavior depends on the routed provider. No portable control.
