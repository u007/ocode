---
type: Concept
title: Web Git history graph
description: 'The Git tab History sub-view, its commit graph and ref-action context menu, the /api/git/log and /api/git/ref-action contract, and the invariants for changing them'
resource: internal/server/handler_git_refs.go
tags: [web, git, history, graph, context-menu]
timestamp: 2026-11-02T02:53:09Z
status: active
---
# Web Git history graph

## What it is

The Git tab has two sub-views, switched by `GitSubTabs`: **Changes** (the existing file panel) and **History** (`GitHistoryPanel`). History shows the commit log as a graph, a synthetic "Uncommitted changes" row when the workspace is dirty, the diff of the selected commit, and a right-click menu of ref actions.

- The sub-view choice persists in `localStorage` under `ocode.ui.git-view.v1` (`GIT_VIEW_KEY` in `GitPanel.tsx`).
- The branch scope select (All branches / Current branch) persists under `ocode.ui.git-history-scope.v1` (`SCOPE_KEY` in `GitHistoryPanel.tsx`).
- `GitPanel` renders `GitHistoryPanel` in place of the body split and the commit box when the view is `history`. The header, the operation banner and the conflicts section stay.
- `GitPanel` bumps `refreshToken` on every successful refresh. The history panel reloads its first page when the token changes, so commits made outside the UI appear.

## Server contract

### GET /api/git/log (`HandleGitLog`)

Returns an array of `GitCommit`, never `null`.

- Fields: `hash`, `short`, `message`, `author`, `email`, `date` (author date, `%aI`), `parents` (full hashes, first parent first, `[]` for a root commit), `refs`.
- `refs` is a list of `GitRef{name, kind, current}`. `kind` is `head`, `remote` or `tag`. `current` is true for the checked-out branch and for a bare detached `HEAD`.
- Params:
  - `limit`: default 50, clamped to 1..200 by `clampGitLogLimit`. The History panel sends 200.
  - `skip=N`: paging offset. A negative or non-numeric value gets 400 `skip must be a non-negative integer` (`parseGitLogOptions`).
  - `all=1`: every local branch, remote-tracking ref and tag. Without it the log is the history of HEAD.
  - `project`, `host`: as on other project routes.
- Format: `gitLogFormat` (`%H%x00%h%x00%an%x00%ae%x00%aI%x00%P%x00%D%x00%s`), built by `gitLogArgs` with `--decorate=full --date-order`.
- `parseGitLogLine` splits on NUL into 8 fields and skips short lines. `parseGitDecorations` maps `HEAD -> refs/heads/x` and `refs/heads/x` to `head`, a bare `HEAD` to a current `head`, `refs/remotes/...` to `remote`, and `tag: refs/tags/...` to `tag`. Stash and notes decorations are dropped.
- Transports: local through `gitRunInDir`; remote through `remoteGitLog` (`handler_remote_git.go`), which reuses `gitLogArgs`. A repository with no commits, or a failing git call, returns 200 with `[]`.

### POST /api/git/ref-action (`HandleGitRefAction`)

Body: `GitRefActionRequest{action, target, name?}`. Success: 200 with `GitRefActionResult{workspace, output}`.

The action table `gitRefActions` is closed:

| action | target accepted | name | git argv |
|---|---|---|---|
| checkout | hash or branch | no | `checkout <t>`, or `checkout --track <t>` when `gitRefIsRemoteTrackingOnly` |
| branch | hash or branch | required | `branch <name> <t>` |
| branch-checkout | hash or branch | required | `checkout -b <name> <t>` |
| branch-delete | branch only | no | `branch -d <t>` |
| merge | hash or branch | no | `merge --no-edit <t>` |
| cherry-pick | hash only | no | `cherry-pick <t>` |
| revert | hash only | no | `revert --no-edit <t>` |
| reset-soft, reset-mixed, reset-hard | hash or branch | no | `reset --soft`, `--mixed` or `--hard <t>` |
| tag | hash or branch | required | `tag <name> <t>` |

Validation (`validateGitRefAction`), all before git runs:

1. A `host` param gives 501 `ref actions are not supported for remote projects yet`. This is checked before the body is read.
2. Undecodable body gives 400. An unknown action gives 400 `<action> is not a supported ref action`.
3. `reset-hard` without a JSON boolean `confirm: true` gives 400 `reset-hard discards uncommitted work; send confirm:true to run it`. A string such as `"true"` fails to decode and gets `invalid request body` from step 2 instead. This check runs before the target is read.
4. A target that is empty or starts with `-` gives 400.
5. The action's shape rule: hash-only actions reject a branch name (`needs a commit hash`), branch-only actions reject a hash (`needs a branch name`).
6. A non-hash target must pass `gitBranchNameValid`, which compares the echo of `check-ref-format --branch`. The same helper validates worktree branch names.
7. Every target must pass `rev-parse --verify --quiet <t>^{commit}`, otherwise 400 `does not resolve to a commit`. This stops a file name from reaching `checkout`, which would restore that file and discard local edits.
8. A name on an action that takes none gives 400 `does not take a name`. A missing name on `branch`, `branch-checkout` or `tag` gives 400 `name is required`. A name starting with `-` is refused. Tags are checked as `refs/tags/<name>` with `check-ref-format`; branches with `gitBranchNameValid`.

