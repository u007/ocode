---
type: Concept
title: In-batch Task DAG (id / depends_on)
description: 'Wave scheduler for parallel task batches that declare id/depends_on: validation, scheduling, dependency output injection, failure and cancellation semantics, group-bus interaction and scope.'
resource: CLAUDE.md
tags:
  - task
  - dag
  - scheduler
  - subagent
timestamp: 2026-09-30T07:37:09Z
---
# In-batch Task DAG (id / depends_on)

A parallel batch of `task` tool calls may declare an in-batch DAG instead
of a flat parallel fan-out. Two new optional properties on the `task`
schema:

- `id` — a caller-chosen label, unique within the batch.
- `depends_on` — array of `id`s in the same batch that must complete
  successfully before this dispatch starts.

Omitting both is the common case and **preserves today's behavior
exactly**: a flat parallel fan-out over the same `WaitGroup`. The
scheduler lives in `internal/agent/task_dag.go` and is only consulted
when at least one call in the batch declares `id` or `depends_on`.

**Only `task` / `agent` calls participate** (`isDAGEligibleCall`). `id` is an
ordinary parameter name — `agent_status`, `bash_output`, and `kill_shell` are
all `Parallel()` with a *required* `id` — so parsing `id`/`depends_on` off every
parallel call would route no-task batches through the scheduler and make two
`bash_output` calls on different shells collide on the duplicate-id rule. Other
parallel calls still run; they are simply invisible to the id namespace. Never
widen this filter.

### Validation (rejected as a hard error, no node in the affected component runs)

1. `depends_on` set with empty `id`.
2. Duplicate `id`s.
3. Self-edge (a node names its own `id` in `depends_on`).
4. Unknown `id` in `depends_on`.
5. Cycle in the resolved graph (first back-edge reported with its two endpoints).
6. `depends_on` naming a node that sets `run_in_background`. A background
   dispatch returns a `state: running` placeholder immediately instead of a
   result, so releasing a dependent against it would silently run the child
   without the input the schema promised it.

The error is reported **only on the subagent-dispatch positions**. Other
parallel calls in the batch (a `read`, a `grep`) are dispatched normally — one
bad `depends_on` must not cancel unrelated work that merely shared the batch.

### Scheduling

A wave scheduler driven by an in-degree map. A node does **not**
acquire a concurrency slot until every one of its predecessors has
resolved — the wait happens in the scheduler, **never inside the
dispatched child**. The shared `AgentRunRegistry` limiter is the only
slot acquisition; the scheduler does not introduce a second limiter.

This is load-bearing. Routing `depends_on` into `TaskTool.Execute` and
letting the child block on its predecessors would acquire a slot in
`AcquireForRun` and *then* wait, so a 3-node chain under
`max_concurrent_agents=2` hangs forever. (Same hazard the
`pauseOwnSlotForNestedCall` machinery exists to defuse for nested
dispatches.)

### Dependency output injection

When a node starts, each satisfied predecessor's final result is
prepended to the child's `context`, labelled with the predecessor's
`id`. This reuses the existing `Background Context:` system message
that `TaskTool.Execute` already builds for `params.Context`. A
predecessor's output is truncated through the helpers in
`internal/agent/truncate.go` so a verbose child cannot blow out its
dependents' context.

A child's first system message is therefore:

```
Background Context:
Predecessor "a" output:
  <truncated text of a's final result>

Predecessor "b" output:
  <truncated text of b's final result>

<caller-supplied context, if any>
```

### Failure semantics

If a node fails, its transitive dependents do **not** run. Each
skipped node returns a result of the form
`skipped: dependency "<id>" failed`, naming the **first** failing
predecessor in the chain (not the intermediate skipper). No fallback, no
substitution, no partial execution of a node whose inputs are missing.

Failure propagation is per-**edge**, evaluated in `predecessorBlocked` when a
node's predecessor waits return. It must never be a graph-global abort flag:
with one, a node holding no dependencies (including every non-task call in the
batch) gets skipped or not depending on how fast the first failure lands
relative to the other goroutines reaching the check — a race, not a property of
the graph. Cancellation is the one genuinely batch-global signal.

### Cancellation

The scheduler checks `isCancelled` at every wait point. A node that
becomes ready while cancelled is marked skipped without running. A
node already running when cancellation arrives is allowed to finish
(cooperative); its result still flows to dependents. No goroutine
remains parked on a dependency that will never resolve.

### Interaction with the group bus

Orthogonal, with two lifecycle requirements:

- The bus (`Bus.Start(ctx)` / `Bus.Stop()`) **brackets the entire DAG
  execution**, not a single wave. Late nodes start after early nodes
  have finished, so a per-wave bus would drop shared history and
  leave `groupTracker` with completions it never sees. The reconcile
  hand-off runs after the last node resolves.
- Group-bus agent ids are assigned in **batch order** (`a1`, `a2`, …)
  and stay stable even though execution order now varies. The id is
  never derived from launch order.

Worth stating plainly: nodes on opposite ends of a dependency edge
never run concurrently, so for them the bus degrades from live
collaboration to an append-only log the later node can read. That is
fine — the dependency edge already carries the predecessor's output
directly — but `shared_notes` and `depends_on` solve different
problems and neither substitutes for the other.

### Cache stability

`id` and `depends_on` are static schema properties (one-time tools
change). They travel in call arguments only, never in the tool
description — a description that enumerated live ids would rewrite the
tools array every turn and bust the whole cached prefix.

### Scope

- **In-batch only, v1.** A `depends_on` referencing a `task_id` from
  an earlier turn (or an in-flight background run) is out of scope.
  Recorded in root `TODO.md` as a deferred item.
- **No nested-batch DAGs.** A child's own `task` batch gets the same
  scheduler independently; edges do not cross dispatch boundaries.
- **Not a workflow engine.** No persistence, no retries-on-edge, no
  conditional routing, no `@router`-style branching. If a declarative
  multi-step pipeline is wanted later, that belongs in
  `internal/orchestrator`, built on this.
