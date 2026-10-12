---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on nextjs (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `nextjs/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/nextjs.step-5-preview-free.SKILL.md` (`nextjs-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (session audited: 1 user +
> 1 assistant message, 0 tool calls). Baseline: `scores/step-5-preview-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **data-fetching, metadata**.

## Provenance

Closed-book (Rule 0). Answerer: `ocode run -m opencode-go/step-5-preview-free` with
the derived skill body prepended to `_prompts/nextjs.md`; no answer key in context.
The session shows exactly 1 user + 1 assistant message and 0 tool calls. The answers
file holds exactly 34 records, one per question id. Unlike the baseline run there
were no duplicate ids, no leaked tool-call envelope and no second-pass answers.
Skill absorption is visible verbatim: `data-fetching-nogssp-02` opens "Two tools, not
one" and repeats the skill's directive sentence ("Always offer the Suspense-isolation
option alongside `Promise.all`; don't stop at parallel awaits") as if it were advice to
the reader — graded on the concepts present, not the register. One cosmetic
self-correction artifact survives in `app-router-conventions-01` ("`@named` parallel
slots are excluded... route groups are excluded from the URL").

Graded against `nextjs/questions.yaml` corpus_rev 1. Grading rule applied (same as
the baseline): a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the same construct withholds that point and is marked
"(judgment)"; tangential errors are noted, not penalized. Minor missing sub-items of a
point are noted, not penalized (the docx with-skill precedent rule) — see the one
place this matters under "Target-tag verdict".

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on nextjs).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | all five files, folder→URL incl. `[slug]`, route groups, `_private`; route.ts = API endpoint ("renders no UI"); page-exclusivity not stated (minor, same leniency as longcat with-skill) |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | also correct on `global-error.tsx` and production `digest` redaction |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | no stray token this run |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | **baseline 0.50 → fixed**: plain functions do not cross ("runtime error, not an implicit conversion to an action"); only `"use server"` functions travel as a reference; children-as-slot correct |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | **baseline 0.50 → fixed**: `Promise.all` AND per-fetch async component + own `<Suspense>` streaming in parallel; skill directive echoed verbatim (see Provenance) |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | Next 13/14 cached; 15+ uncached; opt in via `force-cache` (Next 16 not named separately; "15+" covers it) |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | also names `'auto'` and `'error'` |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | tangential (as baseline): lists "a `no-store`/uncached fetch" as a dynamic trigger |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | correct on `dynamicParams` default and `false` → 404 |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports, Web Request/Response, replaces `pages/api`; page-exclusivity not stated (minor); baseline's layout/loading tangential error gone |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | muddled clause under "to opt into caching" ("or use dynamic APIs ... to force per-request evaluation") — not a wrong claim, just misplaced |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | (judgment) static `metadata` export from page/layout; Next renders it into `<head>` server-side; correct that `next/head` is Pages-only and that `metadata` and `generateMetadata` cannot both be exported. OMITS the "not available in client components" half of point 2 — the baseline answer stated it. Credited as a minor sub-item (hy3 and docx-precedent rule); three other nextjs scorecards (longcat, space-bunny, mimo) scored this omission 0.50 — see Target-tag verdict |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | **baseline 0.50 → fixed**: `generateMetadata` with awaited `params`; fetch deduped with the page automatically via Request Memoization ("no opt-in and no second network call"); `React.cache()` correctly scoped to non-fetch calls; static routes evaluate at build/revalidate time |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | **baseline 0.50 → fixed**: root→leaf merge; nested objects (`openGraph`, `twitter`, `icons`, `robots`, `alternates`) replaced wholesale, icons do not accumulate; `title.template` + `title.absolute` correct |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | no invented `icon-dynamic.tsx` this run |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | `next/navigation`; client hooks → `"use client"`; server side "reading data passed down" (params-props alternative only implied, minor); baseline's `useServerInsertedHTML` error gone |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | 307 / `permanentRedirect` 308 correct; baseline's `type` default error gone |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | trust | target? |
|-----|---------:|-----------:|--:|--:|-------|---------|
| app-router | 1.00 | 1.00 | 0.00 | 6 | ok | no |
| server-components | 0.93 | 1.00 | +0.07 | 6 | ok | no |
| data-fetching | 0.83 | 1.00 | +0.17 | 5 | ok | **yes** |
| caching | 1.00 | 1.00 | 0.00 | 7 | ok | no |
| rendering | 0.94 | 1.00 | +0.06 | 7 | ok | no |
| server-actions | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| route-handlers | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| streaming | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| metadata | 0.75 | 1.00 | +0.25 | 4 | ok | **yes** |
| navigation | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: app-router 13/13; server-components 15/15; data-fetching 12/12
(baseline 10/12); caching 18/18; rendering 17/17 (baseline 16/17); server-actions
11/11; route-handlers 9/9; streaming 7/7; metadata 6/6 (baseline 4.5/6);
navigation 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 74 / 74 = 100.0%
```

Baseline 70.5 / 74 = 95.3%. All four baseline losses (props-03, nogssp-02,
metadata-dynamic-02, metadata-inherit-03) are recovered; no new losses.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| data-fetching | 0.83 | 1.00 | **PASS** |
| metadata | 0.75 | 1.00 | **PASS** |

Both target tags reach the threshold. The skill is validated for
step-5-preview-free @ 5-preview. Every failure the skill cites is fixed in the
with-skill answers, in the skill's own terms (function props are a runtime error, not
an auto-action; Suspense isolation offered alongside `Promise.all`; `generateMetadata`
dedup is automatic; nested metadata objects replace wholesale).

**Sensitivity of the metadata verdict (one judgment).** `metadata-static-01` omits the
"not available in client components" half of its second rubric point, a sub-item the
skill does not cover and the baseline answer got right. It is credited here as a
minor missing sub-item (the rule the docx with-skill precedent and the hy3 nextjs
scorecard apply). Under the stricter reading used by the longcat, space-bunny and
mimo nextjs scorecards (0.50 for that omission), metadata would be 5/6 = 0.83 and
would FAIL by 0.02 on a sub-item unrelated to the skill's two cited failures. If the
reviewer prefers the strict reading, the correct response is to re-sample, not to add
a "metadata is Server-Component-only" bullet to the skill on a single sample.

Non-target drift: none downward. server-components 0.93 → 1.00 and rendering
0.94 → 1.00 moved only because the shared target-tag questions (props-03, nogssp-02)
improved; no non-target tag fell. Nothing to act on.
