---
type: Concept
title: 'Git worktrees: list, open, create, remove (web Git tab)'
description: The Git tab's Worktrees section, the three /api/git/worktree endpoints behind it, and the safety rules that keep create and remove from touching the wrong checkout.
tags:
  - git
  - worktree
  - web
  - git-panel
timestamp: 2026-10-31T01:18:40Z
---
# Git worktrees: list, open, create, remove (web Git tab)

**Type:** Concept
**Scope:** local projects only (remote SSH/WSL projects get 501 and a notice)

## Overview

The Git tab (`web/src/components/Git/GitPanel.tsx`, shared by the web app and the desktop shell) has a collapsible **Worktrees** section, rendered by `web/src/components/Git/WorktreesSection.tsx`. It lists the checkouts of the project's repository and offers four actions:

- **Open**: register the checkout as a project and make it the active one. Its sessions, terminals and Git state then belong to that checkout.
- **New**: check out a branch into `.worktrees/<branch>` under the main checkout.
- **Remove**: delete a linked checkout after a confirmation dialog. The branch is kept.
- Rows show a `main` badge for the main checkout, `open` for the checkout the project is on, `locked` for locked checkouts and `missing` for checkouts whose directory is gone.

## Endpoints

Registered in `internal/server/server.go:341-343` behind `s.authMiddleware`. Handlers live in `internal/server/handler_git_worktrees.go`.

| Method and path | Handler | Body | Returns |
|---|---|---|---|
| `GET /api/git/worktrees?project=` | `HandleGitWorktreeList` (`:126`) | — | `[]GitWorktree` |
| `POST /api/git/worktree?project=` | `HandleGitWorktreeCreate` (`:153`) | `{"branch": "..."}` | refreshed `[]GitWorktree` |
| `POST /api/git/worktree/remove?project=` | `HandleGitWorktreeRemove` (`:236`) | `{"path": "..."}` | refreshed `[]GitWorktree` |

`GitWorktree` is declared at `internal/server/handler_git_worktrees.go:18`. The TypeScript mirror is `web/src/api/types.ts:855`. Client methods are `gitWorktrees`, `gitWorktreeCreate` and `gitWorktreeRemove` in `web/src/api/client.ts:2141-2154`.

Listing parses `git worktree list --porcelain` in `parseGitWorktreeList` (`handler_git_worktrees.go:36`) and then `gitWorktreesForDir` (`:76`). Bare entries are skipped. Git always prints the main checkout first, so `IsMain` marks the first record even when a later record is skipped. The result is sorted with the main checkout first, then by path. `IsCurrent` compares each path to `git rev-parse --show-toplevel` through `samePath` (`:102`).

## Create rules

- The branch name goes through `git check-ref-format --branch`. The command echoes a valid name unchanged, so comparing the echo rejects a leading `-` and `@{-N}`, which would otherwise expand to another branch.
- The checkout path is built on the server as `<main checkout>/.worktrees/<branch>`. The caller never supplies a filesystem path.
- An existing **empty** directory at the target is accepted, since git accepts it too. Anything else there is a 409.
- An existing local branch is checked out. A new branch is forked from the **open project's** `HEAD`, not the main checkout's, because the command runs in the main checkout (`handler_git_worktrees.go:204-218`).
- Before creating, `ensureWorktreesDirExcluded` (`:335`) appends `.worktrees/` to the repository's `info/exclude` if it is not already there. That file is local and never committed, so `git status` stays clean without touching the project's `.gitignore`. The path comes from `git rev-parse --git-path info/exclude`, so it resolves for linked checkouts too.
- A repository with no main checkout (bare, with a linked checkout) is refused with 400 rather than nesting `.worktrees/` inside a linked checkout.

## Remove rules

- The path must match a listed checkout (`samePath`, so `/var` and `/private/var` spellings match). Unknown paths are 400.
- The main checkout is refused with 400.
- The checkout the project is open on is refused with 409 (`IsCurrent`). Switch to another project first.
- A missing directory (`Prunable`) runs `git worktree prune`, which drops the stale administrative entry. Any other checkout runs `git worktree remove <path>` **without** `--force`, so git refuses a dirty or locked checkout and the refusal comes back as 409 with git's message.
- After a successful remove, `removeEmptyWorktreeParents` (`:292`) deletes now-empty parent directories left by nested branch names such as `feature/x`. It stops at `.worktrees/` and uses `os.Remove`, so it can never delete a non-empty directory.

## Project scoping

