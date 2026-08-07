# PLAN: Agent Crew — task contracts, task DAG, persistent todo

Three related but independently shippable changes that bring CrewAI's
task-contract / task-dependency model and Manus's durable todo file into
ocode, reusing existing machinery rather than adding subsystems.

## Motivation

ocode already has the pieces of a "crew": `AgentDefinition` (role + tools +
model + permissions), `TaskTool` → `DispatchSubagent` (task execution),
`AgentRunRegistry` (async lifecycle + concurrency limiter), `notebus`
(peer collaboration), and `orchestrator.Pipeline` (a hardcoded sequential
process). What is missing is the part CrewAI makes load-bearing:

1. **No output contract.** A subagent returns free text; nothing checks it is
   the *shape* of result the caller asked for. Failures surface several steps
   downstream, or never.
2. **No task dependencies.** Ordering and result-passing between subagents is
   the model's manual job — it must serialize work it could parallelize, or
   parallelize work with a real dependency and paste outputs by hand.
3. **No durable plan.** `todowrite`/`todoread` keep the list in a process-global
   map keyed by session id. It does not survive restart, is not visible to the
   user as a file, and is not re-anchored into context after compaction — which
   is precisely the mechanism that keeps long-horizon runs from drifting.

## Parts

| Part | Title | Depends on |
|------|-------|-----------|
| `01-task-contracts.md` | `expected_output` on `task` + verify-and-retry-once | none |
| `02-task-dag.md` | `id` / `depends_on` on `task` + in-batch DAG scheduling | none (independent of 01, composes with it) |
| `03-persistent-todo.md` | Durable markdown todo file, main-agent-only writes, re-anchoring | none |

Each part file is self-contained: it restates the context it needs, lists the
files it touches, and carries its own numbered task list with verification
steps. There are no cross-references between parts.

## Execution plan

Three phases, in this order. Each phase ends at a checkpoint that must hold
before the next begins; a phase is never half-landed.

**Rationale for the order:** 01 has the smallest blast radius and makes 02
trustworthy (a dependency graph of unverified outputs is just faster
wrongness). 03 is independent of both and touches a different area of the tree
(`internal/tool`, `internal/tui`), so it can slot in while 01 settles. 02 goes
last because it modifies the parallel batch executor in
`internal/agent/agent.go` — the highest-risk file of the three — and benefits
from 01 already being there.

### Phase 1 — `01-task-contracts.md` (8 tasks)

Order: 1 → 2 → 3 → 4 → 5 → 6 → 8, with 7 (TUI + web badges) last and
separable — the mechanism is complete without it, since the verdict already
lives on `AgentRun` and in `agent_status`. Cut or defer 7 if the phase needs
trimming; if deferred, record it in `TODO.md`.

**Checkpoint:** a dispatch with no `expected_output` is byte-identical to
today's behavior (assert this explicitly — it is the whole backwards-compat
story); a contracted dispatch that fails verification retries once in place and
returns the child's full result behind a warning prefix; `go build ./...` and
`go test ./internal/agent/...` clean.

### Phase 2 — `03-persistent-todo.md` (9 tasks)

Order: 1 (lock extraction) → 2 (store) → 3 (repoint tool surface) → 4/5
(ops + guards) → 6 (subagent write-scope) → 7 (re-anchoring) → 8 (snapshot/undo)
→ 9 (ignore + docs).

Task 1 lands on its own: extracting `WithFileLock` and re-pointing
`knowledge.WithBundleLock` at it is a pure refactor whose existing tests must
pass untouched before anything is built on top.

**Checkpoint:** a todo survives restart and session resume; `TodoState()` does
no disk I/O; a stale-revision write is rejected with current content; a
destructive full replace is rejected; a subagent has `todoread` and not
`todowrite`/`todo_update`; the re-anchored block is user-role with the
`[ocode:todo]` marker and adds no system-role message.

### Phase 3 — `02-task-dag.md` (8 tasks)

