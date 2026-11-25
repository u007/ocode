---
type: Concept
title: Web Git Branches sub-view
description: 'The Git tab Branches sub-view: GET /api/git/branches, the six branch/tag ref actions, the extended /api/git/push contract, remote-transport quoting, and the client behaviour for switching, deleting and upstreams'
resource: internal/server/handler_git_branches.go
tags: [web, git, branches, tags, ref-action, push, remote]
timestamp: 2026-11-25T17:20:34Z
status: active
---
# Web Git Branches sub-view

## What it is

`GitSubTabs` now offers three sub-views: **Changes**, **History** and **Branches**
(`GitView = "changes" | "history" | "branches"`, `web/src/components/Git/GitSubTabs.tsx:6`).
Branches is `web/src/components/Git/BranchesPanel.tsx:119`, rendered by
`GitPanel.tsx:1642` with `currentBranch`, `dirty`, `refreshToken` and
`onWorkspaceChanged`. The view choice still persists in `localStorage` under
`ocode.ui.git-view.v1` (`loadGitView`, `GitPanel.tsx:260`, which accepts `"branches"`
alongside `"history"`).

The panel shows three collapsible sections — local branches, remote-tracking branches
and tags — behind a filter box (`aria-label="Filter branches"`, `BranchesPanel.tsx:580`)
and a counts line (`{n} local · {n} remote · {n} tags`). Rows show the current-branch
check, a `↑ahead`/`↓behind` pair with a `gone` marker when `upstream_gone`, the short
upstream, the subject and a relative date. Double-click switches branch; the
per-row button or a right-click opens the context menu
(`data-testid="git-branch-menu"`).

`refreshToken` reloads the list in the background (spinner-less) when it changes, and
the panel discards its list on a project/host change so one project's branches are
never shown under another (`BranchesPanel.tsx:211` onwards).

## Server contract

### GET /api/git/branches (`HandleGitBranches`)

Registered at `internal/server/server.go:453` behind `authMiddleware`.
`handler_git_branches.go:67`.

Returns `GitBranches{local, remote, tags}` (`handler_git_branches.go:43`). Every list is
non-nil, so an empty repository marshals as `[]` rather than `null`
(`emptyGitBranches`, `:61`). A non-repository — or an unborn one — also returns 200 with
empty lists, never an error (`:88-92`), matching history's empty-list answer.

Each row is a `GitBranch` (`:14`): `name`, `refname` (full ref), `hash`,
`kind` (`"local"|"remote"|"tag"`), `current`, `upstream` (short, optional),
`ahead`, `behind`, `upstream_gone` (optional), `remote` (remote rows only),
`subject`, `date`.

- **One git call.** `gitBranchArgs` (`:55`) is a single
  `for-each-ref --format=… refs/heads refs/remotes refs/tags`
  (`gitBranchFormat`, `:52`). Fields are NUL-separated with the subject last, so a
  subject containing the separator count cannot shift a field.
- `%(HEAD)` prints `*` on the checked-out branch, which becomes `current: true`
  (`parseGitBranches`, `:99`).
- `refs/remotes/<remote>/HEAD` is skipped — it is a symbolic pointer, not a branch
  (`:124`). `remote` is the first path segment of the remote ref.
- `%(upstream:track)` is parsed by `parseUpstreamTrack` (`:144`) into ahead/behind
  counts, with `[gone]` setting `upstream_gone` and leaving both counts at 0. The
  accepted shapes are `[ahead N]`, `[behind N]`, `[ahead N, behind M]`, `[gone]`, and
  empty for no upstream.
- The panel always sends the SHORT name (`GitBranch.name`), never the full `refname`.
  A full `refs/heads/<name>` checkout detaches HEAD, so checkout, merge, rebase and
  create-from all use the name git resolves like `git checkout <name>` does.
  `refname` exists on the row for display/diagnostics only.

Remote (SSH/WSL) transport: `remoteGitBranches` (`:174`) runs the same argv through
`remoteGitCommand`. The **format argument is `remote.ShellQuote`d** because `%(…)`
would otherwise be read by the remote shell as a subshell; the other arguments are
fixed literals. A transport failure returns the empty lists, the same as locally.

### The new ref actions

`gitRefActions` (`internal/server/handler_git_refs.go:73`) gained six closed entries:

