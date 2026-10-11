# Staged patch — `runSummary` → `runSummaryCall` in `concepts/compaction-config.md`

**Status:** NOT APPLIED. Blocked: the OKF bundle is sole-writer owned by the `context`
sub-agent, whose LLM provider quota is exhausted. Disposition decided by the user:
wait for the canonical writer, do not hand-edit `docs/`.

**Baseline:** `git hash-object docs/concepts/compaction-config.md` =
`506ef0a0eac4c2091fce2e5f619e6e3c7844d708` (pre-edit working-tree copy at
`/tmp/cc-baseline.md`). All line references below are from that copy.

## Verified source facts (`internal/agent/compact.go`)

| line | content |
|---|---|
| 856 | `func runSummary(...)` — caps `max_tokens`, then delegates |
| 877 | `return runSummaryCall(ctx, maxRetries, recordUsage, func() (*Message, error) {` |
| 888 | `func runSummaryCall(...)` — owns retry, validation, cancellation |
| 1334 | `summary, err = runSummaryCall(callCtx, rt.SummaryMaxRetries, ...)` — the inline path |

`runSummaryCall`'s own doc comment: "owns the retry, validation and cancellation handling
shared by the batched summary (runSummary) and the inline summary (runInlineSummary)".

## Anchor verification (mechanical)

Script walked every `\w+\.go:\d+` match in the page: **87 occurrences, 84 unique
anchors**, printed the actual source line for each. Every `compact.go:88x`–`101x` anchor
already resolves INSIDE `runSummaryCall`. Only `internal/agent/compact.go:856` is stale —
because it backs the "semantics live entirely in" claim, which is the sentence being
re-attributed.

## The edits

`doc_write` path is `concepts/compaction-config.md` (relative to `docs/`; do NOT pass a
`docs/` prefix or it lands at `docs/docs/...`).

### 1. §2 retry semantics — page line 54

BEFORE:
- Semantics live entirely in `runSummary` (`internal/agent/compact.go:856`): `maxAttempts := maxRetries + 1` (`compact.go:891-893`), and **one additional attempt is reserved for a malformed (template-violating) summary** even when `maxRetries` is `0` (`compact.go:896-898` — the extra attempt is only taken when a malformed summary was already recorded). Backoff between attempts is `attempt × 500 ms` (`compact.go:902`).

AFTER (opening clause only; remainder byte-identical):
- Semantics live entirely in `runSummaryCall` (`internal/agent/compact.go:888`), which the
  batched (`runSummary`) and inline (`runInlineSummary`) paths both call; `runSummary`
  (`compact.go:856`) only caps `max_tokens` and delegates to it. `maxAttempts := maxRetries + 1` (`compact.go:891-893`), and **one additional attempt is reserved for a malformed (template-violating) summary** even when `maxRetries` is `0` (`compact.go:896-898` — the extra attempt is only taken when a malformed summary was already recorded). Backoff between attempts is `attempt × 500 ms` (`compact.go:902`).

**This 856→888 move is the ONLY line-anchor change permitted.**

### 2. §4 label — page line 97
Replace `runSummary` with `runSummaryCall` in the opening "`runSummary` labels a failed
context through the single helper `summaryContextErr` (`compact.go:1010`)". Nothing else in
that sentence or the two after it.

### 3. §4 dead-context count — page line 99
"There are exactly **four** places in `runSummary` that can report a dead context" →
"...in `runSummaryCall` that can report a dead context". Keep all four anchors and the
trailing "One rule, four call sites, no site able to drift."

### 4. §4 drain race — page line 103
"`runSummary` parks on that channel and on `ctx.Done()` (`compact.go:917`)" →
"`runSummaryCall` parks on that channel and on `ctx.Done()` (`compact.go:917`)".

### 5. §8 testability — page line 212
"not deterministically reproducible from outside `runSummary`" → "...from outside
`runSummaryCall`". Leave `TestUsableSummary`, `TestRunSummaryLabelsNonTimeoutCancelAsCancelled`,
`TestRunSummaryLabelsCompactionDeadlineAsTimeout` EXACTLY as written — real test names.

### 6. Amendment record
Append (never rewrite) to the frontmatter `description:`, the `**Description:**` line and
the `- **Status:**` line:
"Amended 2026-10-04: retry/cancellation attribution corrected from `runSummary` to
`runSummaryCall` (shared by the batched and inline paths); the §2 anchor moved
`compact.go:856` → `compact.go:888`."

## Deliberately unchanged

- Page line 41 — the `summary_max_retries` config-table row says "Retries per summary
  batch inside `runSummary`". Still true from the caller's perspective.
- Historical 2026-09-25 / 09-27 / 09-30 / 10-01 amendment notes naming `runSummary` — changelog.
- §7's inline-summary paragraph — already names `runSummaryCall` correctly.
- Every other line anchor on the page.

## Unblock options (for whoever picks this up)

1. `context_model` — the real override. `OcodeConfig.ContextModel` /
   `ContextModelEnabled` are `json:"context_model"` / `json:"context_model_enabled"`
   (`internal/config/ocodeconfig.go:46-47`), consumed at
   `internal/agent/small_model.go:110-114`, gated on BOTH being set:
   `if cfg.Ocode.ContextModelEnabled && cfg.Ocode.ContextModel != "" { spec.Model = cfg.Ocode.ContextModel }`.
   Settable without hand-editing config via `PUT /api/config/context-model`
   (`internal/server/server.go:424` → `HandleSetContextModel` →
   `config.SaveContextModel`/`SaveContextModelEnabled`, both under `withOcodeConfigLock`).
   The value must be a `provider/model` id the normal client router can serve.
   NOT yet established: that `claude-code/...` works here — the advisor's `claude-code`
   path is a CLI shell-out (`executeClaudeCodeAdvisor` → `claude -p`), not an HTTP provider,
   so it is NOT evidence that the id is routable for a sub-agent.
2. Wait for the `opencode-go` weekly quota to reset, then re-dispatch unchanged.
3. Manual route: a throwaway `go run` calling `knowledge.WithBundleLock` → edit →
   `GenerateIndex` → `AppendLog`, so `docs/index.md` and `docs/log.md` stay byte-identical
   to a real `doc_write`. User declined this on 2026-10-04. Re-open only with their say-so.
   Note `git status docs/` showed ~70 concurrently-modified bundle pages at staging time —
   a hand edit without the lock would race a live `doc_write`.
