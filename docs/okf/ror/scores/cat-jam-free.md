---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: ror
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on ror

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch cwd with only the `_prompts/ror.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs inject
ocode's bundled skill-catalogue digest into the system prompt. `cat-jam-free` is an
opencode-go free/alpha model with no published version; it self-identifies as "CatJam,
developed by LitterBox AI". All 34 ids are present and complete. Answers are terse and
worded differently from the reference key, and the factual slips noted below are not in
the key — no sign of contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 | finder point awarded on "can support normalized lookups" (hedged but on-concept); extra claim that it normalizes "when loading persisted values" is wrong — not the construct a point tests, no deduction |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | map-side cost stated by contrast ("far less memory and less work"), not spelled out |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | missed: use save! when a failure must abort the flow (inside a transaction/service); only "when failure should be exceptional" |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 1 | 0.50 | missed: any exception escaping the block rolls back AND re-propagates — answer presents raising `ActiveRecord::Rollback` as the only way to force a rollback |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | smell explained (couples domain to persistence); no service/after_commit remedy named |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | missed: both files are the canonical dump `db:schema:load` uses to build a fresh/test DB without replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | `disable_ddl_transaction!` not named |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 1 | 0.50 | missed: HTTP verb + path per action — lists the seven actions and "path helpers" but no verbs/paths |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | setup/auth purpose and only:/except: scoping not stated; halt-on-render correct |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | 6.1 version boundary not named |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | god-model failure stated; split-into-services remedy not named |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | `touch: true` propagation not mentioned |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | "not suitable for production; use a persistent adapter" — job loss on restart implied, not stated |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 1.000 | 5 | ok | omit (strong) |
| querying | 1.000 | 4 | ok | omit (strong) |
| callbacks-transactions | 0.722 | 4 | ok | **derive** |
| migrations-schema | 0.889 | 4 | ok | omit (strong) |
| controllers-routing | 0.864 | 5 | ok | omit (strong; marginal) |
| views-helpers | 1.000 | 4 | ok | omit (strong) |
| concerns-services | 1.000 | 4 | ok | omit (strong) |
| caching-jobs | 1.000 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 73 = 93.2%
```

## Derivation targets

Tags below threshold (`< 0.85`): **callbacks-transactions** → feed into
`derived/ror.cat-jam-free.SKILL.md`.