| action | target accepted | name | git argv |
|---|---|---|---|
| `branch-force-delete` | branch only | no | `branch -D <t>` (`:94`) |
| `branch-rename` | branch only | required | `branch -m <t> <name>` (`:97`) |
| `rebase` | hash or branch | no | `rebase <t>` (`:102`) |
| `tag-delete` | tag only | no | `tag -d <t>` (`:126`) |
| `set-upstream` | branch only | required (a remote-tracking ref) | `branch --set-upstream-to=<name> <t>` (`:129`) |
| `unset-upstream` | branch only | no | `branch --unset-upstream <t>` (`:132`) |

`rebase` sets `writesWorktree: true`, so the chat-turn guard applies exactly as it does
for `merge`/`reset-hard`. `branch-force-delete`, `branch-rename`, `tag-delete` and the
two upstream actions do not move tracked files and therefore do not.

`refTargetRule` gained `refTargetTag` (`:48`), used only by `tag-delete`: in
`validateGitRefActionUsing` (`:207`) a tag-only target that does not resolve under
`refs/tags/` is refused with `<action> needs a tag name` (`:219`). The rule runs before
the hash/branch checks, so a hex-shaped tag name is still accepted by `tag-delete`
while a hex-shaped non-tag is refused.

`set-upstream` adds one rule after the name checks (`:257`): the name must resolve under
`refs/remotes/`, otherwise `upstream "<name>" is not a remote-tracking branch`. Setting
an upstream to something git cannot resolve would only fail later inside git.

All existing reasons and their order are unchanged; a new rule belongs in
`validateGitRefActionUsing`, and each transport still supplies only `refProbes`
(see [Web Git history graph](web-git-history-graph.md)).

### Remote probe script: still one round trip

`handler_remote_git_refs.go` keeps the SINGLE batched ssh probe script
(`remoteRefProbes`, `:237`). It now emits **seven** fixed-order answers instead of five
(`:218`): branch-at-target, target-resolves-to-commit, target branch-name format, name
branch-name format, `refs/tags/<name>` format, plus the two new ones —
`refProbeTagAtTarget` (`refs/tags/<target>` resolves) and `refProbeRemoteAtName`
(`refs/remotes/<name>` resolves). `parseRemoteRefProbes` (`:260`) gained the matching
`refResolves` switch cases. Every interpolated value is still `remote.ShellQuote`d and
the script's first line still exports `GIT_OPTIONAL_LOCKS=0`. A short or garbled reply
still reads as "no", so the refusing direction is preserved.

### POST /api/git/push (`HandleGitPush`)

`gitPushRequest` (`internal/server/handler_git_actions.go:265`) gained `remote`,
`branch`, `delete` and `set_upstream`; `force` keeps its `--force-with-lease` meaning.
Empty fields preserve the plain `git push` (`gitPushArgs`, `:280`).

| fields sent | argv |
|---|---|
| none | `push` |
| `set_upstream: true, remote` | `push --set-upstream <remote>` |
| `remote, branch` | `push <remote> <branch>` |
| `delete: true, remote, branch` | `push <remote> --delete <branch>` |

Validation in `gitPushArgs`:

- `remote`/`branch` must be non-empty, must not start with `-`, and must not contain
  whitespace.
- `delete` requires both `remote` and `branch`.
- `branch` without `remote` is refused: `git push <branch>` would read it as a remote
  name and push to the wrong place (or fail).
- `set_upstream` requires a remote.

The local path (`gitPushLocal`, `:448`) then checks `remote` against the repository's
configured remotes (`gitRemoteExists`, `:482`) and `branch` through
`check-ref-format --branch` (`gitBranchNameValid`), answering 400
`unknown remote "<r>"` / `invalid branch name "<b>"` instead of git's own message.

The remote path single-quotes **every** argv element before `remoteGitCommand`
(`:433-437`), because that helper joins argv verbatim and the remote shell interprets
the whole line.

## Client structure

- `web/src/components/Git/BranchesPanel.tsx` — list, filter, sections, row context menu,
  and the dialogs: new branch, rename, delete/force delete, push, upstream, plus
  `ConfirmDialog`s for the dirty-switch warning, rebase and tag/remote deletion.
- `web/src/components/Git/GitSubTabs.tsx` — `GitView` and the three-view switch
  (`GitFork` icon for Branches).
- `web/src/components/Git/GitPanel.tsx` — view state and `refreshToken`; renders the
  panel with `currentBranch={status.branch}` and
  `dirty={status.has_changes || conflicts.length > 0}`.
- `web/src/api/types.ts` — `GitBranch` (`:940`), `GitBranches` (`:960`),
  `GitPushRequest` (`:967`), and the extended `GitRefAction` union (`:911`).
