---
type: Concept
title: 'File search result ordering: shortest path first'
description: 'How file-name search results are ordered: relevance first, then shortest path as tiebreak, with shared Go/TS helpers and explicit carve-outs for content search. Amended 2026-10-20: added what the index contains and why a filter can return fewer matches than expected (walk truncation + virtualizer tail clipping), cross-linked to file-tree-walk-budget.md.'
tags:
  - file search
  - ordering
  - shortest path
  - TUI
  - web
  - file picker
  - truncation
  - index
timestamp: 2026-10-19T23:52:25Z
---
# File search result ordering: shortest path first

## Rule

File-name search result lists are ordered by:

1. **Relevance** (fuzzy/keyword score) descending — the primary key **when a query is present**.
2. **Fewest path segments first** (shortest path) — the tiebreak.
3. **Lexicographic** — final deterministic tiebreak.

An unfiltered/empty-query list is ordered shortest-path first (segments asc, then lexicographic).

## Shared helpers (single source of truth — keep Go and TS in sync)

- Go: `internal/tui/path_order.go` → `pathSegmentCount(p string) int`, `lessPathShortest(a, b string) bool`. Also `fuzzyFilterPaths(items []string, query string)` in `internal/tui/fuzzy.go`.
- TS: `web/src/lib/filePathOrder.ts` → `pathSegmentCount(filePath: string)`, `compareByShortestPath(a, b)`. Imported via `@/lib/filePathOrder`.

## Surfaces

### TUI Ctrl+P
`filterFileSearchResults` (`internal/tui/model.go`) — score desc, then `lessPathShortest`; empty query sorted shortest-first (was raw walk order).

### TUI Files tab `/` fuzzy finder
`fuzzyFilterPaths` (new; score desc then shortest path). The generic `fuzzyFilter` used by the session/project picker (`internal/tui/picker.go`) is **deliberately UNCHANGED** (original-index tiebreak).

### Web/Desktop Ctrl+P
`web/src/components/Files/FilePicker.tsx` — filtered results ranked with `scoreMatch` (relevance) then `compareByShortestPath`; empty query `[...files].sort(compareByShortestPath)`.

### Web/Desktop Files tab path filter
`filterTreeNodes` (`web/src/components/Files/FileTree.tsx`) — keeps `scoreMatch` desc and adds a `pathSegmentCount` tiebreak. Siblings share a depth, so this is normally a no-op and preserves the dirs-first order.

## Deliberately NOT changed

Content-search results (TUI Ctrl+F `startContentSearchCmd`; web Content mode `contentResults` SSE stream) still arrive in filesystem walk order. Sorting a streamed batch list would fight the auto-follow-tail behavior and reorder rows mid-search.

## What the index contains — and why a filter can return fewer matches than you expect

The web/desktop picker filters a **server-built index**, not the filesystem
directly. `FilePicker` fetches `GET /api/files/tree?path=<project>&depth=0` once
on open and flattens the returned tree into `tree.files` / `tree.folders`
(`FilePicker.tsx:126`); `filteredFiles` then filters that in-memory list
(`FilePicker.tsx:151`). So the "N matches" heading counts matches *against the
index*, and the index can legitimately be smaller than the project on disk for
two independent reasons:

1. **The server walk is bounded and stops dead.** The walk is capped by
   `maxTreeNodes = 20000` (`internal/server/handler_files.go:1389`) and symlinked
   subtrees by a collective `maxSymlinkTreeNodes = 2000`
   (`handler_files.go:1409`). Once the cap is hit the walk stops, everything
   not-yet-visited is dropped, and the server sets `Truncated: true`. The picker
   then appends " — results may be incomplete (truncated)" to the heading
   (`FilePicker.tsx:209`), and `filteredFiles` only ever sees the truncated
   subset. This is **not pagination** — there is no "next page" to scroll into;
   the missing files are simply absent from the index.

2. **A row-height/virtualizer mismatch clips the rendered tail.** The virtualizer's
   `estimateSize` was `42` while the real row is `44` (`FILE_ROW_HEIGHT`,
   `FilePicker.tsx:65`), and `virtualizer.measureElement` is *not* wired here, so
   `getTotalSize()` was shorter than the real content: the last matches existed
   in the DOM but could never be scrolled into view. Fixed by making
   `estimateSize` equal the exact row height and giving the path span
   `min-w-0 truncate` so a long path can't wrap the row to 64px
   (`FilePicker.tsx:244`).

So a heading that says "18 matches" while scrolling reveals fewer is usually
**reason 2** (the tail is clipped), and "18 matches" while the user believes 30
files exist is usually **reason 1** (the index is truncated and the heading's
suffix says so). Neither is a pagination bug.

Full semantics of the walk, its budgets, symlink handling, and the truncation
flag: see [File tree walk: node budget, symlink budget, and truncation](./file-tree-walk-budget.md).

## Gotchas

- **"Relevance first, shortest tiebreak"** is the agreed semantic; do NOT make shortest-path the primary key when a query is present — it would surface weak subsequence matches in shallow files above exact matches in deep ones, regressing the documented relevance ranking (CHANGES.md "Web file search relevance ranking (2026-09-02)").
- **The heading count is honest about the *index*, not the filesystem.** "18 matches" means "18 of what the server indexed"; the files that really exist may simply be absent because the walk was truncated (see above). Don't conclude "server pagination" — the tree endpoint has no pagination.
- **Tests**: `internal/tui/file_search_order_test.go`, `web/src/lib/filePathOrder.test.ts`, and the "FilePicker shortest-path ordering" describe in `web/src/components/Files/FilePicker.test.tsx`. All mutation-verified.
- Desktop uses the embedded web UI, so the web changes cover it; there is no desktop-native file search.

## Cross-links

- [File tree walk: node budget, symlink budget, and truncation](./file-tree-walk-budget.md) — what the picker's index contains, why it can be truncated, and how the truncation is surfaced.
- [Web file search relevance ranking](../CHANGES.md) — the 2026-09-02 entry that established relevance-first semantics.
- [File search UX](../concepts/file-search.md) — broader file search design context (if present).
