---
name: test-suite-cleanup
description: Metrics-driven pruning and de-bloating of any test suite (Go, Vitest/Jest/Bun, pytest, PHPUnit, JUnit/JaCoCo, or any runner via a custom command) so the test loop gets faster without losing coverage or regression guards. Use when the user asks to clean up, shrink, prune, dedupe, speed up or audit tests, remove redundant or AI-slop tests, or asks why the suite is slow. Bundles scripts/testdoctor.py: per-test timing, static bloat smells, per-unit coverage isolation (keep/drop plan) and a post-prune coverage+gate verifier. Never prune by reading tests and guessing.
when_to_use: Test suite is slow or bloated; user wants fewer or faster tests, duplicate test cleanup, a test audit, or proof that a reduced suite still covers what the old one did.
---

# Test Suite Cleanup

Shrink a test suite while keeping its coverage and its regression guards.
The script decides what is *redundant*; you decide what is *valuable*; the
verifier decides whether you were right.

## Hard rules

1. **Metrics before opinions.** No test is deleted before `plan` lists it as
   a drop candidate. "Looks trivial" is not evidence.
2. **A drop candidate is a proposal.** Same *lines* ran, not the same
   *behaviour* was asserted. Review every candidate (step 3) before deleting.
3. **Checkpoint = the git index, never a commit.** Before touching a
   directory, `git add` its test files so index == worktree. After `verify`
   passes, `git add` the pruned files. If `verify` fails,
   `git restore <pruned files>` (worktree only, back to the index). Never
   commit, stash, `restore --staged`, or `reset`; the user commits.
4. **Verify or restore.** Done means `verify` exits 0 for every plan touched
   (no coverage lost, suite and gates green). Otherwise restore the units
   named in `covered_by` or write a targeted replacement.
5. Repo rules win: a test named in CLAUDE.md/AGENTS.md, a gotcha doc, or a
   "do not remove" comment stays regardless of the plan.

## Workflow

`$SKILL` = this skill's directory; `TD="python3 -I $SKILL/scripts/testdoctor.py"`.
Run from the repo root. Artifacts land in `.test-doctor/` (gitignore it).
Run `$TD <cmd> -h` first; treat the script as a black box.

### 1. Measure

```bash
$TD timing --go ./...                         # Go: go test -json
$TD timing 'reports/**/*.xml'                 # anything else: JUnit XML the runner wrote
$TD smells internal web/src                   # static scan, any language
```

Most runners emit JUnit XML (`pytest --junitxml`, `phpunit --log-junit`,
`bun test --reporter=junit`, Maven/Gradle surefire, `jest-junit`,
`vitest --reporter=junit`). Report suite time, heaviest dirs, smell counts.
**Stop if the suite is red**; failing tests give unreliable coverage.

Pick targets by payoff: most test LOC or slowest wall time. One directory
or package at a time, never the whole repo.

### 2. Plan one directory

```bash
$TD plan ./internal/foo --preset go                       # unit = test file; --granularity test for per-func
$TD plan src/components/Foo --preset vitest --cwd web     # needs the runner's coverage provider installed
$TD plan tests/Unit --preset phpunit                      # also: jest, bun, pytest
$TD plan . --cmd 'mvn -q -Dtest={unit} test' --full-cmd 'mvn -q test' \
   --format jacoco --cov-file target/site/jacoco/jacoco.xml --unit-list classes.txt
```

A unit is anything runnable alone: a file (default, `--glob`) or a name from
`--unit-list`. Coverage formats: `goprofile`, `lcov` (JS/TS, pytest-cov,
cargo-llvm-cov, coverlet), `clover` (PHP), `jacoco` (Java). `{covdir}` is a
fresh temp dir per run. If a preset's coverage tool is missing the script
says so; ask before adding a dev dependency (pinned major).

The plan (`.test-doctor/plan-*.json`) classifies units:

- `essential` — covers a block nothing else covers. Keep.
- `keep` — chosen by greedy set-cover to reach the baseline.
- `drop_candidates` — add no coverage beyond `keep`; each lists
  `covered_by` and its isolated runtime (includes process overhead, so
  compare units to each other, not to the suite total).
- `empty_units` — cover nothing under root (smoke/integration tests).
- `failed_units` — fail alone: order-dependent. Report; never delete the test
  that exposes the dependency.

### 3. Review each drop candidate — the human-judgement step

For each candidate pick one, with a one-line reason:

- **Delete**: same behaviour asserted by a `covered_by` unit (true duplicate,
  copy-paste variant, assertion-free smoke).
- **Merge**: same shape, different data (`smells` → `same_shape_groups`).
  Fold into one table/parameterised test in the keeper: the cases survive,
  the per-test setup does not.
- **Keep**: regression guard for a named bug, API contract, property or
  invariant, data-driven check over distinct fixtures (one test per bundled
  asset/stack), concurrency or race test. Coverage cannot see any of these.

Show the delete/merge/keep list to the user before editing. Apply with
path-limited edits, then step 5.

### 4. Speed without deleting (often the bigger win)

- Sleeps → poll with a deadline, or inject a fake clock.
- Expensive fixture per test (temp repo, server, DB, rendered app tree) →
  build once per suite/module, copy or reset per test.
- Mark pure tests parallel; never tests that touch env or globals.
- Oversized tests → split slow setup from the many assertions; subtests
  share setup.

### 5. Verify, then checkpoint

```bash
$TD verify .test-doctor/plan-go_internal_foo.json   # full coverage run + gates (go adds -race)
git add <pruned files>                              # pass → checkpoint
git restore <pruned files>                          # fail → back to the index
```

Finish with the project's own full gate, not just the touched directory.
Report before/after test count, LOC and wall time per directory, the
delete/merge/keep tally, and anything left as "keep — needs a fixture
refactor" for TODO.md.

## Pitfalls

- Tests that only fail under a race detector or repeated runs look redundant
  under line coverage. Grep candidates for goroutines/threads/`sync` first.
- Examples, benchmarks and fuzz targets are not units; never proposed.
- Mutation testing is a stronger oracle than coverage, but a mutant that
  does not compile is INVALID, not CAUGHT.
- `plan` runs one process per unit; scope with `--glob`/`--unit-list` on
  directories with hundreds of files.
