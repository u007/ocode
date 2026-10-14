# cat-jam-free — closed-book answers for react (opencode-go, 2026-10-14); answerer saw only _prompts/react.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: react-recon-keys-01
  answer: |
    Keys tell React which list item is which across renders so it can preserve
    DOM nodes, state, and effects for the right item. Array indices are usually bad
    keys when items are inserted, removed, reordered, or filtered, because the index
    identifies a position rather than an identity; React may then reuse the wrong
    instance and corrupt item state.

- id: react-recon-diff-02
  answer: |
    React compares element type and key. If both match, it reuses the existing
    instance and updates its props/children; if either differs, it unmounts the old
    subtree and mounts a new one. Children are matched by key/position within that
    element tree.

- id: react-recon-remount-03
  answer: |
    Give the form a `key` equal to the selected user's id, e.g. `<Form key={userId} />`.
    When the key changes, React unmounts the old form and mounts a new one, clearing
    its internal state.

- id: react-hooks-rules-01
  answer: |
    Rules of Hooks: call hooks only at the top level (not in loops, conditions, or
    nested functions), and only from React function components or custom hooks.
    React tracks hook state by call order, so conditional calls can change that order
    between renders and associate the wrong state with the wrong hook.

- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` closes over the `count` value from that render, so it
    can be stale in async code or batched updates. The functional form
    `setCount(c => c + 1)` receives React's latest pending state for that update.

- id: react-hooks-memo-03
  answer: |
    `useMemo` memoizes a computed value; `useCallback` memoizes a function
    reference. Memoization helps when a computation is expensive or when stable
    references avoid unnecessary child re-renders or effect re-runs, but it has
    overhead and is not useful for cheap calculations.

- id: react-hooks-reducer-04
  answer: |
    Use `useReducer` when state transitions are complex, involve multiple related
    values, depend on previous state in nontrivial ways, or benefit from centralized
    action-based updates and easier testing.

- id: react-hooks-use-05
  answer: |
    React 19's `use` reads a promise or context during render. It can suspend until
    a promise resolves, integrates with Suspense, and unlike other hooks can be
    called conditionally or after early returns.

- id: react-effects-deps-01
  answer: |
    The dependency array should include every reactive value used by the effect. Omitting
    a used value means the effect can use stale data and will not re-run when that
    value changes, causing bugs.

- id: react-effects-cleanup-02
  answer: |
    Cleanup runs before the effect re-runs and when the component unmounts. It removes
    subscriptions, timers, listeners, or other external resources to prevent leaks and
    duplicate subscriptions.

- id: react-effects-misuse-03
  answer: |
    Common misuses: deriving state from props with `useEffect` plus `setState`
    (derive during render instead), and performing user-triggered work in an effect
    reacting to state changes (call it directly from the event handler instead).

- id: react-effects-strictmode-04
  answer: |
    StrictMode intentionally mounts, unmounts, and remounts components in development
    to expose missing cleanups and non-idempotent effects. It tells you to ensure
    effects clean up their resources and can safely run more than once.

- id: react-rsc-boundary-01
  answer: |
    Server Components run on the server and can access server-only resources; they do
    not send their component code to the browser. Client Components run in the
    browser and can use state, effects, and events. `"use client"` marks a module as
    entering the client bundle and all its imports as client code.

- id: react-rsc-props-02
  answer: |
    Across the client boundary, pass serializable props such as primitives, arrays,
    plain objects, and server action references. Non-serializable values such as class
    instances, arbitrary functions, or DOM nodes cannot cross.

- id: react-rsc-data-03
  answer: |
    A Server Component can be async and await data directly during render on the
    server. This avoids the client mount-then-fetch waterfall and keeps secrets server-side,
    while Suspense handles loading states where needed.

- id: react-context-rerender-01
  answer: |
    An inline object is a new value every render, so memoized consumers may re-render
    even when the logical contents are unchanged. Fix it by memoizing the value, e.g.
    with `useMemo`, or by splitting state and dispatch into separate contexts.

- id: react-context-usage-02
  answer: |
    Context is right for widely shared, relatively stable dependencies such as theme,
    locale, or authentication. It is wrong for rapidly changing values or local
    component state because updates can force many consumers to re-render.

- id: react-perf-memo-01
  answer: |
    `React.memo` shallow-compares props and skips re-rendering when they are shallowly
    equal. It fails to help when props include newly created objects/functions or when
    the component consumes context that changed.

- id: react-perf-list-02
  answer: |
    Use virtualization/windowing so only visible rows render, reducing DOM node count.
    This usually has much higher impact than memoizing rows, though stable keys and
    avoiding unnecessary re-renders also matter.

- id: react-refs-useref-01
  answer: |
    `useRef` is mainly used for mutable instance values across renders and for accessing
    DOM nodes. Mutating `ref.current` does not re-render because React does not treat
    ref mutation as reactive state.

- id: react-refs-forward-02
  answer: |
    Previously use `forwardRef` to expose a child's DOM node. In React 19, `ref` is
    an ordinary prop for function components, so `forwardRef` is no longer needed.

- id: react-suspense-01
  answer: |
    `<Suspense>` displays a fallback while a descendant is suspended. A component
    suspends when it throws a promise, as with lazy-loaded components or React 19's
    `use` reading unresolved data.

- id: react-suspense-transition-02
  answer: |
    `useTransition` and `startTransition` mark updates as non-urgent, keeping the UI
    responsive while deferring or interrupting less important renders. They expose a
    pending flag and lower the update's scheduling priority.

- id: react-state-batching-01
  answer: |
    React 18 automatically batches multiple state updates into one re-render, including
    from timers, promises, and native events. Use `flushSync` from `react-dom` to force
    a synchronous update when truly needed.

- id: react-state-lifting-02
  answer: |
    Lift the shared state to the closest common ancestor and pass the value plus
    callback handlers down to the siblings.

- id: react-state-derived-03
  answer: |
    Copying a prop into state creates a second source of truth that can become stale.
    Derive the value during render from the prop, or memoize an expensive derivation
    with `useMemo` when needed.
