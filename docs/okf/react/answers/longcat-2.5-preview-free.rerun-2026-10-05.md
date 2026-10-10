- id: react-recon-keys-01
  answer: |
    React uses `key` to identify which items have changed, been added, or been removed during reconciliation. Keys let React match elements between renders so it can preserve component state, DOM nodes, and focus. Array index is usually a bad key because it's tied to position, not identity — inserting, removing, or reordering items shifts indices, causing React to associate the wrong state with the wrong item (e.g., a checkbox staying checked on the wrong row after a deletion).

- id: react-recon-diff-02
  answer: |
    React reuses an existing component instance when the element's type (and key, if present) matches the previous render at the same position in the tree. If the type changes (e.g., `<div>` → `<span>`) or the key changes, React unmounts the old subtree and mounts a new one. Same type + same key = update in place; different type or key = full remount.

- id: react-recon-remount-03
  answer: |
    Pass a `key` prop to the form component that changes when the selected user changes (e.g., `<UserForm key={selectedUserId} />`). When the key changes, React unmounts the old instance and mounts a fresh one, giving it brand-new initial state. This is the idiomatic "reset via remount" pattern.

- id: react-hooks-rules-01
  answer: |
    The Rules of Hooks: (1) Only call hooks at the top level of a function component or custom hook — never inside loops, conditions, or nested functions. (2) Only call hooks from React function components or custom hooks. Calling a hook conditionally breaks React because hooks are stored in a linked list on the fiber in call order; skipping or reordering a hook shifts every subsequent hook's position, causing state to be mismatched with the wrong hook.

- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` captures `count` from the current render's closure. If multiple updates are batched before a re-render, they all see the same stale `count` value, so calling it three times in a row only increments by 1. The functional updater form `setCount(c => c + 1)` receives the latest pending state from React's queue, so each call builds on the previous result and all three increments apply correctly.

- id: react-hooks-memo-03
  answer: |
    `useMemo` memoizes a computed value (the result of a function), while `useCallback` memoizes a function reference itself. Memoizing actually help when: (1) the computation is expensive and inputs haven't changed, or (2) the memoized value/function is passed as a prop to a child wrapped in `React.memo` or used in a dependency array of another hook — otherwise the referential equality is wasted and you've added overhead for nothing.

- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when: (1) the next state depends on the previous state in complex ways, (2) you have multiple related state values that should update together, (3) the update logic is non-trivial and benefits from being extracted and tested independently, or (4) you want to dispatch actions that describe what happened rather than directly setting state. It's also useful when state transitions need to be predictable and centralized.

- id: react-hooks-use-05
  answer: |
    React 19's `use` is a new API that can read a thenable (Promise) or Context during render, conditionally — unlike other hooks, it can be called in loops and conditionals. When `use` receives a pending Promise, it suspends the component until the Promise resolves. It differs from other hooks in that it's not limited to the top-level rules and is specifically designed for reading resources (promises, context) rather than managing state or side effects.

- id: react-effects-deps-01
  answer: |
    The dependency array should contain every reactive value used inside the effect: props, state, and derived values from the component scope. If you omit a used value (lie about deps), the effect won't re-run when that value changes, leading to stale closures where the effect operates on outdated data. This causes bugs like showing old data after a prop change or missing updates.

- id: react-effects-cleanup-02
  answer: |
    The cleanup function runs before the effect re-runs (when deps change) and before the component unmounts. It's needed for subscriptions and timers to prevent memory leaks and duplicate handlers — without cleanup, every effect re-run would add another listener or interval without removing the old one, causing the component to accumulate stale callbacks and leak memory.

- id: react-effects-misuse-03
  answer: |
    Two common misuses: (1) Using `useEffect` to derive state from props — instead, compute the derived value during render or use a `key` to reset state. (2) Using `useEffect` to respond to a user event that could be handled directly in the event handler — the effect adds an unnecessary render cycle and complexity. In both cases, the logic belongs either in the render body or in the event handler, not in an effect.

- id: react-effects-strictmode-04
  answer: |
    StrictMode double-invokes effects on mount in development to surface missing cleanup logic. If an effect creates a subscription, timer, or listener without returning a cleanup function, the double-invoke exposes the leak by creating two instances. It tells you to fix the effect by adding proper cleanup so the effect is resilient to being run multiple times.

- id: react-rsc-boundary-01
  answer: |
    A Server Component runs only on the server, has no client-side state or effects, and can directly access server resources (databases, filesystems). A Client Component runs in the browser and can use state, effects, and browser APIs. `"use client"` marks the boundary — it declares that the module and everything it imports should be sent to and rendered on the client. It doesn't make the file itself a client component; it marks the entry point into the client bundle.

- id: react-rsc-props-02
  answer: |
    Across the client boundary, props must be serializable: plain objects, arrays, strings, numbers, booleans, null, and dates. What can't cross: functions (including event handlers), class instances, symbols, and non-serializable objects. This is because props are serialized and sent over the network from server to client. To pass interactivity, you pass a Client Component as a child/prop instead of a function.

- id: react-rsc-data-03
  answer: |
    In a Server Component, you fetch data directly with `await` in the component body (or use a loading library) — no `useEffect` needed. The component is async and React awaits its Promise before rendering. This differs from the classic `useEffect` fetch because: (1) it runs on the server, closer to the data source, (2) it doesn't require client-side state or loading flags, (3) the data is available before the first render, eliminating the loading-then-fetch-then-render cycle.

- id: react-context-rerender-01
  answer: |
    When the provider's value is created inline (e.g., `value={{ user, setUser }}`), a new object reference is created on every provider render. Since Context consumers re-render whenever the value's reference changes, all consumers re-render even if `user` and `setUser` haven't changed. Fix: memoize the value with `useMemo` (or `useState` for stable references) so the object identity is stable across renders.

- id: react-context-usage-02
  answer: |
    Context is the right tool for truly global, low-frequency-update data: theme, locale, authenticated user, or app-wide configuration. It's the wrong one for high-frequency updates (like form state or rapidly changing data) because every context change re-renders all consumers. It's also wrong when only a few components need the data — prop drilling or component composition is simpler and more performant in those cases.

- id: react-perf-memo-01
  answer: |
    `React.memo` is a higher-order component that skips re-rendering a component if its props are shallowly equal to the previous render. It fails to prevent re-renders when: (1) props are objects/arrays/functions created inline (new reference every render), (2) the component uses context that changes, (3) the component's own state changes, or (4) the props include non-primitive values that aren't referentially stable.

- id: react-perf-list-02
  answer: |
    The higher-impact fix is virtualization (windowing) — only render the rows visible in the viewport (e.g., using `react-window` or `@tanstack/react-virtual`). With 10,000 rows, the DOM itself is the bottleneck: thousands of nodes cause slow layout, paint, and memory usage. Virtualization reduces the DOM to dozens of nodes regardless of list size, which is a far bigger win than memoizing row components.

- id: react-refs-useref-01
  answer: |
    Two main uses: (1) accessing and mutating a DOM node or element directly (e.g., focusing an input, measuring an element), and (2) storing a mutable value that persists across renders without causing re-renders (e.g., a previous value, an interval ID). Mutating `ref.current` doesn't re-render because refs are not reactive — React doesn't track changes to `ref.current`, so updating it is a direct mutation that bypasses the render cycle.

- id: react-refs-forward-02
  answer: |
    Before React 19, you use `React.forwardRef` to wrap the child component so it receives a `ref` prop and attaches it to a DOM node. In React 19, `ref` is passed as a regular prop to function components — no `forwardRef` needed. The component receives `ref` in its props and can attach it directly to a DOM element. `forwardRef` still works but is no longer required.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` shows the fallback UI while its children are loading or suspending. A component "suspends" when it throws a Promise (or a thenable) during render — React catches it, shows the nearest Suspense boundary's fallback, and retries rendering when the Promise resolves. This is how data fetching libraries and `React.lazy` code-splitting integrate with React's rendering.

- id: react-suspense-transition-02
  answer: |
    `useTransition` / `startTransition` solves the problem of blocking the UI during expensive updates. It marks a state update as non-urgent (a transition), allowing React to keep the current UI responsive and show it while the new state is being prepared. It changes scheduling by deprioritizing the update — React can interrupt it for more urgent updates (like user input) and show a pending indicator instead of blocking the interface.

- id: react-state-batching-01
  answer: |
    Automatic batching in React 18 means that multiple state updates inside any event handler, promise, timeout, or native event are batched into a single re-render, regardless of where they originate. Previously, only updates inside React event handlers were batched. To force a synchronous update when truly needed, you can use `flushSync` from `react-dom`, which flushes updates immediately outside the batching mechanism.

- id: react-state-lifting-02
  answer: |
    The standard pattern is lifting state up: move the shared state to the closest common ancestor of the two siblings, then pass the state down via props along with callbacks to update it. The parent owns the state and passes it to both children, keeping them in sync because they read from the same source of truth.

- id: react-state-derived-03
  answer: |
    Keeping a separate `useState` copy of a prop is an anti-pattern because it creates two sources of truth that can diverge — the prop changes but the state copy doesn't update automatically, leading to stale UI. The fix is to derive the value directly from the prop during render (compute it inline or with `useMemo`), or use a `key` on the component to remount it with new initial state when the prop changes.
