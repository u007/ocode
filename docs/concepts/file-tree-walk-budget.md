---
type: Concept
title: 'File tree walk: node budget, symlink budget, and truncation'
description: How GET /api/files/tree caps its walk (maxTreeNodes, collective maxSymlinkTreeNodes), visits files before directories, detects symlink cycles, and surfaces truncation — and why a filter can legitimately return fewer matches than the user expects.
tags:
  - file search
  - file picker
  - tree walk
  - symlink
  - truncation
  - server
  - web
  - budget
timestamp: 2026-10-19T23:52:11Z
---
# File tree walk: node budget, symlink budget, and truncation

## What this page covers

`GET /api/files/tree?path=<root>&depth=0` is the endpoint behind the web/desktop
file picker (Ctrl/Cmd+P) and the Files tab. `depth=0` asks the server for the
**full tree** so files nested deep in a project are searchable, not just the
shallow ones. A full walk of an arbitrary project can be enormous, so the walk
is bounded — and the way the bounds interact with symlinks is exactly why a
search can report "18 matches" but show fewer rows (see
[What the index contains](#what-the-index-contains-and-why-a-filter-can-return-fewer-matches-than-the-user-expects)).

The implementation lives in `internal/server/handler_files.go`. The key pieces:

| Constant / type | Location | Meaning |
| --- | --- | --- |
| `maxTreeNodes = 20000` | `handler_files.go:1389` | Total node cap for one whole-tree walk. |
| `maxSymlinkTreeNodes = 2000` | `handler_files.go:1409` | Collective cap on nodes contributed by symlinked subtrees. |
| `fileTreeWalk` | `handler_files.go:1412` | Shared per-walk state (`visited`, `limit`, `symlinkVisited`, `truncated`, `ancestors`). |
| `buildFileTree` | `handler_files.go:1442` | The recursive walk itself. |

The response's `Truncated` field is set from `walk.truncated`
(`handler_files.go:144`) — previously it was computed as `count >= maxTreeNodes`,
which counted only the raw node total and could not express the symlink-budget
and cycle cases below.

## The node cap (`maxTreeNodes`)

A single `depth=0` walk may visit at most `maxTreeNodes = 20000` nodes. The
budget is consumed as the walk proceeds and the walk **stops dead** when it is
exhausted (`handler_files.go:1527`). This is a liveness guard, not pagination:
there is no way to ask the endpoint for "the next 20000" — once truncated, the
tree is simply incomplete and the frontend is told so via `Truncated`.

Because the budget stops the walk dead, **order of visitation decides what gets
indexed**. Two consequences follow directly:

1. **Files are visited before directories** (see below), so a directory can't
   swallow the remaining budget and starve the files that come after it.
2. **A symlinked directory is capped separately** (see below), so one link to a
   huge external tree can't spend the whole budget.

## The collective symlink budget (`maxSymlinkTreeNodes`)

`os.Stat` follows symlinks, so a project that symlinks a shared asset directory
(a photo library, a monorepo package, a vendored tree) pulls the whole external
tree into the walk. Before the fix, one such link consumed the entire node
budget; the walk then stopped dead and **every not-yet-visited sibling subtree
was dropped — including the project root's own files**, which became
unsearchable.

Measured on a real project (`~/www/aimsai2`, whose `photos` entry symlinks a
gdrive directory of 40731 files): the index held 19738 of 177489 files and
**zero** root-level files — `README.md`, `package.json`, and `CLAUDE.md` were all
missing from the picker.

The fix gives symlinked subtrees a **collective** budget of
`maxSymlinkTreeNodes = 2000` (`handler_files.go:1560`). A symlinked directory is
still *followed* — the Files tab can expand one and the picker lists its
contents — but *all* symlinked subtrees together may contribute at most 2000
nodes, no matter how many links a project has. The bulk of the node budget is
thereby reserved for the project's own files.

A linked folder is also openable **on demand**: expanding it issues its own
request whose root is that folder, and that request is *not* bounded by this
budget (it only faces `maxTreeNodes`, now against the folder's own subtree).
So the budget caps what an *implicit* full-project walk pulls in through links,
not what a user can explicitly open.

## Files are visited before directories

Within a directory, the walk visits **files first, then directories**, each
name-sorted (`handler_files.go:1506`, the `visit` slice). Since the budget stops
the walk dead, a directory visited first could swallow the remaining budget and
starve the sibling files after it. Visiting a directory's files first guarantees
every file in that directory is indexed before any subtree competes for the
budget — so a project root's own files (`README.md`, `package.json`, …) are
always searchable even in a huge tree.

The **response order is unchanged**: after the visit, children are re-sorted for
display as directories-first then name-sorted (`handler_files.go:1608`). The
frontend renders the tree in server order, so the visit order must not leak into
the response.

## Symlink cycle detection

A symlink that points at one of its own ancestors (`a/loop -> a`) is a **cycle**.
Descending into it would recurse until the budget ran out and index the same
subtree dozens of times under ever-deeper paths (`a/loop/loop/.../file.txt`).

The `fileTreeWalk.ancestors` set holds the resolved real paths
(`filepath.EvalSymlinks`) of the directories on the current path. When a symlink's
resolved target is already an ancestor, the link is **LISTED but not descended
into** (`handler_files.go:1570-1585`). Before the fix the walk recursed to depth
25 and indexed one file 26 times; now it visits 5 nodes and the tree is 2 deep.

## Truncation is surfaced, not hidden

The server has always sent `truncated`; the picker only did `console.warn`.
`web/src/components/Files/FilePicker.tsx` now stores it in `treeTruncated`
(`FilePicker.tsx:84`, set at `FilePicker.tsx:125`) and appends
" — results may be incomplete (truncated)" to the `CommandGroup` heading
(`FilePicker.tsx:209`) — the same wording as the Files-tab hint
(`web/src/components/Files/FileTree.tsx` ~line 2131).

## The remote (SSH/WSL) path is unchanged

`internal/server/handler_remote_files.go` (`remoteWalkTree`, line 227) never had
this bug: its remote `find` pipeline does **not** follow symlinks (no `-L`), so
a symlinked directory is listed as an entry but its external tree is never
pulled into the walk. The budget semantics above are local-walk-only.

## Known limitations

- `/Users/james/www` still exceeds the 20,000-node cap and is genuinely truncated
  (111316 real nodes) — the cap working as intended, now visible in the UI.
- `~/www/aimsai2` still reports "truncated" because its `photos` symlink hits the
  2000-node symlink budget, even though every file in the project itself
  (4539 nodes) is indexed — the flag is deliberately conservative.

## Tests

- `internal/server/handler_files_symlink_test.go` —
  `TestBuildFileTreeSymlinkedDirIsBounded`,
  `TestBuildFileTreeSymlinkBudgetIsCollective`,
  `TestBuildFileTreeIndexesRootFilesBeforeSubtrees`,
  `TestHandleFileTreeReportsTruncation`.
- `internal/server/handler_files_symlink_cycle_test.go` —
  `TestBuildFileTreeHandlesSymlinkCycle`,
  `TestBuildFileTreeContainsSymlinkedSubtree`.
- `web/src/components/Files/FilePicker.rowHeight.test.tsx`.

All mutation-verified.

## Cross-links

- [File search result ordering: shortest path first](./file-search-shortest-path-ordering.md) — the ordering applied to the picker's *filtered* results, and what the index contains.
