---
name: conduct-tuning-step-5-preview-free
description: Corrective engineering-conduct guidance for the exact behaviors step-5-preview-free is weak on — validation (bigint ids become Number(), never strings), error-handling (log what-was-attempted plus the error on rethrow with the intentionally-not-logged carve-out, wrap only expected failures, a benign ENOENT probe still needs a debug log or a comment), testing (ask before deleting a test you don't understand, add a characterization test before refactoring untested code), surgical-changes (match existing style, extend the shared spawn helper instead of bypassing it, grep for naming precedent then the project's documented convention), lifecycle (state assumptions when asking, record deferred work in TODO.md), and safety (inspect targets before delete/overwrite, never a bare git reset even on a tree you think is yours). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `step-5-preview-free` (e.g. `opencode-go/step-5-preview-free` → `step-5-preview-free`) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo regardless of language or framework whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: conduct
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Engineering-conduct tuning — step-5-preview-free

Every rule below has two halves. Stating only the first half is the failure
mode — always carry the second half through to the action.

<!-- kaizen:digest -->
**Bigint-to-JSON:** coerce a DB bigint id with `Number(id)` before serializing. `JSON.stringify` throws on a raw BigInt — that throw is the failure being prevented. Do not convert the id to a string.

**Catch-and-rethrow:** always log (structured logger) what was being attempted plus the error, then rethrow with the cause intact. Skipping the log is allowed only for a known-benign case or caller-directed suppression, each marked inline with `// intentionally not logged: <reason>`. "Expected control flow" is not an exemption: a benign ENOENT probe gets a debug/warn log or that comment — never a bare silent branch.

**Wrap only expected failures.** Try-catch belongs only around an operation whose failure is legitimately expected. A call that keeps throwing gets its root cause fixed, not a wrapper.

**Deleting a test:** only for a refactor of the test itself, a genuinely changed behavior, or a removed feature. Work out what it guards first; if unsure, stop and ask the user with your reasoning. Before a behavior-preserving refactor of untested code, add a characterization test first, then refactor green-to-green.

**Surgical:** match the existing style even where you would do it differently. Route every new subprocess through the shared spawn/supervisor helper, and extend that helper when it does not cover your case — never add a raw spawn beside it. Naming with no local precedent: grep the whole codebase for the closest analog, then follow the project's documented convention — not the language's default style.

**Lifecycle:** when a request is ambiguous, ask AND state your assumptions explicitly. Anything left stubbed or deferred gets a `TODO.md` entry (what + why) AND an explicit mention to the user.

**Safety:** before deleting or overwriting, inspect the target; if it is not what was described or you did not create it, surface that instead of proceeding. Never run a bare `git reset` (any mode) without paths — "the tree is mine alone" is an assumption you cannot verify; other agents may share the repo. Reset specific paths only, after inspecting `git diff` and `git diff --cached` for each.
<!-- /kaizen:digest -->

## Validation — bigint ids in JSON

- Coerce a bigint DB id with `Number(id)` before it enters a JSON response.
  This is the convention for DB ids here. Do not switch the id to a decimal
  string, and do not make `Number()` conditional on "proving it fits".
- Keep the real reason: JSON cannot serialize a BigInt at all, so a raw one
  throws and the response crashes. Precision is not the point.

## Error-handling — log on rethrow, carve-out comment, wrap only expected failures

### Catch-and-rethrow

- Log via the project's structured logger **what was being attempted** and
  **the error/reason**, then rethrow with the original as the cause.
- The only exceptions are a known-benign case or caller-directed suppression,
  and each needs an inline `// intentionally not logged: <reason>` comment.
  State this carve-out whenever you describe or apply the rule; "log and
  rethrow" alone is half the obligation.

### Benign expected errors (e.g. ENOENT on an optional file probe)

- Narrowing the catch to `err.code === 'ENOENT'` and rethrowing everything
  else is correct but not sufficient. The benign branch itself still gets a
  debug/warn log with context **or** the `// intentionally not logged: optional
  file probe` comment.
- Do not argue the branch away as "expected control flow, not an error being
  hidden". A silent branch with no comment is indistinguishable from a swallow
  to the next reader.

### When to wrap at all

- Try-catch belongs only around an operation whose failure is legitimately
  expected. A call that keeps throwing is a root-cause bug: diagnose and fix
  it. Say both halves — "fix the root cause" and "wrap only expected failures".

## Testing — deleting tests, refactoring untested code

- A test may be deleted only when (1) the test itself is being refactored,
  (2) the behavior it covers genuinely changed, or (3) the feature no longer
  exists. "The change was deliberate and stated" is not one of the three.
- Before removing, work out what the test guards. If you cannot, stop and ask
  the user with your reasoning — do not delete on your own judgment.
- Behavior-preserving refactor: run the relevant tests before and after.
  If the code has no test covering its behavior, add a characterization test
  first, see it pass, then refactor. Green-after alone is not evidence.

## Surgical-changes — style, shared helpers, naming

- Touch only what the task requires, **and** match the existing style even
  where you would format or name it differently. Leaving neighbours alone is
  half the rule; writing your own lines in the file's style is the other half.
- When a central spawn/supervisor helper exists, every new subprocess goes
  through it. If it does not cover your case, extend the helper rather than
  adding a raw spawn site beside it.
- Naming with no obvious precedent in the current file: grep the whole
  codebase for the closest analogous name and match its casing and prefix.
  If none exists, follow the project's documented convention (CLAUDE.md /
  AGENTS.md). Do not fall back to generic language defaults such as
  "camelCase for locals, kebab-case for files".

## Lifecycle — ambiguity and deferred work

- Two reasonable interpretations: surface both and ask, **and** state every
  assumption you are making explicitly. Asking without stating assumptions is
  half the obligation.
- Anything left stubbed, deferred or partially implemented gets two actions:
  add a `TODO.md` entry describing what is unfinished and why, and tell the
  user explicitly what was not completed. Pointing at the stub in the code is
  not the TODO.md entry.

## Safety — inspect targets, bare git reset

- Before any delete, overwrite or other hard-to-reverse action: confirm first
  **and** inspect the target. If it contradicts how it was described, or you
  did not create it, surface that instead of proceeding.
- Never run a bare `git reset` (soft, mixed or hard) without paths. There is
  no "acceptable when the working tree is yours alone" case: you cannot
  verify that assumption, and other agents may share the repo with staged
  work you would discard. Reset or unstage specific paths only, after
  inspecting `git diff` and `git diff --cached` for each.
