# Orchestrator Pipeline Design

**Date:** 2026-06-19
**Status:** Draft (rev 2 — advisor review applied)

## Goal

Build a self-healing multi-agent coding pipeline into ocode as a standalone Go
engine. The pipeline accepts a user goal, plans the work, dispatches specialist
subagents (planner, explorer, developer, validator), accumulates context across
iterations, and retries with increasing context until the validator passes or the
circuit breaker escalates to the advisor model.

The pipeline is entirely internal to ocode — no dependency on Claude Code,
external scripts, or any other orchestration runtime.

## Problem

Users currently direct a single agent to implement features or fix bugs. That
agent must plan, explore, write code, and self-validate in one session. This
produces brittle results: context is thin, failures are not systematically
retried, and there is no adversarial check on the output.

The orchestrator pattern separates concerns: a Go state machine handles the loop,
timing, and context accumulation; specialist LLM agents handle planning,
exploration, implementation, and validation. Each agent receives exactly the
context it needs — no more, no less.

## Scope

In scope:
- Go state machine in `internal/orchestrator/`
- Four specialist agent files: `orchestrator-planner`, `orchestrator-explorer`,
  `orchestrator-developer`, `orchestrator-validator`
- Worktree isolation (default on, opt-out via flag/config)
- Adaptive context document with truncation for long runs
- Advisor escalation as one-shot circuit breaker
- Three entry points: slash command (async), named primary agent (intercepted),
  CLI flag (headless)

Out of scope:
- UI for visualising pipeline state (future)
- Per-agent model selection beyond small-model injection for explorer/planner
- Parallel developer branches (future)
- Persisting pipeline state across ocode restarts (future)

## Architecture

### Package Layout

```
internal/orchestrator/
  pipeline.go         # Pipeline struct, Run() entry point, state machine loop
  states.go           # State constants and transition functions
  plan.go             # Plan struct — immutable task contract
  context_doc.go      # ContextDoc — the growing prompt bundle with truncation
  worktree.go         # Worktree setup/teardown (wraps git worktree add/remove)
  backoff.go          # Compile-retry backoff (used only in --no-worktree mode)
  advisor.go          # Advisor escalation
  report.go           # StructuredReport — final output
  parse.go            # LLM output extraction (VALIDATION_PASSED, reports)
```

### State Machine

```
Planning
  └─► Exploring          (iteration 0 only — runs orchestrator-explorer)
        └─► Developing   (runs orchestrator-developer)
              └─► Compiling  (go build + go vet; worktree mode = deterministic,
                    │         no-worktree mode = retries with backoff)
                    ├─► Validating  (runs orchestrator-validator)
                    │     ├─► Done (VALIDATION_PASSED)
                    │     └─► Developing  (retry with cumulative context)
                    └─► Developing  (compile failed → send errors to developer)
                          └─► Advising   (circuit breaker: N iterations)
                                ├─► Developing  (one more attempt with advisor note)
                                └─► Done (HALTED — structured failure report)
```

### Loop-Within-Loop Structure

The pipeline loop is Go. Each agent dispatch is a full child session with its
own internal tool-call loop. The pipeline only receives the agent's final output.

```
Pipeline loop (Go)
  └─► Planner session loop   (LLM tool calls until Plan JSON produced)
  └─► Explorer session loop  (LLM tool calls until snapshot complete)
  └─► Developer session loop (LLM tool calls until code written + self-reviewed)
  └─► Compile / test (Go — deterministic in worktree, backoff in no-worktree)
  └─► Validator session loop (LLM tool calls until verdict reached)
  └─► [retry or done]
```

Each agent's `max_steps` budget governs its internal loop depth.

### Wiring: Pipeline needs a parent `*Agent`

`internal/orchestrator/Pipeline` cannot call child sessions independently —
`TaskTool.Execute()` requires a `*Agent` (mainAgent) for client, config, model,
permission propagation, and supervisor access. The pipeline must be initialised
with the parent `*Agent`:

