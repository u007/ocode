# Part 04 — Doc maintenance worker (post-job write-back)

Spec: `docs/superpowers/specs/2026-07-06-okf-context-agent-design.md` (section: Write-back). Read it before starting. Depends on Parts 01 and 03.

Global constraints (self-contained copy): maintenance is best-effort — never blocks or fails the user session; failures logged (structured logger) and dropped; all bundle writes under `knowledge.WithBundleLock`; triage gate is strict (only durable knowledge — decisions, gotchas, playbooks, schema/architecture changes — produces actions; Q&A and routine edits are noops); deprecation only, never deletion; shutdown closes the channel, drains the in-flight item, drops queued items with a debug log — no write lands after shutdown; runs only when `DocPromptEnabled` AND `knowledge.DetectBundle` marker present; `go build ./...` + `go test ./internal/agent/` per task; TDD; commit with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

Template (verified): `internal/agent/memory_maintenance.go` — buffered channel cap 64 (`memoryMaintCh`, set up `internal/agent/agent.go:561–567`), non-blocking enqueue `QueueMemoryMaintenance` (:57), worker `memoryMaintenanceWorker` (:68), small-model client selection `memoryMaintenanceClient` (:131), JSON-only planner prompt `buildMemoryMaintenancePrompt` (:202). Call site: `queueMemoryMaintenance(ev)` at `internal/tui/model.go:3092`/`:13132`, transcript slice `memoryMaintenanceContext` (:13143). Known template flaw to NOT copy: the memory channel is never closed and `Agent.Shutdown()` (`agent.go:3346`) leaks the worker — the doc worker must hook shutdown.

---

## Task 9: Queue, worker, triage, execute, shutdown drain, call site

**Files:**
- Create: `internal/agent/doc_maintenance.go`
- Modify: `internal/agent/agent.go` — channel + worker startup next to the memory-maintenance setup (:561–567); drain hook in `Shutdown()` (:3346)
- Modify: `internal/tui/model.go` — enqueue on job completion next to `queueMemoryMaintenance` (:3092), reusing the same tail-of-transcript context builder pattern (:13143)
- Test: `internal/agent/doc_maintenance_test.go`

**Interfaces consumed:** `knowledge.DetectBundle`, `knowledge.NewStore` + `GenerateIndex` (Part 01); `context` subagent dispatch via the TaskTool internal path (Part 03); small-model client selection pattern from `memoryMaintenanceClient`.

**Interfaces produced:**
- `func (a *Agent) QueueDocMaintenance(req DocMaintenanceRequest)` — non-blocking (drop + debug log when the buffer is full), `DocMaintenanceRequest` carrying `WorkDir`, transcript excerpt (last ~8 messages, same shape as memory maintenance), and `Forced bool` + `Focus string` for `/docs update` (Part 05 calls this with `Forced=true`).
- Worker flow per item:
  1. Gate: skip (debug log) unless `DocPromptEnabled` AND bundle marker present.
  2. **Triage (pass 1):** small-model, JSON-only response contract: either `{"decision":"noop"}` or `{"decision":"actions","actions":[{"action":"create|update|deprecate","path":"...","reason":"..."}]}`. Prompt inputs: transcript excerpt + current `index.md` + the strict gate wording from the constraints above; when `Forced`, the gate wording relaxes to "review the bundle for staleness, focus: <Focus>". Unparseable/invalid JSON → log warn, drop item.
  3. **Execute (pass 2):** skip when noop. Dispatch the `context` subagent (small-model eligible, doc tools injected per Part 03) with the triage action list and reasons; it verifies against existing docs (`doc_search`/`doc_get`) then applies via `doc_write`/`doc_deprecate` (which already lock, log, and regen the index).
  4. Any error at any stage: structured error log with what was attempted; continue to next item.
- Shutdown: `Shutdown()` closes the doc-maintenance channel; worker finishes the current item (bounded by the item's context, cancelled on shutdown so it stops at the next boundary), logs dropped queue length at debug, exits. Idempotent close guard (sync.Once or nil check under the same pattern the codebase uses).
- Call site: on the same job-completion event that triggers memory maintenance, enqueue doc maintenance with `Forced=false`.

**Steps:**
- [ ] Write failing tests with a stubbed model client (mirror how memory-maintenance tests stub theirs; if none exist, stub at the client interface used by `memoryMaintenanceClient`): gate skips when flag off / marker absent; noop triage produces zero bundle changes; action triage dispatches execute; invalid triage JSON dropped with no writes; queue full drops without blocking; shutdown before processing → no writes land, worker goroutine exits (verify with a done-channel).
- [ ] Run tests → fail.
- [ ] Implement `doc_maintenance.go`, agent wiring, shutdown hook, TUI call site.
- [ ] `go test ./internal/agent/ -v -run DocMaint` → PASS; `go build ./...`; `go test ./internal/...` (full sweep — the Shutdown change touches shared lifecycle).
- [ ] Commit: `feat(agent): post-job doc maintenance worker with triage gating and shutdown drain`.
