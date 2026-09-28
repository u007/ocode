---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on ror

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | required by default since Rails 5 (fails validation) + optional: true |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | join model w/ validations/callbacks vs bare HABTM table; prefer :through except simple attributeless joins |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | destroy per-record w/ callbacks, delete_all single SQL DELETE no callbacks, nullify sets FK NULL |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | concurrent-request race + DB unique index |
| ror-activerecord-05 | activerecord | 1 | 2 | 1 | 0.50 | Rails 7.1 declarative transform before validation present; MISSED that normalizes also applies to finder queries (find_by) — argued "runs before validation / more declarative" instead |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | preload separate queries / eager_load single LEFT OUTER JOIN / includes switches when referenced in where/order |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | N+1 named + includes/preload/eager_load fix |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | find raises RecordNotFound / find_by nil / where returns chainable Relation, may be empty, never raises (laziness not stated explicitly, concept of relation-not-record present) |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | pluck column-only, no AR objects vs map loads every column into full objects |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | transaction may still roll back after after_save → job fired for unpersisted record; after_commit only after commit |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | save true/false vs save! raises RecordInvalid correct; MISSED when to use save! (failure should abort, e.g. inside a transaction/service) |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | any exception rolls back + propagates; Rollback caught at block, not re-raised |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | full correct order incl. INSERT position and after_commit last; smell reasons (testing, hidden side effects, cost of save) |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | change for auto-invertible ops; up/down for execute SQL, typeless remove_column, data migrations |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | DSL-limitation reason for structure.sql correct (PG types, triggers, procedures, extensions); MISSED that both are the canonical dump loaded to build a fresh/test DB without replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | column default → add bare, batch backfill, set default after; index → algorithm: :concurrently outside a transaction (phrased as "or", slightly imprecise — concurrently itself requires disable_ddl_transaction!) |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | post_id + index + FK constraint; FK integrity / index for lookups & joins |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | seven actions with verb/path mapping (named path helpers not mentioned) |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | mass-assignment (admin: true) + require raises if missing / permit whitelists |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | runs before specified actions for auth; render/redirect halts the action |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | member with :id vs collection without + 201 Created |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | has_secure_password + SessionsController + views/routes as owned code (no password-reset mention); minimal/no-dependency vs Devise |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | render partial: + locals:; render @posts infers _post, once per element, local named after partial |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | unifies form_for + form_tag; local by default since 6.1 |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | authenticity token verified on non-GET requests; form_with auto-injects hidden token (protect_from_forgery not named) |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | eager-load in controller/query layer; query logic out of views |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | packages instance + class methods + setup; included block runs in host class → class macros |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | PORO single operation with .call; extract when spanning models / reused / isolated testing |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | thin controllers + god-model failure mode (remedy of splitting into services not stated; question asked only for the failure mode) |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | concern = mixin adding methods to the class; service = separate object invoked explicitly |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 1 | 0.50 | nested fragments + record-derived key (updated_at) → update changes key; MISSED touch: true AND stated a misconception: "when an inner fragment changes, its cache key changes, which busts the outer fragment's cache too" — the outer key only changes if the parent's updated_at is bumped (touch) |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | backend-agnostic interface; adapter = backend; :async in-memory, lost on restart → not production |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | at-least-once / retries → multiple runs; guard on done-state, unique constraints |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | perform_later enqueues vs perform_now inline; id → fresh record on execution (serialization claim ignores GlobalID, stale-data reason sufficient) |

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
| caching-jobs | 0.86 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.5 / 73 = 95.2%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no derived skill written for
`longcat-2.5-preview-free` on ror. Four half-credit misses
(normalizes-affects-finders, when-to-use-save!, schema-dump-purpose,
russian-doll touch propagation — the last with an outright misconception);
none drags a tag below 0.75.

## Contamination check

No leak. Answers are concise, in the model's own wording, and do not track the
reference phrasing. They miss several details the key states prominently
(normalizes on finders, `touch: true`, GlobalID, Solid Queue, password reset in
the Rails 8 generator) and contain one error the key contradicts (inner
fragment change busting the outer cache) — inconsistent with access to the
answer key. The callback-order sequence matches the key, but that is the
canonical Rails ordering, not evidence of copying.
