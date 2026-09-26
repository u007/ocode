---
type: Concept
title: Git conflicts and halted-operation recovery (web Git tab)
description: 'Shipped server behavior for conflicted files and halted git operations (phases 01-05): shared transport-neutral state parser/probe, ours/theirs stage semantics and the rebase relabel rule, per-file resolve + operation action matrix, HTTP status-code contract, literal-pathspec + editor suppression, 60s operation timeout, remote SSH/WSL parity, the shipped Git-tab UI (phase 06) and badge sweep (phase 07), and the completed phase 07 consumer sweep.'
tags:
  - git
  - conflicts
  - rebase
  - merge
  - bisect
  - web
  - remote
  - status
timestamp: 2026-09-26T05:57:29Z
---
# Git conflicts and halted-operation recovery

- **Status:** Server + API implemented for **local and remote (SSH/WSL)** projects — phases 01–05 of `.opencode/plans/2026-09-25-git-conflicts-and-operations/`, plus the web API client/types and the badge counting. **Phase 06 UI shipped:** `GitPanel.tsx` renders the conflicts section and the operation banner, calling `api.gitResolveConflict` / `api.gitOperation`. **Phase 07 consumer sweep complete:** the repo-wide `staged_files`/`changed_files` audit found no unaccounted readers — see *Current limits*.
- **Where:** `internal/server/handler_git_conflicts.go` (shared parser, both endpoints, local paths), `handler_remote_git_state.go` (remote probes), `handler_remote_git_conflicts.go` (remote resolve/operation), `handler_git.go` (status surface), `web/src/api/client.ts` + `types.ts` (client wrappers), `web/src/lib/projectGitCounts.ts`, `web/src/components/Layout/TopTabs.tsx` (badge counting).

## 1. User-facing model

`GET /api/git/status` gained two fields alongside the existing staged/changed lists:

- `conflicts: [{ path, code, ours, theirs }]` — one entry per unmerged path, `code` being git's porcelain `XY` (`DD`, `AU`, `UD`, `UA`, `DU`, `AA`, `UU`), `ours`/`theirs` reporting whether stage 2 / stage 3 exists (a false side is a deletion on that side).
- `operation?: { kind, label, step, total }` — present only while an operation is halted; `kind` is one of `merge`, `rebase`, `rebase-interactive`, `am`, `cherry-pick`, `revert`, `bisect`; `label` is human text (e.g. `Rebasing feature/login onto 1a2b3c4 (3/7)`), with `step`/`total` only where progress exists.

A conflicted path is reported **once**: both transports strip it from `staged_files` and `changed_files` so nothing is double-counted (`internal/server/handler_git.go:221-226`, mirrored in `handler_remote_git.go`). The UI — a conflicts section above the staged list with per-file actions, and an operation banner under the header with the valid buttons — **now ships in `GitPanel.tsx`** (phase 06): the banner drives `api.gitOperation` and each conflict row drives `api.gitResolveConflict`.

## 2. One parser, two transports

Detection is deliberately transport-neutral so local and remote cannot drift:

- **`gitStateEntry`** (`handler_git_conflicts.go:73`) carries a git-dir-relative name, is-dir, and small-file contents. The local path fills it with `os.Stat`/`os.ReadFile` (`gitStateEntriesInDir`), pinning text reads to one `LC_ALL` so byte offsets do not move (`TestGitOperationStateProbePinsOneCLocale`). The remote path emits the *same* names through fixed shell blocks (`remoteGitOperationStateProbe`, `remoteGitConflictsProbe` in `handler_remote_git_state.go`) and decodes them with `parseRemoteStateEntries`.
- **Operation detection** is `gitOperationStateFor(entries)`, a pure function with a fixed precedence: `rebase-merge` (interactive flag inside it) → `rebase-apply` (`applying` ⇒ `am`) → `CHERRY_PICK_HEAD` → `REVERT_HEAD` → `sequencer/todo` (leading verb decides revert vs pick) → `MERGE_HEAD` → `BISECT_START`. `BISECT_LOG` is deliberately **not** consulted because it outlives a finished bisect (false positive).
- **Conflict detection** is `git status --porcelain=v2 -z`, read only for `u` records by `parseUnmergedPorcelain`; an all-zero object id means that stage is absent, which is how a deleted side is detected from the same record. The remote `-z` payload is **base64-wrapped in the shell** so a filename containing the ASCII record separator cannot forge a section boundary; both transports then run the identical parser.

## 3. Ours/theirs — the inversion rule

