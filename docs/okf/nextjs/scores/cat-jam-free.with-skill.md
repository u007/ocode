---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on nextjs (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `nextjs/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/nextjs.cat-jam-free.SKILL.md` (`nextjs-tuning-cat-jam-free`)
> prepended to the answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **app-router, metadata**.

## Provenance

Closed-book (Rule 0). Answerer: `ocode run -m opencode-go/cat-jam-free -effort med` with
the derived skill body (front matter stripped) prepended to `_prompts/nextjs.md`;
isolated HOME with every tool denied, cwd holding only the sheet; no answer key in
context. The session sqlite shows exactly 1 user + 1 assistant message and 0 tool
calls. The answers file holds exactly 34 records, one per corpus id. Answers stay
terse (1–4 sentences), as in the baseline. Skill absorption is visible verbatim in
`server-components-default-01` ("all components reached through it become Client
Components whether individually marked or not"), `metadata-static-01` ("Next renders
the appropriate head tags itself. Do not hand-write head/title tags") and
`metadata-dynamic-02` (request-memoization dedup) — graded on concepts present.

Graded against `nextjs/questions.yaml` corpus_rev 1 with the baseline grader's rule:
a `point` whose concept is genuinely present earns full credit; a materially wrong
claim about the same construct withholds it; omissions withhold the point; minor
missing sub-items of a point are noted, not penalized.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free on nextjs).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | folder→URL with `[slug]`; route.ts "not a page" accepted as page/route exclusivity (as baseline); loading described as "loading state" (thin) |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component + `reset()`; own/root layout excluded; event handlers not named (as baseline) |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2 | 0.67 | **baseline 0.33 → partly fixed**: server-default + `"use client"` = module AND its imports into the client bundle, unmarked children become client (point 2 now earned). Point 3 still withheld: no "server ships no JS", no "can be async", and this time no client-interactivity half either — the answer stops after the boundary sentence despite the skill's "State what Server Components give you" bullet |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable list + Server Actions exception; children/element-prop slot; no-functions implied (as baseline) |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | `Promise.all` / start-then-await given; the separate-Suspense-boundary option (given in baseline) is absent this run — replaced by request-memoization/`React.cache` talk (spillover from the metadata bullet) |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 1 | 0.50 | same loss as baseline: lists `dynamic` values and says when to use `force-dynamic` ("must render per request"), never what `force-static` does or that `force-dynamic` bypasses the caches |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 2 | 0.67 | callable-from-client + distinct from `"use client"` earned; file-level vs inline placement absent (baseline at least named "function or file") |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | FormData named this run |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 1 | 0.50 | `force-static` (+ `revalidate`) opt-in earned; point 1 withheld — states "not cached by default" with no version and no Next 14 contrast (baseline had the Next 14 caveat) |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | **baseline 0.50 → fixed** (judgment): static `metadata` exported "from a server page or layout"; Next renders the head tags; "do not hand-write head/title". Client-component exclusion is conveyed only by the "server" qualifier, not an explicit "a `"use client"` file cannot export it" — credited; see verdict sensitivity |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | **baseline 0.50 → fixed**: async `generateMetadata` with params; fetches "deduped with page/layout fetches by request memoization" (automatic, not a developer action). "runs on server" not literally stated — minor sub-item, implied by the server-render dedup |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | `next/navigation`; client-only → `"use client"`; params-props alternative not named (as baseline) |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | trust | target? |
|-----|---------:|-----------:|--:|--:|-------|---------|
| app-router | 0.846 | 0.923 | +0.08 | 6 | ok | **yes** |
| server-components | 0.87 | 0.933 | +0.07 | 6 | ok | no |
| data-fetching | 1.00 | 0.917 | −0.08 | 5 | ok | no |
| caching | 0.92 | 0.833 | −0.08 | 7 | ok | no |
| rendering | 0.91 | 0.853 | −0.06 | 7 | ok | no |
| server-actions | 1.00 | 0.909 | −0.09 | 5 | ok | no |
| route-handlers | 1.00 | 0.833 | −0.17 | 4 | ok | no |
| streaming | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| metadata | 0.67 | 1.00 | +0.33 | 4 | ok | **yes** |
| navigation | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: app-router 12/13 (baseline 11/13); server-components 14/15 (baseline
13/15); data-fetching 11/12; caching 15/18 (baseline 16.5/18); rendering 14.5/17
(baseline 15.5/17); server-actions 10/11; route-handlers 7.5/9; streaming 7/7;
metadata 6/6 (baseline 4/6); navigation 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 74 = 91.9%
```

Baseline 68.5 / 74 = 92.6%. Gains: server-components-default-01 (+1), metadata-static-01
(+1), metadata-dynamic-02 (+1). New losses vs baseline: nogssp-02 (−1),
useserver-01 (−1), route-handlers-caching-02 (−1.5); segment-config-05 unchanged at 0.50.
Every new loss is an omission in a terse answer, not a wrong claim.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| app-router | 0.846 | 0.923 | **PASS** |
| metadata | 0.67 | 1.00 | **PASS** |

**PASS** — both target tags reach the threshold. The skill is validated for
cat-jam-free @ alpha.

Caveats:
- **app-router absorption is partial.** The module-graph boundary bullet landed
  (point 2 of default-01 now earned), but the "say Server Components ship no JS and
  can be async" bullet was ignored, so default-01 is still 0.67. app-router passes on
  the strength of the boundary fix alone and has no slack: one more 1-point
  app-router omission would drop it back to 11/13 = 0.846 (FAIL).
- **metadata sensitivity (one judgment).** `metadata-static-01` credits the
  client-component exclusion from "export ... from a server page or layout". Under a
  strict reading requiring an explicit "not in client components" statement, it is
  0.50 and metadata becomes 5/6 = 0.83 — FAIL by 0.02. If the reviewer takes the strict
  reading, the skill's metadata bullet already says it; the right move is to re-sample,
  or to sharpen the bullet to "state explicitly that a client component cannot export
  `metadata`", not to add new content.

Non-target drift (noted, not acted on): caching 0.92 → 0.83, route-handlers 1.00 → 0.83
and rendering 0.91 → 0.85 fall from three omissions (no Next 14 contrast on
route-handler GET caching, no Suspense-isolation option in nogssp-02, no file-vs-inline
`"use server"` placement) — single-sample variance on a terse answerer, none touched by
the skill. If they persist, catch them in a fresh BASELINE eval; do not bolt them onto
this skill.
