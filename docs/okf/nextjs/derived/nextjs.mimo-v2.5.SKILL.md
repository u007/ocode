---
name: nextjs-tuning-mimo-v2.5
description: Corrective Next.js App Router guidance for the exact area mimo-v2.5 tests weak on (metadata). Loaded only in Next.js repos when this exact model is active.
when_to_use: The active model id is exactly mimo-v2.5 AND the repo uses Next.js (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Next.js repos.
# --- Kaizen metadata ---
tuned_for: mimo-v2.5
tuned_version: "1"
stack: nextjs
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---

# Next.js tuning — mimo-v2.5

> Generated from `../scores/mimo-v2.5.md` (corpus_rev 1). Covers **only** the
> tags this exact model scored below 0.75 on. It says nothing about app-router,
> server-components, data-fetching, caching, rendering, server-actions,
> route-handlers, streaming, or navigation — the model already handles those
> well, and restating them would waste prompt/cache budget.

## Metadata API (weak: metadata 0.67)

- The static `metadata` export and `generateMetadata` are **Server Component
  only**. A file with `"use client"` cannot export `metadata` — state this
  constraint explicitly when explaining how to set page `<head>` tags, don't
  just show the export.
- `generateMetadata`'s fetches are deduped against the page component's own
  fetches via **Request Memoization** — same URL/options within one render
  pass resolves once. Say this explicitly: it avoids double-fetching the same
  data for the head tags and the page body, it isn't a separate unrelated
  fetch.

---

*Regenerate this file whenever `mimo-v2.5`'s version changes or the Next.js
corpus revision bumps.*
