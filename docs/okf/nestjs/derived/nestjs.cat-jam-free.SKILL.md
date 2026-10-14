---
name: nestjs-tuning-cat-jam-free
description: Corrective NestJS guidance for the exact area cat-jam-free tests weak on (guard attachment and the interceptor next.handle() mechanism). Loaded only in NestJS repos when this exact model is active.
when_to_use: The active model id is exactly cat-jam-free AND the repo uses NestJS (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-NestJS repos.
# --- Kaizen metadata ---
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: nestjs
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# NestJS tuning — cat-jam-free

## Guards and interceptors — complete the mechanism

- When describing a guard, always state **how it is bound and what it is for**:
  attach with `@UseGuards(MyGuard)` on a handler or controller, or globally with
  `app.useGlobalGuards()` / an `APP_GUARD` provider. Guards make
  authentication/authorization decisions from the `ExecutionContext`;
  `canActivate()` returning `false` makes Nest throw `ForbiddenException` (403).
- An interceptor's `intercept(context, next)` does not just "return an
  Observable": code before **`next.handle()`** runs before the handler, and
  `next.handle()` returns an RxJS `Observable` of the handler's result that the
  interceptor transforms **after** the handler with operators (`map`, `tap`,
  `catchError`, `timeout`). Name `next.handle()` and the before/after split
  explicitly.
- Interceptors are bound the same way as guards: `@UseInterceptors()`,
  `app.useGlobalInterceptors()`, or an `APP_INTERCEPTOR` provider.
