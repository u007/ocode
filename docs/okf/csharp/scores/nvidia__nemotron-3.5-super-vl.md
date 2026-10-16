---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: csharp
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on csharp

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark. Graded from `answers/nvidia__nemotron-3.5-super-vl.md`.
> Fractional awards (0.5 steps) were used where a point's sub-element was missing.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a scratch
cwd containing only the `_prompts/csharp.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 32 ids came
back once, none empty. Headless runs inject the bundled skill-catalogue digest into
the system prompt; no answer cites a tool or skill the question never named. The
OpenRouter `:free` route variant is not part of the model id. No thinking-aloud, no
duplicated answers, no fabricated tool output.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 2.5 | 0.83 | compile-time/erased, `?`/`!` no runtime effect, `#nullable` contexts all correct; `!!` never-shipped missing |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | copy-vs-share and defensive copy (readonly field, `List<T>` indexer copy vs array variable) correct; value-type-zeroed vs reference-type-null default omitted |
| csharp-null-03 | types-nullability | 3 | 3 | 2.5 | 0.83 | value equality + `with` (C# 9/10) and class reference equality correct; record class init-only stated, but record struct mutability never addressed — "same code generation but as a value type" implies init-only, which is wrong for `record struct` |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 2 | 1.00 | warning + runtime `SwitchExpressionException` correct (no CS8509 number) |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | no C# 9 version on relational/logical; list patterns dated C# 11 |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | definite assignment on the matching path stated; `is not` inversion not mentioned |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | muddled aside ("supplying a plain `Func` ... compiles the tree to a delegate" — it actually binds the `Enumerable` overload, which the answer also says); conclusion correct |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | suggests `First` + try/catch as a workaround (poor advice, not wrong) |
| csharp-async-01 | async | 3 | 3 | 2 | 0.67 | struct-vs-allocated-Task and consume-once correct; `.AsTask()` conversion never mentioned (says "prefer Task<T>" instead) |
| csharp-async-02 | async | 3 | 3 | 2 | 0.67 | state machine + deadlock mechanism correct; no "async all the way" fix |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | `WithCancellation` present; `[EnumeratorCancellation]` and C# 8 not mentioned |
| csharp-generics-01 | generics | 2 | 2 | 1.5 | 0.75 | unlocking side present; kinds listed, but **invents a `where T : any` constraint and files `notnull` under "reference-type flavor"** — half of the kinds point withheld (judgment call) |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 1.5 | 0.75 | `EqualityComparer<T>.Default` comparison idiom missing |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 1.5 | 0.75 | Action/Func distinction present; lambdas/method groups converting to delegates and passing behavior as a value not stated |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | try/finally stated (exception safety implied) |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | `GC.SuppressFinalize` present; `SafeHandle` not mentioned |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 2.5 | 0.83 | getting a Span from Memory via `.Span` missing |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 1.5 | 0.75 | `List<T>` growth mechanism (amortized Add, realloc + copy) not stated; params collections correct (C# 13, Span/List/IEnumerable) but no allocation-avoidance rationale |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.82 | 4 | ok | **derive** |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |
| linq | 0.96 | 5 | ok | omit (strong) |
| async | 0.83 | 5 | ok | **derive** |
| generics | 0.89 | 4 | ok | omit (strong) |
| delegates-events | 0.94 | 4 | ok | omit (strong) |
| disposal | 1.00 | 4 | ok | omit (strong) |
| collections-spans | 0.89 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.75 / 73 = 91.4%
```

## Derivation targets

Tags below threshold (`< 0.85`): **types-nullability, async** → feed into
`derived/csharp.nvidia__nemotron-3.5-super-vl.SKILL.md`.

Note: both targets fail on omissions, not wrong claims — `!!` never shipped,
type defaults, record struct mutability; `.AsTask()` and "async all the way". The
only fabricated construct in the run is the `where T : any` constraint
(generics-01); generics stays above threshold on its other three answers.

## Contamination check

No verbatim or near-verbatim matches to the reference answers. The answers
consistently miss the specific details a copier would carry (`!!` never shipped,
value-type default, record struct mutability, `.AsTask()`, "async all the way",
`EqualityComparer<T>.Default`, `.Span`), use their own examples (the `List<T>`
indexer-copy case, `<MethodName>d__N` state machine naming), and contain one
invented construct the key contradicts. Consistent with a genuine closed-book run.
