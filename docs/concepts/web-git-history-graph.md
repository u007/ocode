---
type: Concept
title: Web Git history graph
description: 'The Git tab History sub-view, its commit graph and ref-action context menu, the /api/git/log and /api/git/ref-action contract, and the invariants for changing them'
resource: internal/server/handler_git_refs.go
tags: [web, git, history, graph, context-menu]
timestamp: 2026-11-09T07:13:05Z
status: active
---
# Web Git history graph

## What it is

The Git tab has two sub-views, switched by `GitSubTabs`: **Changes** (the existing file panel) and **History** (`GitHistoryPanel`). History shows the commit log as a graph, a synthetic "Uncommitted changes" row when the workspace is dirty, the diff of the selected commit, and a right-click menu of ref actions.

- The sub-view choice persists in `localStorage` under `ocode.ui.git-view.v1` (`GIT_VIEW_KEY` in `GitPanel.tsx`).
- The branch scope select (All branches / Current branch) persists under `ocode.ui.git-history-scope.v1` (`SCOPE_KEY` in `GitHistoryPanel.tsx`).
- The commit-list / detail split is draggable (drag the horizontal divider, or use Arrow Up/Down while focused on it). The split ratio persists under `ocode.ui.git-history-split.v1` (`SPLIT_KEY` in `GitHistoryPanel.tsx`) and defaults so the detail pane takes 40% of the height. Double-clicking the divider resets it to the default.
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
| checkout | hash or branch | no | `checkout <t>`, or `checkout --track <t>` when the target is remote-tracking only (`gitRefIsRemoteTrackingOnly` locally, `remoteGitRefIsRemoteTrackingOnly` remotely) |
| branch | hash or branch | required | `branch <name> <t>` |
| branch-checkout | hash or branch | required | `checkout -b <name> <t>` |
| branch-delete | branch only | no | `branch -d <t>` |
| merge | hash or branch | no | `merge --no-edit <t>` |
| cherry-pick | hash only | no | `cherry-pick <t>` |
| revert | hash only | no | `revert --no-edit <t>` |
| reset-soft, reset-mixed, reset-hard | hash or branch | no | `reset --soft`, `--mixed` or `--hard <t>` |
| tag | hash or branch | required | `tag <name> <t>` |

Validation (`validateGitRefAction` locally, `validateGitRefActionRemote` on a host), all before git runs:

1. Undecodable body gives 400. An unknown action gives 400 `<action> is not a supported ref action`.
2. `reset-hard` without a JSON boolean `confirm: true` gives 400 `reset-hard discards uncommitted work; send confirm:true to run it`. A string such as `"true"` fails to decode and gets `invalid request body` from step 1 instead. This check runs before the target is read.
3. A target that is empty or starts with `-` gives 400.
4. The action's shape rule: hash-only actions reject a branch name (`needs a commit hash`), branch-only actions reject a hash (`needs a branch name`).
5. A non-hash target must pass the branch-name probe, which compares the echo of `check-ref-format --branch` (`gitBranchNameValid` locally, `remoteGitBranchNameValid` on a host). The same check validates worktree branch names.
6. Every target must pass the resolve probe (`rev-parse --verify --quiet <t>^{commit}`; `gitRefResolves` locally, `remoteGitRefResolves` on a host), otherwise 400 `does not resolve to a commit`. This stops a file name from reaching `checkout`, which would restore that file and discard local edits.
7. A name on an action that takes none gives 400 `does not take a name`. A missing name on `branch`, `branch-checkout` or `tag` gives 400 `name is required`. A name starting with `-` is refused. Tags are checked as `refs/tags/<name>` with the `check-ref-format` probe (`gitRefFormatValid` locally); branches with the branch-name probe.

The rules, their order and their wording live once in `validateGitRefActionUsing`; each transport is a thin wrapper that supplies only `refProbes` — `gitRefResolves`/`gitBranchNameValid`/`gitRefFormatValid` locally, the `remoteGit*` probes on a host.

