# Tanstack — Kaizen blind answer sheet (questions only)

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

Total questions: 31

---

### tanstack-query-keys-01

What is a query key in TanStack Query, and how does the client decide that two keys refer to the same cache entry?

### tanstack-query-keys-02

Why should query keys be structured hierarchically (e.g. `['todos']`, `['todos', todoId]`), and how does that interact with invalidation?

### tanstack-query-keys-03

A query's `queryFn` reads a `userId` variable, but the key is just `['projects']`. What breaks, and what is the rule?

### tanstack-query-keys-04

Two components mount `useQuery` with the same key `['user', 5]` at the same time. How many network requests fire, and why?

### tanstack-caching-01

Explain the difference between `staleTime` and `gcTime` (formerly `cacheTime`). This is the most common point of confusion.

### tanstack-caching-02

With the default `staleTime: 0`, what happens when a component remounts and reads a query it fetched a moment ago? Describe the user-visible behavior.

### tanstack-invalidation-01

What does `queryClient.invalidateQueries({ queryKey: ['todos'] })` actually do to matching queries?

### tanstack-invalidation-02

After a successful update, when would you use `setQueryData` instead of `invalidateQueries`, and what's the tradeoff?

### tanstack-mutations-01

How does `useMutation` differ from `useQuery`, and how do you trigger and track a mutation? (Watch the v5 status naming.)

### tanstack-mutations-02

After a mutation succeeds, why is it common to call `queryClient.invalidateQueries` in `onSuccess`, and what does it accomplish?

### tanstack-mutations-03

Describe a correct optimistic update with rollback using `onMutate`/`onError`/`onSettled`. What is each step responsible for?

### tanstack-mutations-04

What's the difference between `mutate` and `mutateAsync`, and how does a mutation know it failed?

### tanstack-query-fn-01

A `queryFn` uses `fetch`, and 500 responses never show as errors in the UI. Why, and what's the fix?

### tanstack-query-fn-02

Why is it a mistake to call `setState` (or otherwise cause side effects) from inside a `queryFn`? What should you do instead?

### tanstack-query-fn-03

You have a query that must only run once `userId` is known (dependent query). How do you express that, and what state is the query in until then?

### tanstack-suspense-01

What does `useSuspenseQuery` change compared to `useQuery`, and what must wrap the component?

### tanstack-suspense-02

With `useSuspenseQuery`, where do fetch errors surface, and how do you handle them?

### tanstack-suspense-03

Two sibling components each call `useSuspenseQuery` for independent data. Why can this create a request waterfall, and how do you avoid it?

### tanstack-suspense-04

Can you make a `useSuspenseQuery` conditional with `enabled: false` to build a dependent query? Why or why not?

### tanstack-prefetch-01

In a router loader you want the data guaranteed available before the component renders. Do you use `prefetchQuery` or `ensureQueryData`, and what's the difference?

### tanstack-prefetch-02

Contrast `prefetchQuery` with `fetchQuery`. When does the error-handling behavior matter?

### tanstack-router-loaders-01

What is a route `loader` in TanStack Router, how does the component read its result, and how does this differ from a `useEffect` fetch?

### tanstack-router-loaders-02

How do you integrate TanStack Query with a Router loader so the two share one cache instead of double-fetching?

### tanstack-router-loaders-03

TanStack Router preloads routes on link hover by default. What does that do, and which route functions run during a preload?

### tanstack-router-loaders-04

What is `beforeLoad` for, how does it differ from `loader`, and what is router `context`?

### tanstack-router-search-01

How do you get typed, validated search params on a route with `validateSearch`, and why validate at all?

### tanstack-router-search-02

In TanStack Router, how should you store and update a UI filter that belongs in the URL (e.g. a page number), and what's the anti-pattern?

### tanstack-router-search-03

A route's `loader` needs to fetch based on a search param (e.g. `page`). Why doesn't the loader see search params automatically, and what do you add?

### tanstack-router-search-04

Once `validateSearch` is in place, what type-safety do you get across the app when linking to that route?

### tanstack-router-typesafety-01

How does file-based routing produce TanStack Router's end-to-end type safety, and what is `routeTree.gen.ts`?

### tanstack-router-typesafety-02

Show what typed navigation buys you: what does `<Link to="/posts/$postId" params={{ postId: '5' }} />` check at compile time?
