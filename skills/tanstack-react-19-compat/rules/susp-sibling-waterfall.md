# susp-sibling-waterfall: Avoid React 19 Suspense Request Waterfalls with useSuspenseQuery

## Priority: HIGH

## Explanation

React 19 changed Suspense scheduling: once a component suspends, React stops rendering its as-yet-unrendered siblings for that pass instead of eagerly rendering all of them first (React 18 behavior). This is an intentional "render-as-you-fetch" push, not a bug — but it means naive **fetch-on-render** code (a component calling `useSuspenseQuery` inside its own render) can turn what looked like parallel sibling fetches into a sequential waterfall, because a later sibling's query doesn't start until an earlier sibling's suspend/resume cycle lets React reach it.

There is a second, unrelated cause of the same symptom: **multiple `useSuspenseQuery` calls inside the same component** are always sequential (each `await`s before the next line runs), in every React version — this is not React 19-specific, use `useSuspenseQueries` instead.

## Bad Example — waterfall from render-order sensitivity

```tsx
// Both calls look "parallel" but React 19 may render UserPosts
// only after UserProfile's promise resolves, delaying its fetch start.
function UserPage({ userId }: { userId: string }) {
  return (
    <Suspense fallback={<Loading />}>
      <UserProfile userId={userId} />
      <UserPosts userId={userId} />
    </Suspense>
  )
}

function UserProfile({ userId }: { userId: string }) {
  const { data } = useSuspenseQuery(userQueryOptions(userId))
  return <Profile user={data} />
}

function UserPosts({ userId }: { userId: string }) {
  const { data } = useSuspenseQuery(postsQueryOptions(userId))
  return <Posts posts={data} />
}
```

## Bad Example — sequential queries in one component (any React version)

```tsx
function UserPage({ userId }: { userId: string }) {
  const { data: user } = useSuspenseQuery(userQueryOptions(userId))
  const { data: posts } = useSuspenseQuery(postsQueryOptions(userId)) // waits for user's query to settle first
  return <Page user={user} posts={posts} />
}
```

## Good Example — fix same-component case with useSuspenseQueries

```tsx
function UserPage({ userId }: { userId: string }) {
  const [{ data: user }, { data: posts }] = useSuspenseQueries({
    queries: [userQueryOptions(userId), postsQueryOptions(userId)],
  })
  return <Page user={user} posts={posts} />
}
```

## Good Example — fix render-order case: prefetch in the route loader (preferred)

This is the primary fix recommended by both the React team and TanStack: initiate the fetches *before* render (render-as-you-fetch) instead of during it. In a TanStack Router / Start route:

```tsx
export const Route = createFileRoute('/users/$userId')({
  loader: ({ context: { queryClient }, params }) => {
    // Both start immediately, in parallel — do not await sequentially here
    queryClient.ensureQueryData(userQueryOptions(params.userId))
    queryClient.ensureQueryData(postsQueryOptions(params.userId))
  },
  component: UserPage,
})

function UserPage() {
  const { userId } = Route.useParams()
  return (
    <Suspense fallback={<Loading />}>
      <UserProfile userId={userId} />
      <UserPosts userId={userId} />
    </Suspense>
  )
}
```

`useSuspenseQuery` inside `UserProfile`/`UserPosts` then just reads the already-in-flight (or already-resolved) query instead of starting it, so React's render-order no longer matters.

## Good Example — fallback fix without a loader: separate Suspense boundaries

When prefetching in a loader isn't available (e.g. no router loader for this data), giving each sibling its own boundary lets React commit each independently instead of gating on the first:

```tsx
<>
  <Suspense fallback={<ProfileSkeleton />}>
    <UserProfile userId={userId} />
  </Suspense>
  <Suspense fallback={<PostsSkeleton />}>
    <UserPosts userId={userId} />
  </Suspense>
</>
```

This is a weaker guarantee than loader prefetching — it helps React commit siblings independently, but does not itself force both queries to start in the same tick the way `ensureQueryData` in a loader does.

## Rule

- Never put two or more `useSuspenseQuery` calls in the same component — use `useSuspenseQueries`.
- Prefer prefetching with `queryClient.ensureQueryData(...)` (not `await`ed sequentially) in the TanStack Router `loader`, so data is already in flight before any component suspends.
- Where a loader isn't practical, give each suspending sibling its own `<Suspense>` boundary as a fallback measure.
- Do not treat this as "React 19 is broken" when reporting bugs upstream — it's a deliberate scheduling change; the fix is architectural (render-as-you-fetch), not a patch to wait for.
