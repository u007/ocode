---
type: Plan
timestamp: 2026-11-25T07:19:25Z
---
# Part 05 — Server, TUI, and web integration

**Goal:** Wire the explainer into the existing endpoint, TUI dialog, and web dialog with authoritative input, cancellation, full Markdown rendering, and benign cross-tab supersession.

**Risk:** high — the user-facing security dialog plus cross-tab concurrency; edit anchored, the files carry concurrent WIP.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md`.

**Global constraints this part must honor:**
- Synchronous POST, 80s runner deadline; 1 MiB HTTP fallback body; full output no clamp.
- Cross-tab `409 explanation_superseded` is always benign; only non-superseded errors surface.
- TUI and web Markdown; secrets-only masking; no content guard.
- No new endpoint / request field / response field / config key / schema / migration.

**Files:**
- Modify: `internal/server/handler_tool_digest.go`, `internal/server/handler_tool_digest_test.go`
- Modify: `internal/server/handler_permissions_resolve.go`, `internal/server/handler_close.go`, and the `NewSessionManager(..., onEvict)` callback wired in `internal/server/handler.go` to invalidate the owner
- Modify: `internal/tui/model.go`, `internal/tui/perm_digest_test.go`
- Modify: `web/src/components/Chat/PermissionDialog.tsx`, `web/src/components/Chat/PermissionDialog.digest.test.tsx`, and `web/src/api/client.ts` only if needed

**Interfaces — consumed (exact, restated):**
```go
func (a *Agent) SummarizeToolCallWithKeyCtx(ctx context.Context, in ToolCallDigestInput, requestID string) (ToolDigest, error)
func (a *Agent) SummarizeToolCallCtx(ctx context.Context, in ToolCallDigestInput) (ToolDigest, error)
var ErrToolDigestSuperseded = errors.New("tool digest superseded")
func (c *toolDigestCoordinator) invalidate(owner string)
var toolDigestCoord = newToolDigestCoordinator(time.Now)

