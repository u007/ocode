# head-use-router-meta: Use TanStack Router's head()/meta() Instead of Raw React 19 <title>/<meta> JSX

## Priority: HIGH

## Explanation

React 19 added native support for rendering `<title>`, `<meta>`, and `<link>` tags anywhere in the component tree, auto-hoisting them into `<head>`. In a TanStack Start SSR app this collides with the router's own head management: [TanStack/router#3050](https://github.com/TanStack/router/issues/3050) (open) reports metadata tags rendering **twice in the DOM**, in both dev and production, even with a `key` applied — the two systems (React 19's hoisting and TanStack Start's SSR/hydration pipeline) don't deduplicate against each other.

TanStack Router/Start ship their own head-management API specifically to be SSR/streaming-safe and to dedupe nested-route tags (child route wins on conflict). Use it instead of raw JSX meta tags in any TanStack Start route.

## Bad Example — raw React 19 JSX tags in a route component

```tsx
export const Route = createFileRoute('/products/$id')({
  component: ProductPage,
})

function ProductPage() {
  const { id } = Route.useParams()
  const { data: product } = useSuspenseQuery(productQueryOptions(id))
  return (
    <>
      {/* Renders duplicated in SSR + hydration per TanStack/router#3050 */}
      <title>{product.name}</title>
      <meta name="description" content={product.summary} />
      <ProductDetails product={product} />
    </>
  )
}
```

## Good Example — route-level head()

```tsx
export const Route = createFileRoute('/products/$id')({
  loader: ({ context: { queryClient }, params }) =>
    queryClient.ensureQueryData(productQueryOptions(params.id)),
  head: ({ loaderData }) => ({
    meta: [
      { title: loaderData.name },
      { name: 'description', content: loaderData.summary },
    ],
  }),
  component: ProductPage,
})
```

Requires `<HeadContent />` and `<Scripts />` rendered in the root route (`__root.tsx`), which is the standard TanStack Start setup.

## Rule

- Never render `<title>`, `<meta>`, or `<link>` directly in a route component's JSX in a TanStack Start/Router SSR app — use the route's `head()` option.
- `head()` receives `loaderData`/`params`/`match` context, so prefer computing titles/descriptions there instead of duplicating loader logic in the component.
- TanStack Router dedupes `title`/`meta` across nested routes automatically (last/child wins) — this dedup does not happen for raw React 19 tags mixed with `head()` output, so don't mix the two approaches on the same route tree.
- Re-check [TanStack/router#3050](https://github.com/TanStack/router/issues/3050) before assuming raw tags are safe — it was open as of Sept 2026.
