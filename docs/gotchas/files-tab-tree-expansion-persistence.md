---
type: Gotcha
title: 'Files tab: directory expansion persists across project switches — but only after the fix'
description: Why tree expansion was lost on project switch/restart and how it is now persisted (and why missing folders no longer break it).
tags:
  - web
  - files
  - file tree
  - persistence
  - gotcha
timestamp: 2026-09-20T17:50:30Z
---
## Problem

In the web/desktop **Files** tab, expanding folders in the file tree reset whenever the user switched project — and on app restart.

## Root cause (the non-obvious part)

Each `TreeNode` (`web/src/components/Files/FileTree.tsx`) held its expansion in **local `useState`**. On a project/root switch, `loadRoot` calls `setLoading(true)`, and the tree JSX is

```jsx
loading ? "Loading…" : tree.map(...)
```

so every `TreeNode` **unmounts** and its expansion state is destroyed. Note the existing `refresh()` path deliberately does **not** toggle `loading`, which is why expansion survived delete/git refresh but not a project switch. There is no `key` on `<FileTree>` in `App.tsx`, so the component itself stays mounted — the loss is the `TreeNode` unmount.

## Fix: persistence layer

- **`web/src/components/Files/fileTreeExpansionPersistence.ts`** — localStorage key `ocode.ui.fileTreeExpansion.v1`, shape `{version:1, roots: Record<key, string[]>}`. Key = `host + "\u0000" + root` where root = `activeRoot ?? projectPath`. Host is in the key because two remote hosts can expose the same relative path (`~/www/app`); active root is in the key because extra allowed paths share node paths. `loadExpandedDirs` returns a `Set`; `saveExpandedDirs` caps at 2000 entries and deletes the entry when empty. Malformed/blocked storage degrades to empty.
- **`web/src/components/Files/FileTree.tsx`** — derives `treeRootKey`, seeds + reloads `expandedDirs` from it, and saves on toggle. `TreeNode` gained optional `expandedPaths`/`onToggleExpanded` props: it seeds `expanded` from the set, re-syncs when the set identity changes (`useEffect` on `expandedPaths`), and reports toggles. The force-expanded filter mode (`forceExpanded`) opts out of persistence.

## Missing-folder contract

A persisted expansion can outlive a folder deleted/renamed while another project was active. Previously, expanding it hit `if (!res.ok) throw` → `console.error("File tree children error:")` and left a stuck expanded node. Now:

- **`internal/server/handler_files.go`** returns **HTTP 404** (not 500) when the requested subtree `os.Stat`s as not-exist — `buildFileTree`'s stat error used to surface as a generic 500.
- The frontend treats any non-ok child fetch as a stale entry: **collapses the node, prunes the persisted path via `onToggleExpanded(path, false)`, and logs `console.warn`** (never throws / never `console.error`). Remote hosts already returned an empty tree for a missing dir.

## Tests

`web/src/components/Files/fileTreeExpansionPersistence.test.ts`, `web/src/components/Files/FileTree.expansionPersistence.test.tsx` (restore-without-click across project switch A→B→A; 404'd persisted folder collapses + is pruned with no error), `internal/server/handler_files_test.go` `TestHandleFileTreeMissingDirReturns404`.
