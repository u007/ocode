---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: nestjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on nestjs

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates
> this scorecard — re-benchmark.
>
> Graded by an independent grader (Claude, Sonnet 5), closed-book: answers were
> produced with no corpus/repo access (`ocode2 run -dir <empty> -yolo -effort medium`,
> confirmed zero tool invocations in the run log — a pure isolated LLM completion).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | all four metadata fields correct |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | exports + import + unresolved-dep error all present |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | DynamicModule at runtime; forRoot vs forFeature correct with examples |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | @Global + encapsulation caveat |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | @Injectable, token=class, singleton cache correct |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | custom token + @Inject correct |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | DEFAULT/REQUEST/TRANSIENT semantics all correct |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | REQUEST bubbles up, perf hit, TRANSIENT doesn't bubble — all correct |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | prefix, verbs, param/query/body all correct |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | param route + v11/Express5 wildcard change correct |
| nestjs-routing-03 | controllers-routing, pipes-validation | 2 | 2 | 2 | 1.00 | erasure reasoning + class-validator/transformer/reflect-metadata |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | 200 (201 POST) + @HttpCode and async return handling |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | onModuleInit per-module vs onApplicationBootstrap after all modules |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | shutdown order + enableShutdownHooks correct |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | reverse-order teardown attributed to v11 |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | app.init() trigger + awaits async hook |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | validate/transform, both libs, useGlobalPipes |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | whitelist strips, forbidNonWhitelisted rejects |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | DTO instances + implicit primitive conversion (correctly notes enableImplicitConversion nuance) |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | ParseIntPipe + global-vs-scoped distinction |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | CanActivate/boolean/403 mechanism correct, but never names @UseGuards attachment or explicit authz/authn use case |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | pipeline order CORRECT: middleware→guards→interceptor(pre)→pipes→handler→interceptor(post)→filters |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | NestInterceptor, next.handle() Observable, five valid uses given |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | SetMetadata + Reflector (getAllAndOverride, handler+class) |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | HttpException base + built-ins extend + JSON response |
| nestjs-filters-02 | exception-filters | 2 | 3 | 2 | 0.67 | @Catch, ExceptionFilter, catch(exception,host), ArgumentsHost all correct, but never mentions @UseFilters registration |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | resolution order route→controller→global correct; useGlobalFilters/APP_FILTER correct |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | 500 default + built-in filter + not-leaked in production correct |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | async useFactory + Nest awaits before ready |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | useValue/useClass/useFactory all distinguished |
| nestjs-providers-03 | providers-async, modules | 2 | 2 | 2 | 1.00 | ConfigModule.forRoot + isGlobal correct |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 3 | 1.00 | createTestingModule/overrideProvider/moduleRef.get all present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 1.00 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 1.00 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 0.90 | 4 | ok | omit (strong) |
| exception-filters | 0.90 | 4 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Tag arithmetic for the two non-perfect tags:
- guards-interceptors = (0.67×3 + 1×3 + 1×2 + 1×2) / (3+3+2+2) = 9.0/10 = 0.90
- exception-filters = (1×2 + 0.67×2 + 1×2 + 1×1) / (2+2+2+1) = 6.33/7 = 0.90

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.33 / 69 = 97.6%
```

(Two partial-credit questions: nestjs-guards-01 and nestjs-filters-02, each losing
1/3 of a point's weighted credit because the answer omits the *attachment
mechanism* — `@UseGuards`/`@UseFilters` — while getting the core interface,
return-value, and behavior fully correct.)

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.90. No derived
skill is produced for muse-spark-1.2 on nestjs (**no derivation**).

## Contamination note

97.6% is high but not a flat 100%, and the two dropped points are small,
specific omissions (attachment decorator names) inside otherwise fully-correct,
independently-worded answers — not verbatim reproductions of the rubric's
reference-answer prose. The run log shows a single LLM completion with **zero
tool invocations** (`[TOOLS] exposing 44 tools` were available but none were
called), confirming this was closed-book. NestJS is extensively documented
public framework material (not this repo's proprietary house rules, unlike the
prior contamination incident that produced a flat 100% across un-learnable
house rules) — a strong model correctly recalling well-published framework
semantics, including version-specific details from official NestJS 11 release
notes (Express 5 wildcard routing change, reversed shutdown-hook ordering), is
plausible without corpus leakage. Flagged for awareness, not treated as
invalidating.