```go
type Pipeline struct {
    parent    *agent.Agent   // required — drives child session dispatch
    config    PipelineConfig
    // ...
}
```

This is not free wiring. The slash command handler, primary agent intercept, and
CLI flag path each need to obtain the active `*Agent` and pass it to
`orchestrator.New(parent, config)`. Plan for this during implementation.

### Worktree Isolation (default on)

By default the pipeline creates a dedicated git worktree for the run:

```
.worktrees/orchestrator-<runID>/
```

Inside the worktree, `go build ./...`, `go vet ./...`, `git diff`, and all file
reads and writes are isolated from the main working tree. Other concurrent agents
cannot corrupt the pipeline's build state. The worktree is removed on pipeline
exit (pass or halt).

**`--no-worktree` mode** disables isolation — the pipeline operates on the main
working tree. In this mode the compile/test backoff policy activates to absorb
transient multi-agent noise (see Backoff section). The developer and validator
diffs in `--no-worktree` mode may include changes from other agents; this is a
known trade-off the user accepts by opting out.

Worktree mode is the default. `--no-worktree` is available via:
- CLI: `ocode --orchestrate "..." --no-worktree`
- Config: `ocodeconfig.json` → `"orchestrator": { "worktree": false }`
- TUI: toggle in pipeline settings (future)

## Data Structures

### `Plan` — immutable task contract

Produced once in the Planning state by the `orchestrator-planner` child session,
which outputs structured JSON. Parsed and validated by the Go layer. Never
mutated after parse. Parse failure halts the pipeline loudly (no silent default).

The Plan contains only high-level goals and success criteria — it does NOT
prescribe which files to touch. Agents (especially the developer) determine their
own subtasks and file targets autonomously based on the goal and codebase context.

```
Plan
  Intent          string    // "feature" | "bugfix"
  Goal            string    // user's original request verbatim
  SuccessCriteria []string  // what VALIDATION_PASSED means for this task
  VerifyMode      string    // "llm_only" | "build_llm" | "build_test_llm"
  MaxIterations   int       // default 4, overridable per task or CLI
```

VerifyMode selected by the planner based on intent:
- `llm_only` — quick config change, small refactor
- `build_llm` — bugfix with known scope
- `build_test_llm` — new feature, API change, anything touching public interfaces

### `ContextDoc` — the loop's memory

Mutated by the pipeline on each iteration. Passed to every agent dispatch via
`ContextDoc.Render()`.

```
ContextDoc
  Plan             Plan
  ExploreSnapshot  string       // set once at iteration 0; merged on re-explore
  Iterations       []Iteration  // one entry appended per developer round
  ReExploreHints   []string     // file paths validator flagged as missing
```

```
Iteration
  Number           int
  DeveloperBrief   string       // what the developer was told to do
  FilesChanged     []FileDiff   // git diff scoped to the worktree (or developer's
                                // reported files in --no-worktree mode)
  CompilerOutput   string       // raw stdout from go build + go vet
  ValidatorReport  string       // structured failure report (empty if passed)
  AdvisorNote      string       // advisor resolution hint (empty unless escalated)
```

### Truncation: `ContextDoc.Render()`

`ContextDoc` grows unbounded. `Render()` applies a truncation policy before
producing the prompt string to prevent context-window blow-up:

- **Last 2 iterations:** rendered verbatim (full diffs, full compiler output,
  full validator report)
- **Older iterations:** collapsed to a one-line summary per attempt:
  `Attempt N: changed [files] — validator rejected: [first issue title]`
- **ExploreSnapshot:** never truncated; it is the foundation all agents build on

