# react knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: react-recon-keys-01
  answer: |
    React needs a `key` on list items so it can match elements between the previous and next render trees and tell which items were added, moved, removed, or updated. Without stable keys, React falls back to position (index) to pair old and new elements, reusing component instances (and their state) purely by order.

    Using the array index as a key is bad because the index stays the same for a *different* item when the list is reordered, filtered, inserted-into, or spliced. React then reuses the wrong component instance (carrying the wrong internal state), and DOM/state drift occurs (e.g., a checkbox stays checked for a different row). Keys should be stable, unique item identities (like an id), not positions.

- id: react-recon-diff-02
  answer: |
    During reconciliation React walks the element tree and, at each position, compares the *type* of the element to what was there before. If the type is identical (same DOM tag, same function/class component reference) it reuses the existing instance, preserves its state, and updates its props. If the type differs, React unmounts the old instance (running cleanup and discarding its state) and mounts a brand-new one. `key` additionally disambiguates siblings of the same type in a list, telling React whether two same-type elements at different positions are the *same* logical item or distinct ones.

- id: react-recon-remount-03
  answer: |
    Give the form component a `key` prop whose value is the selected user's id (or some value that changes when the user changes). When the key changes, React sees a different element at that position and throws away the old instance, mounting a fresh one with cleared internal state — so the form fully resets, no effect or manual resets required. This is the idiomatic "remount by key" approach.

- id: react-hooks-rules-01
  answer: |
    Rules of Hooks: (1) Only call hooks at the top level of a component or custom hook — never inside loops, conditions, or nested functions. (2) Only call hooks from React function components or custom hooks (not regular JS functions or class components).

    React identifies hooks by call *order*: each call corresponds to a slot in a per-component hook list. If you call a hook conditionally, the order shifts between renders, so every subsequent hook reads the wrong slot (stale or mismatched state), producing subtle, hard-to-trace bugs. The order must be stable across renders.

- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` reads `count` from the closure of the current render, so if multiple updates happen before a re-render (batched or from rapid events), each one computes from the *same* stale `count`, and increments get lost — e.g., two quick clicks both compute `0+1` and you end up at 1 instead of 2.

    The functional updater `setCount(c => c + 1)` receives the *latest* queued state from React, so each queued update is applied on top of the previous one and they compose correctly regardless of when the re-render happens.

- id: react-hooks-memo-03
  answer: |
    `useMemo(fn, deps)` memoizes and returns the *value* produced by `fn`; it recomputes only when a dependency changes. `useCallback(fn, deps)` memoizes and returns the *function* itself — it is effectively `useMemo(() => fn, deps)`.

    Memoizing helps when (a) a computation is genuinely expensive and you want to skip recomputing it, or (b) you need a *stable reference* (same object/function identity) to pass to a memoized child (`React.memo`) or as an effect dependency, otherwise the child re-renders or the effect re-runs every time. For trivial computations it adds overhead and shouldn't be used.

- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` over `useState` when state logic is non-trivial or involves multiple related sub-values that change together, when the next state depends on the previous state in a way that's more than a simple set, or when you have many event handlers that update state in varied ways. It's useful for colocating and centralizing transition logic (making updates predictable and testable), for state machines, and for cases where `useState`'s "mirror" of several values becomes awkward.

- id: react-hooks-use-05
  answer: |
    The React 19 `use` API is a hook that reads a *resource* during render: specifically it can read a Promise (unwrapping an async value and suspending until it resolves) and read Context. Unlike the classic hooks, `use` can be called conditionally (e.g., inside an `if`) and does not need to be at the top level, because it is designed primarily as a value reader rather than stateful registration. It integrates with Suspense for promises and lets you consume context without the `useContext` caveats. It is not used for side effects (that remains `useEffect`).

- id: react-effects-deps-01
  answer: |
    The dependency array should list every value that the effect reads and that could change between renders — props, state, and any variable derived from them that the effect closes over. React compares these with `Object.is` and re-runs the effect only when one changes.

    If you omit a used value (lie about deps), the effect won't re-run when that value changes, so the closure keeps capturing the *stale* value from the render when the effect was last created. This produces bugs like timers/intervals or event handlers acting on old data, or subscriptions that don't reflect current props.

