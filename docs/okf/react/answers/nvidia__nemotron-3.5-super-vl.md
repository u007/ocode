# nvidia/nemotron-3.5-super-vl — closed-book answers for react (openrouter, 2026-10-16); answerer saw only _prompts/react.md
- id: react-recon-keys-01
  answer: |
    Keys let React stably identify list items across renders so it can tell which items were inserted, deleted, reordered, or stayed the same, rather than matching purely by position. Without keys, inserting or reordering causes unnecessary DOM mutations and can make state (input focus, uncontrolled inputs, component state) stick to the wrong item.

    The array index is usually a bad key when the list can reorder, filter, insert, or delete items: the same index then refers to a different item after the change, so React reuses the wrong component instance and its state, and does extra reconciliation work. Index keys are acceptable only for static lists that never reorder and have no stateful children.
- id: react-recon-diff-02
  answer: |
    React reconciles by comparing elements at the same position in the tree. If the element type is the same (and the key matches among siblings), React reuses the existing fiber/component instance and updates its props and hooks in place. If the type changes (or the key changes), React unmounts the old tree, running cleanups/unmount logic, and mounts the new one from scratch. Keys let React track moved items across siblings instead of treating them as remove-plus-add.
- id: react-recon-remount-03
  answer: |
    Pass a `key` derived from the selected user's id to the form, e.g. `<Form key={userId} />`. When `userId` changes, React unmounts the old form tree and mounts a fresh one, resetting hooks state, uncontrolled inputs, and DOM state. Alternatives exist (lifting state and resetting it explicitly), but key-based remounting is the idiomatic, least-error-prone way.
- id: react-hooks-rules-01
  answer: |
    Rules of Hooks: (1) only call Hooks at the top level of a component or custom Hook — never inside loops, conditions, or nested functions; (2) only call Hooks from React functions (components and custom Hooks), not plain JavaScript functions.

    Conditional Hook calls break React because Hook state is tied to call order: each useState/useEffect slot belongs to a fixed position in the render's Hook call sequence. Skipping a Hook shifts every subsequent slot, so React would attach the wrong state or effect to the wrong call, producing subtle bugs.
- id: react-hooks-updater-02
  answer: |
    `setCount(count + 1)` closes over the `count` value from the render where the handler was created. With batched or rapid repeated updates, several calls read the same stale `count` and write the same next value, so updates are lost.

    The functional form `setCount(c => c + 1)` receives the latest state at flush time, so each update chains off the most recent value instead of a stale closure.
- id: react-hooks-memo-03
  answer: |
    `useMemo` memoizes a value; `useCallback` memoizes a function (`useCallback(fn, deps)` is equivalent to `useMemo(() => fn, deps)`). Both only recompute when a dependency changes.

    Memoizing helps when the value/function is passed to `React.memo` children (a new identity would defeat their prop comparison) or when the computation is genuinely expensive. It adds its own overhead, so it does not help for cheap values or non-memoized children — measure before adding it.
- id: react-hooks-reducer-04
  answer: |
    Reach for `useReducer` when state logic is complex (multiple sub-fields, transitions that depend on prior state, updates scattered across handlers), when you want transition logic colocated in one reducer, or when you need a stable identity to pass down (`dispatch` is stable) instead of many setter callbacks. Typical cases: multi-step forms, wizards, rich dashboards. Simple independent values stay on `useState`.
- id: react-hooks-use-05
  answer: |
    React 19's `use` reads a thenable/Promise or a context value directly during render. If the promise is pending, React suspends the nearest `<Suspense>` boundary and re-renders with the resolved value when it settles.

    It differs from other Hooks because it can be called conditionally (inside ifs/loops) and because it unwraps async resources during render instead of requiring a separate `useEffect` plus loading state.
- id: react-effects-deps-01
  answer: |
    List every reactive value the effect uses: props, state, and any derived values or functions referenced inside it (functions should be wrapped in `useCallback` or defined inside the effect).

    If you omit a used value, the effect closes over a stale value and/or misses re-runs when that value changes — causing stale subscriptions, missed updates, or wrong behavior. The exhaustive-deps lint rule exists to catch this.
- id: react-effects-cleanup-02
  answer: |
    Cleanup runs before the component unmounts and again before the effect re-runs after a dependency change. It is required for subscriptions, timers, event listeners, and fetch abort controllers, so you do not leak memory, leave dangling listeners, double-subscribe, or call setState on an unmounted component.
- id: react-effects-misuse-03
  answer: |
    (1) Deriving data for rendering from props — compute it during render (memoize with `useMemo` if expensive) instead of `useEffect` plus `setState`, which causes an extra render and possible stale output. (2) Syncing a prop into state (`useEffect(() => setX(prop), [prop])`) — just use the prop directly, or reset via a `key` if you genuinely need a snapshot; the effect approach drifts and double-renders. Don't use effects for event handling either — call the function directly in the handler.
- id: react-effects-strictmode-04
  answer: |
    In development StrictMode intentionally mounts, unmounts, and remounts each component, running effects twice. It surfaces components whose setup is not idempotent or whose cleanup is missing or broken — duplicated subscriptions, leaked timers, state that depends on mount order.

    The fix is to make setup and teardown symmetric so remounting is safe; it also prepares apps for concurrent features that may remount components.
