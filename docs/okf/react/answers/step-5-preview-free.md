# step-5-preview-free — closed-book answers for react (opencode-go, 2026-10-12); answerer saw only _prompts/react.md
- id: react-recon-keys-01
  answer: |
    React needs a `key` to identify which item in a list corresponds to which element on screen across
    renders. Reconciliation matches children by key first; without one, React falls back to positional
    (index) matching. The array index is a bad key because the index is not the item's identity — it's its
    position. When the list is reordered, an item inserted in the middle, or an item deleted, the indexes
    shift, so React pairs the old element at position N with a *different* item's data. The component
    instance (and all its local state, DOM node, focus, scroll position, uncontrolled input text) stays
    pinned to the position rather than following the data, producing visibly wrong state and wasted DOM
    mutations. Use a stable, unique id from the data itself.

- id: react-recon-diff-02
  answer: |
    React compares the newly returned element tree to the previous Fiber tree. It keeps the existing
    instance when the element at that position has the **same type** and the **same key**; it unmounts and
    remounts when either differs. In detail: at the root React forces a remount only if the host root
    changes; within children it does index/key matching against the old children, and for a matched pair
    it recurses, reusing the Fiber, component instance, hook state and DOM node, and only updating the
    changed props. If `type` changes (e.g. `<div>` → `<span>`, or `<CompA>` → `<CompB>`), or a `key`
    changes, the old subtree is deleted and a new one mounted. If types match and keys match, React bails
    out of re-rendering that subtree only when the element is `===` identical to the previous one
    (memo bailout) or the component is wrapped in `React.memo` and props are shallow-equal; otherwise it
    re-renders but keeps the instance. Note the distinction between "re-render" (same Fiber,
    state preserved) and "remount" (new Fiber, state thrown away) — keys decide the latter, not the former.

- id: react-recon-remount-03
  answer: |
    The idiomatic fix is to give the form a `key` that identifies the user, e.g.
    `<UserForm key={selectedUser.id} user={selectedUser} />`. When the selected user changes the key
    changes, so React throws away the old instance and mounts a fresh one — all internal `useState`
    state resets for free, with no effect, no `useEffect` reset dance, and no "extra render with the
    stale value". The alternative (and arguably more robust) approach is to not store derived state at
    all: make the form fully controlled / derive its values from props, and adjust state during render
    with a "previous value" comparison (React's documented "adjusting state when a prop changes"
    pattern) so a render with stale data is discarded before it commits.

- id: react-hooks-rules-01
  answer: |
    (1) Only call hooks at the **top level** of a React function component or custom hook — never inside
    `if`, loops, nested functions, `try/catch` blocks, or early-return branches. (2) Only call hooks
    **from React functions** — components or custom hooks (not plain JS utilities, class components,
    or event handlers).
    Hooks are not looked up by name. React stores hook state on the Fiber as an **ordered linked list**,
    and on each render it walks that list in call order, handing the Nth `useState`/`useEffect` call the
    Nth stored slot. There is no key identifying which hook is which — order *is* the key. A conditional
    hook therefore desynchronizes the call sequence from the stored slots: on renders where the branch
    is skipped, every subsequent hook silently receives the previous hook's state. In practice this
    yields "swapped" values, `undefined` state, effects that run with the wrong deps, and the classic
    error "Rendered fewer hooks than expected. This may be caused by an accidental early return
    statement."

- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` closes over the `count` value captured in the render that created that
    callback. If several updates run in the same tick before a re-render commits — two clicks in one
    batch, two `setCount(count + 1)` calls inside one event handler, or a stale callback fired from a
    timer — every one of them uses the same captured `count`, so `0 + 1` is dispatched twice and the
    result is 1, not 2. The functional form `setCount(prev => prev + 1)` passes a *queue* of updater
    functions to React instead of values; React runs them in order against the latest pending state, so
    the second updater sees the first one's result and you get 2. It's effectively "the reducer
    approach to setState", and it's the only correct form whenever the next state depends on the
    previous one or when the update happens more than once per rendered value. (React also
    bails out of re-rendering if the resulting value is `Object.is`-equal to the current one — but
    that check compares the final value, not the updater identity.)

- id: react-hooks-memo-03
  answer: |
    `useMemo(fn, deps)` caches the **returned value** of `fn` between renders as long as the deps are
    unchanged. `useCallback(fn, deps)` caches the **function itself** (its identity) and is essentially
    `useMemo(() => fn, deps)`. So: use `useMemo` when you care about the value (avoid recomputing
    something expensive, or preserve referential identity of a derived object/array); use
    `useCallback` when you want to hand a stable function identity to something that depends on it.
    Memoizing helps only in three situations: (a) the computation is genuinely expensive and deps rarely
    change; (b) the value's referential identity matters, i.e. it is a dependency of a `useEffect` /
    `useMemo` / `useCallback`, or it is passed to a child wrapped in `React.memo` or relying on
    reference equality; (c) it is passed as a prop to a memoized child that would otherwise remount
    or re-render. Otherwise it is a pure pessimization — you pay the deps array allocation, the closure,
    and the hook bookkeeping every render in exchange for nothing. Don't reach for it to "optimize" a
    component that renders cheaply.

- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when:
    - State is several related values that change together (a form object, a data-fetching state machine
      with `status/idle|loading|success|error` + data + error), where independent `useState`s drift out of
      sync or require multiple setters per action.
    - Transitions are complex or the next state depends on the previous one in non-trivial ways
      (nested objects, counters with floor/ceiling).
    - You want the state logic centralized, named, and testable: `reducer(state, action)` is a pure
      function you can unit-test without rendering anything, and dispatch becomes a stable, readable
      "event" API rather than a scatter of setter calls.
    - You want to pass `dispatch` down instead of many setters — it's stable and doesn't need deps.
    Otherwise, for a single independent primitive value, `useState` is simpler and correct. `useState`
    itself is implemented in terms of a reducer, so this is about ergonomics and scale, not capability.

- id: react-hooks-use-05
  answer: |
    `use` (React 19) reads the value of a resource — a **Promise** (or a Context) — during render. For a
    promise it unwraps the resolved value when available, and if the promise is still pending it
    **throws** it, which Suspense catches to show the nearest fallback. You call it inside an `async`
    component or as `const data = use(promise)` and integrate it with Suspense boundaries and
    transitions. It differs from other hooks in important ways: it is (1) callable **conditionally and
    inside loops/blocks** because it is resolved per-call rather than linked to hook call order;
    (2) callable in **Server Components**, and it's the client-side counterpart to reading a promise;
    (3) not really a "hook" in stateful sense — it doesn't create hook state slots, so it doesn't need
    the top-level rule; (4) tied to the render's suspense/transition semantics, meaning the "pending"
    path is a thrown promise, not a boolean `isLoading` flag you manage yourself. Reading a context
    with `use(context)` is equivalent to `useContext(context)` but conditional.

- id: react-effects-deps-01
  answer: |
    The dependency array must list **every reactive value from the component scope that the effect
    reads** and that can change between renders: props, state, context values, and any functions or
    objects derived from them. Also anything from the render scope used inside, defined inline
    (destructured functions, computed objects). It should not wrap values the effect never reads, nor
    refs (`ref.current` is not reactive — no need and the linter will complain), nor `setState` setters
    (guaranteed stable), nor module-level constants.
    If you omit a used value, the effect closes over the value from the render in which it was created,
    and it will not re-run when that value changes — a **stale closure**. Symptoms: showing stale user
    or id, a fetch for the previous search term, logging old counts, or a subscription that captured an
    old prop. The lint rule (`react-hooks/exhaustive-deps`) exists because most omissions are bugs, and
    the honest fix is almost always to include the dep (and wrap the function in `useCallback` if
    needed) rather than to disable the rule. The only legitimate omission is a value you deliberately
    want frozen for this effect's lifetime, and then it should be a ref or a constant with a comment.

- id: react-effects-cleanup-02
  answer: |
    Cleanup runs (a) **before the next execution of the same effect**, i.e. after any render where a
    dependency changed and before the new effect body runs, and (b) **when the component unmounts**.
    In StrictMode dev only it also runs once extra on the initial mount. It is needed because the
    effect's setup created a resource with a lifetime that is *not* managed by React's commit phase
    and that can outlive the component or be duplicated by a re-run. Examples: a `setInterval` that
    would otherwise keep firing and call `setState` on an unmounted component (leak, duplicate timers,
    double data streams); an event listener on `window`/`document` that keeps the handler alive and
    fires repeatedly after route changes; a WebSocket or RxJS subscription that must be unsubscribed;
    an `AbortController` for an in-flight fetch, so a late response can't overwrite newer state
    (race) and so the request is actually cancelled. The rule of thumb: everything the setup
    *registered with the outside world* must be removed in cleanup, and cleanup must be idempotent
    because React may invoke it more than once.

- id: react-effects-misuse-03
  answer: |
    1. **Synchronizing state with props or other state** — e.g. `useEffect(() => setFullName(first + "
    " + last), [first, last])`. This forces an extra commit pass, briefly renders the old value, and can
    cascade. Instead compute the value **during render** (derived state): it's just a variable, or
    memoize it. If it genuinely needs resetting on a prop change, use the documented
    "adjust state during render" pattern (compare to a stored previous prop) or change the `key`.
    2. **Fetching data in an effect when the data could be fetched in the renderer / server** — or when
    you need caching, deduplication, retries, and races handled. In a framework, fetch in a Server
    Component or route loader; on the client use a data library (React Query/SWR) so the effect doesn't
    boil down to `fetch` + `setState` + manual loading/error flags + stale-response races.
    Other common cases: transforming data already in props (compute it), handling a user event such as
    form submission (`onSubmit` handler, not an effect that watches a "submitted" flag), and initializing
   /resetting a third-party widget (use `useRef` + an init function or a `key`).

- id: react-effects-strictmode-04
  answer: |
    In React 18+ StrictMode, on the initial mount in development React mounts the component, runs
    effects, immediately runs cleanups, and runs the effects a second time. The purpose is to
    **stress-test your effects**: it simulates the fast refresh / real-world situation where a
    component mounts, unmounts, and remounts quickly, and it surfaces effects that are not
    self-cleaning, not idempotent, or that assume they run exactly once. It tells you to fix effects
    that: (a) create a duplicate subscription, timer, listener, or external widget instance because
    cleanup is missing or incomplete; (b) assume one-shot behaviour such as "first mount only"
    gating; (c) use effects for fetch without abort/race handling. The correct reaction is not to
    disable StrictMode but to write setup/cleanup pairs so that a mount–unmount–mount sequence is
    indistinguishable from a single mount. It's also a signal your effect is doing something React
    considers external to render (data fetching should use Suspense/RSC/a data library).

- id: react-rsc-boundary-01
  answer: |
    A **Server Component** executes only on the server. Its code is never shipped to the client, it can
    be `async` and read a database / filesystem / secrets directly, and it cannot use state, effects,
    refs, event handlers, or browser APIs. Its output is a serialized React element tree that streams
    to the client, where it is rendered into host elements.
    A **Client Component** is the classic React component. It is also rendered on the server for the
    initial HTML, but it ships as JavaScript, hydrates, and can be interactive.
    `"use client"` is not a directive that makes the *whole file* client-only in the sense of paying
    bundle cost for everything; it marks the **boundary between the server and client module graphs**:
    everything imported from that module into the client graph must be a client component, and any
    props passed to it must be serializable. Crucially, a Server Component can be `async` and can be
    rendered as `children` of a Client Component — you keep the server work on the server while the
    wrapper is a client component that owns the interactivity.

- id: react-rsc-props-02
  answer: |
    Serializable (React "transferable"/"React Flight") props can cross: primitives (string, number,
    boolean, null, undefined, bigint), arrays and plain objects of those, `Date`, `Map`, `Set`,
    `FormData`, typed arrays, `Promise` (which is then unwrapped/streamed), JSX/ReactNode as `children`
    (the client receives already-rendered server output), and **Server Actions** (functions with the
    `"use server"` directive, which become references the client can call). You can also pass other
    server components as children.
    What cannot cross: arbitrary client functions / event handlers (an inline `onClick` prop passed
    *into* the server component is not allowed — you pass a client component as children instead),
    class instances, symbols, functions that aren't server actions, `BigInt` is fine but arbitrary
    objects with methods, and non-serializable DOM or framework objects. Attempting it raises
    "Only plain objects can be passed to Client Components" style errors. The test is: can it be
    serialized by React's RSC protocol, which is JSON-plus-a-few-special-types?

- id: react-rsc-data-03
  answer: |
    Server Components are `async`, so you simply `await` the data directly in the component body:
    `async function Page() { const items = await db.query(...); return <List items={items} /> }`.
    There is no loading state to model, no `useEffect`, no client waterfall, no `fetch` on the client,
    no API round-trip from the browser. The data is fetched on the server at request time, the render
    is streamed to the client, and `<Suspense>` boundaries decide which parts of the shell stream later
    or show a fallback. You can parallelize with `Promise.all` and avoid the sequential "child awaits
    then its child awaits" waterfall by passing promises down.
    The classic `useEffect` fetch runs **after** mount on the client, so you pay: a network round trip
    from the browser (not the server), a waterfall if multiple components each fetch, and you must
    hand-roll loading/error/race/stale-response handling and usually a caching layer. With RSC, data is
    part of the render, and React's own Suspense/streaming handles the pending UI. The client-side
    counterpart for promises coming from the server is `use(promise)`, not `useEffect`.

- id: react-context-rerender-01
  answer: |
    `const value = { user, setUser }` allocates a **new object every render**, so the context value's
    identity changes each time. When the provider re-renders for any reason, React notifies every
    consumer of that context, and because consumers are re-rendered based on identity change, **all
    consumers re-render even though `user` didn't actually change** — even ones that only read
    `setUser` (which is guaranteed stable). In a large tree this turns one provider render into a large
    burst of work.
    Fixes: (1) memoize the value, `const value = useMemo(() => ({ user, setUser }), [user])` — or pass
    the stable primitives/setters directly; (2) **split the context** into a state context and a
    dispatch context so consumers of the setter never re-render when the state changes; (3) separate
    contexts by domain (theme vs auth vs locale); (4) where appropriate move to a store with selector
    subscriptions (useSyncExternalStore) so a component only re-renders when the slice it reads
    changes.

- id: react-context-usage-02
  answer: |
    Right tool: data that is genuinely **global or cross-cutting and changes infrequently** — theme,
    locale/i18n, current authenticated user/session, feature flags, UI mode, a router's current params.
    It removes prop drilling where the consumers are deep and the value is truly ambient.
    Wrong tool: as a general state-management replacement. Because every provider render re-renders
    every consumer (there are no selectors, no per-value subscriptions), it is a bad fit for
    **high-frequency, high-fan-out, or fine-grained updates** — mouse position, drag state, form field
    state, live counters, anything many consumers read different slices of, or a large consumer tree
    where most consumers don't need most updates. Also wrong when you're just avoiding drilling across
    two or three shallow levels (composition and passing `children` is usually better), or when the real
    need is derived/querying data (use a data library). Rule of thumb: if a value changes often or
    many components need *parts* of it, use a store with selectors; if it's low-frequency ambient
    config, Context is ideal. Also remember a Provider value must be stable to avoid the rebind above.

- id: react-perf-memo-01
  answer: |
    `React.memo(Component)` is a render-phase optimization: it wraps the component so that, before
    rendering, React does a **shallow `===` comparison of the new props against the previous props**, and
    if all are equal it **bails out and reuses the last rendered output** (skipping the render and any
    hooks reconciliation for that subtree).
    It "fails" (doesn't prevent re-renders) because: (1) any prop that is a fresh reference each render
    defeats the check — inline object/array/JSX props, inline `onClick` handlers, and `children` as new
    JSX are all new identities every parent render; (2) the component consumes **context**, which
    bypasses the memo bailout entirely — a context change re-renders all consumers regardless of
    `memo`; (3) the component's own `useState`/`useReducer`/`useContext` changed, so it re-renders
    locally, and `memo` only guards against *props* changes; (4) the parent didn't actually re-render
    via props — memo can't help when the update originates inside the subtree. Fix with stable
    references (`useMemo`/`useCallback`), splitting contexts, or moving state closer to where it's
    used. Also note: `memo` compares shallowly, so a prop that's a new object with identical contents
    still triggers a re-render (or needs a custom comparator).

- id: react-perf-list-02
  answer: |
    The higher-impact fix is **virtualization / windowing** — render only the rows that are (roughly)
    in the viewport, plus a small overscan, using something like `react-window` or `react-virtuoso`,
    with the container scrolling. With 10,000 rows you have 10,000 React elements *and* 10,000 DOM
    nodes; the dominant costs are creating and laying out all those DOM nodes and the browser's paint
    per scroll, not React's reconciliation of virtual elements. Virtualizing drops live DOM nodes to
    ~the visible window (say 20–50), which typically turns jank into smooth scrolling regardless of
    list size. Secondary, still-before-memoization fixes: reduce the per-row cost (simpler row
    component, fewer nested wrappers, avoid expensive layout styles, avoid inline function/object props
    on rows), and paginate/limit if the user doesn't truly need all 10k rows. Memoizing rows with
    `React.memo` can help the re-render cost, but if the DOM is the bottleneck it will not fix jank;
    it is a follow-up optimization, applied after windowing and only where the profiler shows a win.

- id: react-refs-useref-01
  answer: |
    The two uses are: (1) **accessing a DOM node** (focus, measure, scroll into view, play media,
    integrate a third-party widget) — `const ref = useRef(null); <input ref={ref} />` then
    `ref.current.focus()`. (2) **an instance variable / mutable box** that persists for the component's
    lifetime but must not trigger renders — timer or animation frame ids, previous-value tracking,
    an AbortController, a third-party instance, a "latest value" mirror of a prop or callback.
    Mutating `ref.current` doesn't re-render because a ref is a plain mutable object
    (`{ current: ... }`) held outside React's state system. React sets `ref.current` during commit and
    does not track reads or writes to it — there is no subscription and no scheduler involvement — so
    React has no signal that anything changed and no reason to schedule a render. (Consequence: never
    read or write `ref.current` during render logic that the UI depends on; put such values in state.
    Also, refs cannot be passed as render props without care since they are not serialized/stable in
    the same way.)

- id: react-refs-forward-02
  answer: |
    Historically, because function components receive no second argument, you had to wrap a function
    component in **`React.forwardRef`** so it could receive a `ref` and pass it to the inner DOM node:
    `const Input = forwardRef(function Input(props, ref) { return <input ref={ref} /> })`. A parent then
    did `<Input ref={inputRef} />` and got the `<input>` node.
    In **React 19, `ref` is passed as a regular prop** to function components, so `forwardRef` is
    deprecated/unnecessary: `function Input({ ref, ...props }) { return <input ref={ref} /> }` works,
    and a parent passing `ref` to `<Input ref={r} />` gets the underlying node. Additionally, React 19
    added **`ref` cleanup functions**: a `ref` callback may *return* a cleanup function that runs when
    the node unmounts or the ref changes, replacing the old pattern of a detached "null called on
    unmount" callback; ref callbacks no longer need to handle `null` specially.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={<Spinner />}>` declares a boundary: while any descendant "suspends", React
    hides that subtree's content and shows the `fallback` instead, then swaps in the real content once
    the pending work resolves. Multiple siblings each get their own boundary, and boundaries can nest,
    so one slow component doesn't blank the whole page. It also enables streaming SSR: the shell
    renders immediately and suspended chunks stream in later with the fallback replaced as they arrive.
    A component **suspends** by throwing a **thenable** (a promise) during render. Concretely this
    happens when it uses React.lazy (code-splitting), reads a promise with the React 19 `use()` API,
    or reads data through a Suspense-integrated source (a data framework / a Server Component awaiting
    data). React catches that promise, finds the nearest Suspense boundary, and if it's still pending
    shows the fallback; when it resolves, React re-renders. Note: *throwing a promise* is the mechanism —
    plain `await` in a normal client component, or a promise you create and don't integrate with a
    cache, doesn't suspend unless the library/framework is wired to do it; a race/stale-response
    control is needed with hand-rolled fetches.

- id: react-suspense-transition-02
  answer: |
    The problem is that not all updates are equally urgent. Typing in a search box is urgent and must
    feel instant; the resulting list re-render or a Suspense fallback triggered by a navigation is not.
    Without a transition, marking a route change to a lazily-loaded or suspending component forces React
    to immediately replace the visible content with a fallback — the page blanks/flashes for a second
    before the data arrives, which feels like a reload even though it isn't. And React 18+ concurrency
    means an expensive non-urgent update would block the urgent keystroke render.
    `startTransition` / `useTransition` marks an update as **non-urgent / interruptible**. React then:
    (a) keeps showing the current UI (the old list, not a fallback) while the new one prepares, and can
    abandon and restart it if a more important update arrives; (b) does not show a Suspense fallback for
    it by default — the previously revealed content stays until the new content is ready to reveal;
    (c) exposes `isPending` so you can show a subtle "in-flight" indicator on the *existing* content;
    and (d) throttles the non-urgent work behind the urgent updates so input stays responsive. It's a
    scheduling hint, not a perf trick — the same work is done, but it can't hijack the frame.

- id: react-state-batching-01
  answer: |
    Automatic batching is React 18's behaviour of **grouping multiple state updates that happen in the
    same event/tick into a single render pass**, no matter where they were scheduled from. Before React
    18, updates inside React event handlers were batched, but updates inside `setTimeout`, promises, or
    native event listeners (added with `addEventListener` rather than React's synthetic system) each
    triggered their own render. In React 18 (with `createRoot` / automatic batching) all of those are
    batched too: React collects them and performs **one re-render** (and one commit) for the whole
    batch, so the DOM is updated once instead of N times, which avoids intermediate inconsistent UI and
    wasted work. Note that batching still respects order — queued updaters run in sequence — and values
    read from state inside the same handler still come from the current render, which is exactly why
    functional updaters still matter for repeat updates in one batch.
    To force a **synchronous** update when you truly need the DOM updated before continuing (e.g. you
    need to measure the DOM, scroll, or you're integrating with imperative code), use
    **`flushSync`** from `react-dom`: `flushSync(() => setState(...))` forces React to commit that
    update to the DOM immediately. It defeats the batching/YIELD behavior and can hurt performance, so
    use it sparingly and only around the specific update that must be flushed.

- id: react-state-lifting-02
  answer: |
    **Lift the shared state up to the closest common ancestor** (here, the parent that renders both
    siblings), make it the single source of truth, and pass the value down as a prop to each sibling
    while passing a callback down so they can request changes — React's "data flows down, events flow
    up" / one-way data flow model. The parent owns `const [value, setValue] = useState(...)`, gives
    `<A value={value} onChange={setValue} />` and `<B value={value} onChange={setValue} />`, so any
    change from either sibling updates the shared state and both re-render consistently. The alternative
    when there is no meaningful common ancestor (or drilling becomes deep) is to put the state in a
    context provider or an external store, but lifting state is the standard first pattern; do not try
    to keep two sibling states in sync with effects (see derived-state question), and don't mutate
    between siblings directly.

- id: react-state-derived-03
  answer: |
    Keeping a `useState` copy of a prop creates a **second source of truth** for the same data. The copy
    is only initialized once (on mount) from `items`; when the parent later passes a new `items`
    array, the local copy is now stale, and the render shows outdated data. Developers then compound it
    with a `useEffect(() => setItems(props.items), [props.items])` to "sync" — which is both redundant
    (an effect just to recompute what render already knows) and harmful (it forces an extra render pass,
    briefly shows the stale copy, and can flicker or produce a render with mismatched data). The
    general rule: **if a value can be computed from existing props/state, don't store it** — it's
    derived state, and derived values aren't state.
    The fix: drop the second `useState` and **derive the value during render** — either compute it
    directly (`const visible = items.filter(...)`) or memoize it if the computation is expensive
    (`useMemo`). If you genuinely need an editable copy (e.g. an editable form draft seeded from props),
    then either fully control it and reset it deliberately — the idiomatic reset is to change the
    component's `key` so it remounts fresh — or use the "adjust state during render" pattern that
    compares against a stored previous prop and discards the stale render instead of committing it.
