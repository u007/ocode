# Orchestrator Pipeline Design

**Date:** 2026-06-19
**Status:** Draft

## Goal

Build a self-healing multi-agent coding pipeline into ocode as a standalone Go
engine. The pipeline accepts a user goal, plans the work, dispatches specialist
subagents (explorer, developer, validator), accumulates context across iterations,
and retries with increasing context until the validator passes or the circuit
breaker escalates to the advisor model.

The pipeline is entirely internal to ocode — it has no dependency on Claude Code,
external scripts, or any other orchestration runtime.

## Problem

Users currently direct a single agent to implement features or fix bugs. That
agent must plan, explore, write code, and self-validate in one session. This
produces brittle results: context is thin, failures are not systematically
retried, and there is no adversarial check on the output.

The orchestrator pattern separates concerns: a Go state machine handles the loop,
timing, and context accumulation; specialist LLM agents handle exploration,
implementation, and validation. Each agent receives exactly the context it needs
— no more, no less.

## Scope

In scope:
- Go state machine in `internal/orchestrator/`
- Three specialist agent files: `orchestrator-explorer`, `orchestrator-developer`,
  `orchestrator-validator`
- Adaptive context document that grows across iterations
- Multi-agent-aware compile/test retry with exponential backoff
- Advisor escalation as circuit breaker
- Three entry points: slash command, named primary agent, CLI flag

Out of scope:
- UI for visualising pipeline state (future)
- Per-agent model selection beyond small-model injection for explorer
- Parallel developer branches (future)
- Persisting pipeline state across ocode restarts (future)

## Architecture

### Package Layout

```
internal/orchestrator/
  pipeline.go         # Pipeline struct, Run() entry point, state machine loop
  states.go           # State constants and transition functions
  plan.go             # Plan struct — immutable task contract
  context_doc.go      # ContextDoc — the growing prompt bundle
  backoff.go          # Retry counter and multi-agent-aware backoff
  advisor.go          # Advisor escalation
  report.go           # StructuredReport — final output
```

### State Machine

```
Planning
  └─► Exploring          (iteration 0 only — runs orchestrator-explorer)
        └─► Developing   (runs orchestrator-developer)
              └─► Compiling  (go build + go vet + lint; retries with backoff)
                    ├─► Validating  (runs orchestrator-validator)
                    │     ├─► Done (VALIDATION_PASSED)
                    │     └─► Developing  (retry with cumulative context)
                    └─► Developing  (compile failed after backoff → send errors)
                          └─► Advising   (circuit breaker: N iterations)
                                ├─► Developing  (advisor unblocked → fresh budget)
                                └─► Done (HALTED — structured failure report)
```

### Loop-Within-Loop Structure

The pipeline loop is Go. Each agent dispatch is a full child session with its
own internal tool-call loop. The pipeline only receives the agent's final output.

```
Pipeline loop (Go)
  └─► Explorer session loop  (LLM tool calls until snapshot complete)
  └─► Developer session loop (LLM tool calls until code written)
  └─► Compile / test (Go — with backoff)
  └─► Validator session loop (LLM tool calls until verdict reached)
  └─► [retry or done]
```

Each agent's `max_steps` budget governs its internal loop depth.

### Integration Points

- Dispatches child sessions via existing `RunChildSession` / `TaskTool` path in
  `internal/agent/`
- Uses `DefaultAgentRegistry` — specialist agents are registered markdown files
- Advisor escalation calls the existing advisor path in `internal/agent/advisor_tool.go`
- Small model injection for explorer via `smallModelEligibleNames` in
  `internal/agent/small_model.go`

## Data Structures

### `Plan` — immutable task contract

Produced once in the Planning state by a planner child session that outputs
structured JSON. Never mutated after parse.

```
Plan
  Intent          string    // "feature" | "bugfix" — detected by planner
  Goal            string    // user's original request verbatim
  SuccessCriteria []string  // what VALIDATION_PASSED means for this task
  FileTargets     []string  // files the planner expects to be touched
  VerifyMode      string    // "llm_only" | "build_llm" | "build_test_llm"
  MaxIterations   int       // default 4, overridable per task
```

VerifyMode is selected by the planner based on intent:
- `llm_only` — quick config change, small refactor
- `build_llm` — bugfix with known scope
- `build_test_llm` — new feature, API change, anything touching public interfaces

### `ContextDoc` — the loop's memory

Mutated by the pipeline on each iteration. Passed to every agent dispatch via
`ContextDoc.Render()` which produces a single structured prompt string.

```
ContextDoc
  Plan             Plan
  ExploreSnapshot  string       // set once at iteration 0; merged with re-explore hits
  Iterations       []Iteration  // one entry appended per developer round
  ReExploreHints   []string     // file paths validator flagged as missing from context
```

```
Iteration
  Number           int
  DeveloperBrief   string       // what the developer was told to do
  FilesChanged     []FileDiff   // git diff of what the developer produced
  CompilerOutput   string       // raw stdout from go build / go vet / lint
  ValidatorReport  string       // structured failure report (empty if passed)
  AdvisorNote      string       // advisor resolution hint (empty unless escalated)
```

