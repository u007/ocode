# React — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 26

---

### react-recon-keys-01

Why does React need a `key` on items in a list, and why is the array index usually a bad key?

### react-recon-diff-02

When React re-renders, how does it decide whether to reuse an existing component instance or throw it away and mount a new one?

### react-recon-remount-03

You want a form component to fully reset its internal state when the selected user changes. What's the idiomatic React way?

### react-hooks-rules-01

State the Rules of Hooks and explain why calling a hook conditionally breaks React.

### react-hooks-updater-02

Why can `setCount(count + 1)` produce a stale result, and how does the functional updater form fix it?

### react-hooks-memo-03

What is the difference between `useMemo` and `useCallback`, and when does memoizing actually help?

### react-hooks-reducer-04

When would you reach for `useReducer` instead of `useState`?

### react-hooks-use-05

What is the React 19 `use` API, and how does it differ from the other hooks?

### react-effects-deps-01

What belongs in a `useEffect` dependency array, and what goes wrong if you lie about it (omit a used value)?

### react-effects-cleanup-02

When does an effect's cleanup function run, and why is it needed for subscriptions or timers?

### react-effects-misuse-03

Give two common cases where developers use `useEffect` but shouldn't, and what to do instead.

### react-effects-strictmode-04

In development, StrictMode runs your effects twice on mount. Why, and what does it tell you to fix?

### react-rsc-boundary-01

Explain the difference between a Server Component and a Client Component, and what `"use client"` actually marks.

### react-rsc-props-02

What kinds of props can a Server Component pass down across the client boundary, and what can't cross?

### react-rsc-data-03

How do you fetch data in a Server Component, and how does that differ from the classic `useEffect` fetch?

### react-context-rerender-01

A context provider's value is `{ user, setUser }` created inline. Why might this hurt performance, and how do you fix it?

### react-context-usage-02

When is Context the right tool, and when is it the wrong one?

### react-perf-memo-01

What does `React.memo` do, and why does wrapping a component in it sometimes fail to prevent re-renders?

### react-perf-list-02

A list of 10,000 rows is janky. Before reaching for memoization, what is the higher-impact fix and why?

### react-refs-useref-01

Name the two main uses of `useRef`, and explain why mutating `ref.current` doesn't re-render.

### react-refs-forward-02

How do you let a parent attach a ref to a child's DOM node, and what changed in React 19?

### react-suspense-01

What does `<Suspense fallback={...}>` do, and what actually causes a component to "suspend"?

### react-suspense-transition-02

What problem does `useTransition` / `startTransition` solve, and how does it change how an update is scheduled?

### react-state-batching-01

What is automatic batching in React 18, and how do you force a synchronous update when you truly need one?

### react-state-lifting-02

Two sibling components need to stay in sync. What's the standard React pattern?

### react-state-derived-03

A prop `items` comes in and you keep a separate `useState` copy to render. Why is that an anti-pattern, and what's the fix?
