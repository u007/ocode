```yaml
- id: react-recon-keys-01
  answer: |
    Keys give each item in a list a stable identity across renders, so React's reconciler
    can match previous children to new ones: it keeps the same DOM nodes and component
    instances (preserving state, focus, local edits) and only moves/updates what actually
    changed. Without keys, React falls back to matching children by position.
    The array index is a bad key whenever the list can reorder, filter, or have items
    inserted/removed in the middle: after such a change the index maps to a *different*
    item, so React reuses the wrong instance — component state (input values, expanded
    rows, unchecked checkboxes) sticks to the wrong item and the DOM churns needlessly.
    Index keys are only acceptable when the list is static, never re-sorted/filtered, and
    items carry no per-item state or identity.
- id: react-recon-diff-02
  answer: |
    React reconciles the new element tree against the previous one, walking position by
    position. At each slot it compares the element's type and key: if the type (the same
    component function/class) and key match at the same position, React reuses the
    existing instance — it keeps state and effects and just re-renders with new props.
    If the type differs (e.g. div -> span, or a different component), or the key differs,
    React unmounts the old subtree (running effect cleanups) and mounts a brand-new one
    with fresh state. This is why defining a component inline inside another component
    breaks state: the type identity changes every render, and why a changing key forces
    a remount even when the type is unchanged.
- id: react-recon-remount-03
  answer: |
    Give the form a key that changes with the selected user:
    `<Form key={selectedUserId} userId={selectedUserId} />`. When the key changes, React
    treats it as a different element: it unmounts the old form (running cleanups) and
    mounts a new instance, so every `useState` inside starts fresh — no manual reset
    logic, no syncing props to state in an effect.
- id: react-hooks-rules-01
  answer: |
    (1) Only call hooks at the top level of your component or custom hook — never inside
    loops, conditions, nested functions, or after early returns. (2) Only call hooks from
    React function components or custom hooks (not plain JS functions or class components).
    React has no per-call identity for hooks: it associates hook state with the component
    by the *order* the hooks are called, storing them in a list. A conditional call changes
    how many hooks run on a given render, so every subsequent hook lines up with the wrong
    slot — React hands back state that belongs to a different hook (e.g. a useState value
    where an effect was expected), corrupting the component or crashing.
- id: react-hooks-updater-02
  answer: |
    `count` in the handler is a closure snapshot of the render the handler was created in;
    state updates are queued and the re-render is asynchronous/batched. So multiple calls
    like `setCount(count + 1)` all read the same stale snapshot and you end up with +1
    instead of +N. The functional updater, `setCount(c => c + 1)`, receives the latest
    *pending* state as its argument, so each queued update builds on the previous one and
    React applies the pure updaters in order. It also stays correct across batched and
    concurrent renders.
- id: react-hooks-memo-03
  answer: |
    `useMemo(fn, deps)` memoizes the *result* of a computation (a value);
    `useCallback(fn, deps)` memoizes the function itself — it's equivalent to
    `useMemo(() => fn, deps)`. Memoizing helps when (a) the computation is genuinely
    expensive, or (b) referential identity matters: passing stable callbacks/objects to a
    `React.memo` child, satisfying the dependency array of another hook or effect, or
    preventing re-subscription. If neither applies, memoizing just adds dependency
    comparisons, memory, and clutter for no benefit.
- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when: the next state depends on the previous in nontrivial ways;
    several pieces of state must change together as one unit; there are many distinct
    actions/transition rules (state-machine-like logic); you want the update logic
    centralized in one pure, testable function; or you want to pass a single stable
    `dispatch` down to many components instead of a pile of callbacks. For a few
    independent simple values, `useState` is simpler and fine.
- id: react-hooks-use-05
  answer: |
    `use(resource)` reads the value of a resource: `use(promise)` suspends the component
    until the promise resolves and returns its value (integrating with Suspense for
    pending and Error Boundaries for rejection), and `use(context)` reads a context like
    `useContext`. The key difference: it's not formally a hook — it doesn't participate in
    the call-order state list, so you can call it conditionally and inside loops. The
    promise should be created outside rendering (e.g. passed from a Server Component or
    cached in a client cache); creating a fresh promise during render without caching
    causes an infinite loop.
- id: react-effects-deps-01
  answer: |
    Every *reactive* value the effect reads: props, state, and anything derived from them
    in the component body — including functions defined in component scope (or move those
    into the effect itself). If you omit a used value ("lying"), the effect closes over a
    stale snapshot: it keeps reading old props/state, may skip runs it needed or act on
    outdated data, producing bugs that are hard to trace. If you don't *want* it to
    re-run, the honest fixes are restructuring (move the value inside the effect, use a
    ref for latest-value access, or move event-like logic to handlers) — not lying.
    eslint-plugin-react-hooks' exhaustive-deps rule enforces this.
- id: react-effects-cleanup-02
  answer: |
    The cleanup function runs before every re-run of the effect (when its dependencies
    change) and when the component unmounts. It's needed because effects are not
    one-shot: without cleanup, each re-run would create a *second* subscription/timer/
    listener while the old one keeps running, and after unmount dead timers and
    subscriptions keep firing (memory leaks, duplicated handlers, updates to unmounted
    components). Cleanup makes the setup/cleanup pair idempotent so the effect can run
    any number of times safely.
- id: react-effects-misuse-03
  answer: |
    (1) Deriving state from other state or props ("when firstName changes, also set
    fullName"): computing during render is cheaper and correct — just derive the value
    in the render body (useMemo only if the computation is expensive); an effect here
    causes an extra render pass and can loop. (2) Logic that responds to a specific user
    action (e.g. submitting the form, sending analytics on button click): that belongs in
    the event handler, not an effect watching state. Related smell: syncing state to a
    changing prop with an effect — the idiomatic fixes are a changing `key`, "fully
    controlled" props, or adjusting state during render.
- id: react-effects-strictmode-04
  answer: |
    Development-only behavior: StrictMode intentionally runs mount -> unmount -> mount
    (effect setup -> cleanup -> setup) to simulate an immediate remount, anticipating
    features like Fast Refresh and Offscreen/reusable state where effects really do
    re-run. It exposes effects that aren't re-runnable: missing cleanup, one-time-only
    assumptions, non-idempotent setup (double API calls you observe are the symptom).
    What to fix: give every setup a proper cleanup — cancel fetches/AbortControllers,
    unsubscribe, clear timers — so the effect can safely run, clean up, and run again.
    Production runs effects once.
- id: react-rsc-boundary-01
  answer: |
    Server Components execute only on the server (ahead of HTML delivery): they can be
    async, query the database/filesystem directly, ship zero JavaScript to the browser,
    and cannot use state, effects, event handlers, or browser APIs. Client Components run
    in the browser (they are still server-rendered once for initial HTML) and can use
    hooks, interactivity, and browser APIs. `"use client"` marks the *boundary*: it says
    "everything imported from this module onward is part of the client bundle" — it cuts
    the tree at that point; it does not just convert that single component. Modules
    without the directive are Server Components by default (in RSC frameworks like the
    Next.js App Router).
- id: react-rsc-props-02
  answer: |
    What crosses: serializable values — strings, numbers, booleans, null, plain objects
    and arrays — plus the extended RSC-serializable types like Date, Map, Set, TypedArrays,
    and Promises (a client component can receive a promise and unwrap it with `use()`).
    You can also pass Server Components as props (children/slots) into Client Components.
    What cannot cross: functions (except Server Actions marked with "use server"), class
    instances with methods, DOM nodes, symbols, and other non-serializable objects —
    so no event handlers or callbacks from server to client.
- id: react-rsc-data-03
  answer: |
    Make the Server Component `async` and await directly in the body:
    `const data = await db.query(...)` or `await fetch(...)`. There are no hooks, no
    loading state inside the component, and no client-server JSON round trip; the data is
    fetched before HTML is sent, and you can wrap subtrees in Suspense to stream. Compared
    to the classic useEffect fetch: no render-then-fetch waterfall (or at least the
    waterfall can be parallelized on the server), no loading flicker after hydration, no
    race conditions from unmounted components, and secrets stay on the server. The useEffect
    pattern fetches in the browser after mount, requires manual loading/error/race handling,
    and serializes data twice (server->client API, then through props).
- id: react-context-rerender-01
  answer: |
    Because `value` is created inline, it's a new object with a new identity on every
    provider render, so *every* consumer re-renders whenever the provider re-renders —
    even if `user` and `setUser` didn't meaningfully change. Fixes: memoize the value
    (`useMemo(() => ({ user, setUser }), [user])` — the setState function is already
    stable, so only `user` changes trigger consumers); or split into two contexts (a
    state context and an update/dispatch context) so read-only consumers don't re-render
    on updates; and keep unrelated state out of the provider component so it doesn't
    re-render for irrelevant reasons.
- id: react-context-usage-02
  answer: |
    Right tool: low-frequency values needed by many components at arbitrary depth —
    theming, locale, the current authenticated user, dependency injection (a client or
    logger instance), avoiding deep prop drilling of relatively stable data.
    Wrong tool: high-frequency changing state (keystrokes, hover/mouse position, animation
    frames, rapidly changing shared app state) — every context change re-renders every
    consumer; also passing data down only one or two levels (explicit props are clearer);
    and as a general replacement for a state store with selector-based subscriptions
    (Redux/Zustand etc.) for complex, frequently updated state.
- id: react-perf-memo-01
  answer: |
    `React.memo(Component)` skips re-rendering the component when its new props are
    shallowly equal to the previous props. It fails to prevent re-renders when: props
    include freshly created functions or objects/arrays each render (inline handlers,
    inline object literals) so the shallow compare always fails — the parent must also
    use useCallback/useMemo or pass stable references; the component reads a context that
    changed (context updates bypass memo); the component re-renders because of its *own*
    state changing; or children/slots passed as props have new identities.
- id: react-perf-list-02
  answer: |
    Virtualize the list (windowing — react-window, react-virtuoso, TanStack Virtual):
    render only the rows visible in the viewport plus a small overscan. This is
    higher-impact because it attacks both costs at once: React reconciliation drops from
    O(10,000) to O(visible rows), and — more importantly — the browser only lays out and
    paints a few dozen DOM nodes instead of 10,000, which memoization does nothing about.
    Memoizing rows still leaves 10,000 DOM nodes alive and slow. (Pagination or infinite
    scroll are the simpler variants of the same idea.)
- id: react-refs-useref-01
  answer: |
    (1) Holding a mutable value that persists across renders without triggering a
    re-render — timer/interval ids, previous values, "instance fields," the latest value
    for use inside stable callbacks. (2) Accessing and imperatively manipulating a DOM
    node — focus, scroll, measurement, text selection, integrating non-React libraries.
    Mutating `ref.current` doesn't re-render because React does not observe refs: renders
    are triggered only by state/context updates, and the ref object is a mutable side
    channel outside React's data flow. That's also why you shouldn't read or write refs
    during render (except lazy initialization) — the value isn't part of any render
    snapshot, so the render output wouldn't be consistent.
- id: react-refs-forward-02
  answer: |
    Classically: wrap the child in `forwardRef` —
    `const Input = forwardRef((props, ref) => <input ref={ref} {...props} />)` — and use
    `useImperativeHandle` inside if you want to expose a custom handle instead of the raw
    DOM node. React 19 change: `ref` is now a regular prop for function components, so
    `function Input({ ref, ...props }) { return <input ref={ref} {...props} /> }` works
    with no forwardRef wrapper, and forwardRef is deprecated ( slated for removal in a
    future version). React 19 also added ref cleanup functions returned from ref callbacks.
- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` declares a loading placeholder for its subtree: if a
    descendant suspends, React displays the fallback instead of that subtree (keeping any
    already-revealed outer content on screen), and when the pending work finishes it swaps
    in the real content and re-runs effects as needed. A component "suspends" by throwing
    a promise (thenable) during render — thrown by React.lazy (dynamic import), by
    Suspense-enabled data libraries (Relay, react-query/SWR suspense modes), or by
    `use(promise)` in React 19. React catches the thrown promise, waits for it to settle,
    then retries rendering the subtree.
