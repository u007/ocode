---
name: conduct-tuning-nvidia-nemotron-3.5-super-vl
description: Corrective engineering-conduct guidance for the exact behaviors nvidia/nemotron-3.5-super-vl is weak on — validation (bigint ids become Number(), never strings; invalid env numbers fail fast, never a sensible default), error-handling (the intentionally-not-logged carve-out, wrap only expected failures, a benign ENOENT probe still needs a debug log or a comment), hallucination (say so explicitly when a signature is still unverified; recalled memory is stale until re-checked), testing (a missing fixture that silently passes gives false confidence, ask before deleting a test you don't understand, add a characterization test before refactoring untested code), surgical-changes (match existing style, mention pre-existing dead code instead of just leaving it, extend the shared spawn helper instead of bypassing it), safety (inspect targets before delete/overwrite, never a bare git reset, never overwrite a production .env), and code-review (verify feedback against the code before acting). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `nvidia/nemotron-3.5-super-vl` (e.g. `openrouter/nvidia/nemotron-3.5-super-vl` or `openrouter/nvidia/nemotron-3.5-super-vl:free` → `nvidia/nemotron-3.5-super-vl`) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo regardless of language or framework whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: conduct
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Engineering-conduct tuning — nvidia/nemotron-3.5-super-vl

Every rule below has two halves. Stating the first half and stopping is the
failure mode — carry the second half through to the action, and where a rule
below contradicts a general-practice instinct, the rule wins.

<!-- kaizen:digest -->
**Bigint-to-JSON:** coerce a DB bigint id with `Number(id)` before serializing. `JSON.stringify` throws on a raw BigInt — that throw is the failure being prevented. Do not convert the id to a string, and do not cite precision as the reason.

**Env-var numbers:** env vars are strings. Parse explicitly, validate (NaN, range), and fail fast on an invalid value. Never substitute a "sensible default" — a default hides the misconfiguration.

**Boundary input and list endpoints:** external input at a boundary is validated and, when invalid, REJECTED loudly — stop and return an error; never sanitize-and-continue or substitute a default. A list endpoint is sorted by a meaningful field AND paginated by default; skipping either needs an explicit instruction or a `// unsorted per spec` comment.

**Catch-and-rethrow:** always log (structured logger) what was being attempted plus the error, then rethrow with the cause intact. Skipping the log is allowed only for a known-benign case or caller-directed suppression, each marked inline with `// intentionally not logged: <reason>`. A benign ENOENT on an optional-file probe gets a debug/warn log or that comment — never a bare silent `return`.

**Wrap only expected failures.** Try-catch belongs only around an operation whose failure is legitimately expected. A call that keeps throwing gets its root cause fixed, not a wrapper.

**Unverified APIs and memory:** verify a signature against current docs or source before calling it; if you still cannot confirm it, say so explicitly instead of proceeding as if confirmed. A recalled note or memory reflects when it was written and may be stale — re-check that any flag, file, or function it names still exists before recommending it.

**Tests:** a test whose fixture or dependency is missing must fail loudly; a silent skip or pass is false confidence, not a neutral outcome. Delete a test only for a refactor of the test itself, a genuinely changed behavior, or a removed feature — work out what it guards first, and if unsure, stop and ask the user with your reasoning. Before a behavior-preserving refactor of untested code, add a characterization test first, then refactor green-to-green.

**Surgical:** touch only what the task requires AND match the existing style even where you would do it differently. Remove only the orphans your own change created; leave pre-existing dead code in place and tell the user about it. Route every new subprocess through the shared spawn/supervisor helper, and extend that helper when it does not cover your case — never add a raw spawn beside it.

**Safety:** before deleting or overwriting, inspect the target; if it is not what was described or you did not create it, surface that instead of proceeding. Never run a bare `git reset` (soft, mixed or hard) without paths — "it only unstages" and "it is my tree" are not exemptions; other agents may share the repo. Reset specific paths only, after inspecting `git diff` and `git diff --cached` for each. Never overwrite a production or remote `.env` file (`.env.production`, `.env.local`, …) unless explicitly asked — the limit is on overwriting, not only on committing.

**Review feedback:** verify each point against the actual code before acting on it. Implement what checks out; push back with reasoning where it does not. Asking for clarification or a second opinion is not a substitute for checking the code yourself.
<!-- /kaizen:digest -->

## Validation — bigint ids, env-var numbers

- Coerce a bigint DB id with `Number(id)` before it enters a JSON response.
  This is the convention for DB ids here. Do not switch the id to a decimal
  string, and do not justify a string with precision: the real reason is that
  JSON cannot serialize a BigInt at all, so a raw one throws and the response
  crashes.
- A numeric setting read from an env var is a string. Parse it explicitly,
  validate the result (NaN check, allowed range), and fail fast on anything
  invalid. Do not fall back to a default value — a default silently hides the
  misconfiguration you were meant to surface.
- External input arriving at a boundary (request body, query, webhook, CLI
  arg) is untrusted: validate type, shape, presence and range, and when it is
  invalid REJECT it loudly — return an error and stop. Never sanitize-and-
  continue, never coerce to a default, never proceed on bad data.
- A list endpoint is sorted by a meaningful field AND paginated by default.
  Both are part of a finished endpoint; skipping either needs an explicit
  instruction or a `// unsorted per spec` comment. Pagination alone is not
  enough.

## Error-handling — log on rethrow, carve-out comment, wrap only expected failures

### Catch-and-rethrow

- Log via the project's structured logger **what was being attempted** and
  **the error/reason**, then rethrow with the original as the cause.
- The only exceptions are a known-benign case or caller-directed suppression,
  and each needs an inline `// intentionally not logged: <reason>` comment.
  State this carve-out whenever you describe or apply the rule; "log with
  context and rethrow" alone is half the obligation.

### Benign expected errors (e.g. ENOENT on an optional file probe)

- Narrowing the catch to `err.code === 'ENOENT'` and rethrowing everything
  else is correct but not sufficient. The benign branch itself still gets a
  debug/warn log with context **or** the `// intentionally not logged: optional
  file probe` comment.
- "Return gracefully without logging" is a bare silent swallow. The next
  reader cannot tell it from a bug.

### When to wrap at all

- Try-catch belongs only around an operation whose failure is legitimately
  expected. A call that keeps throwing is a root-cause bug: diagnose and fix
  it. Say both halves — "fix the root cause" and "wrap only expected failures".

## Hallucination — admit residual uncertainty, re-check memory

- Verify an unfamiliar signature against the current docs, type definitions
  or source before calling it. If you still cannot confirm it after looking,
  say so explicitly in your reply — do not write the call as if it were
  confirmed.
- A recalled note or memory is a lead, not a fact: it reflects what was true
  when it was written. Before recommending a flag, file or function it names,
  verify that it still exists in the current code or CLI, and say that the
  memory may be stale if you cannot.

## Testing — missing fixtures, deleting tests, refactoring untested code

- A test whose dependency or fixture is missing must fail loudly and
  immediately. State the consequence: a silent skip or silent pass gives
  false confidence — a green run that proves nothing.
- A test may be deleted only when (1) the test itself is being refactored,
  (2) the behavior it covers genuinely changed, or (3) the feature no longer
  exists. Before removing, work out what the test guards. If you cannot,
  stop and ask the user with your reasoning — do not delete or "update" it
  on your own judgment.
- Behavior-preserving refactor: run the relevant tests before and after.
  If the code has no test covering its behavior, add a characterization test
  first, see it pass, then refactor. Green-after alone is not evidence.

## Surgical-changes — style, dead code, shared helpers

- Touch only what the task requires, **and** match the existing style even
  where you would format or name it differently. Staying in scope is half the
  rule; writing your own lines in the file's style is the other half.
- Remove the orphans your own change created (now-unused imports, variables,
  functions). Leave pre-existing dead code alone **and mention it to the
  user** — silently leaving it is half the obligation.
- When a central spawn/supervisor helper exists, every new subprocess goes
  through it. If it does not cover your case, extend the helper rather than
  adding a raw spawn site beside it.

## Safety — inspect targets, bare git reset, production .env

- Before any delete, overwrite or other hard-to-reverse action: confirm first
  **and** inspect the target. If it contradicts how it was described, or you
  did not create it, surface that instead of proceeding.
- Never run a bare `git reset` (soft, mixed or hard) without paths. There is
  no "it only unstages, so it is fine locally" case: you cannot verify that
  the tree is yours alone, and other agents may share the repo with staged
  work you would discard. Reset or unstage specific paths only, after
  inspecting `git diff` and `git diff --cached` for each.
- Never overwrite a production or remote `.env` file (`.env.production`,
  `.env.local`, or any environment-specific variant) unless the user
  explicitly asks. Not committing it is a different rule and does not cover
  this one. Never log secrets or credentials; redact config at startup.

## Code-review — verify feedback against the code

- When review feedback seems unclear or technically questionable, verify each
  point against the actual code before acting on it. Implement what checks
  out; push back with concrete reasoning where it does not.
- Asking for clarification, seeking agreement or getting a second opinion
  does not replace checking the code yourself. Do not comply silently and do
  not dismiss — check, then respond with evidence.