### `ContextDoc.Render()` — developer prompt structure

```
[GOAL]
<Plan.Goal>

[SUCCESS CRITERIA]
- <criterion 1>
- <criterion 2>

[CODEBASE CONTEXT]
<ExploreSnapshot>

[PREVIOUS ATTEMPTS]  (omitted on iteration 0)
Attempt 1:
  Changed: <FileDiff summary>
  Compiler: <output>
  Validator rejection: <ValidatorReport>

Attempt N: ...

[YOUR TASK THIS ROUND]
<specific instruction derived from last failure or advisor note>
```

## Agent File Specs

All three agents live in `.opencode/agents/` and are registered via
`DefaultAgentRegistry`. All are `hidden: true` — pipeline-internal only.

### `orchestrator-explorer.md`

```markdown
---
name: orchestrator-explorer
description: Codebase context gatherer for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 20
permission:
  read: allow
  write: deny
  execute: deny
---

You are the explorer agent in an automated coding pipeline. Your job is to
gather codebase context for a developer who will implement a change.

You will receive a goal and optionally a list of target files and re-explore
hints (specific files the validator said were missing from a prior snapshot).

Your internal loop:
1. Glob broadly to map the relevant area
2. Grep for key symbols, types, and callsites
3. Read the smallest relevant excerpts — not whole files
4. Follow imports and references one level deep for each target file
5. If re-explore hints are provided, read those files and merge into snapshot
6. Re-examine your snapshot: is there anything a developer touching these
   files MUST know that you have not captured yet?
7. Only return when your snapshot is complete

Output a single structured markdown snapshot: file paths, relevant excerpts
with line numbers, key types and interfaces, call relationships. No prose.
No suggestions. No fix proposals.
```

Added to `smallModelEligibleNames` in `internal/agent/small_model.go` so the
configured small model is used when `SmallModelEnabled` is true. No `model`
field in frontmatter — injection is Go-side only.

**Difference from built-in `explore` agent:**

| | `explore` | `orchestrator-explorer` |
|---|---|---|
| Purpose | Answer "where is X?" questions | Manufacture a developer context snapshot |
| Output | Prose answer + remaining unknowns | Structured markdown snapshot for `ContextDoc` |
| Thoroughness | Caller-specified (quick/medium/thorough) | Always thorough — self-reviews before returning |
| Re-explore | Not supported | Accepts `ReExploreHints`, merges into snapshot |
| Hidden | No | Yes |

### `orchestrator-developer.md`

```markdown
---
name: orchestrator-developer
description: Implementation agent for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 30
permission:
  read: allow
  write: allow
  execute: deny
---

You are the developer agent in an automated coding pipeline. You receive a
fully prepared context bundle — do not re-discover what is already there.

Your internal loop:
1. Read the ContextDoc — understand the goal, prior attempts, and what failed
2. Plan your changes before writing (one sentence per file)
3. Write the changes
4. Read back what you wrote — confirm edits landed correctly and completely
5. Self-review: missing imports? broken references? incomplete stubs? Fix them.
6. Re-examine your completion report — is your confidence honest?
7. Only return when you are satisfied the code is correct

Rules:
- Do not argue with validator reports. Treat them as ground truth.
- Do not repeat changes that already failed in a prior iteration.
- Do not touch files outside FileTargets unless strictly necessary — if you
  must, explain why in your report.
- If you cannot implement something, say so explicitly — do not fake it.

Output exactly this format and nothing else after it:

### Developer Completion Report
- **Files Changed:** [list]
- **What Was Done:** [summary]
- **What Was NOT Done:** [anything deferred or out of scope]
- **Confidence:** high | medium | low
- **Suggested Validator Focus:** [where to look hardest for edge cases]
```

### `orchestrator-validator.md`

```markdown
---
name: orchestrator-validator
description: Adversarial QA agent for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 20
permission:
  read: allow
  write: deny
  execute: deny
---

You are the validator agent in an automated coding pipeline. You are
adversarial by design. Your job is to find what is wrong, not to encourage.

You receive: the goal, success criteria, files changed this iteration, the
developer's suggested focus, and full codebase context.

Your internal loop:
1. Read each changed file fully
2. Cross-reference against every success criterion — check each one explicitly
3. Chase imports, callers, and dependents of changed code — bugs hide there
4. Check the developer's suggested validator focus area
5. Generate a draft failure report
6. Re-examine your draft — are these issues real? Would they actually fail in
   production? Remove false positives. Add issues you missed.
7. Check: is there a file you need that is NOT in your context? If yes, read
   it now, then update your report. Add it to Context Gap.
8. Only output your final verdict when you have exhausted your checks

Output rules — exactly one of:

VALIDATION_PASSED

or:

### Validation Failure Report
- **Issue:** [describe the bug]
- **Target File:** [`path/to/file.go`]
- **Target Line:** [line number if known]
- **Expected Behavior:** [what should happen]
- **Observed Risk:** [what can fail or go wrong]
- **Context Gap:** [optional — file path missing from explore snapshot]

Never output both. Never output prose before or after. The pipeline parses
your output directly.
```

