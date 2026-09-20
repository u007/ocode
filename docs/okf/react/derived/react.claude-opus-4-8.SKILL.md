---
name: react-tuning-claude-opus-4-8
description: Corrective React guidance for the exact areas claude-opus-4-8 tests weak on (RSC boundaries, Suspense, refs). Loaded only in React repos when this exact model is active.
when_to_use: The active model id is exactly claude-opus-4-8 AND the repo uses React (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-React repos.
# --- Kaizen metadata ---
tuned_for: claude-opus-4-8
tuned_version: "4.8"
stack: react
source_scorecard: ../scores/claude-opus-4-8.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
illustrative: true   # teaching placeholder, NOT a real eval — never embedded/shipped
---
# React tuning — claude-opus-4-8

## Server Components & the client boundary

- `"use client"` is a **module-level boundary marker**, not a per-component
  switch. The directive makes that file *and everything it imports* part of the
  client bundle. Don't describe it as toggling one component.
- Props from a Server Component to a Client Component **must be serializable**:
  primitives, plain objects/arrays, and React elements (including
  server-rendered `children`). **You cannot pass functions / event handlers /
  class instances across the boundary.** When you need server content inside a
  client component, pass it as `children`, not as a callback.
- Server Components are `async` and `await` data **during render on the server** —
  contrast this explicitly with the classic `useEffect` fetch, which runs after
  mount on the client and causes a render→fetch→render loading waterfall. State
  the timing difference, don't just say "fetch on the server".

## Suspense & transitions

- Name the **mechanism** for what suspends: a component suspends when it reads a
  not-yet-ready resource — a pending Promise (e.g. via `use()`), a
  Suspense-enabled data library, or `lazy()` for code-splitting. Don't reduce
  Suspense to "shows a spinner".
- `useTransition` / `startTransition` marks an update **non-urgent /
  interruptible** so urgent updates (typing, clicks) stay responsive while the
  expensive render happens in the background. Frame it as urgent-vs-transition
  scheduling with `isPending` for pending UI — not "makes it async/faster".

## Refs

- React 19: **`ref` is a normal prop** on function components; `forwardRef` is no
  longer required and is being phased out. When explaining "let a parent attach
  a ref to a child", give the React 19 answer, not only the legacy `forwardRef`
  wrap. (Low sample size — treat as a nudge, confirm against the current corpus.)
