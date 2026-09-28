---
type: Gotcha
title: 'Mutation-check: mutants must compile; filelock non-positive timeout = 10s default'
description: 'Two mutation-check findings: a mutant that only breaks compilation is a false-positive kill (compile-check before tests, classify non-compiling mutants INVALID), and filelock treats non-positive timeouts as the 10s package default, so corpusLockWaitFor must floor at a positive wait. Cross-references concepts/discovery-corpus-cache.md for the underlying lock design.'
tags:
  - mutation-testing
  - gotcha
  - discovery
  - filelock
  - testing
timestamp: 2026-09-28T17:03:30Z
---
# Mutation-check mutants must compile, and `filelock` treats non-positive timeouts as "10s default"

Two findings from a mutation-check session over the discovery corpus-lock code
(`internal/discovery/cache.go`, harness `mutcheck_corpus.py`).

> **Context:** the lock semantics found here — `ErrCorpusLocked`, the bounded
> wait, cross-instance sharing of the on-disk cache, and the warm-gate
> interaction — are documented as design in
> [Discovery Corpus Cache](concepts/discovery-corpus-cache.md). This gotcha is
> the mutation-check story; that page is the architecture.

## 1. A mutant that only breaks compilation is a FALSE POSITIVE

The "deadline clamp removed" mutant removed the clamp in `corpusLockWaitFor`
and left unused variables behind. `go test` then failed with a *build* error —
no test ever observed the behavior change — but the harness counted it as a
kill, inflating the apparent kill rate.

**Invariant:** a mutation harness must run `go build` (or otherwise compile-check)
on each mutant before running tests, and classify non-compiling mutants as
**INVALID / excluded**, never as killed. A test suite that "passes" against a
non-building mutant proves nothing about coverage.

- Harness: `mutcheck_corpus.py` (runs `go test ./internal/discovery/` only —
  it does not build the mutant first).
- Victim: `corpusLockWaitFor` in `internal/discovery/cache.go:94`.

```python
# per-mutant gate before the test run
subprocess.run(['go', 'build', './internal/...'], check=True)  # non-zero → INVALID
```

## 2. `corpusLockWaitFor` must return a small POSITIVE wait for an exhausted deadline

The genuine test gap the mutation exposed: `filelock` treats a **non-positive
timeout as "use the 10s package default"** (`internal/filelock/filelock.go:36,49` —
`Timeout = 10 * time.Second`, `if timeout <= 0 { timeout = Timeout }`).
Returning `0` for an already-exhausted/expired deadline would therefore turn a
zero budget into a **10-second stall** — the exact opposite of the clamp's
purpose (the per-turn Warm runs with a 500ms context and `filelock` does not
observe contexts).

`corpusLockWaitFor` clamps to the caller's remaining deadline but floors the
result at 1ms (`internal/discovery/cache.go:101-103`):

```go
if wait <= 0 {
    wait = time.Millisecond
}
```

**Fixed with an expired-context test** asserting `wait > 0 && wait <= 1s`
(`internal/discovery/cache_lock_test.go:258-264`, inside
`TestCorpusLockWaitNeverExceedsTheCallersDeadline`):

```go
expired, _ := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
if got := corpusLockWaitFor(expired); got <= 0 {
    t.Fatalf("lock wait for an expired caller must stay positive, got %s", got)
} else if got > time.Second {
    t.Fatalf("lock wait for an expired caller must be tiny, got %s", got)
}
```

## Checklist

- [ ] Mutant compiles (`go build`) before any test run; otherwise INVALID.
- [ ] Kill = a *test* observed the behavior change, not a build failure.
- [ ] Any timeout passed to `filelock.WithFileLockTimeout` must be strictly
      positive — `<= 0` silently means the 10s package default.

## Related

- [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) — the design this
  gotcha was found against: probe → lock → re-check → write, `ErrCorpusLocked`
  skip semantics, bounded wait, and why concurrent ocode instances share the
  on-disk `corpus-<model>.json`.
