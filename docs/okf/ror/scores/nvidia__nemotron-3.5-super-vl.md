---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: ror
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on ror

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/ror.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 34 ids came back
once, none empty. Headless runs inject the bundled skill-catalogue digest into the system
prompt; no answer cites a tool or skill, and none contains thinking-aloud, duplicated
text or fabricated tool output. The OpenRouter `:free` route variant is not part of the
model id. Answers are worded independently of the reference key and the misses noted
below are not in the key — no sign of contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-05 | activerecord | 1 | 2 | 1 | 0.50 | missed: `normalizes` also applies to finder queries (`find_by`), which a `before_save` never does; answer argues only consistency/idempotency/explicitness |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 1 | 0.50 | missed: use `save!` when a failure must abort the flow (inside a transaction/service); only "when you want failures to surface as exceptions", no control-flow/transaction reason |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | order point awarded: sequence correct with "validation" standing in for `after_validation`; smell explained (hidden side effects, blurred transaction boundaries, coupling) but no service/after_commit remedy named |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | missed: both files are the canonical dump `db:schema:load` uses to build a fresh/test DB without replaying migrations; answer contrasts formats only |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | named path helpers not mentioned; verbs + paths complete |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 1 | 0.50 | judgment call: halt point withheld. The answer first says a rendering/redirecting filter stops the action, then claims "render/redirect_to alone do not halt execution, so you typically need an explicit return" — Rails halts the chain on `performed?`, not on an explicit return (the return only avoids a double render in the filter body). Setup/auth purpose and `only:`/`except:` scoping also not stated |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 2 | 1.00 | 6.1 version boundary not named |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | `protect_from_forgery` not named; token-per-session + non-GET verification present |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | "makes modules behave more like classes" is a loose framing; the shared-behavior module concept and the included-block/class-macro point are present |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | god-model failure stated; split-into-concerns/services remedy not named |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | `touch` mentioned only in passing ("via cache_key/touch"); outward propagation stated |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 0.955 | 5 | ok | omit (strong) |
| querying | 1.000 | 4 | ok | omit (strong) |
| callbacks-transactions | 0.889 | 4 | ok | omit (strong) |
| migrations-schema | 0.889 | 4 | ok | omit (strong) |
| controllers-routing | 0.909 | 5 | ok | omit (strong) |
| views-helpers | 1.000 | 4 | ok | omit (strong) |
| concerns-services | 1.000 | 4 | ok | omit (strong) |
| caching-jobs | 1.000 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.5 / 73 = 95.2%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none** — no
`derived/ror.nvidia__nemotron-3.5-super-vl.SKILL.md` is written for this model.
