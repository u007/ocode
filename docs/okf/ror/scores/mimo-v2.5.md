---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on ror

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | required-by-default + optional: true both correct |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | join-model vs bare table + prefer :through all present |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | destroy/delete_all/nullify + callbacks distinction correct |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | SELECT-then-INSERT race + DB unique index |
| ror-activerecord-05 | activerecord | 1 | 2 | 1 | 0.50 | got declarative-on-assignment; MISSED that normalizes also normalizes finder-query args (find_by) |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | preload=separate / eager_load=LEFT JOIN / includes auto-promotes |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | N+1 named + includes/preload fix |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | find raises / find_by nil / where lazy relation |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | pluck column-only no objects vs map full objects |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | after_save-in-txn rollback vs after_commit durable |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 2 | 1.00 | save true/false vs save! raises + when to use bang |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | escaping exception re-propagates; AR::Rollback swallowed |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 1 | 0.50 | callback smell reasoning correct; MISPLACED after_commit before after_save in the order |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | change auto-reversible vs up/down for raw SQL/un-invertible |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | correctly contrasts Ruby DSL vs SQL dump + DB-feature reason; MISSED that both are the canonical dump loaded to build a fresh/test DB without replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 1 | 0.50 | backfill-in-batches-then-constrain pattern present (via "adding NOT NULL" example); MISSED the index-build-locks-writes / `algorithm: :concurrently` risk entirely — substituted "removing a column" as the other example, which isn't the rubric's second risk |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | post_id + index + FK; index-for-speed / FK-for-integrity |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | seven actions + verb/path mapping |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | mass-assignment + require/permit semantics |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | before_action for auth/setup + render/redirect halts chain |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | member(:id)/collection + 201 Created |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | Rails 8 auth generator vs Devise engine |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | render partial w/ locals + render @posts collection semantics |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | unifies form_for+form_tag; concludes local-by-default in current Rails |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | per-session token verified on non-GET + form_with auto-injects hidden token |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | eager-load in controller, not view; view renders loaded data |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | ASC module + included-do class-context macros, avoids self.included |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | PORO one operation + when to extract |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | skinny-controller + God-object failure mode + split into POROs |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | concern=mixin (shares state) vs service=standalone call |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | nested fragments + record-keyed auto-invalidation |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | backend-agnostic interface + adapter; :async not production |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | at-least-once/retries → double side effects; guard/unique-key/upsert |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | perform_later async vs perform_now inline; pass id + reload |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 0.95 | 5 | ok | omit (strong) |
| querying | 1.00 | 4 | ok | omit (strong) |
| callbacks-transactions | 0.94 | 4 | ok | omit (strong) |
| migrations-schema | 0.72 | 4 | ok | **derive** |
| controllers-routing | 1.00 | 5 | ok | omit (strong) |
| views-helpers | 1.00 | 4 | ok | omit (strong) |
| concerns-services | 1.00 | 4 | ok | omit (strong) |
| caching-jobs | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.5 / 73 = 95.2%
```

## Derivation targets

Tags below threshold (`< 0.75`): **migrations-schema** → feed into
`derived/ror.mimo-v2.5.SKILL.md`.
