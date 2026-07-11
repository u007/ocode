---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on ror

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates
> this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | required-by-default + optional: true both correct |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | join-model vs bare table + prefer :through all present |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | destroy/delete_all/nullify + callbacks distinction correct |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | SELECT-then-INSERT race + DB unique index |
| ror-activerecord-05 | activerecord | 1 | 2 | 1 | 0.50 | got declarative-on-assignment; MISSED that normalizes also applies to finder queries (find_by), the key edge over before_save. "uniqueness checks see normalized value" is not the finder-normalization point |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | preload=separate / eager_load=LEFT JOIN / includes auto-promotes — no conflation |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | N+1 (1+N) named + includes fix |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | find raises / find_by nil / where lazy empty-relation |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | pluck column-only no objects vs map full objects |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | after_save-in-txn rollback vs after_commit durable — correct semantics |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 2 | 1.00 | save true/false vs save! raises + when to use bang |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | escaping exception re-propagates; AR::Rollback swallowed |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | correct create-callback order + heavy-callback smell |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | change auto-reversible vs up/down for raw SQL/un-invertible |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 2 | 1.00 | schema.rb DSL-limited vs structure.sql for DB features; dump-that-loads present (thin on "avoids replaying migrations" but concept there) |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | column-default lock-rewrite + batch backfill; index concurrently + disable_ddl_transaction! |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | post_id + index + FK; index-for-speed / FK-for-integrity |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | seven actions + verb/path mapping (named helpers not named but verb+path explicit) |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | mass-assignment + require/permit semantics + ParameterMissing |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | before_action for auth + render/redirect halts chain |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | member(:id)/collection + 201 Created w/ Location |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | Rails 8 auth generator (has_secure_password/sessions, owned code) vs Devise engine |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | render partial w/ locals + render @posts collection semantics |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | unifies form_for+form_tag; correctly says local/standard default now (attributes to 7+/Turbo not 6.1 but conclusion right — did not fall into remote-default trap) |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | per-session token verified non-GET + form_with auto-injects hidden token |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | eager-load in controller, not view; view renders loaded data |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | ASC module + included-do class-context macros, avoids self.included |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | PORO one operation + when to extract (spans models/multi-step) |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | skinny-controller + God-object failure mode + split into POROs |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | concern=mixin (shares state) vs service=standalone call |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | nested fragments + record-keyed (id+updated_at) auto-invalidation (touch: true not named but core answer correct) |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | backend-agnostic interface + adapter; :async loses jobs, not production |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | at-least-once/retries → double side effects; guard/unique-key/upsert |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | perform_later async vs perform_now inline; pass id + reload |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 0.95 | 5 | ok | omit (strong) |
| querying | 1.00 | 4 | ok | omit (strong) |
| callbacks-transactions | 1.00 | 4 | ok | omit (strong) |
| migrations-schema | 1.00 | 4 | ok | omit (strong) |
| controllers-routing | 1.00 | 5 | ok | omit (strong) |
| views-helpers | 1.00 | 4 | ok | omit (strong) |
| concerns-services | 1.00 | 4 | ok | omit (strong) |
| caching-jobs | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 72.5 / 73 = 99%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scored ≥ 0.75, so no
`derived/ror.tencent__hy3.SKILL.md` is produced — **no derivation**.

---

Grader note: graded by an independent strict grader (Claude Opus 4.8), no
authorship of the answers. Answers were produced CLOSED-BOOK (no corpus access).
The single deduction is ror-activerecord-05: hy3 explained `normalizes` as
declarative on-assignment normalization but omitted its distinguishing property
over `before_save` — that it also normalizes finder-query arguments (e.g.
`User.find_by(email: " A@B.com ")`). Every other answer earned full marks with
no incorrect statements (no includes/preload/eager_load conflation, correct
after_commit/after_save rollback semantics).
