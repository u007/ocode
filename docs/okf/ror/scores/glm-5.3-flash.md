---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on ror

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

> Note: `ror-activerecord-05` was omitted from the model's batch reply and
> re-asked in a fresh closed-book context (appended to the answers file). It is
> graded normally. A clean 100% sweep is worth a contamination sanity-check
> (see README closed-book rule) — the answers are paraphrased, not key-verbatim,
> and several go beyond the key (e.g. `apply_to_nil`, `normalize_value_for`,
> `form_with_generates_remote_forms`), which is consistent with genuine
> knowledge rather than key leakage.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | required-by-default ("must exist") + optional: true |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | HABTM bare table vs :through join model; prefer :through except pure link table |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | destroy per-record w/ callbacks; delete_all single SQL no callbacks; nullify FK→NULL |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | SELECT-before-INSERT race + unique index; bonus LOWER(email) note |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 | Rails 7.1, assignment-time; find_by/where query-side normalization explicit (re-asked separately) |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | preload separate / eager_load LEFT OUTER JOIN / includes promotes on references |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | N+1 named + includes/preload/eager_load fix at query site |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | find raises RecordNotFound; find_by nil; where lazy Relation, empty not nil |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | pluck column-only raw values vs map loads users.* + full objects |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | after_save inside txn / rollback fires side effect; after_commit only after commit |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 2 | 1.00 | save false vs save! raises RecordInvalid; bang when failure exceptional (seeds/services) |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | escaping exception rolls back + propagates; AR::Rollback swallowed at boundary |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | exact order incl. around_* and after_commit last; smell → services/jobs |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | change for auto-reversible; up/down or reversible for execute/change_column/typeless remove_column |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 2 | 1.00 | dump replayed for dev/test DBs; structure.sql for triggers/functions/views/custom types |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | index build → algorithm: :concurrently + disable_ddl_transaction!; nullable-add → batched backfill → NOT NULL |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | post_id + index + FK; FK integrity; index because PG doesn't auto-index FK side |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | seven actions w/ verb+path + named helpers |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | mass-assignment; require raises ParameterMissing; permit whitelists |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | runs before action for auth/setup; render/redirect halts chain |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | member has :id / collection none; 201 Created + Location |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | Rails 8 generator: User bcrypt, Session, Current, password reset; minimal vs Devise modules |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | partial w/ locals + shorthand; render @posts → _post per record w/ inferred local |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | unifies form_for+form_tag; local by default since 6.1 |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | session token verified on non-GET by protect_from_forgery; form_with injects hidden field |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | includes in controller; view is presentation only |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | ASC mixin module; included block in host class → class macros; avoids self.included boilerplate |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | PORO one operation w/ call; extract when spans models / txn+side effects / multiple entry points |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | thin controllers delegate; god-model failure → concerns/services/query/form objects |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | concern mixin injected into host vs service standalone collaborator called explicitly |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | nested fragments; cache_key_with_version embeds updated_at; touch: true propagates |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | backend-agnostic job API; adapter = real backend; :async in-process, lost on restart, not prod |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | at-least-once → double charge/email; guard-on-state / idempotency keys / conditional update / upsert |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | perform_later enqueues vs perform_now inline; pass id + reload for fresh state |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 1.00 | 5 | ok | omit (strong) |
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
stack_score = Σ(normalized×weight) / Σ(weight) = 73 / 73 = 100.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no
`derived/ror.glm-5.3-flash.SKILL.md` written.
