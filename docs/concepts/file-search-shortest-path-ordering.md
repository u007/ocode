---
type: Concept
title: 'File search result ordering: shortest path first'
description: 'How file-name search results are ordered: relevance first, then shortest path as tiebreak, with shared Go/TS helpers and explicit carve-outs for content search.'
tags:
  - file search
  - ordering
  - shortest path
  - TUI
  - web
  - file picker
timestamp: 2026-09-22T03:06:17Z
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

## Gotchas

- **"Relevance first, shortest tiebreak"** is the agreed semantic; do NOT make shortest-path the primary key when a query is present — it would surface weak subsequence matches in shallow files above exact matches in deep ones, regressing the documented relevance ranking (CHANGES.md "Web file search relevance ranking (2026-09-02)").
- **Tests**: `internal/tui/file_search_order_test.go`, `web/src/lib/filePathOrder.test.ts`, and the "FilePicker shortest-path ordering" describe in `web/src/components/Files/FilePicker.test.tsx`. All mutation-verified.
- Desktop uses the embedded web UI, so the web changes cover it; there is no desktop-native file search.

## Cross-links

- [Web file search relevance ranking](../CHANGES.md) — the 2026-09-02 entry that established relevance-first semantics.
- [File search UX](../concepts/file-search.md) — broader file search design context (if present).
