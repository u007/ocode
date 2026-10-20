# Plan — Fix Ctrl/Cmd+P file search: truncated file list + unreachable tail rows

**STATUS: implemented and live-verified.**

## Problem (user report)

> "Ctrl or Command P file search … shows that I filter it down to like 18 files.
> But when I scroll, it's not even 18, it's less than that. Seems like the
> pagination is not working … when I scroll down, it doesn't show more files."

## User Expectation Checklist — status

| # | Expectation | Status | Evidence |
|---|---|---|---|
| 1 | Every indexed file is reachable by scrolling (scroll count == heading count) | ✅ | Live: `ocode` + "util" → promised 18, any-visible 18, fully-visible 18 |
| 2 | No file that exists on disk is silently absent | ✅ | aimsai2 root files (`README.md`, `package.json`, `CLAUDE.md`) are now indexed; before: ZERO root files |
| 3 | Root-level files always indexed | ✅ | aimsai2 response went from 0 → 22 root files |
| 4 | The last row is fully reachable | ✅ | live `fully-visible == promised` on both engines |
| 5 | Rows have a uniform height | ✅ | live `rowHeights == [44]` for every query |
| 6 | The user is told when the result set is incomplete | ✅ | heading now carries "— results may be incomplete (truncated)" |
| 7 | Web and desktop both fixed | ✅ | one shared component/handler; verified on Chromium **and** WebKit (the desktop engine) |
| 8 | Remote (SSH/WSL) path keeps working | ✅ | remote `find` pipeline never followed symlinks (`-L` absent) — no change needed; verified `remoteWalkTree` untouched |
| 9 | Existing suites stay green | ✅ | `go test ./internal/server/` 249 s green; web suite 3828/3829 (the 1 failure is pre-existing, proven below) |

## Root causes (measured, not guessed)

Measured live with headless Chromium **and** WebKit against a fresh server build
and the running desktop app.

### RC1 — one symlinked directory could eat the whole node budget

`internal/server/handler_files.go`

- `maxTreeNodes = 20000` caps the depth=0 walk; the walk stops DEAD at the cap.
- `buildFileTree` used `os.Stat`, which FOLLOWS symlinks, and the sibling loop
  explicitly reclassified a symlink-to-directory as a directory and descended
  into it.
- Measured with the server's own ignore rules:

  | project | follow symlinks | no follow | cap |
  |---|---|---|---|
  | `www/aimsai2` | **43327** | 2540 | 20000 |
  | `www/ocode` | 7898 | 7897 | 20000 |
  | `www/kakiit` | 6340 | 6340 | 20000 |

- `aimsai2/photos` is a symlink to `~/gdrive/aimsai/photos` (40731 files). It
  alone consumed 17869 of the 20000-node budget.
- Because the walk stops dead, every not-yet-visited sibling subtree was
  dropped entirely **and the root's own file entries were never reached**: the
  aimsai2 response contained **zero root-level files** (`README.md`,
  `package.json`, `CLAUDE.md`, `docker-compose.yml` all absent).
- 19738 files returned of 177489 on disk — 175569 missing.

### RC2 — the walk visited directories before files

Even without symlinks, a directory visited first could swallow the remaining
budget and starve the files after it — including a directory's own files.
Files are now visited first; the response order is unchanged (the children are
re-sorted for display).

### RC3 — the virtualizer's row-height estimate was wrong and never corrected

`web/src/components/Files/FilePicker.tsx`

- `estimateSize: () => 42` but the real row height is **44px** (cmdk item
  `py-3` = 12+12 + 20px line).
- The path `<span className="font-mono text-sm">` had **no truncate/overflow**,
  so long paths **wrapped** — measured **64px** rows.
- `virtualizer.measureElement` was never wired (only `ChatPanel.tsx:1793` uses
  it), so the estimate was never corrected and `getTotalSize()` stayed
  `count × 42`.
- Consequence: the scroll extent was shorter than the real content, so the tail
  of the list was clipped — the last matches existed in the DOM but could never
  be scrolled into view.

### RC4 — 300px viewport (contributing, not the bug)

`web/src/components/ui/command.tsx` — `CommandList` has `max-h-[300px]`, so
only ~7 rows are visible at once. Unchanged (intended design, same as Cmd+K).

## Fixes implemented

### F1 — server: a collective budget for symlinked subtrees

`internal/server/handler_files.go`

- New `fileTreeWalk` state (`visited`, `limit`, `symlinkVisited`, `truncated`).
- A symlinked directory is still FOLLOWED (the Files tab and the picker both
  let the user open one) but gets its own budget: `maxSymlinkTreeNodes` (2000)
  in TOTAL across all symlinks in the walk. It can no longer starve the tree.
