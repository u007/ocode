---
name: nestjs-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective NestJS guidance for nvidia/nemotron-3.5-super-vl, targeting the two
  areas this model showed gaps on in the closed-book nestjs benchmark: the full
  shutdown-hook sequence and its NestJS 11 ordering guarantee, and the guard
  attachment / interceptor next.handle() mechanism. Loaded only in NestJS repos
  when this exact model is active.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `nvidia/nemotron-3.5-super-vl` AND the repository uses NestJS
  (`@nestjs/core` in package.json, per meta.yaml detection). For any other model
  or non-NestJS repo, do not load.
# --- Kaizen metadata ---
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: nestjs
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# NestJS corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## lifecycle

- There are THREE shutdown hooks, run in this exact order:
  **`onModuleDestroy()` → `beforeApplicationShutdown(signal)` →
  `onApplicationShutdown(signal)`**. Never list only the first and last;
  `beforeApplicationShutdown` receives the signal and runs before connections
  close, `onApplicationShutdown` after.
- They fire on `app.close()`; to fire on SIGTERM/SIGINT call
  `app.enableShutdownHooks()`.
- **NestJS 11 made destroy-hook order the reverse of init order, and this IS a
  v11 change.** If init ran C → B → A, destroy runs A → B → C, so a module tears
  down before its dependencies; global modules init first and destroy last.
  Earlier versions did not guarantee it. Never say "reverse-order teardown has
  always been the standard behavior" or "I am not aware of a v11 change" —
  attribute the guarantee to v11 explicitly.

## guards-interceptors

- **Two unconditional rules, regardless of how the question is phrased.**
  Any answer that defines, describes or mentions a guard MUST contain this
  sentence: "A guard is bound with `@UseGuards()` (or `useGlobalGuards()` /
  `APP_GUARD`) and decides authentication/authorization." Any answer that
  defines, describes or mentions an interceptor MUST contain this sentence:
  "Code before `next.handle()` runs before the handler; `next.handle()` returns
  the handler's result as an Observable that is transformed after." A
  definition that omits either sentence is incomplete.
- When describing a guard, always state **how it is bound and what it is for**:
  attach with `@UseGuards(MyGuard)` on a handler or controller, globally with
  `app.useGlobalGuards()`, or as an `APP_GUARD` provider. Guards make
  authentication/authorization decisions from the `ExecutionContext`;
  `canActivate()` returning `false` makes Nest throw `ForbiddenException` (403).
- An interceptor's `intercept(context, next)` is not just a method that "returns
  an Observable": code before **`next.handle()`** runs before the handler, and
  `next.handle()` returns an RxJS `Observable` of the handler's result that the
  interceptor transforms **after** the handler with operators (`map`, `tap`,
  `catchError`, `timeout`). Name `next.handle()` and the before/after split
  whenever you explain an interceptor, not only when ordering the pipeline.
- Interceptors are bound the same way as guards: `@UseInterceptors()`,
  `app.useGlobalInterceptors()`, or an `APP_INTERCEPTOR` provider.
<!-- /kaizen:digest -->