A target counts as a hash when `isHexHash` accepts it (4 to 64 hex characters) and `refs/heads/<target>` does not resolve. That precedence lives once inside `validateGitRefActionUsing` (via the resolve probe), so both transports share it. A local branch named like hex, such as `cafe` or `dead`, is therefore a branch. Tags are not checked, so a hex-shaped name that is only a tag still counts as a hash. Hash-shaped targets skip step 5 and go straight to step 6. `HEAD` and `HEAD~N` are refused by step 5: git rejects `HEAD` as a branch name and `~` is not valid in one. The UI always sends full hashes.

Turn guard: after validation, the four actions with `writesWorktree` in `gitRefActions` (`merge`, `cherry-pick`, `revert`, `reset-hard`) return 409 when a chat turn is running in the project. A validation 400 wins over this check. Locally `projectTurnActive(dir)` scans *this* server's sessions; a remote project's chats are served by the host's own `ocode serve --remote`, so the remote path instead asks the host's server before a worktree-writing action (`GET /api/git/turn-active`, see **Remote projects** below) and **fails closed** — it runs only on a definitive idle answer, never upon a failed or uncertain one. Both guards are best-effort snapshots: a turn that starts just after the check is not seen, and that TOCTOU race is accepted. Their 409 body is `{error}` only, with no workspace, because it is written by `writeError`. Other actions do not check it.

Execution: `runGitOperation` (`handler_git_conflicts.go`). The environment is `gitOperationEnv()`, which builds on `gitexec.Env()` and sets `GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no`, `GIT_TERMINAL_PROMPT=0` and `GIT_LITERAL_PATHSPECS=1`. The context timeout is `gitOperationTimeout` (60 s), and the run is wrapped in `gitexec.WithLockRetry`. Output is combined stdout and stderr, trimmed.