```
[GOAL]
<Plan.Goal>

[SUCCESS CRITERIA]
- <criterion 1>
- <criterion 2>

[CODEBASE CONTEXT]
<ExploreSnapshot>

[PRIOR ATTEMPTS SUMMARY]  (omitted on iteration 0; older than last 2)
Attempt 1: changed [auth.go] — validator rejected: nil dereference on empty input
Attempt 2: changed [auth.go, auth_test.go] — validator rejected: test missing zero case

[RECENT ATTEMPTS]  (last 2 verbatim)
Attempt 3:
  Changed: <FileDiff>
  Compiler: <output>
  Validator rejection: <ValidatorReport>

[YOUR TASK THIS ROUND]
<specific instruction derived from last failure or advisor note>
```

## Agent File Specs

All four agents live in `.opencode/agents/`. All are `hidden: true` —
pipeline-internal only. Registered via `DefaultAgentRegistry`.

### `orchestrator-planner.md`

```markdown
---
name: orchestrator-planner
description: Task planner for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 10
permission:
  read: allow
  write: deny
  execute: deny
---

You are the planner agent in an automated coding pipeline. Your job is to
analyse the user's goal and produce a structured plan that the pipeline will
execute.

You will receive the user's goal. You may read files to understand the
codebase well enough to classify the task, but keep exploration minimal —
the explorer agent handles deep context gathering.

Classify the task:
- "feature" — new behaviour, new API, new capability
- "bugfix" — correcting broken or incorrect existing behaviour

Select verify mode:
- "llm_only" — tiny change, no public interface touched
- "build_llm" — bugfix, internal change
- "build_test_llm" — new feature, public API change, any data-path change

Write 3–5 success criteria: specific, testable conditions the validator can
check. Do not list file names — the developer and explorer determine those.

Output exactly this JSON and nothing else:

{
  "intent": "feature" | "bugfix",
  "goal": "<user's original request verbatim>",
  "success_criteria": ["<criterion>", ...],
  "verify_mode": "llm_only" | "build_llm" | "build_test_llm",
  "max_iterations": 4
}
```

Added to `smallModelEligibleNames` — uses small model when enabled.

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

You will receive a goal and optionally re-explore hints (specific files the
validator said were missing from a prior snapshot).

Your internal loop:
1. Glob broadly to map the relevant area
2. Grep for key symbols, types, and callsites
3. Read the smallest relevant excerpts — not whole files
4. Follow imports and references one level deep for key types
5. If re-explore hints are provided, read those files and merge into snapshot
6. Re-examine your snapshot: is there anything a developer touching this area
   MUST know that you have not captured yet?
7. Only return when your snapshot is complete

Output a single structured markdown snapshot: file paths, relevant excerpts
with line numbers, key types and interfaces, call relationships. No prose.
No suggestions. No fix proposals.
```

Added to `smallModelEligibleNames`. No `model` field in frontmatter —
small model injection is Go-side only (`internal/agent/small_model.go`).

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
  execute: allow  # restricted to: go build ./..., go vet ./...
---

You are the developer agent in an automated coding pipeline. You receive a
fully prepared context bundle — do not re-discover what is already there.

Your internal loop:
1. Read the ContextDoc — understand the goal, prior attempts, and what failed
2. Determine which files to change based on the goal and codebase context
3. Plan your changes before writing (one sentence per file)
4. Write the changes
5. Run: go build ./... and go vet ./... — fix any errors before continuing
6. Read back what you wrote — confirm edits landed correctly and completely
7. Self-review: missing imports? broken references? incomplete stubs? Fix them.
8. Re-examine your completion report — is your confidence honest?
9. Only return when you are satisfied the code compiles and is correct

Allowed shell commands: go build ./... and go vet ./... only.
Do not run tests, install tools, or touch the network.

Rules:
- Do not argue with validator reports. Treat them as ground truth.
- Do not repeat changes that already failed in a prior iteration.
- If you must change a file not obviously related to the goal, explain why.
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
developer's suggested focus area, and full codebase context.

Your internal loop:
1. Read each changed file fully
2. Cross-reference against every success criterion — check each one explicitly
3. Chase imports, callers, and dependents of changed code — bugs hide there
4. Check the developer's suggested validator focus area
5. Generate a draft failure report
6. Re-examine your draft — are these issues real? Would they fail in production?
   Remove false positives. Add issues you missed.
7. Check: is there a file you need that is NOT in your context? If yes, read it
   now, update your report, and add it to Context Gap.
8. Only output your final verdict when you have exhausted your checks

Output rules — your response must contain EXACTLY ONE of the following.
No prose before or after. The pipeline extracts your verdict by substring match.

If everything passes:
VALIDATION_PASSED

If there are issues:
### Validation Failure Report
- **Issue:** [describe the bug]
- **Target File:** [`path/to/file.go`]
- **Target Line:** [line number if known]
- **Expected Behavior:** [what should happen]
- **Observed Risk:** [what can fail or go wrong]
- **Context Gap:** [optional — file path missing from explore snapshot]
```

