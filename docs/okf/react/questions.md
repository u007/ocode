# React Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### react-recon-keys-01 · reconciliation · W2 · medium
**Q:** Why does React need a `key` on list items, and why is the array index usually a bad key?
**A:** Keys give each element a stable identity across renders so React matches old/new children instead of re-creating them. Index keys break on reorder or front insert/remove: the index now points at a different item, so state and uncontrolled DOM stick to the wrong row.
• stable identity across renders • index breaks on reorder/insert ~ "keys are for performance only"

### react-recon-diff-02 · reconciliation · W2 · medium
**Q:** How does React decide to reuse an instance vs mount a new one?
**A:** It compares each position by type and key. Same type + key → reuse and update props (state kept). Different type or key → unmount old subtree, mount new (state lost).
• compares by type • compares by key • type/key change remounts (state reset) ~ "diffing" with no type/key

### react-recon-remount-03 · reconciliation, state · W2 · medium
**Q:** Fully reset a form's state when the selected user changes — idiomatic way?
**A:** `<Form key={userId} />`. Changing the key remounts a fresh instance, resetting all internal state.
• key={userId} forces remount • remount resets state ~ manual field reset in effect

### react-hooks-rules-01 · hooks · W3 · easy
**Q:** State the Rules of Hooks and why conditional calls break React.
**A:** Call hooks only at the top level, only from components/custom hooks. React tracks hook state by call order; a conditional call shifts the order and state lands in the wrong slot.
• top level only • components/custom hooks only • identified by call order/index ~ rule without the call-order reason

### react-hooks-updater-02 · hooks, state · W2 · medium
**Q:** Why is `setCount(count + 1)` stale, and how does the updater form fix it?
**A:** `count` is captured from the render closure, so batched updates read the same stale value and collapse to one. `setCount(c => c + 1)` receives the latest queued state.
• count captured stale from closure • updater gets latest/queued state ~ "use callback form" without why

### react-hooks-memo-03 · hooks, perf · W2 · medium
**Q:** `useMemo` vs `useCallback`, and when does memoizing help?
**A:** `useMemo` caches a value; `useCallback` caches a function identity. Helps for expensive compute, or stable identity passed to a `React.memo` child / effect dep. Not free — can cost more than it saves.
• useMemo=value, useCallback=fn identity • helps for expensive compute or stable identity • not free ~ two defs without "when it helps"

### react-hooks-reducer-04 · hooks, state · W1 · medium
**Q:** When reach for `useReducer` over `useState`?
**A:** Complex/interdependent transitions centralized in a pure, testable reducer; also gives a stable `dispatch`.
• complex interdependent state in one place • pure/testable reducer or stable dispatch ~ "for complex state" with no reason

### react-hooks-use-05 · hooks, rsc, suspense · W1 · medium
**Q:** What is the React 19 `use` API and how does it differ from other hooks?
**A:** `use(resource)` reads a Promise (suspends) or Context. Unlike other hooks it can be called conditionally and in loops.
• reads Promise (suspends) or Context • callable conditionally/in loops ~ "new hook for promises" only

### react-effects-deps-01 · effects · W3 · medium
**Q:** What belongs in a `useEffect` dep array, and what breaks if you omit a used value?
**A:** Every reactive value the effect reads (props/state/derived). Omitting one → stale captured value and the effect won't re-run when it changes. exhaustive-deps lint enforces this.
• every reactive value read • omit → stale closure / no re-run ~ mentions array not consequence

### react-effects-cleanup-02 · effects · W2 · medium
**Q:** When does cleanup run, and why needed for subscriptions/timers?
**A:** Before the effect re-runs (deps changed) and on unmount. Tears down the previous subscription/timer so you don't leak or stack duplicates.
• runs before next run AND on unmount • prevents leaks/duplicates ~ "on unmount" only

### react-effects-misuse-03 · effects, state · W3 · hard
**Q:** Two cases where devs use `useEffect` but shouldn't, and the fix?
**A:** (1) Derived state → compute during render (variable/useMemo), don't store+sync. (2) Event-driven logic → put it in the event handler. Effects sync with external systems.
• derived state → compute in render • event logic → handler • effects = external sync ~ one case only

### react-effects-strictmode-04 · effects · W2 · medium
**Q:** Why does StrictMode run effects twice in dev, and what should you fix?
**A:** Dev-only: it mounts, runs setup+cleanup, then setup again to surface missing/incorrect cleanup. If it breaks double-invoked, fix the cleanup — don't disable StrictMode. Not in production.
• dev-only simulate remount to surface missing cleanup • fix cleanup not disable StrictMode ~ "a StrictMode dev thing" only

### react-rsc-boundary-01 · rsc · W3 · hard
**Q:** Server vs Client Component, and what does `"use client"` mark?
**A:** Server: no state/effects/browser APIs, can be async, accesses server resources, ships no JS. Client: state/effects/events, runs in browser. `"use client"` marks the module entry into the client bundle (module-level boundary), not a per-component toggle.
• server: no hooks, async, no JS • client: interactivity in browser • "use client" = module boundary ~ "use client" as per-component

