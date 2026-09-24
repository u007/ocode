---
type: Gotcha
title: React 19 upgrade — act() timing in tests and the skipLibCheck dependency
description: React 19's async act() can yield to macrotasks, so tests that race an async effect or jsdom's rAF clock go flaky; dnd-kit 6 / monaco-react typings still need skipLibCheck
tags:
  - gotcha
  - react
  - testing
  - web
timestamp: 2026-09-24T00:00:00Z
---
The web app moved from React 18.3 to React 19.3 (2026-09-24). No app code needed
changes. Two things did break, and both will come back if new code or tests
repeat the same patterns.

## 1. `await act(...)` no longer blocks macrotasks

Under React 18, `await act(async () => …)` only drained microtasks. Under React 19
it can yield to the macrotask queue while flushing. Two tests assumed it
couldn't:

- **`BrowserPanel.test.tsx` "offers the bypass for a private host in chrome
  mode…"**: the test clicked the TLS bypass right after `render`. The panel's
  initial iframe load (it waits on async `getBrowseBase()`) now starts *inside*
  the click's `act`. It bumps `loadGeneration`, and `handleBypass`'s
  stale-load guard (correctly) aborts the reload. Fix: wait for the initial
  load (`iframe src` carries `__grant=`) before clicking.
- **`ChatPanel.test.tsx` "prepends older messages…"**: the test changed the
  mocked `scrollHeight` *between* two awaits and used `flushRAF`
  (`setTimeout(0)`), which does **not** service jsdom's ~16ms rAF clock. Under
  full-suite load, the scroll-restore rAF fired before the height changed, so
  it wrote a delta of 0. That failed about 2 in 3 full runs. Fix: tie the
  mocked height to the rendered DOM (grows once `older1` is present) and wait
  a real frame with `advanceFrame`. The assertion is now the exact delta
  (`2 * 96`); the old `> 0` check also passed when the bottom-pin path wrote
  `scrollHeight`.

**Rule:** in tests, never mutate mocked geometry or state between awaits and
assume an async effect or rAF hasn't run yet. Make mocks follow what the DOM
shows, wait on an observable condition (`waitFor`), and use `advanceFrame` (not
`flushRAF`) when the code under test uses `requestAnimationFrame`.

Related open upstream issues: `act()` loops forever when a parent re-render
recreates 3 or more pending sibling `<Suspense>` boundaries
([react#37556](https://github.com/react/react/issues/37556)). A vitest worker
that hangs with no timeout is probably this. See also RTL act/Suspense timing
([testing-library#1375](https://github.com/testing-library/react-testing-library/issues/1375)).

## 2. `skipLibCheck: true` is load-bearing

`@types/react` 19 removed the global `JSX` namespace. The `.d.ts` files of
`@dnd-kit/core` 6.x (unmaintained,
[dnd-kit#1559](https://github.com/clauderic/dnd-kit/issues/1559)) and
`@monaco-editor/react` 4.7
([monaco-react#695](https://github.com/suren-atoyan/monaco-react/issues/695))
still reference it. `web/tsconfig.json`'s `skipLibCheck: true` is what keeps
`pnpm typecheck` green. Don't turn it off until both packages ship fixed typings.

## Also

- Keep `@tanstack/react-virtual` ≥ 3.14.13. Earlier versions give a "flushSync
  was called from inside a lifecycle method" dev warning under React 19
  ([virtual#1281](https://github.com/TanStack/virtual/issues/1281)).
- Don't wrap Monaco in `<Activity>`. Hiding and re-showing it crashes the
  editor ([monaco-react#794](https://github.com/suren-atoyan/monaco-react/issues/794)).
- React 19 alone gives no measurable runtime speedup for this SPA. The speed
  gains people report come from the React Compiler, which is tracked
  separately in TODO.md.
