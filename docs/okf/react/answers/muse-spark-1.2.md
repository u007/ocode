# Closed-book answers — opencode-go/muse-spark-1.2 on react

> Produced by an isolated subprocess (`ocode2 run -model opencode-go/muse-spark-1.2
> -dir /tmp/kaizen-react-answer -yolo -effort medium`) given ONLY
> `docs/okf/_prompts/react.md` as input, in an empty directory with no repo
> access. Verified from the captured log: zero tool calls (no read/edit/grep/bash)
> — a single LLM turn answering from its own knowledge. Raw output below,
> unedited.

```yaml
- id: react-recon-keys-01
  answer: |
    React's reconciliation diffs lists by element identity. `key` is the stable identity for an item across renders — it tells React "this JSX element corresponds to that previous instance" so it can move/reorder/update instead of destroying/recreating DOM and component state.
    Array index is bad because it is identity-by-position, not by data. On insert/delete/reorder/sort/filter the index for a given item changes, so React reuses the wrong instance: state (input value, focus, animation, uncontrolled state) gets associated with the wrong row, it does extra DOM mutations, and performance degrades. Use a stable unique id from the data (db id, uuid). Index is only safe for a static, never-reordered list.

- id: react-recon-diff-02
  answer: |
    React reconciles the element tree. At each position it compares the new element's `type` (string like 'div' or component function/class) and `key`.
    1. If type and key are the same as previous render at that position, reuse the existing instance/fiber: keep state, keep DOM node and patch props.
    2. If type or key differs, discard the old instance: unmount it (run cleanup/effects, destroy state) and mount a new one.
    Same position + same type + same key = update; otherwise = remount. Moving a component in the tree without a key or changing its wrapper type also forces a remount.

- id: react-recon-remount-03
  answer: |
    Give the form a `key` derived from the selected user: `<UserForm key={selectedUser.id} user={selectedUser} />`. When the key changes React tears down the old instance completely and mounts a fresh one, resetting all internal `useState`/`useRef`/uncontrolled inputs automatically. This is idiomatic and preferable to manually syncing state with `useEffect` or imperative reset.

- id: react-hooks-rules-01
  answer: |
    Rules: 1) Only call hooks at the top level of your function — never inside conditions, loops, or nested functions. 2) Only call hooks from React function components or custom hooks — not plain JS functions.
    Why conditional breaks: React stores hooks in an ordered linked list/array per fiber by call order, with no names. On each render it walks the list in order. If you conditionally skip a hook, the order shifts and the next hook call reads the wrong slot — state gets mixed up, leading to stale values or "rendered more hooks than previous render" / "fewer hooks" error.

- id: react-hooks-updater-02
  answer: |
    `setCount(count+1)` closes over the `count` from that render. If you call it multiple times in the same event/timeout or batched update, each call sees the same stale `count` so `setCount(count+1); setCount(count+1)` only increments once (last write wins).
    Functional updater `setCount(c => c+1)` does not close over render state — React queues the updater function and calls it with the freshest pending state at apply time, so multiple queued updates chain correctly: `setCount(c=>c+1); setCount(c=>c+1)` increments by 2.

- id: react-hooks-memo-03
  answer: |
    `useMemo(() => value, deps)` memoizes a *value* (result of expensive calculation). `useCallback((args)=>..., deps)` memoizes a *function reference*; it is equivalent to `useMemo(() => fn, deps)`.
    Memoization only helps when 1) the computation is genuinely expensive, or 2) referential stability matters — you pass the value/callback to a memoized child (`React.memo`/`memo`), to a dependency array, or as a context value where new identity would cause cascading renders/effects. Otherwise it adds cost (memory, deps comparison) with no benefit and can hurt.

- id: react-hooks-reducer-04
  answer: |
    Use `useReducer` when state is complex: multiple related fields that update together, transitions depend on previous state or involve many action types, logic is non-trivial or needs to be tested/centralized, or you want predictable updates and to pass `dispatch` down without recreating callbacks. `useState` is fine for independent primitive values; `useReducer` is better for state machines, forms with validation, undo/redo, or when next state depends on history and action payload, similar to Redux.

- id: react-hooks-use-05
  answer: |
    `use` is React 19's unconditional-unwrap API: `const data = use(promise)` or `const ctx = use(Context)`. If given a Promise/thenable it *suspends* the component until it resolves (integrates with Suspense), returning the resolved value. Unlike other hooks, `use` is not bound by Rules of Hooks order — it can be called conditionally, inside if/loops, and even after early returns, because React can track it differently. It also replaces `useContext` for reading context. Other hooks must remain top-level and cannot unwrap promises.

- id: react-effects-deps-01
  answer: |
    The dependency array must contain every *reactive* value used inside the effect: props, state, and any variables/functions derived from them in the component scope. It tells React when to re-sync the effect.
    Lying (omitting a used value) leaves the effect closed over a stale render: it sees old props/state, misses updates, keeps stale closures for subscriptions/handlers, and causes bugs like stale fetch results or events firing with old data. Linter `eslint-plugin-react-hooks` exhaustive-deps enforces this; fix by adding the dep, or removing the need for it (move value inside effect, use updater form, wrap with useCallback/useMemo, or use effectEvent).

- id: react-effects-cleanup-02
  answer: |
    Cleanup (the function returned from `useEffect`) runs 1) before the effect re-runs due to dependency change, and 2) on unmount of the component.
    It is needed to avoid leaks and duplicates: unsubscribe from subscriptions, remove event listeners, clear timers/intervals, abort fetches/Oberservers, reset external state. Without it you'd accumulate listeners/timers on each re-render and update unmounted components.

- id: react-effects-misuse-03
  answer: |
    1. Deriving state / computing during render: `useEffect(() => setFullName(first+last), [first,last])` — Should compute directly in render: `const fullName = first + ' ' + last` or `useMemo` if expensive. No effect needed.
    2. Responding to a user event: `useEffect(() => { if(submitted) post() }, [submitted])` triggered by a click — Should do the work directly in the event handler `onClick={post}`. Effects are for syncing with external systems, not for chaining state updates that could be done synchronously in handlers.
    Other classics: fetching on every keystroke without tying to render, or using effect to copy props to state.

- id: react-effects-strictmode-04
  answer: |
    In dev, StrictMode intentionally mounts, runs effects, runs cleanup (unmount), then mounts+effects again. It simulates mounting/unmounting to verify effects are idempotent and have correct cleanup.
    It tells you to fix effects that are not resilient to remount: missing cleanup (leaked subscriptions/timers), non-idempotent setup (double fetch without abort, double subscription), or reliance on mount happening only once. If double-run breaks your app, your effect would also break in production on Fast Refresh, Suspense remount, or future offscreen APIs.

- id: react-rsc-boundary-01
  answer: |
    Server Components run only on the server (Node/edge), never ship JS, can be async, can access DB/filesystem, cannot use state/effects/browser APIs (`useState`, `useEffect`, `window`). Client Components run on both server (for SSR) and client, ship JS and can be interactive.
    `"use client"` is a module-level directive marking a boundary: that file and all its imports are part of the Client bundle/graph. It doesn't mean "this component is a client component because it uses state" — it explicitly opts the module into client execution. Anything importing a "use client" module also becomes client. Server Components are the default in App Router.

- id: react-rsc-props-02
  answer: |
    Props crossing the Server -> Client boundary are serialized via React's RSC wire format (Flight), not JSON alone but still serializable.
    Can cross: primitives, plain serializable objects/arrays, Promises, React nodes/JSX, and Server Components as children. Dates, Maps/Sets are supported in recent versions if serializable.
    Cannot cross: non-serializable values — functions/event handlers/closures (except Server Actions — functions marked "use server"), class instances with prototype methods, Symbols, DOM nodes, and client-only state. Attempting to pass a plain function from Server to Client errors; pass data down and let the Client component define handlers.

- id: react-rsc-data-03
  answer: |
    In a Server Component you fetch directly in the component body with async/await: `async function Page(){ const data = await fetch(...) / await db.query(...); return <UI data={data} /> }`. Fetch happens on the server before/during streaming, you can use async directly, no loading state needed (Suspense streams).
    Classic `useEffect` fetch runs on the client *after* mount: initial render is empty/loading, then effect fires, fetches over network, then setState re-renders — causing waterfalls, spinners, and client-side exposure of secrets. Server fetch is earlier, cacheable, and avoids client waterfalls.

- id: react-context-rerender-01
  answer: |
    `{ user, setUser }` created inline is a new object reference every provider render, even if `user` didn't change. Context uses `Object.is` on the `value` reference to decide to notify consumers — new object => all `useContext` consumers re-render, even those only needing `setUser`.
    Fix: memoize the value: `const value = useMemo(() => ({user, setUser}), [user])` (setUser is stable), and/or split into two contexts (`UserContext` and `UserSetterContext`) so setter-only consumers never re-render on user change. Also memoize provider children if needed.

- id: react-context-usage-02
  answer: |
    Right tool: low-frequency, truly global/ambient state needed by many distant descendants — theme, locale/i18n, auth user, router. Avoids deep prop drilling.
    Wrong tool: high-frequency changing state (animation, form inputs, rapidly updating lists), state needed by only one or two levels (use props/composition), or as a general state manager replacement — every consumer re-renders on value change, no selectors, so it scales poorly. For frequent updates use state management with selectors (Zustand/Jotai/Redux) or lift state/colocation and pass props.

- id: react-perf-memo-01
  answer: |
    `React.memo(Component, areEqual?)` is a HOC that memoizes the rendered output: it shallow-compares props and skips re-rendering if props are shallow-equal, reusing last result.
    It fails when: parent creates new prop references each render (inline objects `style={{}}`, arrays, arrow functions `onClick={()=>}`) so shallow compare always false; when component reads Context or uses internal `useState`/`useReducer` that changed; or when children are not memoized. Fix by stabilizing props with `useMemo`/`useCallback` or by passing primitives, and understanding memo only prevents parent->child re-renders, not internal state updates.

- id: react-perf-list-02
  answer: |
    Virtualize/window the list (e.g., react-window, react-virtual, TanStack Virtual). Render only the viewport + small overscan (~10-20 items) instead of 10k DOM nodes.
    Memoization still creates 10k fibers/nodes and lays them out — DOM count is the bottleneck (layout/paint, memory). Virtualization reduces DOM nodes from O(N) to O(viewport), giving orders-of-magnitude speedup. Only after windowing does memoizing rows become useful.

- id: react-refs-useref-01
  answer: |
    Two main uses: 1) Holding a mutable box that persists across renders without causing renders — instance variables, timer ids, previous values, counters. 2) Referencing a DOM node or child instance (`<div ref={ref}>`).
    Mutating `ref.current` doesn't re-render because a ref is just a plain object `{current: ...}` stored on the fiber; React doesn't track its mutation. Only `useState`/`useReducer` updates or parent re-renders schedule a render; writing to ref is an intentional side-effect escape hatch.

- id: react-refs-forward-02
  answer: |
    Before React 19, function components don't receive `ref` as a prop — you had to wrap with `React.forwardRef((props, ref) => ...)` and attach `ref` to inner DOM: `<MyInput ref={parentRef}>`. Parent's `useRef` then points to the DOM node.
    In React 19, `forwardRef` is deprecated/removed: `ref` is a regular prop on function components. You just write `function MyInput({ref, ...props})` or `props.ref` and attach it: `<input ref={ref} />`. No wrapper needed.

- id: react-suspense-01
  answer: |
    `<Suspense fallback={<Spinner/>}>` shows `fallback` while any descendant is suspending, then swaps to the real children when ready. It enables declarative loading states and streaming SSR.
    A component suspends by *throwing a Promise* during render (React catches it). Sources: `React.lazy` dynamic imports, data-fetching libraries/integrations that throw promises (Relay, TanStack with Suspense mode), `use(promise)` in React 19, or frameworks' Suspense-enabled fetch. React suspends that boundary until the promise resolves.

- id: react-suspense-transition-02
  answer: |
    `useTransition` / `startTransition` solves UI blocking on expensive/large updates (filtering, navigation, typing). It marks an update as a *Transition* (non-urgent): React keeps the old UI responsive, defers the transition render to a concurrent low-priority task, and can interrupt it if a more urgent update (typing, click) comes in.
    It exposes `isPending` to show pending state and keeps existing `<Suspense>` fallback from flashing unnecessarily. Updates inside `startTransition(() => setState(...))` are non-blocking; outside are urgent and block.

- id: react-state-batching-01
  answer: |
    Automatic batching (React 18) groups multiple `setState` calls that occur in the same microtask/tick into a single re-render for performance, even inside timeouts, promises, and native handlers (React 17 only batched inside React handlers).
    For a truly synchronous flush — e.g., need to read DOM after state before browser paint — use `ReactDOM.flushSync(() => setState(...))` which forces React to apply the update synchronously and flush DOM. Use sparingly as it hurts performance.

- id: react-state-lifting-02
  answer: |
    Lift state up to their closest common ancestor (parent). The parent holds the shared state via `useState` and passes it down as props plus a setter/callback (`value` + `onChange`) to both siblings. Siblings communicate by calling the parent's handler, which updates the lifted state, causing both to re-render with the same source of truth. For deep trees, Context or composition can avoid deep prop drilling but the principle is the same — single source of truth in common owner.

- id: react-state-derived-03
  answer: |
    Keeping `const [copy, setCopy] = useState(items)` duplicates the source of truth. The prop `items` is canonical; the copy diverges when parent updates `items` but state doesn't sync, causing stale UI and bugs requiring manual `useEffect` syncing.
    Fix: don't copy — derive during render: `const visible = items.filter(...)` or `useMemo(() => expensive(items), [items])`. If you need to edit, either lift editing state up, or use `key` to reset, or sync intentionally with `useEffect` only when you truly need a snapshot (e.g., controlled vs uncontrolled). Prefer derived/computed values.
```
