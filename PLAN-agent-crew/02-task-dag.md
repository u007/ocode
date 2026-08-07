# 02 — Task dependencies (`id` / `depends_on`) and in-batch DAG scheduling

## How it currently works

When the model returns a batch of tool calls, `Agent.Step`
(`internal/agent/agent.go`, around the `parallelTCs` / `sequentialTCs` split)
partitions them by whether the tool reports `Parallel()`. The `task` tool is
parallel, so every `task` call in the batch is launched at once in a single
`sync.WaitGroup` fan-out, each goroutine calling `handleToolCallWithImages`.
Concurrency is bounded downstream by the shared `AgentRunRegistry` limiter, not
by the batch itself.

If two or more of those calls carry `shared_notes:true`, `maybeBuildGroupBus`
creates a `notebus.Bus` and hands each child a stable agent id, so peers can
post notes, touches, and a reconcile hand-off. That is *lateral* coordination
between simultaneous siblings.

Result passing between children does not exist. The only channel is the caller
manually setting the `context` argument, which becomes a `Background Context:`
system message on the child's fresh transcript.

## Where it breaks

There is no way to express "B needs A's output". The model has exactly two bad
options:

- **Serialize by turn.** Dispatch A, wait for the whole turn to round-trip,
  read A's result, then dispatch B with A's output pasted into `context`. Costs
  a full extra model turn per edge in the dependency chain, and the paste is
  lossy and manual.
- **Fan out anyway.** Launch A and B together and hope B does not need A. This
  is what a model under instruction to "work in parallel" actually does, and it
  is the source of children that duplicate work or contradict each other.

`shared_notes` does not solve this: the bus is for peers running *at the same
time*, with no ordering guarantee that A has posted anything before B reads.

CrewAI's `Task(context=[other_task])` is the missing primitive: a declared edge
that both orders execution and injects the predecessor's output.

## Proposed change

Let a single batch of `task` calls describe a DAG, and execute it as one.

### Surface

Two new optional properties on the `task` tool schema:

- `id` — a caller-chosen label for this dispatch, unique within the batch.
- `depends_on` — array of `id`s in the same batch that must complete
  successfully before this dispatch starts.

Omitting both preserves today's behavior exactly: a flat parallel fan-out.

### Scheduling

Replace the single WaitGroup fan-out over `parallelTCs` with a small scheduler
that runs *only* when at least one `task` call in the batch declares
`depends_on`. Otherwise the existing code path runs untouched — this keeps the
common case byte-for-byte identical and keeps the risk contained.

The scheduler:

- Builds the graph from the `id`/`depends_on` values present in this batch.
- **Validates before launching anything.** An unknown `id` in `depends_on`, a
  duplicate `id`, or a cycle is a hard error: the offending calls return an
  error result naming the problem, and no node in the affected component runs.
  Non-task parallel calls in the batch are unaffected and still fan out.
- Launches every node whose dependencies are satisfied, concurrently. As each
  completes, it releases its dependents. Concurrency stays bounded by the
  existing shared `AgentRunRegistry` limiter — the scheduler does not introduce
  a second limiter.
- Honors cancellation at every wait point, using the same `cancelled()` /
  `StopCh()` checks the current fan-out uses. A cancelled batch must not leave
  goroutines parked on a dependency that will never resolve.

### Dependency output injection

When a node starts, each satisfied predecessor's final result is prepended to
the child's `context`, labelled with the predecessor's `id`. This reuses the
existing `Background Context:` system message that `TaskTool.Execute` already
builds for `params.Context` — the child has a fresh transcript, so a system-role
message there carries no cache cost (unlike the parent's turn, where volatile
system content is forbidden).

Predecessor output is truncated through the existing helpers in
`internal/agent/truncate.go` so a verbose child cannot blow out its dependents'
context.

### Failure semantics

If a node fails, its transitive dependents **do not run**. Each skipped node
returns a result that names the failed dependency explicitly. No fallback, no
substitution, no partial execution of a node whose inputs are missing. The
parent model sees exactly which edge broke.

### Scope within the batch

The scheduler governs only `task` calls inside the **parallel** partition of the
batch. Non-parallel tools (`sequentialTCs`) keep their existing separate
execution path and cannot participate in the graph; a `depends_on` naming one is
an unknown-id error.

### Interaction with the group bus

Orthogonal, with two lifecycle requirements:

- `Bus.Start(ctx)` / `Bus.Stop()` must **bracket the entire DAG execution**, not
  a single wave. Late nodes start after early nodes have finished, so a
  per-wave bus would drop the shared history and leave `groupTracker` with
  completions it never sees. The reconcile hand-off runs after the last node
  resolves.
