---
name: nextjs-to-tanstack
description: Migration guide from Next.js (Pages/App Router) to TanStack Start with TanStack Router and TanStack Query. Covers converting file-based routing to TanStack Router, replacing Server Components/RSC with TanStack Query, migrating Server Actions to TanStack Query mutations, converting API routes to TanStack Server Functions, and setting up SSR with TanStack Start.
when_to_use: When migrating a Next.js project to TanStack, setting up a new TanStack project, converting Next.js patterns (app router, pages router, server components, server actions, API routes, middleware) to their TanStack equivalents, or adopting TanStack Start for SSR.
version: 1.0.0
---

# Next.js → TanStack Migration Guide

Migrate from Next.js (App Router or Pages Router) to TanStack Start with TanStack Router and TanStack Query.

## Architecture Comparison

| Concept | Next.js | TanStack |
|---------|---------|----------|
| Routing | `app/` or `pages/` file-based | Code-based `createRouter` + `createRoute` |
| Data Fetching | `getServerSideProps`, RSC, `fetch` in components | `@tanstack/react-query` with SSR hydration |
| Server Mutations | Server Actions | TanStack Query `useMutation` calling Server Functions |
| API Routes | `/api/*` route handlers | [TanStack Server Functions](https://tanstack.com/router/latest/docs/framework/react/start/server-functions) |
| Middleware | `middleware.ts` | Route `beforeLoad` hooks |
| Layouts | `layout.tsx` files | `createRoute` with `component` tree nesting |
| Loading UI | `loading.tsx` | `pendingMinMs` / `pendingComponent` |
| Error UI | `error.tsx` | `errorComponent` on routes |
| Search Params | `useSearchParams` | Type-safe `search` validators (zod) |
| SSR | Built-in | TanStack Start (`start` CLI) |
| Static Generation | `generateStaticParams` | TanStack Router static build |
| Auth guards | Middleware + layout checks | `beforeLoad` with `context` injection |

---

## Quick Start: New TanStack Project

```bash
npm create @tanstack/start@latest my-app
cd my-app
npm install @tanstack/react-router @tanstack/react-query
```

---

## Migration by Feature

### 1. Routing: `app/` → TanStack Router

**Next.js (file-based):**
```
app/
  layout.tsx
  page.tsx
  dashboard/
    layout.tsx
    page.tsx
    settings/
      page.tsx
```

**TanStack Router (code-based):**
```tsx
// src/router.tsx
import { createRouter, createRoute, createRootRoute } from '@tanstack/react-router'

const rootRoute = createRootRoute({
  component: () => <RootLayout />,
})

// / route
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: HomePage,
})

// /dashboard layout + routes
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'dashboard',
  component: DashboardLayout,
})

const dashboardIndexRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/',
  component: DashboardHome,
})

const settingsRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: 'settings',
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  dashboardRoute.addChildren([dashboardIndexRoute, settingsRoute]),
])

const router = createRouter({ routeTree })
```

### 2. Layouts: `layout.tsx` → Route Nesting

**Next.js:**
```tsx
// app/dashboard/layout.tsx
export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <div>
      <Sidebar />
      <main>{children}</main>
    </div>
  )
}
```

**TanStack:**
```tsx
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'dashboard',
  component: DashboardLayout,
})

function DashboardLayout() {
  return (
    <div>
      <Sidebar />
      <main><Outlet /></main>
    </div>
  )
}
```

### 3. Data Fetching: Server Components → TanStack Query

**Next.js (Server Component):**
```tsx
// app/users/page.tsx
async function UsersPage() {
  const users = await db.users.findAll()
  return <UserList users={users} />
}
```

**TanStack (client + SSR hydration):**
```tsx
// src/routes/users.tsx
import { queryOptions, useSuspenseQuery } from '@tanstack/react-query'

const usersQuery = queryOptions({
  queryKey: ['users'],
  queryFn: async () => {
    const res = await fetch('/api/users')
    return res.json()
  },
})

// In route component
function UsersPage() {
  const { data: users } = useSuspenseQuery(usersQuery)
  return <UserList users={users} />
}
```

**With SSR hydration (TanStack Start):**
```tsx
// router.tsx
import { createRouter, defer } from '@tanstack/react-router'

const usersRoute = createRoute({
  path: 'users',
  loader: async ({ context: { queryClient } }) => {
    await queryClient.prefetchQuery(usersQuery)
  },
  component: UsersPage,
})
```

### 4. Server Mutations: Server Actions → TanStack Query Mutations

**Next.js (Server Action):**
```tsx
// app/actions.ts
'use server'
export async function createUser(data: FormData) {
  const user = await db.users.create({ name: data.get('name') })
  revalidatePath('/users')
  return user
}
```

**TanStack:**
```tsx
// src/api/users.ts
import { serverFn } from '@tanstack/react-query' // or plain fetch

async function createUser(name: string) {
  const res = await fetch('/api/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return res.json()
}

// In component
function CreateUserForm() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: createUser,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
    },
  })

  return (
    <form onSubmit={(e) => {
      e.preventDefault()
      mutation.mutate(name)
    }}>
      <input name="name" />
      <button type="submit">Create</button>
    </form>
  )
}
```

### 5. API Routes: `/api/*` → Server Functions

**Next.js:**
```tsx
// app/api/users/route.ts
export async function GET() {
  const users = await db.users.findAll()
  return Response.json(users)
}
```

**TanStack (Server Functions):**
```tsx
// src/server/users.ts
import { createServerFn } from '@tanstack/react-start/server'

export const getUsers = createServerFn({ method: 'GET' }).handler(async () => {
  const users = await db.users.findAll()
  return users
})

// Call from client:
const users = await getUsers()
```

### 6. SSR Data Loading: Loader Pattern

**TanStack Router loader with query client:**
```tsx
import { queryClient } from './queryClient'

const usersRoute = createRoute({
  path: 'users',
  loader: async () => {
    return await queryClient.ensureQueryData({
      queryKey: ['users'],
      queryFn: () => fetchUsers(),
    })
  },
  component: UsersPage,
})

// Use loaded data:
function UsersPage() {
  // Route loader already fetched; useSuspenseQuery reads cache
  const { data } = useSuspenseQuery(usersQuery)
  // ...
}
```

### 7. Loading States: `loading.tsx` → pendingComponent

**TanStack:**
```tsx
const usersRoute = createRoute({
  path: 'users',
  pendingMinMs: 300, // Show spinner only if load takes >300ms
  pendingComponent: () => <Spinner />,
  component: UsersPage,
  loader: async () => { ... },
})
```

### 8. Error States: `error.tsx` → errorComponent

**TanStack:**
```tsx
const usersRoute = createRoute({
  path: 'users',
  errorComponent: ({ error }) => (
    <div>Failed to load: {error.message}</div>
  ),
  component: UsersPage,
})
```

### 9. Middleware: `middleware.ts` → `beforeLoad`

**Next.js:**
```ts
// middleware.ts
export function middleware(request) {
  if (!request.cookies.get('session')) {
    return Response.redirect('/login')
  }
}
```

**TanStack:**
```tsx
const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'protected',
  beforeLoad: async ({ context: { auth }, location }) => {
    if (!auth.isAuthenticated) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
  },
})
```

### 10. Search Params: `useSearchParams` → Type-safe Validators

```tsx
import { z } from 'zod'

const usersRoute = createRoute({
  path: 'users',
  validateSearch: z.object({
    page: z.number().optional().default(1),
    sort: z.enum(['name', 'date']).optional().default('date'),
    q: z.string().optional(),
  }),
  component: UsersPage,
})

function UsersPage() {
  const { page, sort, q } = usersRoute.useSearch()
  // All type-safe!
}
```

---

## Breaking Changes Checklist

- [ ] `next/navigation` → `@tanstack/react-router`
- [ ] `next/link` → TanStack Router `<Link>`
- [ ] `next/image` → plain `<img>` or a zero-config image component
- [ ] `next/head` → `<head>` in root route or meta frameworks
- [ ] `getServerSideProps` / `getStaticProps` → TanStack Router `loader`
- [ ] `revalidatePath` / `revalidateTag` → `queryClient.invalidateQueries`
- [ ] `generateStaticParams` → TanStack Router static config
- [ ] Server Components → Client components with TanStack Query
- [ ] `next.config.js` → Vite config via `tanstack/start/config`
- [ ] `public/` → `public/` works the same in Vite
- [ ] `.env.local` → `.env` (loaded by Vite automatically)

---

## See Also

- [TanStack Router Best Practices](../tanstack-router-best-practices/SKILL.md) — Router patterns
- [TanStack Query Best Practices](../tanstack-query-best-practices/SKILL.md) — Query patterns
- [TanStack Start Docs](https://tanstack.com/start/latest)
