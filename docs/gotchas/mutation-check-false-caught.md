---
type: Gotcha
title: 'Mutation-check false verdicts: compile-broken mutants and equivalent mutants'
description: 'Two mutation-check verdict traps: compile-broken mutants misreported as CAUGHT (gate on go build, classify INVALID) and equivalent mutants misreported as coverage misses (replace with genuine semantic mutations); plus the expired-context regression test for corpusLockWaitFor.'
tags:
  - mutation-testing
  - gotcha
  - testing
  - discovery
  - coverage
  - filelock
timestamp: 2026-09-28T18:02:09Z
---
# Mutation-check false verdicts: compile-broken mutants and equivalent mutants

**Type:** Gotcha  
**Description:** Two ways a mutation check misreports: a mutant that only breaks compilation is counted as CAUGHT though no test observed the behavior (classify as INVALID after a go build gate), and an equivalent mutant can never be caught so a "miss" is not a coverage gap (replace with a genuine semantic mutation). Also records the expired-context test added for corpusLockWaitFor's exhausted-budget branch.  
**Tags:** mutation-testing, gotcha, testing, discovery, coverage, filelock  

---

# Mutation-check false verdicts: false CAUGHT and false MISS

Two methodology traps surfaced while mutation-checking the discovery
corpus-lock code (`internal/discovery/cache.go`, harness `mutcheck_corpus.py`).
Both corrupt the verdict rather than the code — the run reports a result that
no test actually justified.

> **Context:** the underlying `filelock` timeout semantics and the lock design
> are documented in [Mutation-check mutants must compile](gotchas/mutation-check-mutants-must-compile.md)
> (the 10s non-positive-timeout default) and
> [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) (the architecture).
> This gotcha is about *verdict classification* in a mutation run.

## 1. False CAUGHT: a mutant that only breaks compilation

A mutant that removes a clamp body and leaves unused variables behind fails
`go build`. The harness ran `go test ./internal/discovery/`, got a non-zero
exit from the *compile* error, and recorded the mutant as **CAUGHT** — but no
test ever executed the mutated code or observed a behavior change. The kill
rate was inflated by a mutation that could not have compiled in the first
place.

**Rule:** gate every mutant through `go build` first.

- `go build` fails → classify the mutant **INVALID / excluded** — it is a
  malformed mutation, not evidence of coverage.
- Only a test failure (or assertion on observed behavior) against a
  *building* mutant counts as CAUGHT.
- A suite that "passes" against a non-building mutant proves nothing; a
  harness that counts it as a kill proves something false.

```python
# per-mutant gate before the test run
if subprocess.run(['go', 'build', './internal/...'], returncode).returncode != 0:
    verdict = 'INVALID'   # not CAUGHT, not a miss
else:
    verdict = run_tests() # 'CAUGHT' only if a test observed the change
```

## 2. False MISS: equivalent mutants can never be caught

The "equivalent mutant" rewrite — rewriting `min(a, b)` as the two-line form
`if b > a { b = a }` (variable copy + conditional) — is **semantically
identical** to the original for every possible input. No test, however
thorough, can distinguish it, so it is inevitably reported as SURVIVED.

Counting it as a coverage miss is wrong: the miss says nothing about the test
suite. The correct response is to **replace the mutation with a genuine
semantic change** — e.g. force a 2× overshoot of the bound (return `2*wait`
instead of `wait`) — so that a surviving mutant now *does* indicate a real
gap (a branch or assertion that would let a wrong value through).

**Rule:** before recording a SURVIVED verdict, ask "could *any* input
distinguish mutant from original?" If not, the mutant is invalid-by-equivalence
— replace it; never log it as a coverage miss.

## Follow-up fix recorded: expired-context test for `corpusLockWaitFor`

The mutation run exposed that the exhausted-budget branch of
`corpusLockWaitFor` (`internal/discovery/cache.go` — the `if wait <= 0 { wait =
time.Millisecond }` floor) was untested: no test exercised an already-expired
caller context. Fixed by adding an expired-context assertion to
`TestCorpusLockWaitNeverExceedsTheCallersDeadline`
(`internal/discovery/cache_lock_test.go:258-264`):

```go
expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
defer cancelExpired()
if got := corpusLockWaitFor(expired); got <= 0 {
    t.Fatalf("lock wait for an expired caller must stay positive, got %s", got)
} else if got > time.Second {
    t.Fatalf("lock wait for an expired caller must be tiny, got %s", got)
}
```

The floor must stay **positive**: `filelock` treats a non-positive timeout as
the 10s package default, so a zero wait would become a 10-second stall — see
[the compile/timeout gotcha](gotchas/mutation-check-mutants-must-compile.md)
for that trap in detail.

## Checklist

- [ ] `go build` every mutant before testing; build failure → INVALID, never CAUGHT.
- [ ] CAUGHT requires a *test* to have observed the behavior change.
- [ ] Before logging SURVIVED, confirm the mutant is not equivalent to the
      original — replace equivalent mutants with genuine semantic mutations.
- [ ] Newly exposed untested branches (e.g. exhausted-budget) get a regression
      test as part of the same follow-up.

## Related

- [Mutation-check mutants must compile](gotchas/mutation-check-mutants-must-compile.md) —
  the companion gotcha: compile-check gate detail and the `filelock`
  non-positive-timeout = 10s default trap.
- [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) — the design
  this mutation run was checking: probe → lock → re-check → write,
  `ErrCorpusLocked`, bounded wait.