- Group-bus agent ids are assigned in **batch order** (`a1`, `a2`, …) and must
  stay stable even though execution order now varies. The id must never be
  derived from launch order.

Worth stating plainly so nobody expects more than is there: nodes on opposite
ends of a dependency edge never run concurrently, so for them the bus degrades
from live collaboration to an append-only log the later node can read. That is
fine — the dependency edge already carries the predecessor's output directly —
but `shared_notes` and `depends_on` solve different problems and neither
substitutes for the other.

## Constraints and non-goals

- **Slot invariant (deadlock guard):** a node must not acquire a concurrency
  slot until every one of its dependencies has resolved. All waiting happens in
  the scheduler, **never inside a dispatched child**. The tempting alternative —
  thread `depends_on` into `TaskTool.Execute` and let the child block on its
  predecessors — acquires the slot in `AcquireForRun` and *then* waits, so a
  3-node chain under `max_concurrent_agents=2` hangs forever. (This is the same
  hazard `pauseOwnSlotForNestedCall` exists to defuse for nested dispatches.)
- **Cache:** `id` and `depends_on` are static schema properties (one-time tools
  change). They travel in call arguments only, never in the tool description —
  a description that enumerated live ids would rewrite the tools array every
  turn and bust the whole cached prefix.
- **In-batch only, v1.** `depends_on` referencing a background run id from an
  earlier turn is out of scope. It is genuinely useful and genuinely more
  complex (cross-turn lifetime, resume, cancellation). Record the deferral in
  `TODO.md`.
- **No nested-batch DAGs.** A child's own `task` batch gets the same scheduler
  independently; edges do not cross dispatch boundaries.
- **Not a workflow engine.** No persistence, no retries-on-edge, no conditional
  routing, no `@router`-style branching. If a declarative multi-step pipeline is
  wanted later, that belongs in `internal/orchestrator`, built on this.

## Files touched

| File | Change |
|------|--------|
| `internal/agent/task_dag.go` (new) | graph build, validation (unknown id / duplicate / cycle), wave scheduler, cancellation handling |
| `internal/agent/agent.go` | in the parallel-batch section: detect declared dependencies and route to the scheduler; unchanged fan-out otherwise |
| `internal/agent/subagent.go` | `id` / `depends_on` in schema + `taskToolParams`; predecessor-output injection into the child's context |
| `internal/agent/group_bus.go` | assert agent-id assignment stays batch-order-derived, not launch-order-derived |
| `internal/agent/truncate.go` | reuse for predecessor-output bounding (no change expected; confirm the helper fits) |
| `TODO.md` | record the deferred cross-turn `depends_on` |
| `AGENTS.md` | document the DAG batch semantics and the failure/skip rule |

## Tasks

1. Add `id` / `depends_on` to the `task` schema and `taskToolParams`
   → verify: unit test asserts a batch with neither field is classified as
   "no dependencies declared" and takes the legacy path.
2. Build the graph + validator → verify: table test covering valid DAG, unknown
   id, duplicate id, self-edge, and multi-node cycle; each invalid case asserts
   the specific error text and that nothing in the affected component launched.
3. Implement the wave scheduler over the validated graph, bounded by the shared
   limiter → verify: test with instrumented dispatches asserts a dependent never
   starts before its predecessor finished, and that independent nodes overlap.
   **Run the same chain with `max_concurrent_agents=1` and assert it completes**
   — an ordering-only test passes on the deadlocking implementation whenever the
   limiter is unbounded, so the low-limiter case is the one that proves the slot
   invariant.
4. Inject predecessor results into dependents' context, labelled and truncated
   → verify: test asserts the child's first messages contain the predecessor's
   labelled output and that oversize output is bounded.
5. Implement skip-on-failed-dependency with an explicit result message
   → verify: test asserts a transitive dependent two hops down is skipped and
   names the original failing node.
6. Wire cancellation through every wait point → verify: test cancels mid-wave
   and asserts no goroutine remains parked and every unstarted node reports
   cancelled.
7. Confirm group-bus ids remain stable under reordered execution and that the
   bus spans the whole DAG → verify: existing group-bus tests extended with a
   batch that also declares dependencies; test asserts a late node still reads
   notes posted by a predecessor that already finished, and that `Stop()` is
   called once, after the final node.
8. Record the deferred cross-turn dependency support in `TODO.md`, and document
   the semantics in `AGENTS.md` → verify: re-read against shipped behavior.
