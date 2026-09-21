---
type: Concept
title: 'Git stash UI: list, per-file restore, delete'
description: The Git tab's stash surface and the server endpoints behind it.
tags:
  - git
  - stash
  - ui
  - git-panel
timestamp: 2026-09-20T17:44:59Z
---
# Git stash UI: list, per-file restore, delete

## Overview

The Git tab (`web/src/components/Git/GitPanel.tsx`, shared by web + Wails desktop) gained a collapsible "Stashes" section (persisted in the `ocode.ui.git-panel.v1` localStorage key alongside staged/unstaged/commits) with a "Stash all" button, a stash list, a per-file detail pane with checkboxes + "Restore selected (N)", and a per-entry delete button behind a confirmation dialog.

## Server endpoints (local + remote `?host=`)

Registered in `internal/server/server.go`:

- `GET /api/git/stash/list` → `[]GitStash` (index, ref, hash, short, message, author, date). Implementation: `git stash list --format=%gd%x00%H%x00%h%x00%gs%x00%aI%x00%an`; entries are newest-first.
- `GET /api/git/stash/show?index=N` → `[]GitDiffFile`. Implementation: resolve `stash@{N}` via rev-parse --verify, then `git stash show -p --no-color --include-untracked`, parsed by the shared `parseUnifiedDiff`.
- `POST /api/git/stash/apply {index, paths}` → restores the SELECTED files into the working tree, UNSTAGED (`git restore --source=<hash> --worktree`), returns the refreshed `GitWorkspace`. The stash entry is kept.
- `POST /api/git/stash/drop {index}` → `git stash drop`, returns the refreshed list.
- `POST /api/git/stash` gained an `include_untracked` bool (`git stash push -u`); the FileTree context-menu stash still defaults to tracked-only.

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

## Tests

- Go: `internal/server/handler_git_stash_test.go` (list/show, bad-index 400s, per-file restore including untracked + deleted-in-stash while proving the un-selected files are untouched, empty/unknown-path 400s, drop, include_untracked push, and the remote path via installFakeSSH).
- Web: the `GitPanel stash` describe in `web/src/components/Git/GitPanel.test.tsx`.