- id: react-suspense-transition-02
  answer: |
    It solves the jank/flicker when a state update triggers expensive rendering or new
    async data: the urgent feedback (the keystroke, the button press) shouldn't be blocked
    by the expensive follow-up. `startTransition(() => setState(...))` (or the
    `[isPending, startTransition]` pair from `useTransition`) marks that update as
    non-urgent: React schedules it at lower priority, renders it in the background
    interruptibly so urgent updates can preempt it, and can keep the previous UI visible
    (surfacing `isPending`) until the transition and its Suspense content are ready —
    avoiding intermediate loading-fallback flicker.
- id: react-state-batching-01
  answer: |
    React 18's automatic batching groups multiple state updates into a single re-render
    no matter where they happen — React event handlers, promises, setTimeout/native event
    callbacks, fetch handlers — whereas React 17 only batched inside React-managed event
    handlers. This cuts re-render counts and avoids half-updated intermediate states.
    To force a synchronous, immediate commit you use `flushSync` from react-dom:
    `flushSync(() => setX(...))` — React flushes that update synchronously before moving
    on (used rarely: right before measuring the DOM, or for imperative third-party
    integrations). flushSync is a performance/UX escape hatch, not a default.
- id: react-state-lifting-02
  answer: |
    Lift state up to their closest common parent: the parent owns the shared state and
    passes it down to both siblings as props — the value to read and a setter/callback to
    change it. Both siblings become controlled by the same source of truth, so they stay
    in sync automatically (and the parent can also derive anything both need). If the
    tree between them is deep, use context or an external store instead of threading
    props through every level.
- id: react-state-derived-03
  answer: |
    It creates a second source of truth that silently desyncs: `useState(items)` only uses
    the prop at the *first* render — later changes to the `items` prop are ignored, so the
    component renders stale data (the usual "fix," an effect that copies props into state,
    adds an extra render and more staleness). The fix is to derive during render: compute
    the display value from `items` directly in the render body (useMemo only if the
    computation is expensive). If you genuinely need local adjustable state tied to a prop,
    choose one of the official escape hatches: fully controlled (parent passes value +
    onChange), fully uncontrolled with `key={itemsVersion}` to reset on change, or adjust
    state during render by comparing against the previous prop value.
```
