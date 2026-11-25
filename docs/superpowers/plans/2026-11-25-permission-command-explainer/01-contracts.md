---
type: Plan
timestamp: 2026-11-25T07:19:25Z
---
# Part 01 — Contracts, full-parameter pager, context guard, client snapshot

**Goal:** Provide the shared contracts for the explainer: a concrete toolbox, an exact masked canonical payload pager, a context-window guard, an ask key, and a race-safe client snapshot.

**Risk:** high — new JSON canonicalization + masking + paging primitive and a new lock pair guarding a live client swap.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md`.

**Global constraints this part must honor:**
- 8 KiB parameter page; full output no clamp.
- No cumulative parameter cap: every masked parameter byte stays pageable across sequential calls.
- Secrets-only masking, no PII; no content guard.
- No `Agent.Step` / transcript / hooks / discovery / todo / notes side effects.
- No new endpoint / config / schema / migration.

**Files:**
- Create: `internal/agent/tool_digest_contract.go`, `internal/agent/tool_digest_params.go`
- Create: `internal/agent/tool_digest_params_test.go`, `internal/agent/tool_digest_snapshot_test.go`
- Modify: `internal/agent/agent.go`

**Interfaces — consumed (existing):**
- `ToolCallDigestInput` with fields `Tool string`, `Command string`, `Args json.RawMessage`, `Rule string`, `Scope string`.
- `LLMClient` (`Chat`, `GetProvider`, `GetModel`), `Message`, `config.Config`, `*redact.NetHook`, `*redact.Registry`.
- `modelIDForClient(LLMClient) string`, `ModelWindow(modelID string) int64`, `ModelMaxOutputTokens(modelID string) int64`, `charsPerTokenFor(provider, model string) float64`, `CurrentContextEstimate(msgs []Message, cpt float64) (tokens int, source string)`.
- `tool.Tool` (`Name`, `Description`, `Definition() map[string]interface{}`, `Execute(json.RawMessage) (string, error)`, `Parallel`). Optional context-aware form: `ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error)`.

**Interfaces — produced (exact):**
```go
type toolDigestToolbox struct {
    defs    []map[string]interface{}
    tools   map[string]tool.Tool
    cleanup []func()
}
func (b *toolDigestToolbox) add(t tool.Tool)
func (b *toolDigestToolbox) addCleanup(fn func())
func (b *toolDigestToolbox) Definitions() []map[string]interface{}
func (b *toolDigestToolbox) Execute(ctx context.Context, name string, args json.RawMessage) (string, error)
func (b *toolDigestToolbox) Close()
```
`add` appends `t.Definition()` to `defs` and stores `t` by `t.Name()`. `Execute` looks up the name, returns an error for an unknown name, and dispatches to `ExecuteCtx` when the tool implements it, otherwise `Execute(args)`. `addCleanup` appends `fn`; `Close` invokes the registered cleanups in reverse order and is idempotent.

```go
type toolDigestPayload struct {
    canonical  []byte
    totalBytes int
    sha256     string
    fieldBytes map[string]int
}
func buildToolDigestPayload(in ToolCallDigestInput, mask func(string) string) (*toolDigestPayload, error)
func (p *toolDigestPayload) page(offset, maxBytes int) (toolDigestPage, error)

type toolDigestPage struct {
    offset, nextOffset, totalBytes int
    sha256                         string
    remainingBytes                 int
    text                           string
}
```

```go
type toolDigestContextGuard struct {
    Model           string
    CharsPerToken   float64
    ContextWindow   int64
    ResponseReserve int64
}
func newToolDigestContextGuard(client LLMClient) toolDigestContextGuard
func (g toolDigestContextGuard) admit(messages []Message) error