## Backoff: Multi-Agent Awareness

When `Compiling` fails, the pipeline does not immediately forward errors to
the developer. It retries with exponential backoff, assuming another concurrent
agent may have transiently broken the build.

```
BackoffPolicy
  InitialDelay   20s
  MaxDelay       120s
  MaxAttempts    5
  JitterFactor   0.3   // randomise so concurrent agents don't thunderherd
```

Backoff formula: `delay = min(InitialDelay × 2^attempt × jitter, MaxDelay)`

If the build passes within `MaxAttempts` retries, the pipeline continues to
`Validating` — the failure was transient. If still failing after `MaxAttempts`,
the errors belong to the developer's changes and are forwarded.

The developer and validator never see transient multi-agent noise.

## Circuit Breaker & Advisor Escalation

`Pipeline` tracks `iterationCount int`, incremented each time `Developing` is
entered. When `iterationCount >= Plan.MaxIterations` and the validator has not
passed, the pipeline transitions to `Advising`.

### Advisor prompt

```
You are reviewing a failed multi-agent coding pipeline.
The developer has attempted <N> times without satisfying the validator.

[Full ContextDoc.Render() here]

Diagnose WHY convergence failed. Is the plan wrong? Is the validator being
unreasonable? Is there a missing dependency or architectural constraint the
developer was not told about? Provide a specific, actionable resolution
strategy the developer can act on in one more attempt.
```

### Outcomes

**Advisor unblocks:** `iterationCount` resets to 0 (fresh budget). Advisor
note stored in `Iteration.AdvisorNote` and injected into the next developer
prompt. Pipeline transitions back to `Developing`.

**Advisor cannot unblock:** After the advisor resets the iteration budget and
the developer makes one more attempt, if validation still fails the pipeline
transitions immediately to `Done(HALTED)` — it does not continue the retry
loop or re-escalate to the advisor a second time. It emits a
`StructuredReport`:

```
StructuredReport (HALTED)
  Goal                  string
  TotalIterations       int
  AdvisorConsulted      bool
  AdvisorNote           string
  FinalValidatorReport  string     // last rejection with file+line context
  FilesChanged          []FileDiff // what the developer last produced (not reverted)
  RecommendedNextStep   string     // advisor's best guess for human intervention
```

Work-in-progress files are never reverted. The user inherits the last developer
output, tagged as unvalidated.

## Entry Points

### Slash Command

`/orchestrate <goal>` — slash command parser extracts goal, calls
`orchestrator.Run(ctx, goal, session)`. Status lines stream to chat as each
state is entered: `[Planning]`, `[Exploring]`, `[Developing — iteration 2]`,
`[Validating]`, etc.

### Named Primary Agent

`orchestrator` registered in `AgentRegistry` with `Mode: primary`. Selecting
it from the agent picker routes all session messages through
`orchestrator.Run()`. Results and reports accumulate in the transcript.

### CLI Flag

```
ocode --orchestrate "add user validation to the login flow"
ocode --orchestrate "fix nil panic in auth" --verify build_test_llm --max-iterations 6
```

Runs headlessly — no TUI. Streams structured status to stdout. Exits with
code 0 (VALIDATION_PASSED) or 1 (HALTED or error). Final report printed as
markdown.

## Validation Modes

| Mode | Runs before validator LLM call |
|---|---|
| `llm_only` | Nothing. LLM reads files and reasons. |
| `build_llm` | `go build` + `go vet` must pass first. |
| `build_test_llm` | `go build` + `go vet` + `go test ./...` must pass first. |

Selected by the planner during Planning based on intent. Can be overridden via
CLI `--verify` flag.

## Adaptive Context: First vs Retry Iterations

- **Iteration 0:** Explorer runs fully. `ExploreSnapshot` is set. Developer
  receives snapshot + goal.
- **Retry iterations:** Explorer does not re-run unless `ReExploreHints` is
  non-empty. Developer receives cumulative `ContextDoc` with full history of
  all prior attempts, compiler outputs, and validator reports.
- **Re-explore trigger:** If the validator's failure report contains a
  `Context Gap` field naming a file not in the snapshot, those paths are added
  to `ReExploreHints` and the explorer is re-invoked for those specific files
  only before the next developer dispatch.

## Key Design Principle

The loop is the product. Each iteration the pipeline:

1. **Enriches context** — every failure, validator critique, and compiler error
   is folded into the next developer prompt via `ContextDoc`. Nothing is lost.
2. **Controls timing** — the Go loop enforces backoff and circuit breaker
   limits that a prompted LLM cannot reliably honour.
3. **Curates each agent's view** — `ContextDoc.Render()` produces the ideal
   prompt for each dispatch, not a raw transcript dump.

The agents (explorer, developer, validator) succeed because they are given
perfect context on each round — not because they are individually brilliant.
