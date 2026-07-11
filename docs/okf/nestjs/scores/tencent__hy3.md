---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: nestjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on nestjs

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates
> this scorecard — re-benchmark.
>
> Graded by an independent strict grader (Opus 4.8), closed-book: answers were
> produced with no corpus/repo access (`ocode run -dir <empty>`).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | all four metadata fields correct |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | exports + import + unresolved-dep error all present |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | DynamicModule at runtime; forRoot vs forFeature correct |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | @Global + encapsulation caveat |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | @Injectable, token=class, singleton cache correct |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | custom token + @Inject correct |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | DEFAULT/REQUEST/TRANSIENT semantics all correct |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | REQUEST bubbles up, perf hit, TRANSIENT doesn't bubble — all correct |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | prefix, verbs, param/query/body all correct |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | param route + v11/Express5 wildcard change correct |
| nestjs-routing-03 | controllers-routing, pipes-validation | 2 | 2 | 2 | 1.00 | interface-erasure reasoning present |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | verbose/hedged but lands on 200 (201 POST)+@HttpCode and async return |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 1 | 0.50 | WRONG: says onApplicationBootstrap fires "after the application has fully started (listening)"; it fires *before* listening — negates rubric point 2 |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | shutdown order + enableShutdownHooks correct |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | reverse-order teardown attributed to v11 |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | app.init() trigger + awaits async hook |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | validate/transform, both libs, useGlobalPipes |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | whitelist strips, forbidNonWhitelisted rejects |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | DTO instances + implicit primitive conversion |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | ParseIntPipe + global-vs-scoped distinction |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 3 | 1.00 | CanActivate/boolean/403/authz all present (omits @UseGuards name — awarded, concept present) |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | pipeline order CORRECT: middleware→guards→interceptor(pre)→pipes→handler→interceptor(post)→filters |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | NestInterceptor, next.handle() Observable, two uses |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | SetMetadata + Reflector (get/getAllAndOverride, handler+class) |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | HttpException base + built-ins extend + JSON response |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | @Catch, ExceptionFilter, catch(exception,host), ArgumentsHost (omits @UseFilters name — awarded, concept present) |
| nestjs-filters-03 | exception-filters | 2 | 2 | 1 | 0.50 | WRONG resolution order: claims filters resolve "SAME layered order as guards... global→controller→route"; corpus = route→controller→global (reverse). Global-registration half correct (useGlobalFilters/APP_FILTER) |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | 500 default + built-in filter + prod-not-leaked correct; note: wrongly claims dev response includes the stack (Nest logs it server-side, does not leak) |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | async useFactory + Nest awaits before ready |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | useValue/useClass/useFactory all distinguished |
| nestjs-providers-03 | providers-async, modules | 2 | 2 | 2 | 1.00 | ConfigModule.forRoot + isGlobal correct |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 3 | 1.00 | createTestingModule/overrideProvider/get all present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 1.00 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 0.86 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 1.00 | 4 | ok | omit (strong) |
| exception-filters | 0.79 | 4 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Tag arithmetic for the two non-perfect tags:
- lifecycle = (0.5×2 + 1×2 + 1×2 + 1×1) / (2+2+2+1) = 6/7 = 0.857
- exception-filters = (1×2 + 1×2 + 0.5×2 + 1×1) / (2+2+2+1) = 6/7 = 0.857

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 69 = 97.1%
```

(Two half-credit questions: nestjs-lifecycle-01 and nestjs-filters-03, each −1.0 of
weighted credit from a perfect 69.)

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.79. No derived
skill is produced for tencent/hy3 on nestjs (**no derivation**).
