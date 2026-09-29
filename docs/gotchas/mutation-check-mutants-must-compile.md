---
type: Gotcha
title: 'Mutation-check: mutants must compile; filelock non-positive timeout = 10s default; harness backup/trap/preflight hygiene'
description: 'Three mutation-check findings: a mutant that only breaks compilation is a false-positive kill (compile-check before tests, classify INVALID); filelock treats non-positive timeouts as the 10s package default so corpusLockWaitFor must floor at a positive wait; and harness hygiene — mktemp private backup dir, EXIT/INT/TERM trap, cmp-before-restore, dirty-tree preflight and post-restore git-diff guards (web/mutate_speech.sh). Cross-references concepts/discovery-corpus-cache.md and the sibling verdict-classification gotcha.'
tags:
  - mutation-testing
  - gotcha
  - discovery
  - filelock
  - testing
  - harness
  - git
timestamp: 2026-09-29T00:05:20Z
---
# Mutation-check mutants must compile, and `filelock` treats non-positive timeouts as "10s default"

Two findings from a mutation-check session over the discovery corpus-lock code
(`internal/discovery/cache.go`, harness `mutcheck_corpus.py`), plus the
harness-hygiene failure modes found later while hardening a second harness
(`web/mutate_speech.sh`, section 3) — those corrupted the *source tree* rather
than the verdict.

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

## 3. Harness hygiene: private backup, exit trap, cmp-before-restore, dirty-tree preflight

Found while hardening `web/mutate_speech.sh` (the speech-summary mutation
harness) after it exhibited two failure modes. Sections 1–2 corrupt the
*verdict*; these corrupt the **working tree**, so the damage survives the run
and shows up as unrelated downstream test failures.

**Failure mode A — fixed backup path + no exit trap.** The backup lived at a
fixed `/tmp` path, so a stale backup from an unrelated run could be restored
*over* the real source file. And because nothing trapped INT/TERM, a killed run
left the **mutated** source stranded in the tree — the next `go`/vitest run
then failed for reasons unrelated to the code under review, sending the
investigator down a long detour (this happened twice before the fix).

**Failure mode B — no dirty-tree preflight.** Starting without a `git diff`
check let an already-dirty source be baked into the backup and then
"restored" as if pristine — the mutated bytes became the new "original" and
every CAUGHT/SURVIVED verdict below that point is meaningless.

**Fixes (all verified in `web/mutate_speech.sh`):**

```bash
# 1. Dirty-tree abort guard BEFORE any backup (lines 13-16)
if ! git diff --quiet -- "$SRC"; then
  echo "ABORT: $SRC has uncommitted changes; commit or stash them first." >&2
  exit 1
fi

# 2. Private temp dir — never a fixed /tmp path (line 22)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/mutate-speech.XXXXXX")
BAK="$WORK/SpeechProvider.tsx"
cp "$SRC" "$BAK"

# 3. cmp before cp: a restore that silently writes the WRONG bytes is worse
#    than no restore, because it looks like a clean run afterwards (line 29)
restore() { cmp -s "$BAK" "$SRC" || cp "$BAK" "$SRC"; }

# 4. Every exit path — success, failed mutation, SIGINT, SIGTERM — puts the
#    source back and removes the work dir (line 35)
trap 'restore; rm -rf "$WORK"' EXIT INT TERM

# 5. Post-restore confirmation: after the final restore, re-check and refuse
#    to let a still-mutated file be committed (lines 96-100)
restore
if ! git diff --quiet -- "$SRC"; then
  echo "ABORT: $SRC is still mutated after restore — do not commit." >&2
  exit 1
fi
```

Note the harness itself points back at this gotcha (its line-21 comment cites
`docs/gotchas/mutation-check-mutants-must-compile.md`), so the fix and the
recorded lesson stay in sync.

## Checklist

- [ ] Mutant compiles (`go build`) before any test run; otherwise INVALID.
- [ ] Kill = a *test* observed the behavior change, not a build failure.
- [ ] Any timeout passed to `filelock.WithFileLockTimeout` must be strictly
      positive — `<= 0` silently means the 10s package default.
- [ ] Backup lives in a `mktemp -d` private dir, never a fixed `/tmp` path.
- [ ] `trap ... EXIT INT TERM` restores the source on every exit path — a
      killed run must not strand a mutated file in the tree.
- [ ] `cmp` before `cp` on restore, so a restore never silently writes the
      wrong bytes.
- [ ] Preflight `git diff --quiet -- $SRC` aborts when the source is already
      dirty (an old backup must not be "restored" as pristine).
- [ ] Post-restore `git diff` confirms the source is back to pristine before
      anything is committed.

## Related

- [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) — the design this
  gotcha was found against: probe → lock → re-check → write, `ErrCorpusLocked`
  skip semantics, bounded wait, and why concurrent ocode instances share the
  on-disk `corpus-<model>.json`.
- [Mutation-check false verdicts](gotchas/mutation-check-false-caught.md) —
  sibling gotcha on verdict classification: false CAUGHT (compile-broken
  mutants) and false MISS (equivalent mutants).