The server uses git's own stage naming: **stage 2 = ours, stage 3 = theirs**, and passes `--ours`/`--theirs` straight through to `git checkout` with **no server-side inversion**.

The inversion is in the *meaning*: during a rebase, git's "ours" is the **upstream branch being rebased onto** and "theirs" is **your commit being replayed** — the opposite of what a user reading "Use ours" as "keep my work" expects. The contract for the UI (phase 06) is therefore to relabel the two choice buttons while `operation.kind` is `rebase`/`rebase-interactive` — "Keep upstream" / "Keep my commit" — and to keep plain "Use ours" / "Use theirs" otherwise.

`am` deliberately stays in that "otherwise" bucket, and must not be folded into the relabel by analogy with rebase: during `git am --3way` (and `git am -3`), an unmerged path has stage 2 = the current HEAD (the branch you are on) and stage 3 = the incoming patch, so `am` has **cherry-pick side semantics** — "ours" is your branch, "theirs" is the patch being applied — not rebase semantics, where "ours" is the upstream branch being replayed onto. This was confirmed empirically in a scratch repository by reading the index stages (`git show :2:<file>` and `git show :3:<file>`) during a real `git am -3` conflict. A regression test pins it: the case "labels am's conflict sides with git's own ours/theirs, not the rebase wording" in `web/src/components/Git/GitPanel.conflicts.test.tsx` asserts that the rebase wording does not appear during an `am`.

This is the single highest-consequence detail of the feature: the wrong label makes a user discard their own commit while believing they kept it. The wording is asserted in `web/src/components/Git/GitPanel.conflicts.test.tsx`, which passes now that the section is rendered (32 of 32 cases).

A chosen side that does not exist is a deletion, and `git checkout --ours/--theirs` cannot materialize one: the resolver runs `git rm -f -- <path>` instead, then `git add -- <path>` for the checkout case (both under literal pathspecs).

## 4. Action matrix (`POST /api/git/operation`)

`gitOperationCommand(kind, action)` maps a **closed** action vocabulary onto git argv — a request can never smuggle an arbitrary subcommand — and refuses anything git does not offer rather than silently ignoring it:

| kind | continue | abort | skip | extra |
| --- | --- | --- | --- | --- |
| merge | `merge --continue` | `merge --abort` | *not supported* | |
| rebase / rebase-interactive | `rebase --continue` | `rebase --abort` | `rebase --skip` | |
| am | `am --continue` | `am --abort` | `am --skip` | |
| cherry-pick | `cherry-pick --continue` | `cherry-pick --abort` | `cherry-pick --skip` | |
| revert | `revert --continue` | `revert --abort` | `revert --skip` | |
| bisect | *not supported* | `bisect reset` | `bisect skip` | `good` → `bisect good`, `bad` → `bisect bad` |

A merge genuinely has no skip. A bisect genuinely has no continue — it advances by marking the current commit good or bad, which is why `good`/`bad` exist. The request also carries the `kind` the client last saw; the server **re-detects** and answers 409 if it differs (or if no operation is in progress), so a panel left open across operations can never abort a *different* operation. `continue` is refused with 409 while conflicts remain (skip stays allowed — skipping a conflicted step is its purpose); git's own refusal (a hook veto, a moved state) is returned as **409 with git's verbatim output**, not 500.

### 4.1 The client's copy of the action matrix (version-skew fallback)

The web operation banner does not fetch this table — it renders from a hand copy, `OPERATION_ACTIONS` in `web/src/components/Git/GitPanel.tsx`, mirroring the server's `gitOperationCommand`. The copy is typed `Record<GitOperation["kind"], …>` rather than `Record<string, …>`, so it is **exhaustive**: adding a kind to the server's vocabulary without updating that file is a TypeScript compile error, not a silent runtime gap. The one case a type cannot cover is **version skew** — a server *newer* than the shipped bundle reporting a kind this build has no entry for. The banner then still names the operation but renders **no action buttons**, saying it cannot be continued from here and must be finished in the terminal, and it emits a `console.warn` naming the unknown kind — a real skew someone must fix, not a condition to swallow.

- **When adding an operation kind, both tables must move together:** the client `OPERATION_ACTIONS` and the server `gitOperationCommand` — neither is generated from the other.
- The parity test `SERVER_ACTION_TABLE` in `web/src/components/Git/GitPanel.conflicts.test.tsx` exists precisely because the copy once rotted silently: a bisect "Reset" button sent the wire verb `"reset"` and the server answered 400 to every click.

## 5. Environment hardening