- id: react-effects-cleanup-02
  answer: |
    An effect's cleanup function runs right before the effect re-runs (when its dependencies change) and once before the component unmounts. It's needed for subscriptions, timers, and global listeners because otherwise each re-run would add *another* subscription/listener/interval on top of the previous one, and on unmount you'd leak handlers or timers that keep firing against an unmounted component. Cleanup (e.g., `clearInterval`, `removeEventListener`, `unsubscribe`) tears down the previous effect's resources so there's exactly one active at a time.

- id: react-effects-misuse-03
  answer: |
    Two common misuses:

    1. Using `useEffect` to *transform or derive* data for rendering — e.g., `useEffect` computing a filtered list into state. Fix: compute it directly during render (it's pure), or with `useMemo` if expensive.

    2. Using `useEffect` to *sync state that is derived from props* (keeping a copy in sync) or to reset local state on a prop change. Fix: derive the value during render, or reset via a `key` prop on the component, or compute from props directly. (A related one: using an effect to respond to a *user event* — that logic belongs in the event handler, not an effect.)

- id: react-effects-strictmode-04
  answer: |
    In development StrictMode intentionally mounts, unmounts, then remounts components, so your effects run, clean up, and run again — twice. This surfaces bugs where effects aren't idempotent or their cleanup is missing/incorrect: if after the double-run you end up with duplicate subscriptions, leaked timers, or corrupted state, your effect isn't properly cleaning up. It's a deliberate stress test prompting you to make effects resilient to repeated mount/cleanup with correct cleanup logic.

- id: react-rsc-boundary-01
  answer: |
    A Server Component runs on the server: it can be `async`, access server-only resources (DB, filesystem, secrets) directly, and ships *zero JavaScript* to the browser. A Client Component runs in the browser and may use hooks, state, effects, and event handlers.

    `"use client"` is a directive that marks a module as the boundary: everything in and below that module is compiled for and shipped to the client. It tells the bundler, "this code may use client-only features," and establishes where the server→client split happens. (`"use server"` marks server actions, a different thing.)

- id: react-rsc-props-02
  answer: |
    Server Components can pass *serializable* props across the boundary: primitives, plain objects/arrays, dates (via serialization), and even other Server Components passed as `children` to Client Components.

    What can't cross: functions (event handlers), class instances, non-serializable objects, and anything requiring live code/state on the server — because the client only receives data, not the executing code. So you cannot pass a callback *from* a Server Component down into a Client Component; the client must own its own handlers (often via server actions for mutations).

- id: react-rsc-data-03
  answer: |
    In a Server Component you fetch data directly with `async/await` inside the (async) component, on the server, close to the data source — e.g., `const data = await db.query(...)`. The component is `async` and returns the rendered result with data already available.

    This differs from the classic `useEffect` fetch: there, data loads on the client after the component mounts, causing a loading flash, an extra network round-trip from the browser, and requiring client JS. With RSC the data is fetched server-side and streamed/rendered before shipping, so there's no client-side loading state for that initial data and no client fetch library needed.

- id: react-context-rerender-01
  answer: |
    An inline value like `{ user, setUser }` is a *new object reference* on every render, and Context consumers re-render whenever the provider value changes by `Object.is` comparison. So even when `user` is identical, every consumer re-renders every time the provider parent renders — defeating Context's purpose and hurting performance.

    Fix: memoize the value with `useMemo(() => ({ user, setUser }), [user, setUser])` so the reference stays stable, or split contexts (one for `user`, one for the stable `setUser`), or avoid bundling in an object. Note `setUser` from `useState`/`useReducer` is already stable; the wrapper object is the problem.

- id: react-context-usage-02
  answer: |
    Context is right when data must be read by many components across different nesting levels and prop-drilling would be painful — e.g., current user, theme, locale, or low-level config.

    It's the wrong tool when: the data changes very frequently (every consumer re-renders on change, no fine-grained subscription), when only two nearby components need it (just pass props), when you're building a large global store with many independent slices (a state library with selectors scales better), or when it's used as a catch-all for state that should be local.

- id: react-perf-memo-01
  answer: |
    `React.memo` is a higher-order component that skips re-rendering the wrapped component when its props are shallowly equal (`Object.is` per prop). It short-circuits the render if nothing changed.

    It can fail to prevent re-renders when: (a) the parent passes new object/array/function references each render (so props aren't referentially equal — e.g., inline `style={{}}` or inline callbacks), defeating the shallow comparison; (b) the component reads a Context, so it re-renders when that context changes regardless of props; (c) it genuinely receives different props. The fix is usually stabilizing references with `useMemo`/`useCallback` or splitting/colocating state.

- id: react-perf-list-02
  answer: |
    Before memoization, the higher-impact fix is *virtualization (windowing)* — render only the rows currently visible (plus a small buffer) using something like `react-window` or `react-virtual`, so the DOM holds dozens of nodes instead of 10,000.

    Why: the bottleneck is the sheer number of DOM nodes being created, laid out, and reconciled, not wasted re-renders of individual rows. Memoization can't fix the cost of mounting thousands of elements; virtualization reduces the rendered element count by orders of magnitude and is far more impactful. Pagination is a coarser alternative.

- id: react-refs-useref-01
  answer: |
    Two main uses: (1) Storing a *mutable value* that persists across renders without triggering a re-render — e.g., an interval id, a previous value, a counter, a flag, or a reference to the latest props inside a closure. (2) Holding a reference to a DOM node or a child component instance to interact with it imperatively (focus, measure, scroll).

    Mutating `ref.current` doesn't re-render because a ref is just a plain mutable container that React does not track as a reactive dependency — changing it doesn't notify any renderer, so React never schedules an update for it.

- id: react-refs-forward-02
  answer: |
    Traditionally you use `React.forwardRef` to wrap the child: the child accepts a `ref` parameter and forwards it to the underlying DOM element (`const Child = forwardRef((props, ref) => <input ref={ref} />)`), so the parent can attach its ref and reach the DOM node.

    In React 19, `forwardRef` is no longer needed: you can pass `ref` as a *regular prop* to a function component and assign it directly to the DOM element (e.g., `function Child({ ref }) { return <input ref={ref} /> }`). React 19 treats `ref` like any other prop for function components, simplifying the API.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` wraps a subtree and shows the `fallback` UI while any descendant is not yet ready, automatically swapping to the real content once it's available — without you managing loading flags manually.

    A component "suspends" when it *throws a promise* during render (the promise represents work that isn't done, like lazy-loaded code via `React.lazy` or a data resource that integrates with Suspense). Suspense catches that thrown promise, renders the fallback, and re-attempts the suspended subtree when the promise resolves. It is a mechanism for awaiting resources declaratively during render.

- id: react-suspense-transition-02
  answer: |
    `useTransition`/`startTransition` solve the problem of *urgent* updates (like typing in an input) being blocked or janky because a large, non-urgent re-render (e.g., filtering a huge list) takes time. It lets you mark the heavy update as a "transition" so the UI stays responsive.

    It changes scheduling: the wrapped update is treated as low-priority and concurrent — React can interrupt it, keep showing the old UI, render the urgent input immediately, and expose an `isPending` flag so you can show pending state. The transition may be deferred or even discarded if a newer one supersedes it, unlike a normal state update which is processed synchronously in the batch.

- id: react-state-batching-01
  answer: |
    Automatic batching (React 18) means React batches multiple `setState` calls into a single re-render automatically — and, unlike React 17, this now applies everywhere (event handlers, promises, `setTimeout`, native event listeners), not just in React event handlers. This reduces unnecessary re-renders.

    To force a synchronous, immediate update outside the batch, use `ReactDOM.flushSync(() => { ... })`, which flushes the update and re-renders synchronously before continuing. Use sparingly, as it opts out of batching performance benefits.

- id: react-state-lifting-02
  answer: |
    The standard pattern is *lifting state up*: move the shared state into the nearest common parent of the two siblings, then pass the value down to each sibling as a prop and pass a setter (or callbacks) so both can update it. Since both read from and write to the same parent-held state, they stay in sync. If the siblings are deeply nested, Context (or a small store) can avoid prop-drilling, but the principle — single source of truth in a common ancestor — is the same.

- id: react-state-derived-03
  answer: |
    Keeping a separate `useState` copy of an incoming prop is an anti-pattern because it duplicates state that already exists, the two can drift out of sync (the copy won't update when `items` changes unless you also add a syncing effect), and it forces extra effects and renders. You end up maintaining a mirror that can lie.

    The fix is to *derive* during render: compute what you need directly from `items` (e.g., `const rows = items.map(...)`) rather than storing a copy. If you genuinely need editable local state initialized from props, use `items` as the initial value combined with a `key` that resets it when the source changes, but for pure rendering just compute from the prop.
