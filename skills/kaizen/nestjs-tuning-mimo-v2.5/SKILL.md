---
name: nestjs-tuning-mimo-v2.5
description: Corrective NestJS guidance for the exact area mimo-v2.5 tests weak on (shutdown/lifecycle hook order and triggers). Loaded only in NestJS repos when this exact model is active.
when_to_use: The active model id is exactly mimo-v2.5 AND the repo uses NestJS (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-NestJS repos.
# --- Kaizen metadata ---
tuned_for: mimo-v2.5
tuned_version: "1"
stack: nestjs
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# NestJS tuning — mimo-v2.5

## Lifecycle hooks — shutdown order and version facts

- Shutdown hook order is **`onModuleDestroy()` → `beforeApplicationShutdown()`
  → `onApplicationShutdown()`** — in that exact sequence. Do not place
  `onApplicationShutdown` before `beforeApplicationShutdown`.
- **NestJS 11 made destroy-hook order the reverse of init order**, and this
  *is* a v11 change — earlier versions did not guarantee it. If init ran
  C → B → A, destroy runs A → B → C. Do not state that reverse-order teardown
  "did not change in v11" — that is factually wrong; attribute the guarantee
  to v11 explicitly.
- Init hooks (`onModuleInit`, `onApplicationBootstrap`) are triggered by
  **`app.init()`** (which `app.listen()` calls internally) — name that
  trigger explicitly rather than describing it only as "during the bootstrap
  sequence".
- Nest **awaits** a Promise-returning lifecycle hook before moving to the
  next phase — state this explicitly when asked about async hooks, don't just
  say hooks "run during bootstrap".
