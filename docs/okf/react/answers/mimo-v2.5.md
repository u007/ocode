```yaml
- id: react-recon-keys-01
  answer: |
    React uses keys to identify which items in a list changed, were added, or were removed. Without stable keys, React compares items by index and can't efficiently reuse or reorder existing component instances. Using array index as a key is bad because when items are reordered, inserted, or deleted, the index shifts — React then reuses the wrong instance, causing state to appear in the wrong item, stale inputs, and broken animations or focus management.

- id: react-recon-diff-02
  answer: |
    React walks the old and new virtual DOM trees using a reconciliation algorithm. It compares elements by type and key at each position. If the element type (e.g. `<div>` vs `<span>`, or component A vs component B) or key has changed, React unmounts the old instance and mounts a new one. If only props changed, React updates the existing instance in place and triggers a re-render of that component.

- id: react-recon-remount-03
  answer: |
    Give the form component a `key` prop tied to the user identifier. When the user changes, React sees a new key, unmounts the old form instance, and mounts a fresh one — resetting all internal state automatically. Example: `<UserForm key={userId} userId={userId} />`.

- id: react-hooks-rules-01
  answer: |
    The Rules of Hooks: (1) Only call hooks at the top level of a function component or custom hook — never inside loops, conditions, or nested functions. (2) Only call hooks from React function components or custom hooks — not from regular JS functions. Calling hooks conditionally breaks React because hooks rely on call order to match state slots. If a hook is skipped on one render, React loses the mapping between hook index and its internal state, causing crashes or silent bugs.

- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` captures the value of `count` at render time. If you call it multiple times in the same event handler or batch, each call uses the same stale `count` value, so only one increment applies. The functional updater `setCount(prev => prev + 1)` receives the latest pending state, so multiple calls chain correctly, each building on the previous result.

- id: react-hooks-memo-03
  answer: |
    `useMemo` caches a computed **value** between renders — `const memoized = useMemo(() => expensiveCompute(a, b), [a, b])`. `useCallback` caches a **function** reference — `const fn = useCallback(() => doSomething(a), [a])`. They're equivalent to `useMemo(() => fn, [deps])`. Memoizing helps when: (1) the computation is genuinely expensive, or (2) you're passing the value to a memoized child and want to avoid unnecessary re-renders by keeping referential equality. If the child isn't memoized or the computation is cheap, the overhead of memoization itself can exceed the benefit.

- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when: (1) state logic is complex with multiple sub-values that transition together, (2) the next state depends on the current state in non-trivial ways, (3) you want to encapsulate state transitions and make them testable, or (4) you want to avoid the closure-staling issues that come with multiple `useState` calls reading from each other. It also works well with context to avoid passing setter functions. For simple independent values, `useState` is simpler and sufficient.

- id: react-hooks-use-05
  answer: |
    React 19 introduced `use`, which unwraps a Promise or reads a Context. Unlike other hooks, `use` can be called inside conditionals and loops. When used with a Promise, it suspends the component until the Promise resolves (works with Suspense boundaries). It does not create hook state — it's more like `await` for components. This means it can't be inside custom hooks that rely on consistent call order in the traditional sense; its flexibility comes from its non-stateful nature.

- id: react-effects-deps-01
  answer: |
    The dependency array should contain every external value from the component scope that the effect reads — props, state variables, and any function or object referenced inside the effect. Omitting a used value is a bug: the effect captures a stale closure over that value and won't re-run when it changes, leading to stale data, incorrect subscriptions, or missing side effects. Lying about deps (adding extra values that don't affect the effect) just causes unnecessary re-runs, which is less dangerous but still wastes resources.

- id: react-effects-cleanup-02
  answer: |
    The cleanup function runs before the effect re-fires (on every re-render where deps changed) and when the component unmounts. It's needed because effects like subscriptions, timers, or WebSocket connections set up persistent state outside React. Without cleanup, re-subscribing creates duplicates (multiple listeners, multiple intervals), and unmounting leaves dangling references that cause memory leaks or errors.

