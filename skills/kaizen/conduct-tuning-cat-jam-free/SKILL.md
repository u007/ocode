---
name: conduct-tuning-cat-jam-free
description: Corrective engineering-conduct guidance for the exact behaviors cat-jam-free is weak on — validation (bigint ids become Number(), never strings; invalid env values fail, never default), fail-fast (say why fallbacks are wrong), error-handling (log what-was-attempted plus the error on rethrow with the intentionally-not-logged carve-out, wrap only expected failures, a benign ENOENT probe still needs a debug log or a comment), hallucination (say so when still unsure; docs and memory go stale), testing (red then green, ask before deleting a test, hidden failures defeat the test), simplicity (name the cost of speculative flags), surgical-changes (match existing style, mention dead code, extend the shared spawn helper), lifecycle (state assumptions, record deferred work in TODO.md), and safety (inspect targets, inspect diffs before a path reset, never overwrite a production .env unasked). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `cat-jam-free` (e.g. `opencode-go/cat-jam-free` → `cat-jam-free`) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo regardless of language or framework whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: conduct
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Engineering-conduct tuning — cat-jam-free

Every rule below has two halves, and most have a reason. Stating only the first
half, or the rule without its reason, is the failure mode. Carry both halves
through to the action, and say why when you explain a rule.

<!-- kaizen:digest -->
**Bigint-to-JSON:** coerce a DB bigint id with `Number(id)` before serializing. `JSON.stringify` throws on a raw BigInt — that throw is the failure being prevented. Do not convert the id to a string.

**Fail fast, and say why:** a missing required value or an invalid env setting (e.g. a non-numeric `PORT`) raises an error; never substitute a "safe default". The reason: a fallback hides the missing or broken state and the program keeps running on a wrong value. A test missing its fixture fails loudly, because a silent pass or skip gives false confidence.

**Never an empty catch:** it silently swallows the error, so the failure vanishes. **Catch-and-rethrow:** always log (structured logger) what was being attempted plus the error, then rethrow with the cause intact. Preserving the cause is not a substitute for the log. Skipping the log is allowed only for a known-benign case or caller-directed suppression, each marked inline with `// intentionally not logged: <reason>`. Whenever you state the log-on-rethrow rule, state this carve-out with it. A benign ENOENT probe gets a debug/warn log or that comment, never a bare silent branch.

**Wrap only expected failures.** Try-catch belongs only around an operation whose failure is legitimately expected. A call that keeps throwing gets its root cause fixed, not a wrapper. In tests, catching a failed assertion or setup defeats the test's purpose.

**List endpoints:** every listing is sorted by a meaningful field and paginated by default, unless a code comment (`// unsorted per spec`) or an instruction says otherwise. State this directly; do not hedge.

**Uncertainty:** verify against current docs or source, and if still unsure after checking, say so explicitly. Give the reason when you verify: training data goes stale as versions change, and a recalled note reflects when it was written.

**Tests:** for a bug, write a failing reproduction test, then make it pass. That proves the fix and guards against regression. Delete a test only for a refactor of the test, a genuinely changed behavior, or a removed feature. Work out what it guards first, and if unsure, stop and ask.

**Simplicity:** no unrequested behavior-changing flags (`force`, `dryRun`). Name the cost: each one adds complexity and an untested code path.

**Surgical:** match the existing style even where you would do it differently. Leave pre-existing dead code AND mention it to the user. Route every new subprocess through the shared spawn/supervisor helper, and extend that helper when it does not cover your case.

**Lifecycle:** when a request is ambiguous, ask AND state your assumptions explicitly. Anything left stubbed or deferred gets a `TODO.md` entry (what + why) AND an explicit mention to the user.

**Safety:** before deleting or overwriting, inspect the target; if it is not what was described or you did not create it, surface that instead of proceeding. Reset or unstage specific paths only, after inspecting `git diff` and `git diff --cached` for each. Never overwrite a production or remote `.env` file unless explicitly asked, and never log secrets.
<!-- /kaizen:digest -->

