---
name: nestjs-tuning-longcat-2.5-preview-free
description: >
  Corrective NestJS knowledge for longcat-2.5-preview-free, targeting its gaps
  in lifecycle hooks (shutdown order, v11 reverse ordering, what triggers init)
  and guards/interceptors (guard usage, interceptor Observable mechanics).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `longcat-2.5-preview-free` AND the repository is a NestJS project
  (@nestjs/core in package.json, per meta.yaml detection). For any other model
  or non-NestJS repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: nestjs
source_scorecard: ../scores/longcat-2.5-preview-free.rerun-2026-10-05.md
threshold: 0.85
revalidate_when: model_version changes
---
# NestJS corrections for longcat-2.5-preview-free

## Lifecycle

- Init hooks: `onModuleInit()` per module once its dependencies resolve, then `onApplicationBootstrap()` after all modules are initialized, before listening.
- Shutdown hooks, in order: `onModuleDestroy()`, then `beforeApplicationShutdown(signal?)`, then `onApplicationShutdown(signal?)`. List all three; never omit `onModuleDestroy`.
- Shutdown hooks fire on `app.close()`; for OS signals (SIGTERM/SIGINT) also call `app.enableShutdownHooks()`.
- NestJS 11 changed termination order: shutdown hooks run in the reverse order of the init hooks. Init C -> B -> A means destroy A -> B -> C. State that this is new in v11; do not say it is unchanged.
- In v11, global modules count as a dependency of every module: initialized first, destroyed last.
- Init hooks are triggered by `app.init()`; `app.listen()` calls `app.init()` internally if it has not run. They are not triggered by "application start" in general.
- Nest awaits a hook that returns a Promise (or is `async`) before moving to the next phase.

## Guards and interceptors

- A guard is a class implementing `CanActivate` with `canActivate(context: ExecutionContext)`, returning `boolean | Promise<boolean> | Observable<boolean>`.
- Guards decide authentication/authorization. Attach with `@UseGuards(MyGuard)` at method or controller level, or globally with `app.useGlobalGuards()`.
- `false` makes Nest throw `ForbiddenException` (403). A guard may instead throw its own exception, e.g. `UnauthorizedException` (401).
- An interceptor implements `NestInterceptor` with `intercept(context: ExecutionContext, next: CallHandler)`.
- `next.handle()` returns an RxJS `Observable` of the route handler's result. Code before `next.handle()` runs pre-handler; operators piped onto it run post-handler (`map` to transform the response, `tap` for logging/timing, `catchError` to remap errors, `timeout`).
- If `next.handle()` is never called, the route handler does not run (this is how response caching short-circuits).
- Attach with `@UseInterceptors(MyInterceptor)` or `app.useGlobalInterceptors()`.
- Pipeline order: middleware -> guards -> interceptors (pre) -> pipes -> handler -> interceptors (post) -> exception filters.
