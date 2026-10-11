---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: ror
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on ror (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. Baseline rerun 2026-10-05 (full re-run; an earlier 2026-09-28 scorecard exists beside it). A version bump invalidates this scorecard.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 |  |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 |  |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 |  |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 |  |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 |  |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 |  |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 |  |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 |  |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 |  |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 |  |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | missed: save! should be used when failure must abort (txn/service); only said "exceptional" |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 |  |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 |  |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 |  |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | missed: both are canonical dump loaded to build fresh/test DB instead of replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 |  |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 |  |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 |  |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 |  |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 |  |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 |  |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 |  |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 |  |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 |  |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 |  |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 |  |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 |  |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 |  |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 |  |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 |  |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 1.5 | 0.75 | no touch: true; wrongly implies outer key embeds inner keys |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 |  |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 |  |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 |  |

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
| caching-jobs | 0.929 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.5/73 = 96.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): none. No derived skill written.