- id: react-rsc-boundary-01
  answer: |
    A Server Component renders on the server/build and emits serialized output; it can fetch directly and uses no client JS for its own code, but cannot use state, effects, browser APIs, or event handlers. A Client Component runs in the browser, is interactive, and may use Hooks and browser APIs.

    `"use client"` is a directive at the top of a file marking that module as a Client Component boundary — its code is bundled for the client and can be imported across the boundary. It does not make every descendant a Client Component; Server Components can still be passed as children.
- id: react-rsc-props-02
  answer: |
    Props crossing from a Server Component to a Client Component must be serializable: strings, numbers, booleans, plain objects/arrays, null, and React elements (including Client Components passed as children). Functions, class instances, DOM nodes, Promises, Symbols, and other non-serializable values cannot cross — the boundary serializes props over the network. Component references passed as children resolve by module identity rather than by value.
- id: react-rsc-data-03
  answer: |
    Make the Server Component async and await your data source directly (`async function Page() { const data = await db.query(); ... }`); Suspense boundaries handle loading UI.

    Unlike the classic `useEffect` fetch — which runs after the client mounts, requires the client JS to load first, creates a server-request waterfall, and needs loading/error state — the RSC fetch happens on the server before HTML is sent, with no client round-trip for that data, no effect, and no client loading state.
- id: react-context-rerender-01
  answer: |
    Inline `{ user, setUser }` creates a new object identity on every provider render, so every consumer re-renders whenever the provider re-renders — even consumers that only need the stable setter.

    Fix: split into separate contexts (one for the changing `user`, one for the stable `setUser`/dispatch, since `useReducer` dispatch never changes), or memoize the value with `useMemo`. That way components subscribe only to what they actually use.
- id: react-context-usage-02
  answer: |
    Context is right for truly global, low-frequency data consumed by many deeply nested components (theme, auth/user, locale, UI mode) where prop drilling would be painful.

    It is wrong for high-frequency values (every change re-renders all consumers), data needed by only a component or two (pass props), and as a blanket state-management replacement. Fine-grained or high-throughput state is better kept in component state, state libraries, or pushed to Server Components.
- id: react-perf-memo-01
  answer: |
    `React.memo` shallow-compares a component's props and skips re-rendering when they are unchanged — it only bails out of re-renders triggered by a parent. It does nothing for the component's own state, context, or hooks.

    It fails when props get new identities each render (inline objects, arrays, functions not wrapped in `useCallback`), when deep comparison is needed, or when the component re-renders for its own internal reasons — then memoization just adds overhead.
- id: react-perf-list-02
  answer: |
    Virtualize/window the list (react-window, TanStack Virtual, react-virtuoso) or paginate, so only visible rows are in the DOM. The dominant cost at 10,000 rows is mount/layout/paint of DOM nodes, not render logic; memoization does not reduce the number of mounted nodes or the initial cost, so virtualization gives a far bigger win. Memoize rows only after virtualizing, if re-renders remain expensive.
- id: react-refs-useref-01
  answer: |
    Two main uses: (1) accessing a DOM node via the `ref` attribute; (2) holding a mutable value that persists across renders without triggering re-renders (timers, previous values, cancel tokens, mutable objects).

    Mutating `ref.current` does not re-render because refs are outside React's render state — React schedules no work on ref mutation (unlike `setState`). Refs are an imperative escape hatch.
- id: react-refs-forward-02
  answer: |
    Wrap the child in `React.forwardRef((props, ref) => <div ref={ref} .../>)` and pass the parent's `useRef` as `<Child ref={myRef} />`; React attaches the ref to the forwarded DOM node.

    In React 19, `ref` works as a regular prop on function components without `forwardRef`, and ref callbacks can return a cleanup function. `forwardRef` is still supported but no longer required.
- id: react-suspense-01
  answer: |
    `<Suspense fallback={...}>` renders the fallback while its descendants are loading and swaps in the real content when ready.

    A component "suspends" by throwing a thenable (Promise) during render. React catches it, shows the nearest Suspense fallback, subscribes to the promise, and re-renders once it resolves. It is promise-throwing, not an error.
- id: react-suspense-transition-02
  answer: |
    `useTransition`/`startTransition` mark an update as non-urgent so React can keep the UI responsive (e.g., typing stays fluid while a heavy list re-renders in the background).

    Urgent updates (discrete events) are synchronous; transition updates are lower-priority, interruptible, and may be discarded if superseded, while `isPending` lets you show a temporary loading state.
- id: react-state-batching-01
  answer: |
    Automatic batching (React 18) groups multiple state updates into a single re-render regardless of where they originate (promises, timeouts, native event handlers), not just React event handlers.

    To force a synchronous flush when you must read the DOM immediately after updating, wrap the update in `flushSync` from `react-dom/client`. Use sparingly, since it hurts performance.
- id: react-state-lifting-02
  answer: |
    Lift the shared state up to the siblings' closest common parent: the parent owns the single source of truth, passes the value down as a prop to both children, and passes a handler/setter down so either sibling can update it. Data flows down, events flow up; both siblings stay in sync because they read from the same state.
- id: react-state-derived-03
  answer: |
    Mirroring a prop in `useState` creates two sources of truth that diverge when the prop changes (unless you add a fragile syncing effect), causing stale UI, extra renders, and race conditions.

    Fix: render directly from the prop; compute any transformed version during render (memoize with `useMemo` if expensive). Only duplicate into state for genuinely non-controlled initialization, and be ready to reset via `key` when the source changes.