- id: react-effects-misuse-03
  answer: |
    1. Using useEffect to compute derived state from props — this causes unnecessary re-renders and is synchronous anyway. Instead, compute directly in the component body or use useMemo. 2. Using useEffect as a fetch-or-fetch-again-on-prop-change pipeline — this is complex and error-prone. Instead, use a data-fetching library like TanStack Query (React Query), SWR, or React 19's `use` with Suspense for declarative fetching. Other misuse: using useEffect to transform an event handler into a side effect when you should just read the latest value.

- id: react-effects-strictmode-04
  answer: |
    StrictMode double-invokes effects (and their cleanups) on mount in development to help you find missing cleanup functions. If your effect doesn't have a proper cleanup, the double-fire exposes bugs like leaked subscriptions or duplicated listeners. It tells you to make effects safe for unmounting and remounting — every effect setup should have a corresponding cleanup that undoes what it did. This catches a class of bugs that would otherwise only surface in concurrent features or when components remount in production.

- id: react-rsc-boundary-01
  answer: |
    Server Components run only on the server during rendering — they can access the database, filesystem, and backend directly, and their JavaScript never ships to the client. Client Components are marked with `"use client"` at the top of the file, which is a directive that tells the bundler where the client/server boundary is. Everything below the directive (imports, code) is part of the client bundle. Client Components run on the client in the browser and can use hooks, event handlers, and browser APIs. Server Components can render into the tree above Client Components but not the other way around.

- id: react-rsc-props-02
  answer: |
    A Server Component can pass: (1) serializable props — primitives, plain objects, arrays, dates, etc., and (2) React elements (JSX) as children or prop values. It **cannot** pass: functions (including event handlers), class instances, non-serializable objects (like DOM nodes, Maps, Sets with complex contents), or anything that relies on client-side state. In practice, if you need to pass a function to a child, that function must be defined in or below the Client Component boundary.

- id: react-rsc-data-03
  answer: |
    In a Server Component, you can fetch data directly — `await fetch(url)` works because the component runs on the server. There's no need for useEffect. The component can be async (React supports `async` server components) and the data is available at render time, before anything is sent to the client. This differs from the classic useEffect fetch: no loading state management, no race conditions from stale closures, no dependency arrays to get right, and no JavaScript shipped to the client for the fetch. Data is serialized into the rendered output.

- id: react-context-rerender-01
  answer: |
    Creating `{ user, setUser }` inline on every render creates a new object reference each time, causing all consumers of that context to re-render — even if `user` and `setUser` haven't actually changed. Fix: memoize the value with `useMemo` so the reference stays stable across renders, and memoize the setter with `useCallback` if it's created fresh. `const value = useMemo(() => ({ user, setUser }), [user, setUser])`. Also consider splitting into two contexts (data and dispatch) so consumers that only need one don't re-render when the other changes.

- id: react-context-usage-02
  answer: |
    Context is the right tool for truly global or deeply shared data that doesn't change frequently — auth state, theme, locale, feature flags. It avoids prop drilling. It's the wrong tool for: (1) high-frequency updates like form input state or animations — every consumer re-renders on each change, (2) data that changes often enough that context consumers are constantly re-rendering when they don't need to. For those cases, use local state lifted to the nearest common ancestor, or a state management library with selectors (Zustand, Redux with useSelector, Jotai). Context doesn't provide selective subscriptions — consumers always re-render when the value changes.

- id: react-perf-memo-01
  answer: |
    `React.memo` is a higher-order component that skips re-rendering a component if its props are shallowly equal to the previous props. It fails to prevent re-renders when: (1) props include new object or function references on every render (inline objects, arrow functions, arrays created in the parent), (2) the parent re-renders for unrelated reasons and passes non-primitive props that are structurally equal but referentially different. Without stable references on the memoized child's props, shallow comparison sees "new" objects and re-renders anyway.