Mutations resolve the project through `mutationProjectDir`, the same gate every other git write uses (a registered root or an extra allowed path). The read endpoint uses `gitProjectDir`. Using the read resolver for writes would let an unregistered repository be mutated. Both are checked by `TestGitWorktreeMutationsRejectUnregisteredProject`.

A linked checkout gets its own project identity. `paths.ProjectRoot` (`internal/paths/paths.go:67`) resolves through `git rev-parse --show-toplevel`, and `ProjectSlug` (`:85`) hashes that root. So opening a worktree gives it separate sessions rather than sharing the main repository's.

The project **list** is the one place linked worktrees are grouped. `GET /api/projects` (`HandleListProjects`) returns each local project with a derived `worktree_of` field, the main checkout's path, set by `withWorktreeParents` in `internal/server/project_worktree_parent.go`. Each project is probed with `git rev-parse --path-format=absolute --git-dir --git-common-dir` under a 3 s per-project deadline (`projectWorktreeProbeTimeout`), and results are cached for 60 s (`projectWorktreeCacheTTL`). `worktree_of` is never persisted, so it cannot go stale on disk. A probe failure, a main checkout and a remote project all yield no parent. In the web sidebar, `buildProjectSidebarOrder` (`web/src/components/Layout/ProjectSidebar.tsx`) folds a linked worktree under its main checkout when that checkout is also a saved project, and `ProjectWorktreeTree` renders the sub tree: an `original` row for the main checkout, then one row per worktree. A project with no saved worktrees gets no sub tree, and a worktree whose main checkout is not saved stays top-level. The collapsed rail flattens each parent followed by its worktrees. The grouping changes only the list: each checkout still has its own sessions.

## Web behaviour

- **Collapsed by default.** The `worktrees` key in `ocode.ui.git-panel.v1` defaults to `false` (`GitPanel.tsx`, `DEFAULT_SECTIONS`). The list is fetched only once the section is expanded, so projects that never use worktrees pay nothing.
- **Keyed by project.** `GitPanel.tsx:1501` mounts the section with `key` set to the host and project path, so switching projects discards the old list instead of briefly showing it.
- **Loaded separately.** The list is not part of the workspace refresh `Promise.all`. A failed list shows an inline error in the section and cannot break the Files or Changes panes.
- **Open is optional.** `GitPanel` takes `onOpenWorktree` as an optional prop (`GitPanel.tsx:245`). When it is omitted the list still shows and the Open action is hidden. The app wires it at `web/src/App.tsx:1695` to `openProjectPath`.
- **Open registers, then selects.** `openProjectPath` (`web/src/stores/projectStore.tsx:1027`) calls `POST /api/projects` (idempotent on the server), reloads the project list, and selects the entry for that path. It rejects on failure instead of swallowing the error the way `addProject` does, so the section can show the reason.
- Create and remove use a dialog and a `ConfirmDialog`. A rejected request keeps the dialog open with git's message.

## Tests

- Go: `internal/server/handler_git_worktrees_test.go`. Covers list parsing (including a bare main), create, remove, the dirty-checkout refusal, an existing branch, invalid branch names, the main and current-checkout refusals, the remote 501, idempotent exclude writes, the unregistered-project gate and the bare-main refusal.
- Web: `web/src/components/Git/WorktreesSection.test.tsx` (13 cases). Covers collapsed-no-fetch, markers, remove restrictions, open and its failure, the optional Open action, remote notice, load error, create and its refusal, and remove with and without a refusal.

**Not yet covered by a test:** the prune branch for missing checkouts, `samePath` symlink matching, empty-parent cleanup, the base-`HEAD` fork, and accepting an existing empty target directory. They are tracked in `TODO.md`.

## Gotchas

- `.worktrees/` is gitignored in this repository (`.gitignore`), but most user repositories don't ignore it. The `info/exclude` write is what keeps those repositories clean. Do not replace it with a `.gitignore` edit, which would dirty the user's tracked files.
- Orchestrator pipeline checkouts (`internal/orchestrator/worktree.go`, `.worktrees/orchestrator-<runID>/`) show up in this list too. They are real checkouts and can be removed here like any other.
- Remote projects are not served here: `?host=` answers 501 before anything resolves, so a remote request never falls through to this machine's repository.

## Open items

Tracked in `TODO.md` under "Git worktrees follow-ups (2026-10-15)": remote-project support, removing the saved project entry that outlives a removed checkout, pagination of the list, and the missing tests listed above.
