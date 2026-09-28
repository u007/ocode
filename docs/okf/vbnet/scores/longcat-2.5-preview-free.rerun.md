---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on vbnet (baseline run 2)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

**Baseline run 2 (no skill).** Second closed-book baseline, run to settle
`linq-query`, which sat exactly at threshold (0.75) in baseline run 1
(`scores/longcat-2.5-preview-free.md`). Answers:
`answers/longcat-2.5-preview-free.rerun.md`, produced via `ocode run` with only
`docs/okf/_prompts/vbnet.md` as input, no derived skill injected.

Contamination check: answers are in the model's own wording; distinctive
reference phrases ("rewires", "no-op", "zero date", "diagnostic trail",
"project-wide", "sealed override") are absent. Session audit: 1 user + 1
assistant turn, zero tool calls. Verdict: **clean**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | implicit continuation (VB 10+) not mentioned |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | names `_PropertyName`, but wrongly says it is "not directly accessible in source code" (it is accessible in-class) |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | this run states "Nothing is the default value for a type" (missed in run 1) |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | 3-arg `If(c,t,f)` ternary only implied via "only evaluates the needed branch" (same as run 1) |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | `= Nothing` framed as overloadable value comparison; no String `"" = Nothing` case |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | specific-first ordering not mentioned |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | doesn't note the two models can't mix in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 1 | 0.50 | `Group p By p.Department Into g = Group` correct; aggregates again done as `g.Count()` in `Select`, not inside `Into` (same miss as run 1) |
| vbnet-linq-03 | linq-query | 1 | 2 | 1 | 0.50 | shows `Aggregate x Into Sum(x.Value)` and "folding/reducing" (single result) — better than run 1; still no scalar-vs-sequence contrast with `From` (From only described as "establishes the source"); "custom accumulation" still echoes `Enumerable.Aggregate` |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | re-run-on-each-enumeration not mentioned |
| vbnet-events-01 | events | 3 | 2 | 1 | 0.50 | Handles "connects" method to WithEvents field; omits "no explicit AddHandler" and multiple events per Handles clause |
| vbnet-events-02 | events | 2 | 2 | 1 | 0.50 | AddHandler/RemoveHandler/AddressOf correct; when-required limited to dynamic/non-WithEvents — no `Shared` events or `Structure`s |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | one-base/many-interfaces correct; omits per-member `Implements IFoo.Member` clause |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | run 1's wrong "NotOverridable is default for Overrides" claim is gone |
| vbnet-oop-03 | oop | 2 | 2 | 1 | 0.50 | type-level + type-name access correct; never says Shared members can't touch instance state/`Me` (run 1 did) |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | plain `ReDim` discarding contents only implied |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline-1 | baseline-2 | mean | n | trust | action |
|-----|-----------:|-----------:|-----:|--:|-------|--------|
| syntax-basics | 1.00 | 1.00 | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 1.00 | 1.00 | 4 | ok | omit (strong) |
| nullability | 0.90 | 1.00 | 0.95 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 1.00 | 1.00 | 4 | ok | omit (strong) |
| linq-query | 0.75 | 0.79 | 0.77 | 4 | ok | omit (mean ≥ 0.75) |
| events | 0.69 | 0.69 | 0.69 | 4 | ok | already derived (existing skill section) |
| oop | 0.79 | 0.79 | 0.79 | 5 | ok | omit (mean ≥ 0.75) |
| conversions-arrays | 1.00 | 1.00 | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Multi-tag questions count toward every tag they carry (syntax-03 → oop,
syntax-02 → conversions-arrays, convarr-04 → syntax-basics).

Baseline-2 arithmetic: linq-query = 5.5 / 7 = 0.786; events = 5.5 / 8 = 0.6875;
oop = 9.5 / 12 = 0.792.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62.5 / 69 = 91%
```

(baseline run 1: 89%)

## Threshold tags

Tags whose two-run mean is < 0.80:

- **linq-query** — mean (0.750 + 0.786) / 2 = **0.768**, **not < 0.75**. No
  skill section. Robust to the one judgment call: if linq-03 were graded at the
  partial only (0.25, as in run 1), baseline-2 would be 0.750 and the mean 0.750,
  still not below threshold. The consistent weak spot across both runs is VB's
  `Into` clause (aggregates placed in `Select` instead of `Into`) and the
  `Aggregate ... Into` scalar form — worth watching, not deriving.
- **events** — mean (0.6875 + 0.6875) / 2 = **0.69**, **< 0.75**. Already
  covered by the existing derived-skill section; both runs miss the same
  concepts that section targets (no explicit AddHandler / multi-event Handles;
  Shared events and Structures). No change.
- **oop** — mean (0.792 + 0.792) / 2 = **0.79**, **not < 0.75**. No skill
  section. Different misses per run (run 1: wrong NotOverridable default; run 2:
  Shared/instance-state detail); only oop-01's per-member `Implements` clause is
  missed in both.

## Derivation targets

No new targets. The only tag with a two-run mean below 0.75 is **events**,
which `derived/vbnet.longcat-2.5-preview-free.SKILL.md` already covers. The
derived skill is unchanged.