- **Editor/prompt suppression** — `--continue` and `--skip` commit and would open an editor on a request that has no terminal. `gitOperationEnv` drops inherited `GIT_EDITOR`/`GIT_SEQUENCE_EDITOR`/`GIT_MERGE_AUTOEDIT`/`GIT_TERMINAL_PROMPT`/`GIT_LITERAL_PATHSPECS` (duplicates would resolve OS-specifically) and then sets `GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no`, `GIT_TERMINAL_PROMPT=0`, `GIT_LITERAL_PATHSPECS=1`. On the remote path the same values are written **inline immediately before `git`** in the shell string, because ssh has no TTY and an editor already exported in the remote shell would beat `-c core.editor`. The suppression is load-bearing — `git merge --continue` really does invoke `GIT_EDITOR` with no controlling TTY, and a non-zero-exiting editor stops the commit — but it cannot be pinned by an end-to-end test: under the fake-SSH harness an editor such as `vi` reads EOF and exits 0, so the merge still commits and such a test stays green even with a corrupted value. The guard that works is `TestRemoteGitOperationCommandSuppressesTheEditor`, a pure string assertion on the generated command verifying the assignments sit in the env prefix of the `git` invocation rather than on the `cd`; note also that a corrupted `GIT_EDITOR` makes git invoke an editor that blocks on an inherited TTY, so mutation-testing that path hangs instead of failing — pass an explicit `-timeout` and prefer the string-assertion test.
- **Literal pathspecs** — every conflict/operation command runs through `gitRunInDirLiteral` (`GIT_LITERAL_PATHSPECS=1`) or its remote twin `remoteGitMutationCommand`, always with `--` before the path, so a caller-supplied filename can never be read as pathspec magic (`*`, `[]`, `!`, etc.). This is scoped to these endpoints on purpose: the shared `gitexec.Env` is used by every git call in the product, and changing pathspec interpretation globally would alter unrelated behavior (`TestResolveConflictLiteralPathspec`, `TestRemoteGitResolveConflictRefusesPathspecMagicName`).
- **Index lock** — both paths wrap runs in `gitexec.WithLockRetry`, because a halted repository is exactly when the user's terminal or editor is also touching `.git/index.lock` (see `gotchas/git-index-lock-contention.md`).

## 6. Timeout and status-code contract

`gitOperationTimeout = 60 * time.Second` bounds one operation command locally — continue/skip run commit hooks and can invoke credential helpers, so an exiting-less child must not hang the request forever; it matches the existing network-action bound (`runGitNetwork`). The remote path rides the shared remote exec bound (`remoteExecTimeout` in `handler_remote_work.go`) through `remoteRun` + lock retry rather than a separate constant.

| Condition | Resolve endpoint | Operation endpoint |
| --- | --- | --- |
| Invalid JSON / empty path / unknown resolution / path outside repo | 400 | 400 (invalid body, missing `kind`/`action`) |
| Not a git repository / unknown project | 400 | 400 |
| Path is not a conflicted file (re-detected server-side) | 409 | — |
| Unsupported action for the detected kind | — | 400 |
| No operation in progress, or `kind` mismatch (stale client) | — | 409 |
| `continue` while conflicts remain | — | 409 |
| Mark-resolved but conflict markers remain | 400 | — |
| File larger than the marker-scan limit (4 MiB) | 400 (use ours/theirs) | — |
| git refused (hook veto, state moved, checkout/add failure) | 500 for infrastructure failures | 409 with git's own words |
| Success | 200 + refreshed workspace | 200 + workspace (+ `output` locally) |

Two deliberate asymmetries: git refusing a *requested action* is 409 (a legitimate answer to a legitimate request, not a server fault), while an unexpected failure of the git process is 500; and mark-resolved answers 400 for both "markers left" and "too large to verify", so the client is told to pick a side instead of silently staging an unresolved file.

## 7. Remote parity (SSH / WSL)

The same two endpoints serve remote projects — they dispatch on `hostParam(r)` to `remoteGitResolveConflict` / `remoteGitOperation`, and the status shape comes from `remoteGitStatus`, which appends the conflicts and operation probes as **two extra sections of the single batched status script** (an extra section is an ssh round trip otherwise; `TestRemoteGitStatusUsesOneBatchedRoundTrip` pins one trip). Response types are identical, so the web cannot tell the transports apart. Known remote-only limits:

1. **Paths with shell-special characters cannot be resolved remotely.** `remoteSafeSpec` rejects `'"` + backtick + `$&|<>\!*?[](){}#:` and control characters — the shell-injection guard shared by every remote git mutation — so a conflicted path containing any of them (including ordinary routes like `app/[slug]/page.tsx` or `(group)/layout.tsx`, not just a colon) answers a clear **400** instead of resolving. The validator is deliberately not relaxed; the real fix is passing the path via stdin/argv the way `remoteGitHunk` already carries its patch.
2. **The remote operation response omits `output`** (only the workspace is returned), so hook output is not surfaced for remote continue/skip today.
3. **The bound differs:** local 60s, remote the shared exec bound above.

## 8. Badge counting

Because conflicted paths leave the staged/changed lists, consumers add them back: `web/src/lib/projectGitCounts.ts` includes a `conflicted` count in the sidebar total (with a version-skew default of 0 for a server that predates the field), and `web/src/components/Layout/TopTabs.tsx` adds it into the session Git-tab badge, whose title reads `N conflicted · N staged · N unstaged` when conflicts exist. Both landed with phase 02.

## 9. Current limits and status

- **Git-tab UI (phase 06) has shipped:** `web/src/components/Git/GitPanel.tsx` renders the conflicts section and the operation banner and calls `api.gitResolveConflict` / `api.gitOperation`, built on the client wrappers and the `GitConflict`/`GitOperation` types in `web/src/api/types.ts`. The TDD suite `web/src/components/Git/GitPanel.conflicts.test.tsx` exists and passes (32 of 32 cases); it also pins the rebase relabel wording from §3.
- **Phase 07 sweep is complete:** the `editorDiffSource` decision is made and shipped — `resolveEditorDiffSource` lives in `web/src/lib/editorDiffSource.ts`, has a live consumer in `web/src/components/Files/FileEditor.tsx`, and is covered by `web/src/lib/editorDiffSource.test.ts` — and the repo-wide audit of `staged_files`/`changed_files` readers is done. Every non-test web reader is either `web/src/components/Layout/TopTabs.tsx` (sets `gitConflicted` from `status.conflicts?.length ?? 0` and folds it into the tab badge total) or `web/src/lib/projectGitCounts.ts` (`total: staged + unstaged + conflicted`); both already account for conflicts, with the deliberate version-skew default from §8 for a server that omits the field. `web/src/components/Git/GitPanel.tsx` does **not** fold conflicts into a total — it renders a separate red "N conflicted" alongside "N staged · M unstaged" — and that is intentional, not a parity gap: the two *badge* totals (tab badge, sidebar badge) are the surfaces that must agree, while the Git panel shows the breakdown. On the Go side there are no other readers of `GitStatus.StagedFiles` / `GitStatus.ChangedFiles`; the `ChangedFiles()` hits in `internal/snapshot`, `internal/agent` and `internal/changes` are the Changes-tab journal, a different concept entirely.
- **No TUI surface, no conflict-marker editor, no stash/reset/force-push changes** — all explicit non-goals of the plan.
- Cross-links: `concepts/git-stash-ui.md` (the neighbouring stash surface), `gotchas/git-action-errors-disappear.md` (the sticky-error contract the new operation banner deliberately supersedes in exactly one narrow case: a stale pull error is cleared on the idle→operation transition, and only then), `gotchas/git-index-lock-contention.md`, `gotchas/remote-git-shell-quoting.md`.

## 10. Tests worth knowing

- Parser/detection: `TestGitOperationStateForDetectsEveryKind`, `…Precedence`, `…LabelsAndProgress`, `…RealMergeConflict`, `…CleanRepoIsIdle`, `…LinkedWorktree`, `TestGitStatusReportsConflictOnce`, `TestGitStatusModifyDeleteConflict`.
- Resolve: `TestResolveConflictOursThenTheirs`, `…DeletedSideUsesGitRm`, `…MarkRefusesLeftoverMarkers`, `…MarkAcceptsMarkdownSetextUnderline`, `…MarkAcceptsSeparatorInsideCodeFence`, `…LiteralPathspec`, `…RejectsNonConflictedPath`, `…RefusesPathOutsideRepo`.
- Operation: `TestGitOperationCommandMapping`, `…ContinueSuppressesTheEditor`, `…RejectsStaleKind`, `…RejectsUnsupportedCombination`, `…ContinueRefusesWhileConflictsRemain`, `…DetectsARealBisect`, `…BisectRejectsContinue`.
- Remote: `TestRemoteGitStatusReportsConflicts`, `…ReportsMergeOperation`, `…ReportsRebaseOperation`, `…UsesOneBatchedRoundTrip`, `TestRemoteGitResolveConflictOursThenTheirs`, `…RefusesPathspecMagicName`, `TestRemoteGitOperationAbort`, `…RejectsStaleKind`, `…OperationStateNamesAreSafeProbeLiterals`.
