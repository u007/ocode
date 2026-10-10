# Part 07 — Agreement eval

A skippable live eval that is the gate on trusting clef with the permission judge. **No behaviour change.**

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently.
- Fail-open is a hard invariant.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance. **This part is the evidence that either confirms or falsifies that choice** — it does not assume the answer.
- Never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Never `git stash`, `reset`, `checkout --`, or `clean`.

## Files

- Create `internal/agent/clef_agreement_eval_test.go`

## What this measures, and why a plain agreement percentage is not enough

A reviewer's objection, accepted here: **agreement over a fixture set is dominated by easy cases.** Two backends can agree on 95% of fixtures — all of them obvious — while disagreeing on exactly the ones that matter. A symmetric threshold therefore flatters a backend that fails in one direction.

So this eval reports three things, and the third is the one that decides:

1. **Directional disagreement.** Count cases where clef **allows** and the incumbent denies, separately from the reverse. Allow-where-incumbent-denied is the dangerous direction for a permission gate and is a veto on adoption for that slot, regardless of the aggregate rate. The aggregate is reported for context, not as the gate.
2. **Calibration comparison, not just verdict match.** Agreement says the two backends chose the same option; it says nothing about whether a reported confidence of 0.85 means the same thing on both. Compute a calibration measure over the fixtures — a binned reliability comparison is sufficient — and report it. Shared floors are only defensible if the confidences are comparable, and this is the only evidence that they are.
3. **Latency, tokens and cost**, using each provider's documented rates. Mark those rates as vendor-published and re-check against an invoice.

Also measure, and report separately, **choice option-order sensitivity**: TypeSafe documents that the incumbent "leans toward the option that comes first", and the permission judge's criteria are exactly a two-option choice. Score every choice fixture with the option order reversed and report any verdict that flips. Note in the test that Go map iteration is unordered, so achieving a genuinely deterministic order comparison requires serialising the request explicitly rather than relying on map ordering.

## Steps

1. Write the harness, guarded so it **skips unless explicitly enabled by an environment variable**. A normal `go test ./...` must never touch the network. The skip message must state what to set.
2. Reuse the existing judge fixtures rather than inventing a new set — the eval is only meaningful if it measures the same decisions production makes. `internal/agent/content_guard_eval_test.go` is the template for shape and for how an opt-in live eval is structured in this package.
3. Build both clients through the normal resolution path. If a credential is missing, fail with a clear operator-facing message — an explicitly requested eval that quietly reports nothing is worse than one that fails.
4. Replay every fixture against both, recording verdict, confidence and elapsed time.
5. Emit a table plus the three aggregate results above, and print the fixture count so a silently shortened run is visible.
6. **Contamination guards.** Pin the fixture order to a fixed sorted sequence. Assert both backends receive byte-identical `state` and `question` payloads by comparing the marshalled request the caller builds — not the backend's own re-serialisation. Do not let the eval choose which fixtures to run.
7. Add the deferral entry to `TODO.md` (see below) and tell the user it exists.

## Checks

- `go test ./internal/agent/ -run TestClefAgreement -v -count=1` **skips** with the explanatory message. **This skip is itself the assertion** — a harness that silently no-ops is worse than none.
- `go vet ./internal/agent/` clean, so the fixture references compile.
- The skip path is tested, not assumed.

## Deferred

Add a `TODO.md` bullet recording that the live eval has not been run against real credentials, that its results must be pasted into the spec, and that until they exist, sharing the confidence floors across backends rests on Cloudflare's own benchmark of its own model — with the incumbent's latency figure coming from a competitor's measurement.

## Review focus

Confirm the gate is the directional count, not the aggregate. Confirm a reported confidence of the same value on both backends is being compared for meaning, not just for equality.

Confirm the eval cannot pass by running zero or fewer fixtures than intended.

## Done when

The harness skips cleanly by default, and running it with credentials produces the directional, calibration, latency and cost numbers needed to accept or reject clef for the permission slot.