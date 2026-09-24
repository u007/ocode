---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on ror

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | required-by-default (save false / save! raises) + optional: true; bonus note on DB NOT NULL still rejecting nil |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | join-model with attrs/validations/callbacks vs HABTM no explicit join model; prefer :through except simple symmetric join |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | destroy per-record w/ callbacks, delete_all direct SQL skipping callbacks, nullify sets FK NULL |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | SELECT-then-INSERT race + unique index + RecordNotUnique |
| ror-activerecord-05 | activerecord | 1 | 2 | 1 | 0.50 | declarative transform before validation/persistence present; MISSED that normalizes also applies to finder queries (find_by) — "supported comparisons" is too vague to count; contrasted before_save on update_column bypass instead |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | preload separate / eager_load LEFT OUTER JOIN / includes promotes to join when referenced |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | N+1 named + includes/preload fix |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | find raises RecordNotFound / find_by nil / where Relation, empty relation not nil, chainable |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | pluck column-only scalars, no objects vs map loads all columns + object per row |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | after_save inside txn → rollback leaves effect fired; after_commit durable; bonus outbox/idempotent-job note |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | save true/false vs save! raises RecordInvalid correct; MISSED when to use save! (failure should abort, e.g. in a transaction/service) |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | Rollback caught and not propagated; other exceptions roll back and re-raise |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | full correct order incl. INSERT position and after_commit last; smell reasons (hidden side effects, coupling, tests) present |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | change for auto-reversible ops; up/down or reversible for raw SQL / data transforms |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | DSL-limitation reason for structure.sql correct (extensions/views/functions/triggers/types); MISSED that both are the canonical dump loaded by db:schema:load to build a fresh/test DB without replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | NOT NULL default → nullable column, batch backfill, constrain later; index → algorithm: :concurrently outside a transaction |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | post_id + index + FK constraint; FK integrity / index for joins; notes PG doesn't auto-index referencing side |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | seven actions with verb/path mapping + path helpers |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | mass-assignment + require raises ParameterMissing / permit whitelists; nested permit example |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | only:/except: scoping; render/redirect halts chain; bare return does not halt |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | member :id vs collection no id + 201 Created with Location |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | generator scaffolds has_secure_password + sessions as owned code; minimal vs Devise modules |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | locals: {} + shorthand; render @posts via to_partial_path with inferred local |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | unifies form_for + form_tag; local by default (no 6.1 version cited, concept present) |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | session-bound token verified on state-changing requests via protect_from_forgery; form_with auto-includes hidden token |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | eager-load at controller/query boundary; view renders loaded data |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | shared-behavior module; included block evaluated in host class → class macros; cleaner than plain included hook |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | PORO one use-case + extract when spanning records/systems/transactions; don't over-extract |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | thin controllers on HTTP concerns + god-model failure mode → services |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | concern = mixin supplying capabilities to the class; service = instantiated per operation |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | nested fragments + cache_key_with_version (id + version/updated_at); update changes key automatically; nested records must be versioned/touched |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | backend-agnostic interface + adapter backends; async in-process not durable → not production |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | retries/crashes/visibility timeouts → runs >1; idempotency key + unique constraint + conditional update/upsert |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | perform_later enqueues vs perform_now inline; id avoids stale objects; correctly notes GlobalID makes records passable |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 0.95 | 5 | ok | omit (strong) |
| querying | 1.00 | 4 | ok | omit (strong) |
| callbacks-transactions | 0.89 | 4 | ok | omit (strong) |
| migrations-schema | 0.89 | 4 | ok | omit (strong) |
| controllers-routing | 1.00 | 5 | ok | omit (strong) |
| views-helpers | 1.00 | 4 | ok | omit (strong) |
| concerns-services | 1.00 | 4 | ok | omit (strong) |
| caching-jobs | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.5 / 73 = 96.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no derived skill written for
`space-bunny-free` on ror. Only three half-credit misses across the corpus
(normalizes-affects-finders, when-to-use-save!, schema-dump-purpose); none
drags a tag below 0.75.

## Contamination check

Answers are in the model's own wording and structure throughout — no verbatim
or near-verbatim copies of the reference answers. Several answers carry
details absent from the key (after_rollback, outbox pattern, bare `return`
not halting a before_action, Postgres not auto-indexing FK referencing side,
GlobalID serialization), consistent with independent closed-book knowledge.
