---
type: Concept
title: 'Git stash UI: list, per-file restore, delete'
description: The Git tab's stash surface, per-file/multi-file stash, multi-select, and the server endpoints behind them.
tags: []
timestamp: 2026-09-21T08:22:29Z
---
# Git stash UI: list, per-file restore, delete, stash-from-context-menu

**Type:** Concept  
**Description:** The Git tab's stash surface, per-file/multi-file stash, and multi-select; the server endpoints behind them.  
**Tags:** git, stash, ui, git-panel, filetree  

---

# Git stash UI: list, per-file restore, delete, stash-from-context-menu

## Overview

The Git tab (`web/src/components/Git/GitPanel.tsx`, shared by web + Wails desktop) has:

- A collapsible "Stashes" section (persisted in the `ocode.ui.git-panel.v1` localStorage key alongside staged/unstaged/commits) with a "Stash all" button, a stash list, a per-file detail pane with checkboxes + "Restore selected (N)", and a per-entry delete button behind a confirmation dialog.
- **Per-file / multi-file stash from the row context menus** in both the staged and unstaged panes ("Stash file" / "Stash N files"), in addition to the whole-working-tree "Stash all".
- **Pane-qualified multi-select** on the working-tree lists (Cmd/Ctrl toggle, Shift range select/deselect).

## Per-file / multi-file stash

`POST /api/git/stash` already accepted `{paths, message, include_untracked}` → `git stash push [-m msg] [-u] -- <paths>` (`stashPushArgs` in `internal/server/handler_git_actions.go`). Partial-path stash was already implemented and is what the Files tab used; the Git tab now uses it too.

- **"Stash all"** keeps `paths=[]` and stashes the whole working tree — unchanged semantics.
- **"Stash file" / "Stash N files"** passes the selected row paths as `paths` so only those files are stashed; the others keep their changes.
- The stash dialog adapts its title, body, and copy: "Stash all changes" vs "Stash N file(s)", and renders the targeted path list when `paths` is non-empty. The handler that previously handled "stash all" only (`doStashAll`) is now the general `doStash`, opened with no target (`stashTargets = null`) or pre-targeted via `openStashDialog(paths)` from the row menus.
- Both the **staged** and **unstaged** pane context menus expose the stash item. A partially-staged file appears in both panes; the context menu for it stashes that same path from whichever pane opened it.

## Working-tree list multi-select (shared with Files tab)

The staged and unstaged working-tree lists in GitPanel and the file tree in the Files tab (`web/src/components/Files/FileTree.tsx`) share the same modifier semantics:

| Modifier | Behavior |
|---|---|
| Plain click | Select one row; show its diff |
| Cmd/Ctrl-click | Toggle that row in/out of the selection |
| Shift-click | Range-select between the last anchor and the clicked row; if both anchor and clicked row are already selected, **deselect** the block (repeat-safe) |

Right-click context menus (Stash… in FileTree, Stage/Unstage/Discard in GitPanel) and per-row checkboxes already existed; multi-select made them operate on a set rather than a single row.

### Pane-qualified keys in GitPanel

A partially-staged file appears in **both** the staged and unstaged panes. Selection therefore uses pane-qualified keys (`"staged:<path>"` / `"unstaged:<path>"`) in a `pickedPaths: Set<string>`, with `lastPickedKey` as the Shift anchor. Without pane qualification, a partially-staged file would toggle out of one pane but remain selected in the other, breaking bulk Stage/Unstage/Stash.

Stale picks are pruned by a `useEffect` keyed on `workspace` (the lists are re-fetched on git status changes). Bulk menu items ("Stage N files" / "Unstage N files" / "Stash N files") operate on the whole pane selection; Discard stays single-file because mixed tracked/untracked semantics don't generalize to a batch.

In the Files tab, the same modifier pattern gained `deselectRange(paths)` so Shift-click now selects the block normally but clears it when both the anchor and the clicked node are already selected.

## Server endpoints (local + remote `?host=`)

Registered in `internal/server/server.go`:

