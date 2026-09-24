- id: react-recon-keys-01
  answer: |
    A key identifies an item across renders so React can distinguish which element corresponds to which previous element. When element type and key both match, React reuses the existing component instance and preserves its state. An array index is usually a bad key because inserting, removing, or reordering items changes the index assigned to existing items, making React reuse the wrong instances and potentially carrying state or DOM state to a different item. Use a stable, unique identifier such as a database ID.

- id: react-recon-diff-02
  answer: |
    React primarily compares an element's type and key at each position in the reconciled tree. If the type and key match the previous element, React updates and reuses that fiber. If either changes in a way that requires a different identity, React unmounts the old component and mounts a new one. A component's position also matters, but keys allow React to preserve identity across moves within a list.

- id: react-recon-remount-03
  answer: |
    Give the form's position a key derived from the selected user, usually by placing it on a small wrapper component:

    `<UserForm key={selectedUserId} user={selectedUser} />`

    When the key changes, React treats the form as a new instance, remounts it, and discards its local state. If the parent directly renders a form, the form element can receive the key instead. For inputs that are controlled by props, the parent should also clear or replace their values as appropriate.

- id: react-hooks-rules-01
  answer: |
    The Rules of Hooks are:

    - Call hooks only at the top level of a React function component or a custom hook.
    - Do not call hooks inside loops, conditional branches, `try`/`catch` blocks, or ordinary nested functions.
    - Call hooks only from React functions, not from ordinary utility functions or event handlers.
    - Every render must call the same hooks in the same order.

    React tracks hooks through the component's current fiber. Each hook call advances an internal position in the hook list. Conditionally skipping or adding a hook changes that list between renders, so later hooks can read the wrong hook state, lose state, or trigger a “Rendered fewer hooks than expected” error. Custom hooks do not relax these rules; their callers must obey them too.

- id: react-hooks-updater-02
  answer: |
    `count` is a value captured by the current render's closure. If several updates are queued before React renders again, each call such as `setCount(count + 1)` computes from that same captured value, so the last queued update can overwrite the others.

    `setCount(current => current + 1)` passes a function to React. React applies each updater to the latest queued state in order, so multiple updates compose correctly. The functional form is important for updates based on previous state, and its function should remain pure.

- id: react-hooks-memo-03
  answer: |
    `useMemo` caches the result of a calculation, while `useCallback` caches a function's identity. Both recompute when their dependency arrays change.

    Memoization helps when computation is genuinely expensive or when a stable object or function identity affects something else, such as a memoized child, a dependency array, a memoized value passed deeper, or a custom hook that uses identity as a cache key.

    It does not prevent a component itself from rendering and usually does not reduce work for the component containing the hook. It can also add complexity, stale-value hazards from incorrect dependencies, and allocation overhead. Memoize only measured or structurally important work, not simply every callback or derived value.

- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when state updates are complex events or actions rather than isolated field assignments. It is useful when multiple values must change together, when the next state depends on prior state, when transitions and invariants are easier to express as actions, or when a parent wants a clearer command-based interface.

    The reducer receives the current state and action, then returns the next state. Keeping the reducer pure makes transitions easier to test and reason about. `dispatch` is also stable, though components consuming the reducer state still re-render when that state changes.

- id: react-hooks-use-05
  answer: |
    React 19's `use` API lets a component read a resource during rendering, including an existing Promise or context:

    `const value = use(context);`
    `const data = use(dataPromise);`

    It is not a conventional state-setting hook. It suspends rendering until a Promise resolves and can work in both Server and Client Components. Unlike hooks such as `useState` or `useEffect`, `use` may be called conditionally, though it should not be placed in a loop. It is normally used in a Server Component or another render-time boundary, not event handlers or effects. The Promise passed to it should have a stable, cached identity to avoid creating a new suspension on every render.

- id: react-effects-deps-01
  answer: |
    The dependency array should contain every reactive value from the component that the effect reads and that should cause the effect to run again, including props, state, context values, and functions derived from those values. In practice, the React ESLint exhaustive-deps rule helps identify them. Values imported from modules and values guaranteed stable, such as a `setState` function or a ref object, do not need to be listed.

    Omitting a used value lets the effect close over an old value while retaining an older version of that function or value. When the omitted value changes but the effect does not rerun, it may read stale props or state, produce incorrect behavior, or create duplicate listeners. Extra dependencies can also be harmful because they cause unnecessary effect executions, so the array should describe actual reactive dependencies rather than being used to suppress a warning.

