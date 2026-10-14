---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: ror
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on ror (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `ror/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the derived skill `derived/ror.cat-jam-free.SKILL.md`
> (`ror-tuning-cat-jam-free`) prepended to the answerer prompt as active guidance
> (audited zero tool calls). Baseline: `scores/cat-jam-free.md`. No new derived skill
> is produced from this run.

> Target tags for this skill (baseline < 0.85): **callbacks-transactions**.

Graded against `ror/questions.yaml` corpus_rev 1, at the same strictness as the
baseline: a `point` whose concept is genuinely present earns full credit (missing
sub-items are noted); a materially wrong claim about the same construct withholds
that point. Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
with the SKILL.md body (front matter stripped) prepended to the `_prompts/ror.md` sheet,
isolated HOME with every tool denied, cwd holding only the sheet; the session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. All 34 ids are present and
complete. The harvester re-indented one under-indented continuation line in
ror-querying-04 (whitespace only) so the YAML parses. The target-tag answers echo the
skill's wording ("abort the surrounding flow", "swallowed … returns `nil` without
re-raising"), which is the intended overlap (absorption); no reference-key phrasing
from the non-target questions recurs.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free on ror).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 | finder point on "queries see the normalized value" (on-concept, as baseline); "(and loaded from the database)" repeats the baseline's wrong side claim — not the tested construct, no deduction; 7.1 not named |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | map side now stated ("loads and instantiates every record") |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 2 | 1.00 | "use bang methods when failure must abort the surrounding flow" — the baseline's missing point; transaction/service example not named |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | "raise any real exception to roll back and re-raise it" — any escaping exception rolls back and propagates (terse); Rollback swallowed, block returns nil. Baseline's "Rollback is the only way" claim is gone |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | order correct (around_create placed); smell explained, no service/after_commit remedy named (same as baseline) |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | missed (same as baseline): both are the canonical dump `db:schema:load` uses to build a fresh/test DB without replaying migrations |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | `disable_ddl_transaction!` now named |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 1 | 0.50 | missed (same as baseline): HTTP verb + path per action and named path helpers — only "mapped to the corresponding actions" |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | setup/auth purpose and only:/except: not stated (same as baseline); halt-on-render correct |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | 6.1 version boundary not named ("in current Rails") |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | god-model failure stated; split-into-services remedy not named (same as baseline) |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | `touch: true` propagation not mentioned |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | job loss on restart now stated explicitly |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| activerecord | 1.000 | 1.000 | +0.000 | 5 | ok | no |
| querying | 1.000 | 1.000 | +0.000 | 4 | ok | no |
| callbacks-transactions | 0.722 | 1.000 | +0.278 | 4 | ok | **yes** |
| migrations-schema | 0.889 | 0.889 | +0.000 | 4 | ok | no |
| controllers-routing | 0.864 | 0.864 | +0.000 | 5 | ok | no |
| views-helpers | 1.000 | 1.000 | +0.000 | 4 | ok | no |
| concerns-services | 1.000 | 1.000 | +0.000 | 4 | ok | no |
| caching-jobs | 1.000 | 1.000 | +0.000 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: activerecord 11/11; querying 10/10; callbacks-transactions 9/9 (baseline
6.5/9); migrations-schema 8/9; controllers-routing 9.5/11; views-helpers 9/9;
concerns-services 7/7; caching-jobs 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.5 / 73 = 96.6%
```

(Losses: controllers-routing-01 −1.5, migrations-schema-02 −1.0.) Baseline 68 / 73 = 93.2%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| callbacks-transactions | 0.722 | 1.000 | **PASS** |

**PASS.** The only target tag reaches 0.85. The skill is validated for cat-jam-free @ alpha.

Both baseline misses the skill names were absorbed: callbacks-transactions-02 now ties
`save!` to aborting the surrounding flow, and callbacks-transactions-03 now says a real
exception rolls back and re-raises, with `ActiveRecord::Rollback` as the swallowed case.
The answer to 03 is terse ("raise any real exception to roll back and re-raise it"), but
the concept is present and the baseline's wrong "Rollback is the only way" framing is gone.

Non-target drift (not a reason to edit the skill): none. The two non-target misses
(controllers-routing-01 verbs/paths, migrations-schema-02 canonical-dump purpose) are
identical to the baseline, so controllers-routing stays at the marginal 0.864 and
migrations-schema at 0.889. Several non-target answers picked up detail the baseline
lacked (`disable_ddl_transaction!`, `:async` job loss on restart, the map-side cost on
querying-04) without changing any score.
