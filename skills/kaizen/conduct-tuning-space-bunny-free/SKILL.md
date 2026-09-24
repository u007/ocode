---
name: conduct-tuning-space-bunny-free
description: Corrective engineering-conduct guidance for the exact behaviors space-bunny-free tests weak on — validation (bigint-to-JSON coercion to Number, sort-and-paginate list defaults), error-handling (empty catch is banned not "normally" discouraged, the always-log obligation with context and the intentionally-not-logged carve-out on rethrow), and testing (failing test first then make it pass, ask before deleting a test you don't understand, why a silently-skipped test is worse than a failing one). Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `space-bunny-free` (e.g. `opencode-go/space-bunny-free` → `space-bunny-free`) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo regardless of language or framework whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: conduct
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Engineering-conduct tuning — space-bunny-free

<!-- kaizen:digest -->
**Bigint-to-JSON:** coerce a DB bigint id to `Number(id)` before JSON serialization — not a decimal string. `JSON.stringify` throws a TypeError on a raw BigInt; that throw is the failure being prevented, not precision loss. String ids are the wrong fix under this codebase's convention.

**List endpoints:** every listing is sorted by a meaningful field AND paginated by default. Pagination alone is not done. Skipping either needs an explicit `// unsorted per spec` (or equivalent) comment or a task instruction — silence is not allowed.

**Empty catch is banned, not "normally" discouraged.** Never hedge with "usually" or "normally". Every caught error is handled and logged, or carries an explicit `// intentionally not logged: <reason>` comment. There is no third option.

**Catch-and-rethrow owes a structured log, not just a preserved stack.** Preserving cause/stack and rethrowing is necessary but NOT sufficient. Before or during the rethrow, log via the project's structured logger (1) what was being attempted and (2) the error/reason. The only exceptions are a known-benign case or caller-directed suppression, and those still need the `// intentionally not logged: <reason>` comment.

**"Fix the bug" = failing test first, then make it pass.** Write the reproducing test before touching implementation — not "preferably", always — and the fix is done when that test goes green. The test is both the proof and the regression guard.

**Deleting a test you don't fully understand: stop and ask.** Removal is only allowed for a refactor of the test, a genuine behavior change, or a removed feature. If you cannot say what the test guarded, stop and ask with your reasoning instead of deleting or weakening it.

**A missing fixture or dependency must fail the test, never skip or pass it.** A silent skip/pass gives false confidence: the suite reports green while the code under test never ran. Make the broken setup loud.
<!-- /kaizen:digest -->

## Validation — bigint JSON coercion and list defaults

### Bigint DB ids in JSON responses

- Coerce a DB bigint id to `Number(id)` before serializing into a JSON API
  response. That is this codebase's convention for ids — not a decimal string.
- The reason to give is the throw, not precision: JSON cannot natively
  serialize BigInt at all, so returning one raw crashes the response. Picking
  a string wire format avoids the crash but is the wrong fix here.

### List endpoints are sorted AND paginated by default

- Before calling a list endpoint done, it must be sorted by a meaningful field
  and paginated. Naming only pagination (or only a max-limit bound) leaves the
  ordering undefined and is incomplete.
- Skipping either sort or pagination requires an explicit code comment (e.g.
  `// unsorted per spec`) or a task instruction. Never skip silently.

## Error-handling — no hedging on empty catch, and the always-log rule on rethrow

### Empty catch blocks

- `catch (e) {}` is never acceptable. State it as a ban, not as "normally no"
  or "usually discouraged" — hedged language turns a hard rule into a
  judgment call and is how empty catches survive review.
- Every caught error is handled and logged, or carries an explicit
  `// intentionally not logged: <reason>` comment. Those are the only two
  outcomes.

### The minimum you owe a caught-and-rethrown error

- Log, via the project's structured logger, **what was being attempted** and
  **the error/reason**, then rethrow with the original error/cause intact.
  "Record it through the normal error channel" without those two pieces of
  context does not satisfy the rule.
- Preserving the stack/cause is necessary but not the obligation being asked
  about. An answer framed purely around rethrow mechanics has skipped the
  logging step.
- The only carve-outs are a known-benign case or caller-directed suppression,
  and each still needs the `// intentionally not logged: <reason>` comment
  inline.

## Testing — reproduce-then-fix, deletion discipline, loud missing fixtures

### "Fix the bug" starts with a failing test

- First step, before any implementation change: write a test that reproduces
  the bug and fails. This is mandatory, not "preferably as a regression test".
- Second step: make that test pass. The green test is the evidence the fix
  works and the guard against regression. Understanding the cause is part of
  step one, not a substitute for the test.

### When a test may be deleted

- Only when you are refactoring the test itself, the behavior it covered has
  genuinely changed, or the feature no longer exists.
- Before removing, work out what the test guarded. If you are unsure, stop
  and ask the user with your reasoning and a recommendation — do not delete
  or weaken it silently, and never because it is failing or in the way.

### Missing fixtures and dependencies

- A test whose fixture or dependency is missing must fail loudly and
  immediately. Do not skip it, do not fabricate data, do not let it pass.
- Say why: a silent skip or pass produces a green suite that never exercised
  the code, which is false confidence — worse than a visible failure because
  nobody looks at green.
