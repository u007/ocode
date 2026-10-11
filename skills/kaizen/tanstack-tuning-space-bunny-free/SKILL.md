---
name: tanstack-tuning-space-bunny-free
description: >
  Two things for space-bunny-free in a TanStack repo. (1) Stack hygiene: the
  route tree is GENERATED, so never hand-edit, reformat, lint, or "fix" its
  format — change the route files and let the plugin regenerate; when the tree
  looks wrong, diagnose the input. (2) The one gap the closed-book tanstack
  benchmark actually flagged: `suspense` (weakest tag 0.88; question
  tanstack-suspense-02 at 0.50) — name the Suspense + ErrorBoundary PAIRING and
  the queryFn-must-throw contract, not just "use an ErrorBoundary".
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `space-bunny-free` (e.g. `opencode-go/space-bunny-free` →
  `space-bunny-free`) AND the repository is a TanStack project
  (`@tanstack/react-query` or `@tanstack/react-router` dep present — per
  meta.yaml detection). For any other model or non-TanStack repo, do not load.
# --- Kaizen metadata ---
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: tanstack
# `source_scorecard:` is deliberately ABSENT. Every sibling under skills/kaizen/
# carries one because it was derived from a benchmark whose scorecard had a tag
# below threshold; this one was not, and the field means "derived from". Nothing
# parses it (no Go/Python/shell reader), so keeping it would only assert a
# derivation that did not happen. The benchmark evidence for section 2 is cited
# in the provenance note below instead.
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# TanStack corrections for space-bunny-free

**Provenance — read this before adding to it.** The closed-book tanstack scorecard
for this model (`../scores/space-bunny-free.md`, `stack_corpus_rev: 1`) put it at
**92.6% with NO tag below the 0.75 threshold**, so per the corpus derivation rule it
explicitly derived *no* skill. Only `suspense` is weak (0.88, with one question at
0.50), and every other deduction was a missing secondary clause rather than a wrong
mechanism. **Section 2 is the only scorecard-derived content here.** **Section 1 is
not** — it is a requested stack-hygiene correction, kept because the
generated-file instinct costs a wasted turn every time. Do not pad this file with
`router-search` (0.94), `router-loaders` (0.91), `router-typesafety` (0.91),
`invalidation` (1.00), `mutations` (0.94), `query-fn` (0.92) or `prefetch` (0.90)
material: this model already aced those, and restating them is noise that dilutes the
two corrections that matter.

<!-- kaizen:digest -->
**The route tree is GENERATED — never hand-edit it, never reformat or lint it, never report its formatting as a defect.** `routeTree.gen.ts` is written by `@tanstack/router-plugin` (or `@tanstack/router-cli`) during dev/build from the route files in `routesDirectory`. Its format is not yours to fix and its diff is not your change. Change the route `.tsx` file and let the plugin regenerate.

**The documented remedy for a mis-formatted route tree is an IGNORE entry, never a reformat.** The docs state the file "is managed by TanStack Router and therefore shouldn't be changed by your linter or formatter" — add it to `.prettierignore`, ESLint `ignores`, or Biome `files.ignore`. The generator's `quoteStyle` defaults to `"single"`, so it can legitimately disagree with a repo's Prettier config; that disagreement is correct by design.

**When the route tree looks wrong, diagnose the INPUT, not the output.** A missing route is a missing/renamed file or a wrong `routeFileIgnorePrefix` (default `"-"`); a wrong parent is a missing `getParentRoute` or a file-path naming mistake; a stale tree means the dev server/CLI hasn't run. Never patch the generated file.

**Suspense needs BOTH boundaries, as a pair — naming only the ErrorBoundary is the incomplete answer.** `useSuspenseQuery` replaces `status`/`error` with `<Suspense>` for the pending state AND an error boundary for the error state. A component using it needs both wrapped around it; saying "errors go to an ErrorBoundary" without the pairing is half an answer.

**`queryFn` must THROW (or reject) on failure — that is what feeds the error boundary.** Returning an error object, a result envelope, or `null` is not an error and renders as data. State the throw contract explicitly whenever you describe error handling in suspense mode.
<!-- /kaizen:digest -->