- id: react-effects-cleanup-02
  answer: |
    A cleanup function runs immediately before an effect is run again because its dependencies changed, and when the component unmounts. It does not run for every ordinary render when the dependencies remain equal.

    Cleanup is needed to undo subscriptions, event listeners, timers, observers, network work, and other external resources. Without it, a rerun can leave multiple subscriptions active, an interval can continue after unmounting, or stale work can update a component that is no longer needed. A typical cleanup removes a listener, clears a timeout, aborts a request, or disconnects an observer.

- id: react-effects-misuse-03
  answer: |
    Two common cases are:

    1. **Fetching data with `useEffect`.** This adds a client-side request after rendering, creates loading and error-state complexity, and can produce a request waterfall. Prefer a framework loader, a Server Component, or a Suspense-aware data source; reserve effects for synchronization with external systems.

    2. **Deriving UI from props or state in an `useEffect`.** If a value can be calculated during render, calculating it in an effect causes an unnecessary extra render and risks temporary inconsistent state. Calculate the value during render, using `useMemo` only if the calculation is expensive enough to justify it.

- id: react-effects-strictmode-04
  answer: |
    StrictMode intentionally performs an extra development-only setup, cleanup, and setup cycle for effects on mount, and also re-renders components an extra time. This simulates remounting that can happen in real applications and exposes effects that are not safe to repeat.

    It reveals missing cleanup, subscriptions or timers that are registered repeatedly, impure rendering, accidental side effects during render, and non-idempotent setup logic. An effect should be able to clean up after itself and set up the same resource again without duplicating work. Production does not run this development double-invocation behavior.

- id: react-rsc-boundary-01
  answer: |
    A Server Component executes on the server and can be asynchronous, but it cannot directly use client-only state or effects, access browser APIs, or attach DOM event handlers. A Client Component runs in the browser and can use state, effects, refs, event handlers, and browser APIs; it can also be server-rendered to produce its initial HTML.

    `"use client"` marks a module as the entry to the client-side module graph. From that module onward, imported modules are treated as client components. It does not mean the whole application is client-only or that the component is never server-rendered. It establishes a serialization boundary between server code and client code.

- id: react-rsc-props-02
  answer: |
    A Server Component can pass values supported by React's server-to-client serialization, including primitives, plain objects and arrays, other serializable values such as `Date`, `Map`, `Set`, typed arrays, and `ArrayBuffer`, Promises, JSX elements, and Server Actions. JSX passed as `children` is a common way to compose server-rendered UI around a client component.

    It cannot pass arbitrary functions, class instances, or closures as ordinary data. A function can cross only when it is a Server Action created on the server and passed according to React's action mechanism. Non-serializable runtime objects and client-side functions cannot be serialized into the RSC payload.

- id: react-rsc-data-03
  answer: |
    A Server Component can be declared `async` and use `await` directly while rendering:

    `async function Page() {`
    `  const data = await getData();`
    `  return <View data={data} />;`
    `}`

    The request and data access stay on the server, where database and internal services can be used without shipping secrets to the browser. The component can colocate data access with the UI that needs it, and a Suspense boundary can provide a loading state. Caching, request deduplication, and freshness policies depend on the framework or data layer.

    A classic client `useEffect` fetch runs after the component commits in the browser. It needs manual loading, error, cancellation, dependency, and race-condition handling, and it cannot provide the data during the initial server render. It is better reserved for genuine synchronization with an external client-side system rather than ordinary data loading.

- id: react-context-rerender-01
  answer: |
    An inline object is a new value on every render. Context consumers compare the provider value with `Object.is`, so they see a change even when `user` is unchanged and may re-render unnecessarily.

    Memoize the value:

    `const value = useMemo(() => ({ user, setUser }), [user]);`

    `setUser` is normally stable, so it does not need to be a dependency. If the state and dispatch need independent update frequencies, split them into separate contexts. Memoization can reduce unnecessary work, but it does not change Context's fundamental behavior: consumers subscribed to a changed context value can still re-render.

- id: react-context-usage-02
  answer: |
    Context is appropriate for data that many descendants need regardless of their position in the tree, such as authentication status, theme, locale, a router abstraction, or a dependency that should be provided to a subtree. It avoids passing the same props through every intermediate component.

    It is a poor substitute for ordinary local state when only one component needs the value, or for frequently changing, high-volume data that would cause broad consumer re-renders. It is also not automatically a state-management or data-fetching solution. Keep state near its owner when possible, and split contexts or use a more specialized store when Context would create excessive updates.

