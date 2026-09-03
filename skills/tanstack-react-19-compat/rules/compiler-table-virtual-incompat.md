# compiler-table-virtual-incompat: React Compiler Breaks useReactTable() / useVirtualizer()

## Priority: MEDIUM — only applies if the React Compiler (`babel-plugin-react-compiler`) is enabled

## Not applicable if the project has no React Compiler dependency

This is a React Compiler issue, not a plain React 19 issue — plain React 19 without the compiler is unaffected. Skip this rule entirely unless `babel-plugin-react-compiler` (or `eslint-plugin-react-compiler` enforcing it) is actually wired into the build. Check `package.json`/`vite.config.ts`/babel config first.

## Explanation

`useReactTable()` (`@tanstack/react-table`) and `useVirtualizer()` (`@tanstack/react-virtual`) both return the **same object reference** across renders while mutating its internals (methods, computed virtual items) in place. React Compiler's auto-memoization assumes a stable reference means unchanged output, so it caches the first result and never recomputes — table state stops reflecting sort/filter/pagination changes, and virtualized lists stop updating on scroll.

React's own compiler team has added both libraries to its known-incompatible-library list ([react.dev — incompatible-library lint](https://react.dev/reference/eslint-plugin-react-hooks/lints/incompatible-library)), and the lint should already flag `useReactTable`/`useVirtualizer` usage with "Compilation Skipped: Use of incompatible library" — meaning the compiler often *does* correctly skip memoizing the component itself. Bugs mainly surface when a *derived* value or a *child component* reads mutable methods off the table/virtualizer instance outside the component the compiler already skipped (e.g. `table.getCanNextPage()` called inside a separately-compiled child), where the compiler doesn't know to skip.

- [TanStack/virtual#736](https://github.com/TanStack/virtual/issues/736) — open; `getVirtualItems()` results cached at initial values.
- [TanStack/table#6137](https://github.com/TanStack/table/issues/6137) — closed as expected/by-design behavior of a library with a mutable-instance API.
- [facebook/react#36331](https://github.com/facebook/react/issues/36331) — table methods called from an extracted child component get stale-memoized even though the top-level table hook itself is skipped.

## Bad Example — reading table state in a compiled child component

```tsx
function DataTable() {
  const table = useReactTable({ data, columns, getCoreRowModel: getCoreRowModel() })
  return <Pagination table={table} /> // separate component: compiler may still memoize its output
}

function Pagination({ table }: { table: Table<Row> }) {
  // Can go stale under React Compiler even though DataTable itself was skipped
  return (
    <button disabled={!table.getCanNextPage()} onClick={() => table.nextPage()}>
      Next
    </button>
  )
}
```

## Good Example — opt the affected function out with 'use no memo'

```tsx
function Pagination({ table }: { table: Table<Row> }) {
  const getCanNextPage = () => {
    'use no memo'
    return table.getCanNextPage()
  }
  return (
    <button disabled={!getCanNextPage()} onClick={() => table.nextPage()}>
      Next
    </button>
  )
}
```

Same pattern for virtualized lists:

```tsx
function VirtualList() {
  const virtualizer = useVirtualizer({ count: rows.length, getScrollElement: () => parentRef.current, estimateSize: () => 35 })

  const getVirtualItemsNoMemo = () => {
    'use no memo'
    return virtualizer.getVirtualItems()
  }

  return (
    <div ref={parentRef}>
      {getVirtualItemsNoMemo().map((item) => (
        <div key={item.key} style={{ transform: `translateY(${item.start}px)` }}>
          {rows[item.index]}
        </div>
      ))}
    </div>
  )
}
```

## Rule

- Do not enable the React Compiler on files that read mutable methods/properties off a `useReactTable()` or `useVirtualizer()` instance without verifying the compiler's incompatible-library warning actually fired for every affected component — it can miss child components that merely receive the instance as a prop.
- When the compiler warning does NOT auto-skip a component that reads table/virtualizer state, wrap the specific read in a `'use no memo'`-directived function rather than disabling the compiler for the whole file.
- Re-verify against [TanStack/virtual#736](https://github.com/TanStack/virtual/issues/736) before assuming this is fixed — it was open as of Sept 2026.