## LLM Output Parsing (`parse.go`)

LLM output is never parsed with exact string equality. The Go layer uses
substring/sentinel extraction with a re-ask fallback.

**Validator verdict extraction:**
1. Search response for the substring `VALIDATION_PASSED` (case-sensitive)
2. Search response for the substring `### Validation Failure Report`
3. If neither found: re-ask the validator once with:
   `"Your previous response did not contain a verdict. Output only VALIDATION_PASSED or a Validation Failure Report."`
4. If second attempt also malformed: treat as failure with a synthetic report:
   `Issue: validator produced unreadable output — human review required`

**Developer report extraction:**
1. Search for `### Developer Completion Report` sentinel
2. If absent: treat as a completion with `Confidence: low` and log a parse warning
3. Pipeline continues (developer output is informational, not a gate)

**Planner JSON extraction:**
1. Extract first JSON object from response
2. Validate required fields: `intent`, `goal`, `success_criteria`, `verify_mode`
3. If invalid: halt pipeline loudly with:
   `"Planning failed: planner produced invalid JSON — <parse error>"`
   No silent defaults.

## Worktree Isolation & Backoff

### Worktree mode (default)

Pipeline creates `.worktrees/orchestrator-<runID>/` at the start of Planning.
All developer writes, compile checks, and git diffs operate inside the worktree.
On exit (pass or halt), the worktree is removed with `git worktree remove --force`.

Compile failures in worktree mode are deterministic — they belong to the
developer's changes. No backoff. Forward errors to developer immediately.

### `--no-worktree` mode

Pipeline operates on the main working tree. Compile failures may be transient
(another concurrent agent). Backoff activates:

```
BackoffPolicy
  InitialDelay   20s
  MaxDelay       120s
  MaxAttempts    5
  JitterFactor   0.3   // randomise so concurrent agents don't thunderherd
```

Formula: `delay = min(InitialDelay × 2^attempt × jitter, MaxDelay)`

Worst-case backoff per compile entry: ~6 minutes. In no-worktree mode the slash
command runs async/background (see Entry Points) to avoid blocking the TUI.

If build passes within `MaxAttempts` the failure was transient — continue to
`Validating`. If still failing, errors belong to the developer — forward them.

## Circuit Breaker & Advisor Escalation

`Pipeline` tracks `iterationCount int`, incremented each time `Developing` is
entered. When `iterationCount >= Plan.MaxIterations` and the validator has not
passed, the pipeline transitions to `Advising`.

### Advisor prompt

```
You are reviewing a failed multi-agent coding pipeline.
The developer has attempted <N> times without satisfying the validator.

[Full ContextDoc.Render() here — with truncation applied]

Diagnose WHY convergence failed. Is the goal underspecified? Is the validator
applying an unreasonable standard? Is there a missing dependency or architectural
constraint the developer was not told about? Provide one specific, actionable
resolution strategy the developer can act on in a single attempt.
```

### Outcomes

**Advisor unblocks:** Advisor note stored in `Iteration.AdvisorNote` and
injected at the top of the next developer prompt (highest priority — read first).
Pipeline transitions to `Developing` for **one final attempt only**. The advisor
is never consulted a second time.