- id: react-perf-list-02
  answer: |
    Virtualize the list — only render the rows visible in the viewport. Libraries like `react-window` or `react-virtuoso` handle this by rendering a small slice of the 10,000 rows and swapping them as the user scrolls. This is higher-impact than memoization because memoization only helps if individual rows are skipping re-renders; even with perfect memo, mounting 10,000 DOM nodes upfront is expensive for layout, paint, and memory. Virtualization reduces DOM node count from 10,000 to roughly 20-50 visible rows.

- id: react-refs-useref-01
  answer: |
    Two main uses of `useRef`: (1) Accessing an underlying DOM node — `<input ref={inputRef} />` gives you the actual DOM element for imperative operations like focus, scroll position, or measuring. (2) Holding a mutable value that persists across renders without triggering re-renders — like storing a timer ID, previous value, or a count that doesn't need to affect the UI. Mutating `ref.current` doesn't re-render because refs are plain JavaScript objects outside React's state system. React doesn't track or compare them — changes are invisible to the render cycle by design.

- id: react-refs-forward-02
  answer: |
    Traditionally, you use `React.forwardRef` to let a parent attach a ref that points to the child's DOM node: the child wraps its component in `forwardRef((props, ref) => <input ref={ref} />)`. In React 19, `forwardRef` is no longer necessary — ref can be passed as a regular prop directly to function components. You just declare `ref` in the component's props: `function MyInput({ ref }) { return <input ref={ref} />; }`. This simplifies the API by removing the special HOC wrapper.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` renders the fallback UI whenever any descendant is suspended. A component "suspends" when it throws a Promise — this happens when reading a resource (like a lazily loaded module, a data source, or a promise via `use`) that isn't ready yet. React walks up the tree looking for the nearest `<Suspense>` boundary and renders its fallback there. When the promise resolves, React retries rendering the suspended component with the resolved data. Without a Suspense boundary, a suspended component causes the entire app to throw.

- id: react-suspense-transition-02
  answer: |
    `useTransition` solves the problem of urgent vs non-urgent updates competing for the same state. For example, filtering a list with many results causes jank if every keystroke triggers an immediate re-render of thousands of items. `startTransition` marks the expensive update as non-urgent, so React can keep the input responsive (urgent) while deferring the list re-render (low priority). The update is scheduled at lower priority — React can interrupt it, pause it, or discard it in favor of more urgent work like user input. The state is still updated, just not all at once.

- id: react-state-batching-01
  answer: |
    Automatic batching in React 18 means React groups multiple state updates within the same event handler, timeout, promise, or native event into a single re-render — regardless of whether they're inside `setTimeout`, `Promise.then`, or native event handlers. Previously (React 17 and earlier), only updates inside React event handlers were batched; `setTimeout` and promise updates triggered synchronous re-renders. To force a synchronous re-render when you truly need one (e.g., to read DOM after state update), use `flushSync` from `react-dom`: `flushSync(() => setCount(c => c + 1))`.

- id: react-state-lifting-02
  answer: |
    Lift state up to their nearest common ancestor. Move the shared state into a parent component, pass it down to both siblings as props, and pass callbacks for updates. The parent becomes the single source of truth, and both siblings re-render when the shared state changes. For deeply nested components or many siblings, this can get verbose — in those cases, consider Context or a state management library as an alternative.

- id: react-state-derived-03
  answer: |
    Keeping a separate `useState` copy of a prop is an anti-pattern because it creates two sources of truth that can drift out of sync — when the prop updates, the state doesn't automatically follow, leading to stale renders or manual syncing bugs. The fix: don't copy props into state. Compute the derived value directly in the component body during render (like `const visibleItems = items.filter(...)`) so it's always fresh. If the derivation is expensive, use `useMemo(() => items.filter(...), [items])`. The rule is: "derived state should not be duplicated with state." If you genuinely need to initialize from a prop and then diverge (like a controlled input), use a `key` prop to reset the state.
```