type ToolCallDigestInput struct {
    Tool    string
    Command string
    Args    json.RawMessage
    Rule    string
    Scope   string
}
type ToolDigest struct {
    Summary string
    Model   string
}
```
The owner is `OpenCodeSessionID()` when non-empty, otherwise the server session id. The success response remains `{"summary": digest.Summary, "model": digest.Model}`.

**Server behavior:** resolve the authoritative ask with the existing `pendingAskDigestInput(sessionID, requestID)` (lock-free `as.mu.TryLock`, trailing tool-call round) or an accepted posted fallback **before** any coordinator admission, cache lookup, or semaphore acquisition. A live parent ask posts a lightweight `request_id` (optionally `tool`), not duplicate `args`; an authoritative ask wins over every posted field, including raw `bash.env.*` assignments. Apply `http.MaxBytesReader` at 1 MiB before JSON decode and return explicit `413` on overflow; never truncate or explain a clipped body. Remove the busy-session `409`. Unknown ask with no usable fallback returns `404 ask_unavailable` without starting a run. Map `ErrToolDigestSuperseded` to `409` with code `explanation_superseded`. Propagate request-context cancellation. Keep the handler synchronous for the full 80s and never hold `h.mu` or `as.mu` across explanation work. Call `toolDigestCoord.invalidate(owner)` from the ask-resolution path (`handler_permissions_resolve.go`), the close path (`handler_close.go`), and the `NewSessionManager(..., onEvict)` callback wired in `handler.go`.

**TUI behavior:** pass the complete `ToolCallDigestInput` plus the pending tool-call id to the coordinator; never serialize through HTTP or rebuild `Args` from `Command`. Store a cancel function with the active digest and cancel it on dialog close, pending-ask change, reset, or replacement. Strip model-supplied ANSI first, then `sanitizeForTUI`, then `renderMarkdown`, then wrap/reflow to the dialog content width. Keep the result inside `permissionBodyText` and the existing permission viewport so height budget, buttons, hit-test rows, and scroll indicator stay stable; support wheel/key scrolling through the full answer and never truncate. `permDigestSeq` rejects late replies. Keep the label `AI summary · {model} · not a safety verdict`.

**Web behavior:** for a live parent ask send the lightweight authoritative payload first; on `ask_unavailable` with complete props, retry once with the full fallback payload; surface `413` rather than clipping. Render with `react-markdown` plus `remark-gfm`, never raw HTML, and disable image rendering entirely so model output creates no outbound image request. Route anchors through `MarkdownLink` / `requestURLOpen`; no direct navigation or automatic external launch. Use wrapping styles for prose, code blocks and tables, plus `max-height` and `overflow-y-auto`. Abort on dialog close and ask change, ignore late replies from superseded controllers, and treat any `409 explanation_superseded` as benign — clear the spinner and optional retry affordance with no error banner, including when another tab or surface replaced the run. Only a non-superseded error surfaces. Keep the same label.

**Red-green steps:**
- [ ] **Step 1: Write the failing tests.** Server (`handler_tool_digest_test.go`):
```go
func TestHandleToolCallDigest_BusySessionNoConflict(t *testing.T)
func TestHandleToolCallDigest_AuthoritativeAskWins(t *testing.T)
func TestHandleToolCallDigest_AskUnavailable404(t *testing.T)
func TestHandleToolCallDigest_BodyOverLimit413(t *testing.T)
func TestHandleToolCallDigest_Superseded409(t *testing.T)
func TestHandleToolCallDigest_CrossTabSuperseded(t *testing.T)
func TestHandleToolCallDigest_CancelOnRequestAbort(t *testing.T)
func TestHandleToolCallDigest_InvalidatesOnAskResolve(t *testing.T)
```
Cover: a running chat turn no longer yields 409; `{request_id}` alone resolves full `Args` including raw `bash.env.*` assignments and posted conflicts lose; missing ask with no fallback returns 404 `ask_unavailable` with zero coordinator starts, cache lookups, or semaphore slots; a fallback body over 1 MiB returns 413 with no truncated explanation; same-key replacement returns 409 `explanation_superseded` while different asks stay 200; another tab replacing the run leaves the losing tab with no banner; client abort cancels the run and adds no transcript row; ask resolution invalidates the owner's active and cached entries.

TUI (`perm_digest_test.go`):
```go
func TestPermDigest_FullStructAndRequestIDHandoff(t *testing.T)
func TestPermDigest_MarkdownAndSanitize(t *testing.T)
func TestPermDigest_ScrollsFullOutput(t *testing.T)
func TestPermDigest_CancelsOnCloseAskChangeReset(t *testing.T)
func TestPermDigest_StaleReplyDropped(t *testing.T)
```
Cover: the full struct and call id reach the coordinator and `Args` is not rebuilt from `Command`; headings/lists/fences render through `renderMarkdown` and ANSI/OSC/bidi are stripped; a long answer scrolls to its end with a stable height budget and nothing truncated; close, reset, and ask change cancel exactly once; a reply from an old `permDigestSeq` is dropped.

Web (`PermissionDialog.digest.test.tsx`): `"sends lightweight live request, retries once with full props"`, `"renders markdown headings/lists/code and never raw HTML or images"`, `"routes links through the confirmation flow"`, `"scrolls long output"`, `"aborts on close and ask change and ignores stale replies"`, `"cross-tab 409 clears spinner with no error banner but shows other errors"`, `"surfaces 413"`.

- [ ] **Step 2: Run to verify failure.**
`go test ./internal/server/ -run 'TestHandleToolCallDigest' -count=1 && go test ./internal/tui/ -run 'TestPermDigest' -count=1 && cd web && pnpm test -- PermissionDialog.digest`
Expected: FAIL — old 409/one-shot behavior; TUI still calls `SummarizeToolCall`; web tests fail on the new request shape.

- [ ] **Step 3: Implement the wiring.** Server: reorder so input resolution precedes coordinator admission, keep `TryLock` only for the transcript read, apply the 1 MiB cap and error mapping, remove the busy-session 409, and add owner invalidation on ask resolve, close, and the `onEvict` callback. TUI: thread the full input and call id, add the cancel func and Markdown/viewport rendering. Web: add the lightweight-then-full retry, safe Markdown scroller, abort handling, the benign-409 branch, and 413 surfacing. Do not introduce an endpoint, request/response field, config key, or schema change.

- [ ] **Step 4: Run to verify pass.**
`go test ./internal/server/ -run 'TestHandleToolCallDigest' -count=1 && go test ./internal/tui/ -run 'TestPermDigest' -count=1 && cd web && pnpm test -- PermissionDialog.digest`
Also run `gofmt` and `go vet` on the touched Go packages. Expected: PASS (use the repo's package manager; plain `pnpm`, not corepack-pinned).
