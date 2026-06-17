---
name: review-changes
description: AI code review using parallel agents with shared context. Use when the user asks to review code changes, run a code review, review uncommitted changes, review a commit, review a branch, or review a PR. Also triggered by: "/review", "review changes", "code review", "review diff".
when_to_use: When the user asks to review code changes, run a code review, review uncommitted changes, review a commit, review a branch, or review a PR. Also triggered by: "/review", "review changes", "code review", "review diff".
---

# Code Review with Parallel Agents

The `/review` command performs AI code review using a **grouped fan-out** pattern with parallel agents sharing context via the notes bus.

## How It Works

### 1. Orchestrator Computes Shared Brief

Before spawning agents, the orchestrator (main agent) computes a **shared brief** once:
- A one-paragraph change set summary
- A caller map for modified symbols (who depends on them)
- Any doc-rule digest the agents need (e.g., project's API conventions)

This brief is seeded into all child agents so they have consistent context.

### 2. Parallel Agent Fan-Out with `shared_notes: true`

The orchestrator spawns 2+ subagent calls in ONE parallel batch, each with `shared_notes: true`:

```
task(agent="code-reviewer", prompt="...", shared_notes=true)
task(agent="code-reviewer", prompt="...", shared_notes=true)
task(agent="code-reviewer", prompt="...", shared_notes=true)
```

When 2+ subagent calls have `shared_notes: true` in the same batch, they become a **group** with a shared notes bus. A single call has nobody to coordinate with and gets no bus.

### 3. Partitioning by Dimension

For typical small diffs, agents are partitioned by review dimension:
- **Correctness**: logic errors, missing nil checks, off-by-one, panic paths
- **Security**: auth bypass, secret leakage, injection, unsafe input
- **Style**: API consistency, naming conventions, code organization
- **Performance**: resource use, algorithmic complexity, unnecessary allocations

For very large diffs (>2000 lines), partition by file instead.

### 4. Cross-Agent Communication via Notes Bus

Each agent can emit findings to the shared bus:

```xml
<oc-note at="symbol-or-snippet">caveman text describing the finding</oc-note>
```

**Rules:**
- Emit only **cross-agent-value findings** (cross-cutting facts, claims on shared files, risks that change another agent's scope)
- Keep own-report-only findings in the agent's final report
- Treat received notes as **LEADS, not facts** — verify against actual code
- Resolve incorrect leads with `<oc-resolve ref="N"/>`

### 5. Reconcile at End

When the group finishes, run reconcile on the bus:
- Dedup exact-duplicate notes (keep all authors in provenance)
- Resolve contradictions (cluster by file/symbol, decide severity)
- For contradictions that can't be settled from notes alone, spawn ONE focused verify agent
- Flag any partition whose agent failed or was cancelled as **UNREVIEWED**

## Output Format

The final reconciled report uses this format (parsed by the TUI):

```
### Summary
[Overall summary of findings]

### Findings

SEVERITY: [error|warning|info|suggestion]
FILE: [file path]
LINE: [line number or 0]
MESSAGE: [description]
SUGGESTION: [suggested fix, if applicable]
```

## Usage

```
/review                    # Review uncommitted changes (git diff HEAD)
/review <file>             # Review specific file(s)
/review commit <sha>       # Review specific commit
/review branch <branch>    # Compare branches
/review pr <number>        # Review GitHub PR
```

## Implementation Details

- **Prompt builder**: `internal/tui/review.go:buildReviewPrompt()`
- **Target detection**: `internal/tui/review.go:detectReviewTarget()`
- **Context retrieval**: `internal/tui/review.go:getReviewContext()`
- **Output parsing**: `internal/tui/review.go:parseReviewOutput()`
- **Overlay rendering**: `internal/tui/review_overlay.go`

## Related

- The notes bus implementation is in `internal/notebus/`
- Parallel agent groups are managed in `internal/agent/group_bus.go`
- Child agent prompts are assembled in `internal/agent/subagent.go`
