  - id: react-recon-keys-01
    answer: |
      React uses `key` to identify which items have changed, been added, or been removed across renders. It lets React match elements between the old and new trees so it can preserve component state and DOM nodes for items that are still present, rather than tearing everything down and rebuilding from scratch. Without keys (or with non-stable keys), React cannot reliably match elements by position, so it may reuse the wrong component instance, leading to corrupted internal state, lost focus, or incorrect animations.
      Using the array index as a key is usually a bad idea because index is not a stable identity — it is a position. When the list reorders, items are inserted/removed, or filtered, the index for a given logical item changes. React then treats a completely different item as the same element, causing state to be applied to the wrong row, uncontrolled inputs to show the wrong value, and unnecessary DOM mutations. A stable, unique ID from the data itself (e.g. `item.id`) is the correct key.

  - id: react-recon-diff-02
    answer: |
      React reconciles by walking the element tree top-down and comparing each element at the same position in the old and new trees. Two rules drive reuse vs. remount: (1) element type — if the type at a given position changes (e.g. `<div>` becomes `<span>`, or one component type becomes a different component type), React unmounts the entire old subtree and mounts a fresh one; (2) if the type is the same, React reuses the existing instance and only patches the changed props. Within a list, the `key` prop extends this same-type matching: React matches elements by key, so a keyed item that moves position is moved, not remounted, while an item whose type changed at the same position is always replaced regardless of key.

  - id: react-recon-remount-03
    answer: |
      The idiomatic way is to set the `key` prop on the component tied to the selected user, e.g. `<UserForm key={selectedUserId} />`. When `selectedUserId` changes, React sees a different key at the same position, treats it as a different element, unmounts the old instance (destroying all its state), and mounts a fresh instance with initial state. This is preferred over resetting state imperatively inside the component (e.g. via `useEffect` watching the id) because it is declarative, runs before the first render of the new user, and avoids a flash of stale state or an extra render cycle.

  - id: react-hooks-rules-01
    answer: |
      The Rules of Hooks are: (1) Only call hooks at the top level of a React function component or custom hook — never inside loops, conditions, or nested functions. (2) Only call hooks from React function components or from other custom hooks, never from ordinary JavaScript functions.
      Calling a hook conditionally breaks React because React tracks hook calls by their order of invocation, relying on a fixed sequence across renders. If a hook is called inside a conditional, the number and order of hook calls changes between renders, so React's internal hook list becomes misaligned — the state or effect that belongs to one hook gets associated with a different hook on the next render, causing crashes or silently corrupted state.

  - id: react-hooks-updater-02
    answer: |
      `setCount(count + 1)` reads `count` from the closure of the render in which the callback was created. If multiple `setCount(count + 1)` calls are batched in the same event handler, they all close over the same stale `count` value, so each call computes the same result (e.g. 0 + 1 = 1), and only the last write wins — you end up incrementing by 1 instead of by 3. The same staleness problem occurs in async callbacks (timeouts, promises) that captured an old `count`.
      The functional updater form `setCount(prev => prev + 1)` fixes this by having React pass the most recent committed state into the updater. React queues the updaters and runs them in sequence against the latest state, so each call computes from the previous result, guaranteeing correct increments regardless of batching or staleness.

  - id: react-hooks-memo-03
    answer: |
      `useMemo` memoizes a computed value — it runs a factory function and returns its cached result, recomputing only when its dependencies change. `useCallback` memoizes a function itself — it returns the same function reference across renders unless dependencies change, which is equivalent to `useMemo(() => fn, deps)`. Both exist to give stable references to values/functions that are passed as props to children or used in dependency arrays of other hooks.
      Memoizing actually helps only when the cost of recomputation is meaningful AND the stable reference is being leveraged — e.g. an expensive derived value passed to a `React.memo`'d child, or a function used in a `useEffect` dependency that would otherwise retrigger the effect. If the computation is trivial, or the consuming component is not memoized (so it re-renders anyway), the memoization itself adds overhead without benefit.

  - id: react-hooks-reducer-04
    answer: |
      Reach for `useReducer` instead of `useState` when: (1) the next state depends on the previous state in complex ways or multiple sub-values need to update together atomically (e.g. a form with interdependent fields); (2) the update logic is nontrivial and involves multiple branches — a reducer centralizes that logic in one testable place rather than scattering it across many `setState` calls; (3) you have multiple related state variables that change together (e.g. `{ status, data, error }` for async operations); (4) the state shape is a complex object or union type where invalid transitions should be impossible — the reducer makes illegal transitions unrepresentable. For simple, independent values, `useState` is usually clearer.

  - id: react-hooks-use-05
    answer: |
      `use` is a React 19 API that lets you read a resource (a Promise or a Context) conditionally during render. Unlike other hooks, `use` can be called inside loops and conditionals, and it is the only hook that can suspend a component: when given a not-yet-resolved Promise, it throws the Promise to the nearest `<Suspense>` boundary, which shows the fallback until the data resolves, then re-renders the component. It can also read Context, and unlike `useContext` it works conditionally.
      Key differences from other hooks: it is not limited to top-level calls; it integrates with Suspense and error boundaries; it reads promises directly without needing `useEffect` + `useState`; and it works in both Server and Client Components. It is not a general-purpose hook for arbitrary logic — it is specifically for reading a resource.

  - id: react-effects-deps-01
    answer: |
      A `useEffect` dependency array should list every reactive value the effect uses that can change between renders: props, state, context values, and derived values computed from them. Omitting an internal function that uses only stable helpers is fine, but omitting any value that changes and that the effect reads is a lie about the dependency graph.
      If you omit a used value, the effect captures a stale closure — it keeps referencing the value from the render when the effect was created/updated, so it never sees the new value. This causes bugs like effects that don't re-run when they should (a subscription to the wrong id, a fetch that never fires for updated params, a timer that clears the wrong interval). The exhaustive-deps lint rule exists to catch this.

  - id: react-effects-cleanup-02
    answer: |
      The cleanup function runs in two cases: (1) before the effect re-runs due to changed dependencies, and (2) when the component unmounts. So for an effect with deps `[id]`, cleanup runs every time `id` changes (before the new effect runs) and once on unmount.
      Cleanup is needed for subscriptions, timers, event listeners, and other side effects that hold external resources or references. Without it, when the effect re-runs or the component unmounts, the old subscription/timer stays active — you accumulate duplicate listeners, leak memory, fire callbacks on unmounted components, or keep sending network requests. Cleanup ensures exactly one active subscription/timer per effect instance: the old one is torn down before the new one is created.

  - id: react-effects-misuse-03
    answer: |
      Two common misuses: (1) Using `useEffect` to derive state from props — e.g. `useEffect(() => setFiltered(items.filter(...)), [items])` — when you can compute the derived value directly during render. The fix is to compute it inline (or with `useMemo` if expensive) and skip the state + effect entirely. (2) Using `useEffect` to react to a prop change and "sync" it into local state — e.g. `useEffect(() => setName(props.name), [props.name])` — when the component could just render from the prop directly. The fix is to remove the local state and use the prop, or, if you truly need a reset-on-change, use the `key` prop to remount. Other examples include using effects for event handling that should be done in the handler itself, and using effects to fetch data when the data can be fetched by the parent and passed down as props.

  - id: react-effects-strictmode-04
    answer: |
      In development only, React StrictMode intentionally runs your effect setup, cleanup, and setup again on mount (setup → cleanup → setup), and similarly re-runs cleanup+setup for every dependency change. This is a deliberate double-invocation to surface bugs.
      It tells you to fix effects that are not idempotent and do not have proper cleanup. If the double-invoke causes a problem — you see a subscription created twice, a timer leaking, a network request duplicated — it means your effect's setup is not safe to run multiple times or your cleanup does not fully undo the setup. The fix is to make every effect symmetric: whatever setup does, cleanup must undo. This ensures correctness not just under StrictMode but also in production when effects genuinely re-run due to dependency changes or remounts.

  - id: react-rsc-boundary-01
    answer: |
      A Server Component runs only on the server. It can read databases, files, and secrets directly, and its output is sent to the client as a serialized render tree. It cannot use client-only features like `useState`, `useEffect`, event handlers, or browser APIs. A Client Component runs in the browser and has full interactivity. The boundary is not about where the code is authored but where it executes and what capabilities it has.
      `"use client"` marks the module boundary: it tells the bundler that this module and everything it imports are Client Components that should be included in the client bundle and hydrated. It does not mean "this file runs only on the client" — Client Components are still server-rendered for the initial HTML. The directive is a one-way door: a Server Component can import a Client Component, but a Client Component cannot import a Server Component directly (it can only receive one as a prop/children, which is how the server tree embeds client islands).

  - id: react-rsc-props-02
    answer: |
      Props that can cross the client boundary must be serializable: plain objects, arrays, strings, numbers, booleans, `null`, `undefined`, `Date`, `Map`, `Set`, and React elements (including Server Component elements passed as `children`). These can be serialized and sent to the client.
      What cannot cross: functions (event handlers, callbacks), class instances, symbols, and non-serializable object graphs (objects with methods, circular references, or prototype chains). You cannot pass a callback from a Server Component to a Client Component. If you need the client to trigger server-side behavior, you use Server Actions (`action` props on forms) or route through a server endpoint. React elements are the exception that makes composition possible — a Server Component can pass a `<ClientComponent />` element as `children` to a Client Component, and the server will render that element's placeholder into the client tree.

  - id: react-rsc-data-03
    answer: |
      In a Server Component, you fetch data directly with `await` in the component body — no hook, no effect, no state. The component is async, runs on the server, awaits the data (database query, API call), and renders the result. Because it runs on the server, there is no network waterfalls, no loading skeleton for the data itself, and the data never becomes a client-side state management problem.
      This differs from the classic `useEffect` fetch in several ways: (1) it runs on the server, not the browser, so secrets stay server-side and the client bundle is smaller; (2) it is sequential and awaited during render, so the component does not render until data is ready — no `loading` state needed; (3) it integrates with Suspense — if you wrap the async component in `<Suspense>`, the fallback shows while the server resolves the promise; (4) there is no stale-closure or dependency-array bug surface, and no cleanup is needed.

  - id: react-context-rerender-01
    answer: |
      When a context provider's value is an inline object literal like `value={{ user, setUser }}`, a brand-new object reference is created on every render of the provider. Since context consumers re-render whenever the context value's reference changes, every consumer re-renders on every provider render — even if `user` and `setUser` are unchanged. This defeats any `React.memo` on intermediate components and causes unnecessary re-renders of the entire consumer subtree.
      Fix it by memoizing the value with `useMemo` (and memoizing `setUser` if it's not already stable, e.g. from `useState` or `useCallback`): `const value = useMemo(() => ({ user, setUser }), [user, setUser])`. This keeps the reference stable when the underlying data hasn't changed, so consumers only re-render when `user` actually changes.

  - id: react-context-usage-02
    answer: |
      Context is the right tool for truly global, low-frequency-changing data that needs to be accessed by many components at different depths without prop drilling: theme, locale, authenticated user, router instance, or a dependency-injection container. It is also appropriate when you want to pass a stable API surface (dispatch functions, service objects) to a deeply nested tree.
      Context is the wrong tool when the data changes frequently (every keystroke, every animation frame) because all consumers re-render on every change — for high-frequency updates, a state management library with selective subscriptions (Zustand, Jotai, Redux) is more appropriate. It is also wrong when only one or two levels of nesting need the data — prop drilling is simpler and more explicit. Context is not a state manager; it is a dependency injection mechanism that happens to trigger re-renders.

  - id: react-perf-memo-01
    answer: |
      `React.memo` is a higher-order component that shallowly compares a component's old and new props. If they are equal, React skips re-rendering that component and reuses the previous output. It is a render-time optimization that prevents a parent's re-render from cascading into a child whose inputs haven't changed.
      It sometimes fails to prevent re-renders because: (1) props include a new object/array/function reference every render (inline `style={{}}`, `onClick={() => ...}`), so shallow comparison always sees a difference — fix with `useMemo`/`useCallback` or hoisting static values; (2) the component uses context that changed — `React.memo` does not block context-driven re-renders; (3) the component uses internal state that changed, which `React.memo` cannot and should not block; (4) props are primitive but actually changed, which is a real change and should re-render. `React.memo` is not a guarantee; it is a bailout that only works when props are stable and truly unchanged.

  - id: react-perf-list-02
    answer: |
      Before reaching for memoization, the higher-impact fix is list virtualization (windowing) — rendering only the rows currently visible in the viewport (plus a small overscan buffer) instead of all 10,000. Libraries like `react-window`, `react-virtualized`, or `@tanstack/react-virtual` measure the scroll container and mount only ~20–50 rows at a time, recycling DOM nodes as you scroll.
      This is higher-impact than memoization because the bottleneck is DOM node count and layout/paint cost, not React's reconciliation. 10,000 rows mean 10,000 DOM nodes with event listeners, layout boxes, and paint operations — the browser becomes janky regardless of how efficiently React reconciles. Virtualization reduces the actual DOM to a tiny constant, making the list smooth. Memoizing row components helps but does not address the fundamental problem of too many DOM nodes; virtualization does.

  - id: react-refs-useref-01
    answer: |
      The two main uses of `useRef` are: (1) holding a mutable reference to a DOM node (e.g. `inputRef.current.focus()`), giving you direct access to the underlying element; (2) holding a mutable value that persists across renders without causing re-renders — e.g. a previous value for comparison, a timer ID, a subscription handle, or any mutable box that should not trigger an update when changed.
      Mutating `ref.current` does not re-render because a ref is not state — React does not track changes to it. The ref object returned by `useRef` is a stable container (same reference across renders), and its `.current` property is a plain mutable field. React has no way to know when `.current` changes, so it never schedules a re-render. This is exactly why refs are the escape hatch for values that need to persist but should not drive the UI.

  - id: react-refs-forward-02
    answer: |
      To let a parent attach a ref to a child's DOM node, you use `forwardRef` to wrap the child component so it receives `ref` as a second argument and attaches it to the appropriate DOM element: `const Child = React.forwardRef((props, ref) => <input ref={ref} />)`. The parent then does `<Child ref={myRef} />` and `myRef.current` points to the DOM node.
      In React 19, `forwardRef` is no longer necessary — `ref` is passed as a regular prop to function components. You simply accept `ref` as a prop and attach it: `const Child = ({ ref }) => <input ref={ref} />`. This works because function components now receive `ref` in their props object. `forwardRef` still works in React 19 (it is not removed), but it is no longer the idiomatic way; the docs recommend the plain-prop approach for new code.

  - id: react-suspense-01
    answer: |
      `<Suspense fallback={...}>` declares a boundary: while any descendant is not ready to render (suspended), React shows the `fallback` UI instead of that subtree. Once the descendant resolves (its promise resolves), React re-renders and swaps the fallback for the real content. Suspense enables declarative loading states and allows you to coordinate multiple async children behind a single fallback.
      A component "suspends" when it throws a promise during render. This happens when: (1) it reads a not-yet-resolved promise via the `use` hook; (2) it reads data from a library that throws promises (e.g. `react-fetch`, `Next.js` `cache`); (3) it is a lazy-loaded component (`React.lazy`) whose bundle is still loading. React catches the thrown promise, finds the nearest `<Suspense>` boundary, shows its fallback, subscribes to the promise, and retries rendering when it resolves.

  - id: react-suspense-transition-02
    answer: |
      `useTransition` (and `startTransition`) solve the problem of blocking urgent updates behind non-urgent ones. When you update state (e.g. typing in a search box that filters a large list), that update is rendered synchronously — React commits it before the browser can paint, so the UI freezes until the heavy render completes. This makes the app feel janky.
      `startTransition` / `useTransition` mark an update as non-urgent (a transition). React schedules it at lower priority, allowing the browser to paint urgent updates (like the keystroke appearing in the input) first. The transition renders in the background; if a new urgent update arrives, React abandons the stale transition and starts over. This keeps the UI responsive during heavy re-renders. `useTransition` additionally returns an `isPending` flag so you can show a loading indicator while the transition is in flight.

  - id: react-state-batching-01
    answer: |
      Automatic batching in React 18 means that multiple state updates inside any event handler, timeout, promise, or native event are grouped into a single re-render instead of causing one render per update. Previously (React 17), only updates inside React event handlers were batched; updates in `setTimeout`, promises, or native event handlers were not. React 18 batches everywhere, reducing renders and improving performance without any code changes.
      To force a synchronous update when you truly need it (e.g. you need to read the DOM after the update commits, or you need to flush before a third-party library manipulates the DOM), use `flushSync` from `react-dom`: `flushSync(() => { setCount(c => c + 1) })`. This forces React to process the update and synchronously re-render before `flushSync` returns. Note that `flushSync` cannot be called during render or inside another `flushSync`.

  - id: react-state-lifting-02
    answer: |
      The standard pattern is lifting state up: move the shared state to the closest common ancestor of the two sibling components. The ancestor holds the state via `useState` (or a reducer/context if more complex) and passes the current state down to each sibling as a prop, along with a setter (or a callback) to update it. Each sibling receives its value from props and calls the shared setter to request changes.
      This creates a single source of truth. When one sibling calls the setter, the parent re-renders, both siblings receive the new value as props, and they stay in sync. If the siblings are far apart in the tree, lifting all the way to the root is impractical — in that case, move the state to a Context provider or a state management library, but the principle is the same: one shared source, distributed down.

  - id: react-state-derived-03
    answer: |
      Keeping a separate `useState` copy of a prop to render is an anti-pattern because it creates two sources of truth that can diverge. The local state is initialized from the prop but never automatically updates when the prop changes — you need a `useEffect` to sync them, which causes an extra render, introduces a window where the UI shows stale data, and adds a class of bugs (the sync effect has dependency issues, can race, and can be forgotten). It is redundant state that adds complexity without benefit.
      The fix is to render directly from the prop — just use `items` in the render. If you need to transform it (filter, sort, map), compute the transformed value inline or with `useMemo` keyed on `items`, but do not store it in state. If you need to modify the data, derive the modified version from the prop during render, or lift the state up and make the prop the controlled value. The principle is: props are already state owned by the parent — do not copy them into child state.