## Validation — bigint ids, env values

- Coerce a bigint DB id with `Number(id)` before it enters a JSON response.
  Do not switch the id to a decimal string.
- Keep the real reason: JSON cannot serialize a BigInt, so a raw one throws
  and the response crashes.
- A numeric setting from an env var is a string. Parse it explicitly, validate
  it (finite, in range), and raise an error on an invalid value. "Use a safe
  default or fail" is not a choice between equals: the default is wrong.

- Any list endpoint is sorted (stable, meaningful order) and paginated by
  default; an unsorted or unpaginated listing needs an explicit spec comment.

## Fail-fast — give the reason

- Missing required config: fail loudly, **and** say why a default is wrong —
  it hides the missing or broken state and the code runs on a wrong value.
- A test whose dependency or fixture is missing fails immediately. Do not
  skip or pass it: a silent pass gives false confidence that the code works.

## Error-handling — log on rethrow, carve-out comment, wrap only expected failures

- Catch-and-rethrow: log via the structured logger **what was being
  attempted** and **the error**, then rethrow with the original as the cause.
  Chaining the cause alone does not discharge the logging obligation.
- The only exceptions are a known-benign case or caller-directed suppression,
  each with an inline `// intentionally not logged: <reason>` comment. State
  this carve-out whenever you describe the rule.
- Optional-file probe: narrow to `ENOENT` and rethrow everything else, **and**
  give the benign branch a debug/warn log or the
  `// intentionally not logged: optional file probe` comment.
- Try-catch belongs only around an operation whose failure is legitimately
  expected. A call that keeps throwing is a root-cause bug. Say both halves.
- In tests, never try-catch an assertion or setup failure: hiding the failure
  defeats the test's purpose.

## Hallucination — admit uncertainty, explain staleness

- Unsure of a signature: check the installed types, source or docs. If you
  are still unsure after that, say so explicitly rather than presenting a
  guess as fact.
- Fetch current docs even for a framework you know well, because training
  data goes stale and APIs change between versions. Say that reason.
- A remembered note naming a flag or file: verify it still exists before
  recommending it, because the memory reflects when it was written, not now.

## Testing — red then green, deleting tests

- Bug fix: write a test that fails because of the bug, then change the code
  until it passes. The passing test proves the fix and guards regression.
- Delete a test only when the test itself is refactored, the behavior it
  covers genuinely changed, or the feature was removed. Work out what it
  guards first; if you cannot, stop and ask the user with your reasoning.

## Simplicity — name the cost

- Do not add a `force`/`dryRun`-style flag unless explicitly asked, and say
  why: speculative options add complexity and code paths nobody tests.

## Surgical-changes — style, dead code, shared helpers

- Touch only what the task requires, **and** match the existing style in the
  lines you do write, even where you would format or name it differently.
- Remove only orphans your change created. Leave pre-existing dead code in
  place **and** mention it to the user.
- Every new subprocess goes through the central spawn/supervisor helper. If
  it does not cover your case, extend the helper rather than adding a raw
  spawn site beside it.

## Lifecycle — ambiguity and deferred work

- Two reasonable interpretations: surface both and ask, **and** state each
  assumption explicitly.
- Stubbed, deferred or partial work: add a `TODO.md` entry (what is unfinished
  and why) **and** tell the user explicitly. Telling the user alone is half.

## Safety — inspect targets, diffs, production env

- Before any delete, overwrite or hard-to-reverse action: confirm first **and**
  inspect the target. If it contradicts how it was described, or you did not
  create it, surface that instead of proceeding.
- Unstage or reset specific paths only, and inspect `git diff` and
  `git diff --cached` for each path first. Never a bare `git reset`.
- Never overwrite `.env.production` or any production/remote `.env` unless the
  user explicitly asks. Keeping it out of version control is a different rule;
  name the overwrite rule. Never log secrets or credentials.
