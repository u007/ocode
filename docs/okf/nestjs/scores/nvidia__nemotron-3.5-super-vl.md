---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on nestjs

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark. Graded from `answers/nvidia__nemotron-3.5-super-vl.md`.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a scratch
cwd containing only the `_prompts/nestjs.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. The batch run
returned 28 of 32 ids; `nestjs-providers-01..04` were silently skipped and were
re-asked alone, closed-book, in a second fresh session with a 4-question sheet and the
same instruction (that session also shows 1 user + 1 assistant, 0 tool calls). The
four re-asked ids are marked `(re-asked)` in the per-question table; their answers are
noticeably longer and code-heavy compared with the terse batch answers. Headless runs
inject the bundled skill-catalogue digest into the system prompt; no answer cites a
tool or skill the question never named. The OpenRouter `:free` route variant is not
part of the model id. No thinking-aloud, no duplicated answers, no fabricated tool
output. Two answers (routing-02, lifecycle-03) explicitly hedge ("I don't know the
exact v11 details", "verify against v11 notes") rather than fabricate.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | "class is its own token" not stated explicitly; "caches a single instance per provider token" + constructor-metadata resolution present |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 1 | 0.50 | param route ok; says only that v11 "adjusted wildcard route handling" and admits it does not know what changed — no Express 5, no named `*splat` wildcard, no "bare `*` discouraged"; point withheld (judgment call: an acknowledged unknown is not the concept) |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 1 | 0.50 | names only two shutdown hooks (`onModuleDestroy` → `onApplicationShutdown`); `beforeApplicationShutdown` omitted entirely, so the order point is withheld; `enableShutdownHooks()` present |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | reverse order A → B → C correct, but states "I am not aware of a NestJS 11 change to this ordering; reverse-order teardown has been the standard behavior" (wrong: the guarantee is a v11 change) |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | `CanActivate` + boolean semantics ok; never says how a guard is attached (`@UseGuards`) or what it is for (authn/authz); 403 not named |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2 | 0.67 | gives only the `intercept(context, next): Observable<any>` signature — no `next.handle()` stream, no before/after RxJS transform in this answer (the mechanism appears in its guards-02 answer, but each answer is graded on its own); interface + two uses ok |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | `@UseFilters` binding not named; ArgumentsHost → response access present |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | writes `app.useGlobalFilter(...)` (singular; the method is `useGlobalFilters`) — awarded as the concept is present, noted; `APP_FILTER` not mentioned, "module-scoped via app.select()/ModuleRef" is dubious |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | (re-asked) |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | (re-asked) |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 | (re-asked) |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 2 | 0.67 | (re-asked) `createTestingModule().compile()` + `moduleRef.get()` present (snippets omit `await` on `compile()`); `overrideProvider` point withheld: it is placed on the compiled `moduleRef` "after compilation" with an impossible `.useValue(mock).useClass(...)` chain "followed by `.compile()`" — materially wrong about the construct (it belongs on the builder before `compile()`); its primary mock recipe is a plain `{ provide, useClass }` entry, which is valid but not `overrideProvider` |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| exception-filters | 1.00 | 4 | ok | omit (strong) |
| di | 0.96 | 7 | ok | omit (strong) (17.33/18) |
| providers-async | 0.94 | 6 | ok | omit (strong) (11.33/12) |
| controllers-routing | 0.88 | 4 | ok | omit (above threshold: 7/8 = 0.875) |
| guards-interceptors | 0.83 | 4 | ok | **derive** (8.33/10) |
| lifecycle | 0.71 | 4 | ok | **derive** (5/7) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.67 / 69 = 92.3%
```

## Derivation targets

Tags below threshold (`< 0.85`): **lifecycle, guards-interceptors** → feed into
`derived/nestjs.nvidia__nemotron-3.5-super-vl.SKILL.md`.

`controllers-routing` sits at 0.875, just above the line, from the unknown v11
wildcard change (routing-02). Treat it as a watch item; it is not covered in the
derived skill.