Remote projects: `HandleGitRefAction` branches on the `host` param to `remoteGitRefAction` (`handler_remote_git_refs.go`); without a `host` it keeps the local path. Admission is `remoteMutationWorkFor` — a saved host plus a project registered with this server, with a sub-path accepted only after the canonical (symlink-resolved) containment check against its registered project root — and then `remoteGitToplevelFor`, which runs `git rev-parse --show-toplevel` on the host, normalises the workspace to the repository toplevel and answers 400 `not a git repository` outside a repo. That is the same repo check and normalisation `remoteGitDirForMutation` gives sibling remote mutations. Validation is `validateGitRefActionRemote`, which supplies host-run probes to the same `validateGitRefActionUsing` table, so the reasons and their order are identical by construction rather than by convention (for example `invalid branch name %q`). The host-run probes are answered by a single ssh script (`remoteRefProbes`) with a fixed five-line output — branch-at-target, target-resolves, target branch-name format, name branch-name format, and `refs/tags/<name>` format — whose every interpolated value is quoted with `remote.ShellQuote` and whose first line exports `GIT_OPTIONAL_LOCKS=0`, so validation costs one round trip rather than up to four. A truncated or garbled reply reads as "no" (`parseRemoteRefProbes`), so an incomplete answer refuses the action instead of accepting it. A transport failure while the probe script runs returns an error and is mapped to 502 through `writeRemoteGitError`, never to a 400 validation reason. After validation, and only when `spec.writesWorktree` is set, the handler calls `remoteProjectTurnActive(ctx, host, project)`: `GET /api/git/turn-active` over the **already-connected** workspace (it never dials), reusing that workspace's bearer token, and bounded to `remoteTurnCheckTimeout` (3 s) through `remoteCallCtx`, the ctx-aware variant `pulseRemoteCall` now delegates to. The action runs only on a definitive `turn_active: false`, and there is no fallback: a running turn is 409 `<action> refused: a chat turn is running in this project; try again when it finishes` (the same wording as the local path), a host with no live connection is 409 `cannot verify whether an agent turn is running on <host>; connect the host and retry`, an older host whose server predates the endpoint (404) is 409 `the remote server on <host> cannot report turn state; restart that host's server to upgrade it`, and any other error is 409 `cannot verify whether an agent turn is running on <host>: <error>`. A failed check never runs the action, and there is no local fallback. **Every probe argument and every action argument is single-quoted with `remote.ShellQuote`**, because `remoteGitCommand` does not quote argv and the remote shell interprets the whole line — an interpolation of a request-controlled target or name is command injection. Execution builds `remoteGitRefCommand`, which is `remoteGitCommand` with the `gitOperationEnv` suppression expressed inline (`GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no`, `GIT_TERMINAL_PROMPT=0`, `GIT_LITERAL_PATHSPECS=1`), and runs it through `remoteGitRefRun` → `remoteRunGitOperation`, bounded by `gitOperationTimeout` (60 s) and wrapped in `gitexec.WithLockRetry`. Output is combined stdout and stderr (`2>&1`), so it matches the local `CombinedOutput`. The workspace is re-read with `remoteGitWorkspace`. The action table stays the single source of truth: `refActionSpec.trackingCheckout` marks the checkout row, the caller supplies the "remote-tracking only" answer (`gitRefIsRemoteTrackingOnly` locally, `remoteGitRefIsRemoteTrackingOnly` remotely), and `argv(target, name, remoteTrackingOnly)` builds the arguments. There is no per-transport checkout special case.

Status codes:

- 200: git succeeded. `output` is git's output, and `workspace` is re-read with `gitWorkspaceForDir` (or `remoteGitWorkspace` on a remote project).
- 400: validation failure (steps 1 to 7). A remote project answers 400 the same way, including `not a git repository` from the toplevel probe.
- 409, git refused (for example an unmerged branch on `branch-delete` or a conflict on `merge`): body `GitRefActionConflict{error, workspace}`. `error` is git's output verbatim. `workspace` is re-read with `gitWorkspaceForDir` (or `remoteGitWorkspace`) after git stopped, so a conflicted cherry-pick or revert shows its unmerged paths without a second request. If that re-read fails, the response is the `writeLocalGitError` (or `writeRemoteGitError`) error instead, not a 409.
- 409, turn running: `{error}` only, see the turn guard above. The git-refusal and turn-guard 409s share the status code, so a client tells them apart by the presence of `workspace`.
- 409, remote turn running: a worktree-writing action on a remote project whose host server reports `turn_active: true` returns `{error}` only, `<action> refused: a chat turn is running in this project; try again when it finishes` — the same wording as the local turn guard.
- 409, remote turn unverifiable: the host has no live connection (`cannot verify whether an agent turn is running on <host>; connect the host and retry`) or the check failed for any other reason (`cannot verify whether an agent turn is running on <host>: <error>`). `{error}` only.
- 409, older remote server: the host's server predates `GET /api/git/turn-active` and answers 404, giving `the remote server on <host> cannot report turn state; restart that host's server to upgrade it`. `{error}` only. The action is never run without a definitive idle answer.
- Remote projects otherwise match the local list: a validation failure is 400, a non-zero remote git exit is 409 with `{error, workspace}`, and a transport failure (dead or timed-out ssh) is 502 through `writeRemoteGitError`.
- A failed workspace re-read after a successful git run goes through `writeLocalGitError` locally and `writeRemoteGitError` remotely.

### GET /api/git/turn-active (`HandleGitTurnActive`)

Read-only: `project=<abs path>` in, `{"turn_active": bool}` out, computed from `h.sessions.Snapshot()` through the existing `projectTurnActive`. Registered in `registerRoutes` under `authMiddleware`, and the project must pass `mutationProjectDir`, the same admission a git mutation uses, so it cannot be used to probe arbitrary paths. It exists solely for the remote turn guard and has no side effects.

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

Policy: mutating items wrap with one `mutate` helper and are disabled only while an action is busy (`disabled: busy` in `buildMenuItems`). Every action is enabled for remote projects, including `merge`, `cherry-pick`, `revert` and `reset-hard`; a refusal (for example the remote turn guard) surfaces as the panel's server error. Copy items stay enabled everywhere. Name-dialog errors show inline in the dialog.

## Invariants for future changes

