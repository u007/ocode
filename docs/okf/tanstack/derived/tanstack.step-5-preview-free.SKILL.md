---
name: tanstack-tuning-step-5-preview-free
description: >
  Corrective TanStack knowledge for step-5-preview-free, targeting the
  query-fn and router-search gaps this model showed on the closed-book tanstack
  benchmark: the throw-to-fail contract as it applies to `mutate` vs
  `mutateAsync` and to `useSuspenseQuery` + ErrorBoundary, and `loaderDeps` as
  the mechanism (not `validateSearch`) that lets a loader see search params.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `step-5-preview-free` AND the repository is a TanStack project
  (`@tanstack/react-query` or `@tanstack/react-router` dep present — per
  meta.yaml detection). For any other model or non-TanStack repo, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: tanstack
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# TanStack corrections for step-5-preview-free

## query-fn: one throw-to-fail contract, two very different surfaces

step-5-preview-free knows that a `queryFn` must throw (check `res.ok`) to put a
query in the error state, but misdescribes where that thrown error goes on the
two surfaces that hide the `isError` branch.

- `mutate` **never** produces an unhandled rejection. It is fire-and-forget:
  TanStack Query catches the rejection internally (`.catch(noop)`), and the
  failure surfaces only through `onError`, `isError` / `status === 'error'` and
  `error`. Do not say "an unhandled rejection if there is no handler" about
  `mutate` — that is exactly the behaviour `mutateAsync` has, and the reason the
  two exist. `mutateAsync` returns the Promise; the caller must `await` inside
  `try/catch` (or `.catch`) or the rejection is unhandled.
- The failure contract is identical for `queryFn` and `mutationFn`: throw or
  return a rejected Promise. Returning an error object, or a resolved `Response`
  with `ok === false`, is success.
- With `useSuspenseQuery` there is no `isError`, so the thrown `queryFn` error
  propagates during render to the nearest React `<ErrorBoundary>`. State the
  pairing explicitly: `<Suspense fallback>` owns loading, `<ErrorBoundary>` owns
  failure, both wrap the suspending component. And say the precondition out
  loud: the `queryFn` must still throw on a bad response — if it resolves with
  the 500 body, the boundary never fires and the error body renders as data.
  `useQueryErrorResetBoundary` / `resetErrorBoundary` is the retry hook, not a
  substitute for the throw.

## router-search: loaders see search params via `loaderDeps`, not `validateSearch`

step-5-preview-free attributes "my loader doesn't get the `page` search param"
to a missing search schema. That conflates typing with dependency tracking;
they are separate mechanisms.

- By default a route loader is keyed and re-run on **path params only**. Search
  params are not loader dependencies, even after `validateSearch` is defined.
  `validateSearch` parses the raw query string into a typed object for
  `useSearch()`, `Link search`, and `navigate({ search })` — it does not make
  the loader receive or react to any search value.
- To make a loader depend on a search param declare
  `loaderDeps: ({ search }) => ({ page: search.page })`. The returned object
  becomes the loader's `deps` argument **and** part of its cache identity, so
  the loader receives `deps.page` and reloads when it changes. Name
  `loaderDeps` whenever the task is "fetch in a loader based on a search param".
- The loader argument has no `search` field. It has `params`, `deps`,
  `context`, `location`, `abortController`, `cause`, `preload`, `route`,
  `navigate`. Do not describe the loader as "receiving `search` once
  `validateSearch` is defined" — read search values from `deps`, or from
  `location.search` when you deliberately want no reload tracking.