### react-rsc-props-02 · rsc · W2 · hard
**Q:** What props can cross Server→Client, and what can't?
**A:** Serializable only: primitives, plain objects/arrays, React elements (incl. server-rendered `children`). Functions/handlers/class instances cannot cross. Pass elements as `children` instead of callbacks.
• props must be serializable • functions/handlers can't cross ~ "serializable" without no-functions

### react-rsc-data-03 · rsc, suspense · W2 · medium
**Q:** Fetching data in a Server Component vs the `useEffect` fetch?
**A:** A Server Component can be `async` and `await` data during render on the server — no client waterfall, no data-fetch JS shipped. `useEffect` fetch runs after mount → render-fetch-render.
• async server component awaits data on server • avoids fetch-after-mount/no JS ~ "fetch on server" without contrast

### react-context-rerender-01 · context, perf · W2 · medium
**Q:** Inline `value={{user,setUser}}` — perf problem and fix?
**A:** A new object each render is a new reference, so every consumer re-renders. Memoize with `useMemo(() => ({user,setUser}), [user])`, and/or split contexts.
• new identity re-renders all consumers • fix: useMemo value / split ~ notices issue, no fix

### react-context-usage-02 · context, state · W1 · easy
**Q:** When is Context right vs wrong?
**A:** Right for low-frequency shared values (theme/auth/locale). Wrong as a high-frequency state manager — it re-renders all consumers. It solves prop drilling, not state management.
• good for low-frequency shared • bad for high-frequency/not a store ~ "for global state" no caveat

### react-perf-memo-01 · perf · W2 · medium
**Q:** What does `React.memo` do, and why does it sometimes not prevent re-renders?
**A:** Skips re-render when props are shallowly equal. Fails when the parent passes new inline object/array/function identities each render. Needs stable prop identities to help.
• skips on shallow-equal props • fails on new identities from parent ~ "memoizes component" no detail

### react-perf-list-02 · perf, reconciliation · W2 · hard
**Q:** 10,000 janky rows — higher-impact fix than memoization, and why?
**A:** Virtualize/window: render only visible rows. Cost is dominated by mounted node count; memoizing rows barely helps if you still create 10k.
• virtualization/windowing • cost = node count, memo won't fix rendering all 10k ~ "React.memo the rows" as primary

### react-refs-useref-01 · refs · W2 · easy
**Q:** Two uses of `useRef`, and why mutating `ref.current` doesn't re-render?
**A:** (1) DOM node access. (2) Persistent mutable non-render value (timers, prev value). Mutating `ref.current` doesn't schedule a render — it's an escape hatch. Use state for anything the UI reflects.
• DOM + persistent mutable value • mutation doesn't re-render ~ one use, or says it re-renders

### react-refs-forward-02 · refs · W1 · medium
**Q:** Let a parent attach a ref to a child DOM node — and what changed in React 19?
**A:** Historically `forwardRef` to receive/forward `ref`. React 19: `ref` is a normal prop to function components; `forwardRef` no longer required (being deprecated).
• forwardRef forwards ref pre-19 • React 19 ref-as-prop ~ forwardRef only, misses 19

### react-suspense-01 · suspense · W2 · hard
**Q:** What does `<Suspense fallback>` do, and what makes a component suspend?
**A:** Shows the fallback until descendants are ready, then swaps in content. A component suspends when it reads a not-yet-ready resource — a pending Promise via `use()`, a Suspense data lib, or `lazy()`.
• fallback until ready then swap • suspends on pending resource/Promise ~ "shows a spinner" no mechanism

### react-suspense-transition-02 · suspense, perf · W2 · hard
**Q:** What does `useTransition`/`startTransition` solve and how does scheduling change?
**A:** Marks an update non-urgent so urgent updates (typing/clicks) stay responsive while the expensive update renders interruptibly in the background. `isPending` shows subtle pending UI. Not I/O throttling.
• non-urgent/interruptible so urgent updates stay responsive • isPending / non-blocking ~ "makes it async/faster"

### react-state-batching-01 · state · W2 · medium
**Q:** Automatic batching in React 18, and how to force a sync update?
**A:** 18 batches updates everywhere (promises, timeouts, native handlers), not just React events (17). `flushSync` opts a specific update out when you must read the DOM immediately.
• 18 batches everywhere not just React events • flushSync for sync flush ~ batching without 17→18 change

### react-state-lifting-02 · state · W1 · easy
**Q:** Two siblings must stay in sync — standard pattern?
**A:** Lift shared state to the closest common parent, pass down as props + setter. Parent is the single source of truth.
• lift to common ancestor, pass as props • single source of truth ~ "share state" without lifting

### react-state-derived-03 · state, effects · W2 · medium
**Q:** Keeping a `useState` copy of an `items` prop — why anti-pattern, and fix?
**A:** Duplicates the source of truth; the copy goes stale unless synced via a fragile effect. Derive during render (or `useMemo` if expensive). Only store state the component owns.
• duplicates source of truth / needs effect sync • derive in render, store only owned ~ "just use the prop" no reasoning
