---
name: tanstack-react-19-compat
description: "Known React 19 compatibility issues in the TanStack ecosystem (Query, Router, Start, Table, Virtual, Form) and their workarounds. Activate when debugging Suspense/useSuspenseQuery request waterfalls, TanStack Start SSR duplicate title/meta tags, 'Compilation Skipped - incompatible library' warnings from useReactTable/useVirtualizer under the React Compiler, or deciding between React 19 Actions (useActionState) and TanStack Query/Form for a form. Also use when auditing a TanStack Start + React 19 app for these known pitfalls before shipping."
---

# TanStack + React 19 Compatibility

React 19 changed Suspense scheduling and shipped native metadata tags; the TanStack ecosystem has known friction points with both, plus a separate, unrelated friction point with the (opt-in) React Compiler. These are not all "bugs to fix" — some are architecture decisions. Use the table below to find the right rule file.

## Applicability check first

Before applying any rule, check the project's `package.json`:

- `@tanstack/react-query` + `useSuspenseQuery`/`useSuspenseQueries` → suspense waterfall rule applies.
- `@tanstack/react-router` / `@tanstack/react-start` with SSR → head/meta rule applies.
- `babel-plugin-react-compiler` present AND `@tanstack/react-table` and/or `@tanstack/react-virtual` present → compiler-incompatibility rule applies. **No React Compiler dependency = this rule is not applicable; skip it.**
- Any `useActionState`/form `action=` usage alongside `@tanstack/react-form` or Query mutations → architecture rule applies.

## Quick Reference

| Rule | Priority | Status (as of the versions below) | Applies when |
|------|----------|------------------------------------|--------------|
| [`susp-sibling-waterfall`](rules/susp-sibling-waterfall.md) | HIGH | Real, current behavior (React 19 design change, not a bug) | Any `useSuspenseQuery`/`useSuspenseQueries` under Suspense |
| [`head-use-router-meta`](rules/head-use-router-meta.md) | HIGH | Open bug: [TanStack/router#3050](https://github.com/TanStack/router/issues/3050) | TanStack Start/Router SSR apps rendering `<title>`/`<meta>` |
| [`compiler-table-virtual-incompat`](rules/compiler-table-virtual-incompat.md) | MEDIUM (N/A unless React Compiler adopted) | Open/by-design: [TanStack/virtual#736](https://github.com/TanStack/virtual/issues/736), [TanStack/table#6137](https://github.com/TanStack/table/issues/6137) | Only if `babel-plugin-react-compiler` is enabled |
| [`arch-actions-vs-tanstack`](rules/arch-actions-vs-tanstack.md) | LOW (guidance, not a bug) | N/A — architectural | Choosing between `useActionState`/form actions and TanStack Form/Query mutations |

## Versions this was verified against (Sept 2026)

`react`/`react-dom` 19.2.3, `@tanstack/react-query` 5.90.x, `@tanstack/react-router` 1.170.x, `@tanstack/react-start` 1.168.x, `@tanstack/react-table` 8.21.x, `@tanstack/react-form` 1.27.x. Re-check the linked GitHub issues before relying on "open" status — TanStack ships frequently and these may close.

## Sources

- [TkDodo — React 19 and Suspense: A Drama in 3 Acts](https://tkdodo.eu/blog/react-19-and-suspense-a-drama-in-3-acts)
- [TanStack/router#3050 — TanStack Start and React 19 Metadata Tags](https://github.com/TanStack/router/issues/3050)
- [TanStack/virtual#736 — React Compiler: useVirtual results are cached at their initial values](https://github.com/TanStack/virtual/issues/736)
- [TanStack/table#6137 — React Compiler skips memoization for useReactTable()](https://github.com/TanStack/table/issues/6137)
- [react.dev — incompatible-library lint rule](https://react.dev/reference/eslint-plugin-react-hooks/lints/incompatible-library)
- [TanStack Query docs — Suspense](https://tanstack.com/query/latest/docs/framework/react/guides/suspense)
- [TanStack Router docs — Document Head Management](https://tanstack.com/router/latest/docs/framework/react/guide/document-head-management)