- The action vocabulary is closed. A new action goes into `gitRefActions`, the `GitRefAction` union in `web/src/api/types.ts` and the menu, in one change, with a test.
- Every target must pass the resolve-to-commit check. Never pass a raw request string to git without `validateGitRefAction`.
- Run local ref actions only through `runGitOperation`; remote ones build `remoteGitRefCommand` and run through `remoteGitRefRun` / `remoteRunGitOperation`, bounded by the 60 s `gitOperationTimeout` and wrapped in `gitexec.WithLockRetry`. A bare `exec.Command` skips the editor suppression, the timeout and the lock retry.
- A worktree-writing action sets `writesWorktree` in `gitRefActions`. That flag is the only thing the turn guard reads, so a new action that moves files must set it.
- The remote turn guard is `GET /api/git/turn-active` plus `remoteProjectTurnActive`. It must stay read-only and reuse `projectTurnActive`; a `writesWorktree` action must never run without a definitive idle answer (no fallback, no local fallback), and a host whose server predates the endpoint must be upgraded, not bypassed.
- The local and remote turn guards are snapshots. A new turn that races the check is accepted; do not "fix" that by holding a session lock across the git command.
- The 409 from a git refusal keeps `workspace` in its body (`GitRefActionConflict`), so the UI can show a mid-conflict state without a second request.
- `reset-hard` needs JSON `confirm: true` on the server, and the web client sends it only after the confirm dialog. Do not relax either side alone.
- Keep `--date-order` and `--decorate=full` on both transports (`gitLogArgs`). The lane layout relies on the child-before-parent order that `--date-order` gives, and `parseGitDecorations` relies on the full ref prefixes.
- Chips and menus hide remote `*/HEAD` pointers. The menu never offers a bare `HEAD` as a branch.
- Destructive items (hard reset, branch delete, revert) confirm before they run.
- A new validation rule goes **once** into `validateGitRefActionUsing`; a transport supplies only `refProbes`. `TestGitRefActionLocalRemoteValidationParity` is the guard: it pins each case to a literal 400 reason on BOTH transports (add the literal with any new case). Comparing the transports to each other alone would not catch a corrupted shared rule — both would return the same wrong text.
- A new remote probe must ride in the existing batched script (`remoteRefProbes`), never a separate ssh call. `TestRemoteRefValidationUsesOneRoundTrip` pins the trip count and `TestRemoteRefProbeParseFailsClosed` pins the parser's fail-closed direction.
- Every argument of a remote probe or a remote action command must be shell-quoted with `remote.ShellQuote`: `remoteGitCommand` does not quote argv, and the remote shell interprets the whole line.
- `trackingCheckout` is how checkout's `--track` decision stays in the action table: the caller resolves the remote-tracking question (local git, or a remote probe) and passes the answer to `argv`. Never add a per-transport checkout special case.
- A background refresh never clears a user-action error. See [Git action errors](../gotchas/git-action-errors-disappear.md).
- Changing `layoutCommitGraph` means updating `commitGraph.test.ts`.

## Known quirks

- **Remote-tracking check.** `gitRefIsRemoteTrackingOnly` tests `refs/heads/<target>` with the remote prefix still attached (`refs/heads/origin/x`), not the local branch `x`. So any remote name counts as tracking-only and `checkout origin/x` always runs `--track`. When a local `x` already exists, git refuses and the server returns 409 `a branch named 'x' already exists`. The menu hides the item only when `x` is among the loaded commits, so it can still appear for a branch beyond the loaded pages.
- **Bare detached HEAD chip.** The menu hides the bare `HEAD` entry, but `visibleRefs` does not filter it, so a detached commit row shows a current `HEAD` chip.
- **Hex-shaped names that are not local branches.** A target of 4 to 64 hex characters is a hash unless `refs/heads/<target>` resolves, so a local branch named `cafe` works for every action. Tags are not checked: a hex-shaped tag is still read as a hash, so a branch-only action on it fails with 400 `branch-delete needs a branch name`, while checkout still works. A remote-only branch named like hex is read as a hash and fails the resolve check with 400 `does not resolve to a commit`.