- `GET /api/git/stash/list` → `[]GitStash` (index, ref, hash, short, message, author, date). Implementation: `git stash list --format=%gd%x00%H%x00%h%x00%gs%x00%aI%x00%an`; entries are newest-first.
- `GET /api/git/stash/show?index=N` → `[]GitDiffFile`. Implementation: resolve `stash@{N}` via rev-parse --verify, then `git stash show -p --no-color --include-untracked`, parsed by the shared `parseUnifiedDiff`.
- `POST /api/git/stash/apply {index, paths}` → restores the SELECTED files into the working tree, UNSTAGED (`git restore --source=<hash> --worktree`), returns the refreshed `GitWorkspace`. The stash entry is kept.
- `POST /api/git/stash/drop {index}` → `git stash drop`, returns the refreshed list.
- `POST /api/git/stash` → `git stash push [-m msg] [-u] -- <paths>`. `paths=[]` means the whole working tree ("Stash all"). Non-empty `paths` restricts the stash to those files. `include_untracked` bool → `-u`.

## Restore semantics (the non-obvious part)

A stash is a merge-shaped commit: tree = working tree, parents = [HEAD-at-stash-time, index commit, optional untracked commit]. Files untracked at stash time are NOT in the stash tree — they live in the third parent `stash@{N}^3`. So restore partitions requested paths by membership in `ls-tree -r --name-only <hash>` (tracked-in-stash), `<hash>^3` (untracked-in-stash), or `<hash>^1` (present in base but absent from the stash tree = the stash DELETED it; restore removes it from the working tree). A path in none of the three is a 400 ("path not found in stash"), never a silent filesystem change.

- `git restore` requires git >= 2.23.
- The web UI shows an "Overwrite local changes?" confirmation before restoring when a selected file is also present in the current staged/unstaged lists, because `git restore --source` overwrites the working-tree copy silently.

## Security / shape

Stash indices are integers formatted server-side into `stash@{N}`; a caller never supplies a rev string. Remote (SSH/WSL) commands quote the rev and run paths through the existing `remoteSafeSpec` / `remote.ShellQuote` guards.

`prepareGitAction` / `remotePrepareGitAction` were split into a body decoder (`decodeGitAction`) + an already-decoded validator (`prepareGitActionFor` / `remotePrepareGitActionFor`) so the stash handler can read the extra `include_untracked` field while sharing path validation with stage/unstage/discard/commit.

## Gotchas

- `%gs` reflog subject is "WIP on <branch>: <base subject>" or "On <branch>: <message>"; the UI's `stashLabel()` strips everything through the first ": " for display.
- Dropping a stash shifts the indices of the entries below it, so `drop` returns the refreshed list and the panel re-reconciles its selection against it (a dropped/shifted entry clears the detail pane) rather than keeping a stale index.
- The periodic 10s refresh and the `git_status` eventBus refresh must not clobber a user-visible error: the stash list fetch is fail-soft (`.catch(() => [])`) so an older server can't take down the whole workspace refresh.
- Shift-click range selection is **repeat-safe**: if the anchor and the clicked row are both already selected, the block is deselected rather than re-selected. This prevents the "can't unselect a range" trap present in many codebase multi-select implementations.

## Tests

- Go: `internal/server/handler_git_stash_test.go` (list/show, bad-index 400s, per-file restore including untracked + deleted-in-stash while proving the un-selected files are untouched, empty/unknown-path 400s, drop, include_untracked push, `TestGitStashPushSelectedPaths` — two selected files revert to HEAD and land in the stash while an unselected third keeps its change, and the remote path via installFakeSSH).
- Web: the `GitPanel stash` and `GitPanel per-file stash & multi-select` describes in `web/src/components/Git/GitPanel.test.tsx` (per-path stash, staged-pane "Stash file" item, Cmd-click → "Stash 2 files" arguments, Shift-click select→deselect).
- Web: `web/src/components/Files/FileTree.multiSelect.test.tsx` (Cmd-click toggle without opening the file; Shift-click range select→deselect).