Order: 1 → 2 (graph + validation, no execution yet) → 3 (scheduler) → 4
(output injection) → 5 (skip-on-failure) → 6 (cancellation) → 7 (group bus) →
8 (`TODO.md` + `AGENTS.md`).

Tasks 1–2 are safe to land alone: validation with no scheduler still routes
every batch down the legacy path, so nothing changes behaviorally until task 3.

**Checkpoint:** a batch declaring no dependencies takes the legacy fan-out path
unchanged; a 3-node chain completes under `max_concurrent_agents=1` (the test
that actually proves the slot invariant); a failed node's transitive dependents
are skipped and name the original failure; a cancelled batch leaves no parked
goroutine.

## Review findings incorporated

This plan was reviewed against the code once written. Six findings, all now
anchored in the part files — recorded here so the reasoning is not re-litigated
during implementation.

| # | Finding | Anchored in |
|---|---------|-------------|
| 1 | `TodoState()` is called from `renderSidebar` — a per-frame render path, so the store must never read from disk there | 03 → "The file is durable state; memory stays the read path" |
| 2 | `ResetTodoState()` (`/new`, `/clear`, session switch) must clear memory only — deleting the file would destroy the outgoing session's plan | 03 → "Signatures and lifecycle" |
| 3 | `Bus.Start`/`Stop` must bracket the whole DAG, not one wave — late nodes start after early nodes finish | 02 → "Interaction with the group bus" |
| 4 | Default contracts on built-in agents would tax every `knowledge_lookup`; ship contracts opt-in per call | 01 → "Constraints and non-goals" |
| 5 | The verifier checks output *shape*, not truth — must not be presented as "verified correct" | 01 → "Constraints and non-goals" |
| 6 | The DAG governs only `task` calls in the parallel partition; `sequentialTCs` keep their existing path | 02 → "Scope within the batch" |

Verified during review and deliberately **not** mitigated: `NewSessionID()` has
one-second granularity, so two sessions born in the same second share a todo
filename. That collision already exists at the session-storage layer; the todo
file inherits it and introduces no new failure mode.

## Cross-cutting invariants (apply to all three parts)

**Prompt-cache stability.** Per `AGENTS.md`, the Anthropic prefix is
`tools` → `system` → `messages`, so any change to the tools array invalidates
the cached system block and message prefix too.

- Adding static properties to the `task` / todo tool schemas is a **one-time**
  change: byte-identical on every subsequent turn, so it re-caches once and then
  costs nothing. This is acceptable.
- **Invariant:** contract text, dependency ids, and todo content live in tool
  **call arguments** and tool **results** — never interpolated into a tool
  **description**. A description that lists "current task ids" or "current todo
  items" would rewrite the tools array every turn and bust the entire prefix.
- **Invariant:** any re-injection of todo content into the turn goes into the
  **user-role volatile tail** (the `injectDiscoveryContext` pattern), never a
  `system`-role message — `collectAndRemoveSystemMessages` hoists every
  system-role message, including tail ones, into the cached system block, so
  per-turn-varying system content rewrites the cache every turn.

**Fail loud, no fallbacks.** A failed contract check, an unparseable todo file,
a cyclic dependency graph, or a dependency whose predecessor failed must produce
a visible error in the tool result. No silent defaults, no "best effort"
substitution, no empty `catch`-equivalents. Every swallowed error path gets a
log line with what was attempted and why it failed.

**No new behavior flags.** None of these parts introduces an optional parameter
that changes behavior (`force`, `strict`, `confirm_destructive`, …). Where a
guard is needed, the guard is unconditional and the rejection message tells the
model what to do instead.

**Backwards compatibility.** Every new schema property is optional. Omitting
`expected_output` skips verification entirely (zero added cost, unchanged
behavior). Omitting `depends_on` runs the batch exactly as it runs today.

## Deferred work

Anything scoped out in a part file must also be recorded in `TODO.md` at the
repo root, per project rules — a note inside a plan file is not sufficient
tracking for an unfinished feature.