- A linked folder is still openable on demand: expanding it issues its own
  request whose root is that folder, and that request is not bounded by this
  budget.
- The `Truncated` flag is now driven by the walk's own state.

### F2 — server: files are visited before directories

Every file in a directory is indexed before any subtree competes for the
budget. The response order is unchanged (children are re-sorted for display:
directories first, then name-sorted).

### F3 — client: uniform row height

`web/src/components/Files/FilePicker.tsx`

- The path span is `min-w-0 truncate` so a long path cannot wrap the row.
- `estimateSize: () => FILE_ROW_HEIGHT` (44) matches the real row height
  exactly, so `getTotalSize()` is the exact content height and the last row is
  reachable.

### F4 — client: surface truncation in the UI

The `truncated` flag the server already sent is now shown in the group heading
("— results may be incomplete (truncated)"), reusing the wording already in
`FileTree.tsx:2131`, instead of a `console.warn` the user never sees.

## Files changed

| File | Change |
|---|---|
| `internal/server/handler_files.go` | `fileTreeWalk` state; collective symlink budget; files-before-directories visit order; `Truncated` from walk state |
| `internal/server/handler_files_symlink_test.go` | NEW — 4 Go regression tests |
| `web/src/components/Files/FilePicker.tsx` | `FILE_ROW_HEIGHT`; `truncate` on the path; `estimateSize` 44; truncation hint |
| `web/src/components/Files/FilePicker.rowHeight.test.tsx` | NEW — 3 web regression tests |

**Not changed:** `internal/server/handler_remote_files.go` — its remote
`find` pipeline never followed symlinks (`-L` is absent), so it never had
RC1. Verified `remoteWalkTree` is unaffected.

## Validation performed

1. `go build ./internal/server/`, `go vet`, `gofmt -l` — clean.
2. `go test ./internal/server/ -count=1` — **249 s, green** (full package).
3. `cd web && bun check .` — "No type errors in 731 files".
4. `cd web && pnpm vitest run` — **3828/3829 pass**. The 1 failure
   (`src/App.folderPick.test.tsx`) is **pre-existing**: it fails identically
   with the ORIGINAL `FilePicker.tsx` restored (`git show HEAD:`), with an
   unrelated cause (`eventBus.onHostsChange is not a function` — an
   incomplete eventBus mock in that test).
5. **Live end-to-end**, headless Chromium AND WebKit, against a fresh build:
   - `ocode` + "util" → promised 18, any-visible 18, **fully-visible 18**,
     `rowHeights == [44]`.
   - `aimsai2` + "package" → 512/512; + "readme" → 4/4; + "index" → 23/23.
   - aimsai2 root files now indexed (README.md, package.json, …): 0 → 22.
   - The aimsai2 tree walk dropped from **2.0 s → 0.30 s** (the symlinked
     40731-file tree is no longer walked in full).
6. **Mutation-verified** — every fix reverted and the new test observed the
   regression (each mutant compiled first):
   | Mutant | Test that caught it |
   |---|---|
   | symlink subtree gets the whole tree budget | `TestBuildFileTreeSymlinkedDirIsBounded` (fails: "sub/nested.txt missing from tree; the symlinked subtree starved the walk") |
   | visit order reverted to directories-first | `TestBuildFileTreeIndexesRootFilesBeforeSubtrees` (fails: "zzz1.txt missing from tree") |
   | symlink budget made per-link instead of collective | `TestBuildFileTreeSymlinkBudgetIsCollective` (fails: "symlinked subtrees contributed 6000 nodes, want <= 2000") |
   | `estimateSize` reverted to 42 | `FilePicker.rowHeight.test.tsx` (fails: "expected 1680 to be 1760") |
   | `truncate`/`min-w-0` removed from the path | `FilePicker.rowHeight.test.tsx` (fails: "expected 'font-mono text-sm' to contain 'truncate'") |
   | truncation hint removed | `FilePicker.rowHeight.test.tsx` (fails: "tells the user the result set is incomplete") |

## Remaining known limitation (pre-existing, unchanged)

`aimsai2` still reports "results may be incomplete": its `photos` symlink
contributes 2000 of the 2000-node symlink budget, so the tree is marked
truncated even though every file in the PROJECT (4539 nodes, well under
`maxTreeNodes`) is now indexed. The flag is conservative — it reports that the
symlinked subtree was clipped, not that the project is missing files. The
picker now surfaces it instead of hiding it.

`/Users/james/www` (111316 real nodes) still exceeds `maxTreeNodes` and is
genuinely truncated; that is the cap doing its job, and it is now visible in
the UI.