- id: react-perf-memo-01
  answer: |
    `React.memo(Component)` lets React skip re-rendering that component when it receives a new element with props that compare equal. The comparison uses `Object.is` for each prop, with special handling for React elements and some ref-related values; it is not a deep comparison.

    It can fail to prevent a render when a prop is a newly created object, array, arrow function, or other value that is not reference-equal between renders. `React.memo` also does not block updates caused by the component's own state or by a Context value it consumes, and it does not help if the parent passes genuinely changing data or recreates expensive child props. First stabilize unnecessary identities and keep prop data pure; add a custom comparison only when shallow comparison is insufficient and its correctness is understood.

- id: react-perf-list-02
  answer: |
    Use list virtualization, or windowing. Render only the rows near the visible viewport plus a small overscan, and represent the full scroll height with spacer elements or a size model. This avoids creating and updating 10,000 row components and DOM nodes, so the browser performs much less rendering, layout, and reconciliation work.

    Stable keys, lightweight row markup, and localized state remain important, but memoization cannot remove the cost of keeping 10,000 full rows mounted. Pagination or showing fewer items can also be appropriate when the product does not require a continuous virtualized list.

- id: react-refs-useref-01
  answer: |
    The two main uses are:

    - Accessing and manipulating a mutable value, commonly a DOM node for focus, selection, measurement, or scrolling, and sometimes an instance or the latest value of a callback or timer.
    - Storing mutable data that should not trigger rendering, such as a previous value, an interval handle, or a flag for an idempotent operation.

    `useRef` returns the same ref object for the component's lifetime. Updating `ref.current` changes a property on that mutable object rather than dispatching a state update, so React has no reason to schedule another render. If a value affects what is displayed, use state instead.

- id: react-refs-forward-02
  answer: |
    Traditionally, the parent passes `ref={myRef}` to a child, the child accepts it with `forwardRef`, and the child attaches the received ref to its DOM node or uses `useImperativeHandle` to expose a custom handle:

    `const Input = forwardRef((props, ref) => (`
    `  <input ref={ref} {...props} />`
    `));`

    In React 19, function components can accept `ref` directly as a prop, so a separate `forwardRef` wrapper is no longer required for that use case. A ref can be forwarded to a host element; attaching it to a component instance exposes only what that component chooses to expose.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` provides a fallback UI for its descendants when one of them suspends during rendering. React hides or replaces the pending subtree with the nearest applicable fallback, then shows the completed subtree when the suspended work is ready. During a transition, already-revealed content can remain on screen instead of being replaced by a fallback.

    A component suspends when its render discovers that a required resource is not ready and throws a Promise, or when a lazy-loaded module or another Suspense-aware data source is pending. Suspense does not itself fetch data; it coordinates the rendering of work that reports itself as pending.

- id: react-suspense-transition-02
  answer: |
    A transition marks a state update as non-urgent so React can keep the current interaction responsive while preparing the next UI. For example, a filter or navigation update may suspend on new data without making an input feel blocked.

    `startTransition(() => setQuery(nextQuery))` asks React to schedule that update at a lower priority. React can work on it concurrently, yield to urgent work such as typing, and suspend without replacing already-revealed content with an undesirable loading fallback. `useTransition` provides `isPending` and `startTransition`; `useDeferredValue` is useful for deferring a value used in derived rendering. Not every update should be transitioned—direct user input and other urgent updates should remain immediate.

- id: react-state-batching-01
  answer: |
    Automatic batching in React 18 means multiple state updates made during the same event, promise callback, timer, or other asynchronous callback are grouped and cause one render rather than one render per update. React 18 extended batching beyond its previous event-handler behavior.

    If a truly synchronous update is required, wrap it in `flushSync` from `react-dom`:

    `flushSync(() => setValue(nextValue));`

    This forces React to process and commit the update synchronously. It should be reserved for exceptional cases where reading or observing the committed DOM immediately is essential, because forcing synchronous work can hurt responsiveness.

- id: react-state-lifting-02
  answer: |
    Move the shared state to the closest common parent component, have that parent own it, and pass the value plus setter or event handlers down to the siblings as props. The siblings communicate through the parent's state rather than modifying each other directly. If the siblings become deeply separated, place the owner higher in the tree or use a shared abstraction appropriate to the application's scale.

- id: react-state-derived-03
  answer: |
    `useState(props.items)` only uses `items` as the initial value; it does not automatically track later prop changes. A synchronization effect may temporarily leave the copy out of date, adds an extra render, and can lose user changes or other updates through competing sources of truth.

    If the component only displays prop data, render directly from `props.items` and calculate any derived values during render. If the user needs to edit it, make the parent the owner of that state and make the child controlled, passing the value and an update callback. Use a separate local state only when it represents genuinely independent state, not a copy of a prop.