## 1. The generated route tree: the format is not yours to fix

The wrong instinct here costs a turn every time and produces a diff the user throws
away. Name the rule, then move on to the route file.

- `routeTree.gen.ts` (default `generatedRouteTree: "./src/routeTree.gen.ts"`) is
  emitted from the route files in `routesDirectory` (default `"./src/routes"`) by the
  bundler plugin — `@tanstack/router-plugin` for Vite/Rspack/Webpack/Esbuild — or by
  `@tanstack/router-cli`, and is regenerated on dev and build.
- So: never edit it by hand, never run a rewriter over it, never reflow or tidy it,
  and never open a review comment about its formatting. There is nothing to say there.
- Diagnose the input. Missing route → missing/renamed file, or a `routeFileIgnorePrefix`
  collision (default `"-"`). Wrong parent → missing `getParentRoute` (code-based
  routing) or a naming mistake in the path. Stale tree → the dev server or CLI hasn't
  regenerated; run it rather than editing.
- Naming that decides the generated shape: `.` nests (`posts.$postId.tsx` →
  `/posts/$postId`), `_pathlessLayout` contributes no path segment, `$postId` is a
  param, `__root.tsx` is the root, and a trailing `_` on a directory escapes nesting
  (`posts_/`). Directory and flat routes mix freely.
- If the repo hasn't ignored the file, the deliverable is the ignore entry, and that is
  in scope. The file is normally committed, so it appears in diffs — expected, not a
  change you own.

### Ask one generated question, not a broad one

Do not hand back "do you know TanStack Router?" — that costs a turn and returns
nothing. Instead pick **one concrete spot in the generated tree that no linter or
formatter covers** (by definition, since the file is ignored), turn it into a single
concrete question, and state your recommended answer:

> In `routeTree.gen.ts`, `<route path>` is generated with `loaderDeps` but no
> `staleTime`, so its loader data is stale immediately on every navigation. Add
> `staleTime` here — yes or no? (Recommend yes; the route refetches on each entry
> today.)

Generated code that tooling skips is the one region where nobody has already answered
for you, so it is the highest-information question available — and because the file is
generated, it can never degenerate into a formatting nit. One question, one proposed
answer, one word to reply.

## 2. suspense: name the pairing, not just the boundary

*Scorecard-derived — `suspense` is the weakest tag (0.88); `tanstack-suspense-02`
scored 0.50 because the ErrorBoundary was named but the pairing and the throw contract
were not.*

- Suspense mode **replaces** `status` states and `error` objects: `<Suspense>` handles
  the pending state, an error boundary handles the error state. Both are required
  around any component using `useSuspenseQuery` — the pair is the answer, and naming
  only the error boundary leaves the mechanism half-described.
- `queryFn` must **throw** (or reject) for the error to reach the boundary. Returning
  an error object or a result envelope renders as *data*. Always state this when
  describing suspense error handling — it is the half of the contract that gets missed.
- `data` is typed as defined, because loading and error are handled by the boundaries.
  The costs are the flip side: you cannot conditionally `enabled`-disable the query,
  and there is no `placeholderData` (wrap key-changing updates in `startTransition`).
- Not every error reaches the boundary by default: `throwOnError` defaults to
  `(error, query) => typeof query.state.data === 'undefined'`, so a query that already
  has cached data keeps rendering while stale. To throw unconditionally you must do it
  manually (`if (error && !isFetching) throw error`), because the option itself cannot
  be overridden.
- Retrying needs `QueryErrorResetBoundary` / `useQueryErrorResetBoundary` to reset the
  query errors before re-rendering.

## Deliberately omitted

`router-search` (0.94), `router-loaders` (0.91), `router-typesafety` (0.91),
`query-keys` (0.89), `caching` (0.89), `prefetch` (0.90), `mutations` (0.94),
`query-fn` (0.92), `suspense` non-gap mechanics, and `invalidation` (1.00) are all at
or above threshold. Do not add sections for them on a hunch — if a future benchmark run
drops a tag below 0.75, that run's scorecard is the warrant.