- `web/src/api/client.ts` — `api.gitBranches` (`:2429`) and `api.gitPushBranch`
  (`:2663`). `api.gitPush` keeps the header's plain push; the Branches view uses
  `gitPushBranch`.

Behaviour worth keeping:

- **Remote-row switch.** With no same-named local branch, the panel runs
  `branch-checkout` with `target` = the remote short name (`origin/feature`) and `name` =
  `localNameForRemote(name)` (`BranchesPanel.tsx:66`), so git auto-tracks. When a local
  branch of that name already exists it runs a plain `checkout` of the local name.
  `localNameForRemote` (`:66`) drops everything before the first `/`; remote names
  cannot contain a slash.
- **Dirty tree.** A switch on a dirty workspace opens a `ConfirmDialog`
  (`:681`) that says git will refuse if the changes would be overwritten and asks the
  user to commit or stash first. Confirming runs the switch; there is **no auto-stash**.
- **Delete has a force fallback in the same dialog.** `branch-delete` failing (git
  refuses an unmerged branch) keeps the dialog open with the error inline and reveals a
  **Force delete** button (`:818`) that sends `branch-force-delete`. The dialog is only
  dismissed on success.
- **New branch** starts from a short ref name or `HEAD` (`START_HEAD`, `:43`; start
  options are every row's `name`, `:511`), and the "Check out" box picks
  `branch-checkout` instead of `branch`. Short names matter: `git checkout
  refs/heads/<name>` detaches HEAD instead of attaching to the branch.
- **Push** defaults the remote to the branch's upstream remote or the first known
  remote, and sets `set_upstream` when the branch has none.
- Errors are shown in a dismissible alert; a success notice auto-clears after
  `NOTICE_MS` (5 s).

## Test pins

Server — `internal/server/handler_git_branches_test.go`:

- `TestGitBranchesListsLocalRemoteAndTags` — local/remote/tag rows with upstream
  ahead/behind, `origin/HEAD` skipped.
- `TestGitBranchesEmptyOutsideRepository` — empty lists, 200, no error.
- `TestParseUpstreamTrack` — the four `%(upstream:track)` shapes.
- `TestGitRefActionNewBranchActions` — rename, unmerged `branch-delete` (409) then
  `branch-force-delete`, `tag-delete`.
- `TestGitRefActionSetAndUnsetUpstream`, `TestGitRefActionRebase`.
- `TestGitRefActionNewActionValidation` — the new action reasons, including
  `needs a tag name` and the `refs/remotes/` upstream rule.
- `TestGitPushArgs`, `TestGitPushRejectsUnknownRemote`.
- `TestRemoteGitBranchesOverFakeSSH` — the remote listing over a fake ssh.

Web — `web/src/components/Git/BranchesPanel.test.tsx` (12 tests): render/current/
ahead-behind, local switch, remote tracking checkout, dirty warning, create from another
branch, delete + force delete, merge, rebase confirm, rename, remote delete, set
upstream, push `-u`.

## Invariants for future changes

- The action vocabulary stays closed. A new branch action goes into `gitRefActions`, the
  `GitRefAction` union in `web/src/api/types.ts` and the panel's menu, in one change,
  with a test.
- A new target kind gets a `refTargetRule` case in `validateGitRefActionUsing`, never a
  transport-local check; the remote path must answer it from the same batched script.
- A new remote probe rides in `remoteRefProbes`' fixed-order output and gets a
  `refResolves`/format case in `parseRemoteRefProbes`. Never add a second ssh call.
- Every value interpolated into a remote command line is `remote.ShellQuote`d — in the
  probe script, in ref actions, and in `gitPushArgs`' remote path. `remoteGitCommand`
  does not quote.
- **A branch/tag listing must stay one `for-each-ref`.** Adding a per-ref git call turns
  one poll into hundreds on a large repository.
- The listing is read-only: it never touches the worktree and never fails on a
  non-repository.
- An action that moves tracked files must set `writesWorktree` so the turn guard sees
  it; `checkout`, `branch-checkout`, `rebase`, `merge`, `cherry-pick`, `revert` and
  `reset-hard` currently do.
- Deleting a local branch keeps its force fallback behind the same dialog; the UI must
  not silently escalate to `branch-force-delete`.

## See also

- [Web Git history graph](web-git-history-graph.md) — the History sub-view, the full
  ref-action table and the turn guard both views share.
- [Git worktrees: list, open, create, remove (web Git tab)](git-worktrees.md) — the
  Worktrees section of the same tab.
- [Remote git commit/stash messages must be shell-quoted](../gotchas/remote-git-shell-quoting.md)
  — the `remoteGitCommand` quoting contract this page depends on.