A target counts as a hash when `isHexHash` accepts it (4 to 64 hex characters) and `refs/heads/<target>` does not resolve (`gitRefTargetIsHash`). A local branch named like hex, such as `cafe` or `dead`, is therefore a branch. Tags are not checked, so a hex-shaped name that is only a tag still counts as a hash. Hash-shaped targets skip step 6 and go straight to step 7. `HEAD` and `HEAD~N` are refused by step 6: git rejects `HEAD` as a branch name and `~` is not valid in one. The UI always sends full hashes.

Turn guard: after validation, the four actions with `writesWorktree` in `gitRefActions` (`merge`, `cherry-pick`, `revert`, `reset-hard`) return 409 when `projectTurnActive(dir)` finds a chat turn running in the project. A validation 400 wins over this check. The check is a snapshot over the session list, so a turn that starts just after it is not seen. Its 409 body is `{error}` only, with no workspace, because it is written by `writeError`. Other actions do not check it.

Execution: `runGitOperation` (`handler_git_conflicts.go`). The environment is `gitOperationEnv()`, which builds on `gitexec.Env()` and sets `GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no`, `GIT_TERMINAL_PROMPT=0` and `GIT_LITERAL_PATHSPECS=1`. The context timeout is `gitOperationTimeout` (60 s), and the run is wrapped in `gitexec.WithLockRetry`. Output is combined stdout and stderr, trimmed.

Status codes:

- 200: git succeeded. `output` is git's output, and `workspace` is re-read with `gitWorkspaceForDir`.
- 400: validation failure (steps 2 to 8).
- 409, git refused (for example an unmerged branch on `branch-delete` or a conflict on `merge`): body `GitRefActionConflict{error, workspace}`. `error` is git's output verbatim. `workspace` is re-read with `gitWorkspaceForDir` after git stopped, so a conflicted cherry-pick or revert shows its unmerged paths without a second request. If that re-read fails, the response is the `writeLocalGitError` error instead, not a 409.
- 409, turn running: `{error}` only, see the turn guard above. Both 409s share the status code, so a client tells them apart by the presence of `workspace`.
- 501: remote project.
- A failed workspace re-read after a successful git run goes through `writeLocalGitError`.

## Client structure

Files:

- `web/src/components/Git/GitSubTabs.tsx`: the Changes/History switch. Plain buttons with `role="tablist"`, modelled on `Cron/CronSubTabs.tsx`, so the parent keeps the commit box and dialogs mounted.
- `web/src/components/Git/GitPanel.tsx`: owns the view state (`changeView`) and `refreshToken`. Exports `CommitDiff`, the commit detail renderer that history reuses, and `timeAgo`.
- `web/src/components/Git/GitHistoryPanel.tsx`: scope select, paged list, row selection, context menu, confirm and name dialogs. `PAGE_SIZE` is 200.
- `web/src/components/Git/commitGraph.ts`: `layoutCommitGraph`, a pure function with no DOM.
- `web/src/api/client.ts` (`api.gitLog`, `api.gitShow`, `api.gitRefAction`) and `web/src/api/types.ts`. `api.gitRefAction` adds `confirm: true` to a `reset-hard` body, so the server's confirm guard is met only after the UI's confirm dialog. (`GitCommit`, `GitRef`, `GitRefAction`, `GitRefActionRequest`).
- Tests: `web/src/components/Git/commitGraph.test.ts`, `GitHistoryPanel.test.tsx`, `GitPanel.history.test.tsx`; server `internal/server/handler_git_log_test.go`, `handler_remote_git_log_test.go`, `handler_git_refs_test.go`.

Lane layout (`layoutCommitGraph`):

- One row per commit, in input order. `lanes[i]` holds the hash that lane i waits for, or null.
- A row uses the lane that waits for its hash, or claims a free lane. A commit that no lane waits for gets a fresh lane.
- Every other open lane emits a `pass` edge. A lane that waits for this commit emits an `incoming` edge into the node.
- The node's lane is released, then the parents claim lanes. The first parent takes the node's lane unless another lane already waits for it. Later parents reuse a waiting lane or open one (`parent` edges).
- A parent that was already emitted earlier gets no lane and no edge.
- Colours come from an 8-slot palette (`GRAPH_PALETTE_SIZE`), assigned when a lane opens. Trailing free lanes are trimmed.
- `GraphCell` draws one SVG per row from those edges. Merge rows are those with more than one parent.

