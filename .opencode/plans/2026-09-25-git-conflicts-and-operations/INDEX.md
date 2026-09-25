# Plan: Web Git tab — conflict detection, resolution, operation recovery

Date: 2026-09-25
Status: COMPLETE. Phases 01-08 are DONE and VERIFIED (server + web UI, local + remote SSH/WSL, docs).

## Goal

The web Git tab must (1) identify conflicted files, (2) offer a per-file
conflict-resolution UI, and (3) let the user continue / abort / skip a git
operation that stopped mid-flight — for example a `git pull` that entered a
rebase or merge and halted on a conflict.

## Scope (user-decided)

- Resolution is **per-file only**: Use ours, Use theirs, Mark resolved.
- **All** in-progress operations, including `bisect`.
- **Local + remote (SSH/WSL) parity.**

## Non-goals

- No in-app conflict-marker block editor.
- No TUI changes (this is the web Git tab).
- No custom merge drivers or `.gitattributes` conflict-style configuration.
- No changes to the existing stash, force-push, or reset-to-remote features.

## Standing constraints (apply to every part)

- **Never use `git stash`** in this work, in tests, or as a resolution
  mechanism. It is a persistent user-state hazard and the user has ruled it
  out.
- **Never run a bare `git reset --soft`.**
- **Resetting specific files requires inspecting the diff first.**
- Conflict resolution is never implemented as a reset/rollback. Resolution is
  `checkout --ours/--theirs` (or `git rm` on a deleted side) followed by
  `git add`, nothing else.
- TDD: every new behavior gets a failing test before the implementation.
- A missing fixture must fail loudly — never skip or pass around it.

## Phase order

| Part | Phase | Status | One line |
| --- | --- | --- | --- |
| `01-op-state-parser.md` | Operation-state detection | **DONE** | One shared, transport-neutral parser for a halted git operation, built on git's own state files. |
| `02-status-detection.md` | Status surface | **DONE** | Put the conflicted-file list and the operation into `GitStatus`, fix the double/triple counting of conflicted paths. |
| `03-resolve-endpoint.md` | Per-file resolution | **DONE** | `POST /api/git/conflict/resolve` with correct handling of the rebase swap and deleted sides. |
| `04-operation-endpoint.md` | Operation recovery | **DONE** | `POST /api/git/operation` for continue / abort / skip, plus bisect good / bad / abort (which runs `git bisect reset`). |
| `05-remote-parity.md` | Remote projects | **DONE** | Mirror every new server behavior across the SSH/WSL transport. |
| `06-web-api-and-conflicts-ui.md` | Web types + Git tab UI | **DONE** | Conflicts section and operation banner, with destructive actions disabled mid-operation. |
| `07-badge-parity.md` | Badge totals | **DONE** | Keep the session Git tab and project sidebar badges equal to the Git tab's totals. Partly landed in phase 02. |
| `08-docs.md` | Documentation | **DONE** | Concept page through the context agent, plus a CHANGES.md entry. |

## Why the phases are ordered this way

Phase 1 is pure parsing with no I/O, so it is testable in isolation. Phase 2
consumes it and is the first place a real repo is involved. Phases 3 and 4
add mutations and must not start until the detection they validate against
exists. Phase 5 mirrors 2–4 over the remote transport. Phase 6 consumes the
finished API surface. Phase 7 must land in the same change as phase 2, because
phase 2 changes what the badge counts; it is sequenced last only so the
correct total is known by then. Phase 8 closes the work.

## User Expectation Checklist

- [ ] Conflicted files are identified in the web Git tab: a dedicated
      section, a per-file status badge, a header count, and inclusion in the
      session-tab and sidebar badges.
- [ ] A conflicted path is no longer counted more than once.
- [ ] Per-file resolution works: Use ours, Use theirs, Mark resolved —
      including the rebase ours/theirs swap and conflicts where one side is a
      deletion.
- [ ] Mark resolved refuses while conflict markers remain, and does not
      false-positive on ordinary content such as a Markdown setext underline.
- [ ] A halted operation is surfaced with the right kind, a human label, and
      progress, and Continue / Abort / Skip work.
- [ ] Bisect exposes Good / Bad / Skip / Reset, because it has no "continue".
- [ ] Stale operation state cannot abort a different operation.
- [ ] All of the above works for remote SSH/WSL projects.
- [ ] Commit, pull, push, force-push, reset-remote, stash, and the hunk
      stage/unstage/discard controls are disabled while an operation is in
      progress.
- [ ] Badge totals stay consistent across the Git tab, the session Git tab,
      and the project sidebar.
- [ ] Go and web tests exist, are mutation-checked, and docs are updated.

## Known limitations accepted by this design

- The remote path validator (`remoteSafeSpec`) rejects `'";` + backtick +
  `$&|<>\!*?[](){}#:` and control characters, so a conflicted path containing
  any of them — including ordinary Next.js routes like `app/[slug]/page.tsx`
  and `(group)/layout.tsx`, not just a colon — cannot be resolved on a remote
  project, even though the same file resolves locally. This surfaces as a
  clear 400 rather than a silent no-op. The validator is deliberately not
  relaxed: it is the shell-injection guard shared by every remote git
  mutation. The real fix is passing the path via stdin/argv, as
  `remoteGitHunk` already does for its patch.
- `git rebase` and `git checkout` are denied by this environment's
  permission rules, so the rebase-specific behavior is covered by
  synthesized repository state and by asserting the git command mapping,
  rather than by driving a real rebase end to end. Phase 1 and phase 4 say
  exactly how.
- Untracked-path parsing in the existing status pipeline still uses
  non-`-z` porcelain and therefore still trims quotes. Unchanged by this
  work; conflicted paths are unaffected because they come from a `-z` record.

## Tooling note

`plan_enter` cannot create this plan in this checkout — it attempts
`/.opencode/plans/<date>.md` at the filesystem root instead of the
repository's `.opencode/plans/`, and fails because that path does not
exist. This directory was written directly as a workaround. The underlying
path bug is already tracked in `TODO.md` and is not part of this feature.