**Advisor cannot unblock:** If the single post-advisor developer attempt fails
validation, the pipeline transitions immediately to `Done(HALTED)`. No further
retries. Emits `StructuredReport`:

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
output, tagged as unvalidated. In worktree mode, the worktree is preserved (not
removed) on HALTED so the user can inspect or cherry-pick the work.

**Rationale for single post-advisor attempt:** The advisor synthesises all
context and produces its highest-quality hint. If one developer attempt with that
hint still fails, the problem is beyond autonomous resolution — more iterations
will not change the outcome. The user gets the preserved work and advisor note,
which is sufficient to continue manually.

## Entry Points

### Slash Command (`/orchestrate`) — async

`/orchestrate <goal>` launches the pipeline as a background agent run (uses the
existing background task infrastructure). The TUI is not blocked. A status card
appears in the agent activity panel showing current state. Final report is
delivered as a message in the originating session when done.

In worktree mode: no timing concerns. In `--no-worktree` mode: backoff can take
minutes — async is essential.

### Named Primary Agent — session intercept

`orchestrator` is NOT a normal markdown primary agent. It is a special-cased
name intercepted in the session message dispatch loop. When the active agent
name is `"orchestrator"`, the session loop calls `orchestrator.Run()` instead of
the normal LLM turn. This is the only way to route TUI messages to a Go engine
rather than a prompted agent.

The `orchestrator` entry in `AgentRegistry` exists solely for the agent picker
UI (name + description). It carries no system prompt and is never dispatched as
an LLM agent.

### CLI Flag — headless

```
ocode --orchestrate "add user validation to the login flow"
ocode --orchestrate "fix nil panic in auth" --verify build_test_llm --max-iterations 6
ocode --orchestrate "refactor config loader" --no-worktree
```

Runs headlessly — no TUI. Streams structured status to stdout (one line per
state transition). Exits with code 0 (VALIDATION_PASSED) or 1 (HALTED or
error). Final report printed as markdown.

## Compile / Test Commands

No golangci-lint is configured in this repo. The compile step runs:

```
go build ./...
go vet ./...
```

`go test ./...` is added when `VerifyMode` is `build_test_llm`. No other lint
or static analysis tool is invoked unless explicitly added later.

## Validation Modes

| Mode | What runs before validator LLM call |
|---|---|
| `llm_only` | Nothing. Validator reads files and reasons. |
| `build_llm` | `go build ./...` + `go vet ./...` must pass first. |
| `build_test_llm` | `go build ./...` + `go vet ./...` + `go test ./...` must pass first. |

## Adaptive Context: First vs Retry Iterations

- **Iteration 0:** Explorer runs fully. `ExploreSnapshot` is set. Developer
  receives snapshot + goal.
- **Retry iterations:** Explorer does not re-run unless `ReExploreHints` is
  non-empty. Developer receives `ContextDoc.Render()` with truncated history.
- **Re-explore trigger:** Validator failure report `Context Gap` field names a
  missing file → added to `ReExploreHints` → explorer re-invoked for those
  specific paths before next developer dispatch.

## Key Design Principle

The loop is the product. Each iteration the pipeline:

1. **Enriches context** — every failure, validator critique, and compiler error
   is folded into the next developer prompt via `ContextDoc`. Nothing is lost
   within the truncation window.
2. **Controls timing** — the Go loop enforces backoff, circuit breaker limits,
   and async execution that a prompted LLM cannot reliably honour.
3. **Curates each agent's view** — `ContextDoc.Render()` produces the ideal
   prompt for each dispatch, not a raw transcript dump.
4. **Isolates the workspace** — worktree mode ensures compile/test results
   reflect only the pipeline's own changes, not ambient multi-agent noise.

The agents (planner, explorer, developer, validator) succeed because they are
given perfect context on each round — not because they are individually brilliant.
