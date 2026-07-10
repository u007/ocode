---
model_id: novita-ai/tencent/hy3
model_version: "3.0"
provider: novita
evaluated_on: 2026-07-11
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — novita-ai/tencent/hy3 on golang

> Valid ONLY for `novita-ai/tencent/hy3` @ `3.0`. A version bump invalidates
> this scorecard — re-benchmark.

> **Methodology caveat (read before trusting this number).** Per
> `HOW-TO-EVALUATE.md`, the *best* practice is to grade real answers produced by
> the actual target model. Here the answering sub-agent and the evaluator are the
> **same model family** (`novita-ai/tencent/hy3`), so this is effectively a model
> grading its own (sibling) answers — the "generous" scenario the runbook warns
> about. Treat the 100% below as an **upper bound**; re-run with answers from a
> genuinely independent target model (or a human grader) for a less inflated, more
> trustworthy score. The rubric was applied strictly on *concepts present*, not
> wording, exactly as `rubric-guide.md` prescribes.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | all points present |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | ready-case/random, default non-blocking, no-default blocks/leak |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | sender-only close, send-on-closed panic, receive zero+ok, range termination |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 3 | 1.00 | pre-1.22 shared var, 1.22 per-iter copy, shadow/pass-arg fix |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | race = concurrent+write+no-sync (UB); mutex or atomic |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | instruments at runtime; dynamic + overhead |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | atomic for single-word; mutex for multi-field/critical section |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | Add-before-launch, deferred Done, Wait; Add-inside/negative-counter misuse |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | %w preserves chain; %v/%s breaks Is/As |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | Is=sentinel value; As=extract typed error via pointer |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | pkg-level exported sentinel; compare with errors.Is not == |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | (type,value) both-nil; typed-nil boxed non-nil; return untyped nil |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | implicit/structural; accept iface, return struct |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | any holds any type; single-result panics, comma-ok safe |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | interface=method set/runtime poly; generics=preserve type relationship |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | `[T Constraint]` after name; constraint bounds types+ops |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | comparable = ==/!= (map keys); ~T matches underlying type incl named |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | infer from value args; specify when only in return/no arg |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 3 | 1.00 | cancel closes Done+propagates; cooperative; no force-kill/leak |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | WithTimeout/WithDeadline auto-cancel; always defer cancel (timer leak) |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | request-scoped data + custom key type; not for optional params/deps |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | first param `ctx`; don't store in struct; never nil (TODO/Background) |
| go-slices-01 | slices-maps | 3 | 3 | 3 | 1.00 | shared backing array aliasing; must assign return; Clone/copy |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | nil map read safe, write panics; nil slice appendable |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | copy = min(len dst,len src); s[i:j] shares array; clone/copy to detach |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | randomized order → sort; can't &m[k] or mutate struct-value field |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | args evaluated at defer stmt; LIFO |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | recover only inside deferred fn; only same goroutine |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | defer fires at fn return → loop defers accumulate; close/extract fix |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | requires named returns; wrap error / panic→error |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | slice of cases + one body; named t.Run subtests |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | t.Parallel after parent returns; pre-1.22 shared loop var, shadow (unneeded 1.22+) |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | t.Cleanup LIFO teardown; t.Helper points at caller |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 1.00 | 5 | ok | omit (strong) |
| sync | 1.00 | 5 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| context | 1.00 | 4 | ok | omit (strong) |
| slices-maps | 1.00 | 4 | ok | omit (strong) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 1.00 | 4 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 74 / 74 = 100%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no corrective skill written to
`derived/golang.novita-ai__tencent__hy3.SKILL.md`.

> Note: file name `novita-ai__tencent__hy3.md` is a slash-sanitized form of the
> `model_id` (`/` → `__`) so it is a valid single file path.