type toolDigestContextError struct {
    RemainingTokens int64
}
func (e *toolDigestContextError) Error() string
```
The constructor stores `Model = modelIDForClient(client)`, `CharsPerToken = charsPerTokenFor(client.GetProvider(), client.GetModel())`, `ContextWindow = ModelWindow(Model)` (fallback `100000`), and `ResponseReserve = ModelMaxOutputTokens(Model)` capped at `ContextWindow`, falling back to `8192` when unknown. `admit` calls `CurrentContextEstimate(messages, g.CharsPerToken)` and uses its `tokens` result (the `source` label is informational), adds `ResponseReserve`, and returns a typed `*toolDigestContextError` when the total exceeds `ContextWindow`; its exported `RemainingTokens int64` records the remaining token budget and its `Error()` names `remaining_context_tokens`; it never truncates. The guard cannot know which byte ranges were left uninspected, so the `call_parameters` implementation appends the requested and still-uninspected ranges to the tool error it returns.

```go
func toolDigestKey(owner, requestID, payloadHash string) string
```
Non-empty `requestID` returns `sha256(owner + "\x00" + requestID)` as lowercase hex; empty returns `sha256(owner + "\x00" + payloadHash)`. `payloadHash` is `toolDigestPayload.sha256`, i.e. the SHA-256 of the canonical **masked** input; the function never hashes raw unmasked input.

```go
type toolDigestSnapshot struct {
    WorkDir           string
    SessionID         string
    Profile           string
    Config            config.Config
    RedactionHook     *redact.NetHook
    RedactionRegistry *redact.Registry
    Client            LLMClient
    OnSideUsage       func(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int64, spend *float64)
}
func (a *Agent) snapshotToolDigestState() toolDigestSnapshot
func (a *Agent) clientSnapshot() LLMClient
```

**Pager and masking rules:** validate `Args` as exactly one JSON value with `json.Decoder` (reject malformed JSON, trailing data and duplicate keys; accept any JSON value, including objects, arrays and scalars; an absent `Args` is JSON `null`), decode with `UseNumber`, recursively apply `mask` to every string value and object key, fail on a masked-key collision, marshal with stable top-level order `tool, command, args, rule, scope` and sorted nested keys, then compute `totalBytes`, lowercase SHA-256, and per-field byte lengths. `page` slices exact byte windows, shortens only to a UTF-8 boundary, advances `nextOffset`, and errors on a negative/out-of-range offset, `maxBytes <= 0`, or `maxBytes > 8192`; it never clips silently.

**Snapshot and lock rules:** add `clientSnapshotMu sync.RWMutex` to `Agent`. `applySpecModel` builds the client and captures the debug session id before `clientSnapshotMu.Lock`, then swaps the pointer under the lock with no I/O and without `opencodeSessionMu`. `SetOpenCodeSessionID` and `OpenCodeSessionID` read the client under `clientSnapshotMu.RLock`. Lock order is `opencodeSessionMu` → `clientSnapshotMu`; `clientSnapshotMu` is a leaf. `snapshotToolDigestState` value-copies `*a.config` nil-safely and copies `WorkDir`, `SessionID`, `Profile`, hook, registry, client, and `OnSideUsage`; it holds no lock across model or tool work.

**Red-green steps:**
- [ ] **Step 1: Write the failing tests.** In `tool_digest_params_test.go`:
```go
func TestToolDigestPayloadPage_ExactWindowsLargePayloads(t *testing.T)
func TestToolDigestPayloadPage_UTF8Boundaries(t *testing.T)
func TestToolDigestPayloadPage_InvalidOffsetAndWindow(t *testing.T)
func TestBuildToolDigestPayload_MalformedAndDuplicateArgs(t *testing.T)
func TestBuildToolDigestPayload_MasksNestedStringsAndKeysBeforeHash(t *testing.T)
func TestBuildToolDigestPayload_BashEnvRawArgsPreserved(t *testing.T)
func TestToolDigestContextGuard_KnownUnknownAndRefusal(t *testing.T)
func TestToolDigestKey_RequestIDAndMaskedPayloadHash(t *testing.T)
```
Cover: an 80 KiB payload paged in 8192-byte windows concatenates to the canonical bytes with a running SHA and `total_bytes == len(canonical)`; multi-byte runes never split; `{"a":1,"a":2}` and trailing data are rejected before masking, an array or scalar `Args` is accepted as exactly one value, and an absent `Args` is treated as JSON `null`; a nested `{"token":"sk-live-..."}` leaves no secret in the canonical bytes and keys sort stably; `Command:"git commit"` with `Args:{"command":"EDITOR=vim git commit"}` keeps `EDITOR=vim` (raw `Args` authoritative); a known model window is used, an unknown model falls back to `100000`/`8192`, and an over-budget candidate refuses; the request-ID key is stable across payload hashes and the empty-ID key changes with the masked payload hash.

In `tool_digest_snapshot_test.go` (run with `-race`):
```go
func TestToolDigestSnapshot_ApplySpecModelSwapRace(t *testing.T)
```
One goroutine loops `snapshotToolDigestState()`; another loops `applySpecModel` with a stub `newClient`. Assert no race, no deadlock within 5s, every snapshotted `Client` is either the old or the new pointer, and the swap completes (so `opencodeSessionMu` is never acquired while `clientSnapshotMu` is held).

- [ ] **Step 2: Run to verify failure.**
`go test ./internal/agent/ -run 'TestToolDigestPayload|TestBuildToolDigestPayload|TestToolDigestContextGuard|TestToolDigestKey|TestToolDigestSnapshot' -count=1`
Expected: FAIL — undefined payload, pager, guard, key, and snapshot symbols.

- [ ] **Step 3: Implement the contracts, pager, guard, and snapshot.** Put the toolbox, payload/page structs, guard, key, and snapshot type in `tool_digest_contract.go`; put validation, recursive masking, canonical marshal, and paging in `tool_digest_params.go`; put the lock, `applySpecModel` swap, session-id reads, and snapshot methods in `agent.go`.

- [ ] **Step 4: Run to verify pass.**
`go test ./internal/agent/ -run 'TestToolDigestPayload|TestBuildToolDigestPayload|TestToolDigestContextGuard|TestToolDigestKey|TestToolDigestSnapshot' -count=1 -race`
Also run `gofmt` and `go vet` on `internal/agent`. Expected: PASS.
