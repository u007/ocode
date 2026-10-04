# Part 05 — The shared state-size budget (behaviour change)

**This is the only part that changes what a user experiences.** It is isolated so it can be rejected without touching parts 01–04.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently: never reachable from chat, compaction, small-model, recap, or task-contract paths.
- Fail-open is a hard invariant: end in "keep everything" or "ask the human", never a verdict.
- No silent backend fallback — in particular, do **not** fall back to the other backend on an over-budget state.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased. `permissions.auto.min_confidence` keeps governing the opaque relaxation.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance.
- `TypesafeQuestion` and `TypesafeResponse` shapes are unchanged.
- Never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- `internal/agent/permission_typesafe.go` is `MM` in git — stage only your own hunks. Never `git stash`, `reset`, `checkout --`, or `clean`.

## Status: implementable, but one premise is unresolved

Everything in this part can be implemented now. What **cannot** be settled from the repository is whether the incumbent truncates an oversized `state` or rejects it — that is server-side behaviour, and it needs a live probe. The guard is correct either way, so this is not a blocker on writing the code; it is a blocker on *describing the severity* accurately.

The other thing that was a guess is no longer one: the earlier draft of the boundary test picked a comparison operator without pinning the boundary. This part now requires constructing a state whose **marshalled** size equals the budget exactly, which distinguishes a strict from a non-strict comparison. The framing overhead is real and non-trivial — a 100-byte payload marshals to 116 bytes — so sizing a payload "just under the budget" lands well under and never exercises the comparison at all.

## What this guards, and the honest framing

`buildTypesafePermissionState` places the tool's raw `arguments` into the judge state **uncapped** — the existing `max_context_bytes` setting bounds only `project_context`. A 200 KB `write` therefore sends roughly 55K tokens of arguments.

TypeSafe's own documentation states Jev's limit as 64k tokens per request but only **32k for `state` plus the longest question**. So an ordinary large write is already past the documented budget **today**, on the existing Jev path. This part does not introduce that; it makes it visible.

**Two premises were challenged in review and both are unresolved. State them rather than assume them:**

1. **Nobody has verified whether the incumbent truncates an oversized `state` or rejects it with 400.** Characterised from the code as far as the repo allows: there is **no** state-size guard in `internal/agent/typesafe.go` today, and a non-2xx becomes `newProviderStatusError` (line 130), which the permission judge already surfaces as "model unavailable" and defers to the human. So the two worlds differ in severity but not in outcome: a 400 is *already* a deferral, carrying a confusing message; silent truncation would instead grade an invisible command tail, which is a safety problem rather than a UX one. Either way the human is asked, which is why the guard is correct — but **which of the two it is remains unverified and needs a live probe.** Add that probe as a test skipped unless credentials are present, and do not claim a truncation behaviour in the commit message that nobody observed.
2. **Asking on every large write defeats auto-permission on its most common large input.** `write` is frequently large, so this could make auto mode feel broken for ordinary work. Before shipping, project the state rather than refusing it wholesale: **keep short, security-relevant fields in full** (the command line, the tool, the rule, the scope, allowed roots, banned prefixes) and **project bulky content fields** (file contents, diff bodies) to a bounded preview. That keeps the security-relevant surface intact while fitting the budget. Rejection stays as the fallback for what still does not fit.

## The trap in this part — the boundary test must be exact

An earlier draft of these tests was **vacuous**: it sized the state's *payload* to just under the budget, but the guard measures the state's **marshalled** size, which is the payload plus JSON framing. The payload therefore always landed comfortably under, the guard's comparison never executed, and the test passed without exercising anything. The over-budget case was likewise over by far more than one byte, so it could not distinguish `>` from `>=`.

Fix: construct the state, marshal it, measure the result, and **adjust the payload until the marshalled size is exactly the budget**. Then assert the budget accepts it. Then add exactly one byte and assert it refuses. A helper that binary-searches for the smallest rejected payload is a reasonable way to find the true boundary and makes the test independent of the estimator's arithmetic. Do not leave a helper defined but never called, and do not reference an undefined helper.

## A second correction: the token estimate errs in the opposite direction

A chars-per-four-bytes estimate **under**-estimates the token count for source code and JSON (punctuation and short identifiers tokenise poorly). A guard sized as `budget × 4` bytes therefore tends to let borderline-over states *through*, not to over-ask. State that direction honestly in the comment rather than claiming the guard errs toward asking. If a real tokenizer is not available, note that the budget is approximate, and add a regression test that rebuilds every judge request at its worst case and asserts the sizes, so a future change that grows a payload past the ceiling fails loudly.

## A third correction: the probability tolerance was too tight

Requiring probabilities to sum to one within one part in a million will reject ordinary rounded responses from a real backend, turning a valid verdict into a "no verdict" and hiding candidates that were actually judged. Use a tolerance appropriate for values rounded to a few decimal places, and treat a genuinely unnormalised distribution as invalid only outside that.

## Answer validation

Add a validator that reports whether a returned answer may be trusted to gate on. It must reject: a type that disagrees with the question's type; a `Noul` outside the unit interval or non-finite; a `Confidence` outside the unit interval; a `choice` answer with no chosen option; and a probability set that does not sum to one within tolerance.

Wire it into the relevance judge's per-candidate loop, replacing its current type check. **A rejected answer must keep the candidate.** This is the single most important line in the part: the zero value of `Noul` is `0`, which reads as "definitely irrelevant" and vetoes — a fail-*closed* bug hiding inside a system whose contract is fail-open.

## Checks

- `go build ./...` and `go vet ./...` clean.
- `gofmt -l` prints nothing for touched files.
- `go test ./internal/agent/ -run 'TestSharedStateBudget|TestValidateAnswer|TestJudgeRelevance' -v -count=1` passes.
- `go test -race ./internal/agent/ -count=1` passes.
- **Mutation check:** flip the budget comparison from strict to non-strict; the exact-boundary test must catch it. Confirm the mutant compiles first.

## Review focus

Verify the boundary test really does construct a state whose marshalled size equals the budget. Assert that in the test itself if you can — a test that claims to pin the boundary should fail if the estimator's arithmetic changes.

Verify the relevance judge keeps the candidate on every rejection path, including a valid-looking answer of the wrong type.

Verify no path falls back to the other backend.

## Done when

Oversized choice-judge state produces a human ask, malformed answers fail open, the boundary is genuinely pinned, and the commit message states the two unresolved premises rather than asserting them.