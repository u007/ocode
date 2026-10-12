---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: ror
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on ror

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/ror.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 34 ids are
present and complete (no truncation). Wording, structure and length differ from the
reference answers throughout, and the three factual slips noted below do not appear in
the key — no sign of contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 | both points present; extra claim that `normalizes` also applies on read-from-DB is wrong (existing rows need `normalize_attribute`) — not a rubric point, no deduction |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | both points present; test caveat ("after_commit doesn't fire under transactional fixtures") is outdated since Rails 5 — not a rubric point, no deduction |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | missed: use save! when a failure must abort the flow (transaction/service); only gave contexts ("seeds, scripts, jobs, tests, exceptional and loud") |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | second classic framed as `change_column` expand/contract, but the NOT NULL/default rewrite risk and batched-backfill + separate-constraint fix are both stated |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 1 | 0.50 | missed: `add_reference` adds the index by default — answer explicitly claims the index is NOT created unless `index: true` is passed (column + FK + purpose of each were correct) |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 1.000 | 5 | ok | omit (strong) |
| querying | 1.000 | 4 | ok | omit (strong) |
| callbacks-transactions | 0.889 | 4 | ok | omit (strong) |
| migrations-schema | 0.889 | 4 | ok | omit (strong) |
| controllers-routing | 1.000 | 5 | ok | omit (strong) |
| views-helpers | 1.000 | 4 | ok | omit (strong) |
| concerns-services | 1.000 | 4 | ok | omit (strong) |
| caching-jobs | 1.000 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 73 = 97.3%
```

## Derivation targets

Tags below threshold (`< 0.85`): none. No derived skill written
(`derived/ror.step-5-preview-free.SKILL.md` intentionally absent).
