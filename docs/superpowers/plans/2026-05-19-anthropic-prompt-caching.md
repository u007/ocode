# Anthropic Prompt Caching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Anthropic prompt caching cache_control markers to system prompt, tool definitions, and first user message in `chatAnthropic`.

**Architecture:** Modify the `chatAnthropic` method in `internal/agent/client.go` to convert the system string to an array with `cache_control`, add cache markers to tool definitions, and mark the first user message content. All other providers are unaffected (OpenAI caching is automatic).

**Tech Stack:** Go, Anthropic Messages API

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agent/client.go` | Modify | Add cache_control markers in `chatAnthropic` (lines 590-761) |

Only one file changes. The `chatAnthropic` method currently:
- Accumulates `system` as a string (line 593-598)
- Builds `anthropicMsgs` as `[]map[string]interface{}` (line 594-658)
- Builds `anthropicTools` as `[]map[string]interface{}` (line 668-675)
- Sets payload with `"system": system` as string (line 662)

Changes:
1. Convert `system` to array with `cache_control` on last element
2. Add `cache_control` to last tool definition
3. Add `cache_control` to last element of first user message content

---

### Task 1: Add Anthropic cache_control markers

**Files:**
- Modify: `internal/agent/client.go:590-777` (entire `chatAnthropic` method)

- [ ] **Step 1: Replace the system string with cached system array**

After the message loop (after line 658) and before the payload construction (before line 660), add code to convert the system string to a cached array format:

```go
// Build system payload with cache_control for prompt caching
var systemPayload interface{}
if system != "" {
    systemPayload = []interface{}{
        map[string]interface{}{
            "type":          "text",
            "text":          system,
            "cache_control": map[string]interface{}{"type": "ephemeral"},
        },
    }
}
```

- [ ] **Step 2: Add cache_control to last tool definition**

Replace the tool building block (lines 667-677) with:

```go
if len(tools) > 0 {
    var anthropicTools []map[string]interface{}
    for _, t := range tools {
        anthropicTools = append(anthropicTools, map[string]interface{}{
            "name":         t["name"],
            "description":  t["description"],
            "input_schema": t["parameters"],
        })
    }
    // Add cache_control to last tool for prompt caching
    if len(anthropicTools) > 0 {
        anthropicTools[len(anthropicTools)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
    }
    payload["tools"] = anthropicTools
}
```

- [ ] **Step 3: Update payload to use systemPayload instead of system string**

Change line 662 from:
```go
"system":     system,
```
To:
```go
"system":     systemPayload,
```

The full payload block (lines 660-665) becomes:
```go
payload := map[string]interface{}{
    "model":      c.Model,
    "system":     systemPayload,
    "messages":   anthropicMsgs,
    "max_tokens": 4096,
}
```

- [ ] **Step 4: Add cache_control to first user message content**

After the message loop finishes building `anthropicMsgs` (after line 658), add code to mark the first user message for caching. Insert this between line 658 and the payload construction:

```go
// Add cache_control to first user message content for prompt caching
for i := range anthropicMsgs {
    if anthropicMsgs[i]["role"] == "user" {
        if content, ok := anthropicMsgs[i]["content"].([]interface{}); ok && len(content) > 0 {
            content[len(content)-1].(map[string]interface{})["cache_control"] = map[string]interface{}{"type": "ephemeral"}
        }
        break
    }
}
```

- [ ] **Step 5: Verify build compiles**

Run:
```bash
go build ./...
```
Expected: No errors.

- [ ] **Step 6: Run existing tests**

Run:
```bash
go test ./internal/agent/... -v
```
Expected: All existing tests pass (no test changes needed — this is HTTP-level behavior).

- [ ] **Step 7: Commit**

```bash
git add internal/agent/client.go
git commit -m "feat: add Anthropic prompt caching with cache_control markers"
```

---

## Spec Coverage Check

| Spec Requirement | Task |
|-----------------|------|
| Convert system to array with cache_control | Task 1, Steps 1, 3 |
| Add cache_control to last tool definition | Task 1, Step 2 |
| Add cache_control to first user message content | Task 1, Step 4 |
| Only add markers when content non-empty | Task 1, Steps 1, 4 (guard clauses) |
| No changes to OpenAI/Copilot/compatible | Confirmed — no other methods modified |
| Auto-invalidation (Anthropic handles) | Documented in spec, no code needed |

## Placeholder Scan

No TBD, TODO, or vague steps found. All code blocks are complete.

## Type Consistency

- `systemPayload` is `interface{}` — matches existing `map[string]interface{}` usage in payload
- `anthropicTools` is `[]map[string]interface{}` — same type as before, only adding a key to last element
- `cache_control` value is `map[string]interface{}{"type": "ephemeral"}` — matches Anthropic API spec