Rows and chips: `CommitRow` renders `visibleRefs(commit.refs)` as chips. Head chips are blue, the current head is brighter (`REF_CHIP_CURRENT_CLASS`), remote chips are orange and tag chips are green. `visibleRefs` drops remote `*/HEAD` pointers. A dirty workspace adds a synthetic "Uncommitted changes" row whose click switches to Changes. Clicking a commit calls `api.gitShow` and renders `CommitDiff`. "Load more" fetches with `skip = commits.length`. `hasMore` is true when a page comes back full.

Refresh: `refreshHead` merges the head page (`mergeHead`). A background refresh never shows a spinner or a load error, and never clears `actionError`. A `generation` counter drops stale responses, and a background refresh is skipped while a fetch is in flight.

Menu (`buildMenuItems`, rendered through `Layout/ContextMenu` in controlled mode with `open`, `position` and `onClose`). Items, in order:

- Checkout `<local head>` for each non-current local head. Bare `HEAD` is excluded.
- Checkout `<remote>` (track) for each remote ref with no same-named local head among the loaded commits. Sends `branch-checkout` with `name` set to the local name and `target` set to the remote ref.
- Checkout commit `<short>` (detached). Sends `checkout` with the hash.
- Merge `<ref>` into `<current>`, only when on a branch. Offers each non-current local head and each remote ref.
- Cherry-pick commit; Revert commit (confirm first).
- Reset `<current>` to here: soft, mixed, and hard (hard asks for confirmation as a destructive action).
- New branch here (dialog with a "Check out" checkbox: on sends `branch-checkout`, off sends `branch`); New tag here (dialog).
- Delete branch `<b>` (confirm, destructive) for each non-current local head.
- Copy SHA; Copy message. These stay enabled everywhere.

Policy: mutating items are disabled while an action is busy and for every remote project (`projectHost` set). A remote project also shows a leading disabled line "Not available for remote projects yet". Errors from an action show the server message in the panel. Name-dialog errors show inline in the dialog.

## Invariants for future changes

- The action vocabulary is closed. A new action goes into `gitRefActions`, the `GitRefAction` union in `web/src/api/types.ts` and the menu, in one change, with a test.
- Every target must pass the resolve-to-commit check. Never pass a raw request string to git without `validateGitRefAction`.
- Run ref actions only through `runGitOperation`. A bare `exec.Command` skips the editor suppression, the timeout and the lock retry.
- A worktree-writing action sets `writesWorktree` in `gitRefActions`. That flag is the only thing the turn guard reads, so a new action that moves files must set it.
- The 409 from a git refusal keeps `workspace` in its body (`GitRefActionConflict`), so the UI can show a mid-conflict state without a second request.
- `reset-hard` needs JSON `confirm: true` on the server, and the web client sends it only after the confirm dialog. Do not relax either side alone.
- Keep `--date-order` and `--decorate=full` on both transports (`gitLogArgs`). The lane layout relies on the child-before-parent order that `--date-order` gives, and `parseGitDecorations` relies on the full ref prefixes.
- Chips and menus hide remote `*/HEAD` pointers. The menu never offers a bare `HEAD` as a branch.
- Destructive items (hard reset, branch delete, revert) confirm before they run.
- Remote projects: the server's 501 guard and the disabled menu items must change together. Tracked in `TODO.md`.
- A background refresh never clears a user-action error. See [Git action errors](../gotchas/git-action-errors-disappear.md).
- Changing `layoutCommitGraph` means updating `commitGraph.test.ts`.

## Known quirks

- **Remote-tracking check.** `gitRefIsRemoteTrackingOnly` tests `refs/heads/<target>` with the remote prefix still attached (`refs/heads/origin/x`), not the local branch `x`. So any remote name counts as tracking-only and `checkout origin/x` always runs `--track`. When a local `x` already exists, git refuses and the server returns 409 `a branch named 'x' already exists`. The menu hides the item only when `x` is among the loaded commits, so it can still appear for a branch beyond the loaded pages.
- **Bare detached HEAD chip.** The menu hides the bare `HEAD` entry, but `visibleRefs` does not filter it, so a detached commit row shows a current `HEAD` chip.
- **Hex-shaped names that are not local branches.** A target of 4 to 64 hex characters is a hash unless `refs/heads/<target>` resolves, so a local branch named `cafe` works for every action. Tags are not checked: a hex-shaped tag is still read as a hash, so a branch-only action on it fails with 400 `branch-delete needs a branch name`, while checkout still works. A remote-only branch named like hex is read as a hash and fails the resolve check with 400 `does not resolve to a commit`.
- **Remote projects** have no ref actions yet. The menu is disabled rather than hidden.
