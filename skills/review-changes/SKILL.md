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
- The assumed load, read from the README or deploy config: one person running a tool, or many users and processes at once. Scale and performance findings are judged against it, and the summary names it.

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
- **Lean**: code that should not exist or should be smaller, plus consistency with what the repo already does
  - delete: dead code, unused options, speculative features
  - reuse: the repo already has this helper, or already uses a library for this job (name the path). An existing project helper or library beats both a new dependency and a hand-rolled standard-library version; consistency wins.
  - stdlib / native: nothing in the repo covers it and the standard library or platform does; a new dependency for a few lines
  - yagni: an abstraction with one implementation, config nobody sets
  - dry: the diff repeats logic that already exists in the repo, or repeats itself. Search the repo for an existing equivalent before reporting; name both locations. The fix is to call the existing code, or to extract once when two or more copies must change together. Code that looks alike but changes for different reasons is not a finding.
  - split: one function doing several unrelated jobs. Split by job, never by line count.
  - conventions: naming and API shape that differ from the surrounding code
- **Performance**: resource use, algorithmic complexity, unnecessary allocations

For very large diffs (>2000 lines), partition by file instead.

### 4. Classify Every Finding: Logic vs Non-Logic

Triage is useless if a style nit and a changed branch look identical. Every finding MUST be classified by its **origin in the diff**, never by its severity:

- **Logic** — the finding is about a hunk that can change runtime behaviour: control flow (branches, loops, early returns), state or mutation, data shape / schema, ordering and sequencing, boundary conditions, defaults, nullability, error handling / retries / fallbacks, concurrency, side effects (IO, network, persistence), or validation / permission / routing rules.
- **Non-logic** — presentation, metadata, or mechanics only: naming, formatting, comments, docs, log wording, dead-code removal, a pure move/rename, or a dependency bump with no call-site change.

Tie-breakers (apply in order — they close the usual loopholes):

1. **Classify the subject of the finding, not the shape of the fix.** "Missing test for the new branch" and "this branch is unreachable" are both `logic`; the fix being a test or a comment does not make it non-logic.
2. **If deciding requires reasoning about runtime behaviour, it is `logic`.** A changed default, config value, or constant that selects a different code path is `logic`; a comment describing that same value is not.
3. **Pre-existing does not mean non-logic.** A bug that already existed but was found in a logic hunk stays `logic`; append `(pre-existing)` to the message so triage can separate it from what this diff introduced.
4. **Never downgrade to `non-logic` because the logic change looks intentional.** Severity records intent and risk; the tag only records origin.

### 5. Evidence Gate: No Case, No Finding

Every reviewer applies this before reporting, in every dimension:

- A finding needs a concrete case: "this input or situation leads to this wrong result". For a Lean finding the case is the named helper, library or caller that makes the code unnecessary. No case, no finding.
- Re-read the lines and confirm the claim: the caller exists, the value can really be empty, the code really is unused, the helper really does the same job.
- A shortcut whose code comment names its limit (`shortcut: <limit>, <when to upgrade>`) is a decision, not a finding, unless the assumed load already crosses the limit.
- SUGGESTION is the smallest fix that works. Prefer a fix that deletes code; never propose a layer, framework or config the problem does not need.
- No style taste, no "consider", no vague worries.

### 6. Cross-Agent Communication via Notes Bus

Each agent can emit findings to the shared bus:

```xml
<oc-note at="symbol-or-snippet">caveman text describing the finding</oc-note>
```

**Rules:**
- Emit only **cross-agent-value findings** (cross-cutting facts, claims on shared files, risks that change another agent's scope)
- Keep own-report-only findings in the agent's final report
- Treat received notes as **LEADS, not facts** — verify against actual code
- Resolve incorrect leads with `<oc-resolve ref="N"/>`

### 7. Reconcile at End

When the group finishes, run reconcile on the bus:
- Dedup exact-duplicate notes (keep all authors in provenance)
- Resolve contradictions (cluster by file/symbol, decide severity)
- For contradictions that can't be settled from notes alone, spawn ONE focused verify agent
- Flag any partition whose agent failed or was cancelled as **UNREVIEWED**
- Require exactly one `[logic]` / `[non-logic]` tag on every finding; retag a missing or ambiguous tag yourself (ambiguous ⇒ `logic`)
- Never merge a `[logic]` finding into a `[non-logic]` one — if one hunk yields both, report them separately
- Recount the `Logic-related: N of M findings.` line after dedup and merges; never copy an agent's count
- Drop any finding that fails the evidence gate; do not soften it to `info`

## Tools Beyond Grep

Reviewer agents default to `grep`/`read`, but two structural tools often find things text search misses — worth mentioning in agent prompts:

- **`ast_grep`**: matches code by syntax SHAPE (tree-sitter patterns, `$VAR`/`$$$ARGS`), not text. Use to check a bad pattern isn't repeated elsewhere in the diff/codebase (e.g. `if err != nil { return $E }` swallowing errors), or to preview a structural rewrite. Opt-in — requires `plugins.ast` enabled (`/plugin enable ast`) and the `ast-grep` binary on PATH.
- **`ast`**: LSP-backed semantic lookup (references/definitions/callers). Use to check the blast radius of a changed function/symbol signature before flagging a breaking change.

Both are plain tool calls agents can invoke directly when reasoning about correctness or cross-cutting risk — no special wiring needed.

## Output Format

The final reconciled report uses this format (parsed by the TUI):

```
### Summary
[Overall summary of findings — end with: "Logic-related: N of M findings."]

### Findings

SEVERITY: [error|warning|info|suggestion]
FILE: [file path]
LINE: [line number or 0]
MESSAGE: [logic|non-logic] [description]
SUGGESTION: [suggested fix, if applicable]
```

The tag lives **inside** MESSAGE on purpose: it adds no key to the parsed block, so consumers that expect only SEVERITY/FILE/LINE/MESSAGE/SUGGESTION keep working.

Tag rules:
- Exactly one tag, as the first token of MESSAGE, lowercase, brackets included. No `[logic-fix]`, `[refactor]`, `[functional]` synonyms and no severity word as a substitute.
- Order findings by severity; within one severity, put `[logic]` before `[non-logic]` so logic issues are visible at a glance without reordering triage.
- A finding that cannot be tagged is not ready to report — retag it or drop it.
- Lean findings follow the same tag rules and are `suggestion` by default; a `dry` finding is `warning`, because the copies will drift.
- `MESSAGE: [logic] (pre-existing) parse drops the last row` — the `(pre-existing)` marker follows the tag, never replaces it.

## Self-check Before Reporting

- [ ] Every finding's MESSAGE starts with exactly one `[logic]` / `[non-logic]` tag.
- [ ] Findings this diff did not introduce carry `(pre-existing)` after the tag.
- [ ] Summary ends with `Logic-related: N of M findings.` and N equals the count of `[logic]` findings.
- [ ] Every finding names its concrete case, and the lines were re-read to confirm it.
- [ ] Within each severity, `[logic]` findings come first.
- [ ] No finding was retagged to `[non-logic]` merely because the fix is cosmetic or the change looked intentional.

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
