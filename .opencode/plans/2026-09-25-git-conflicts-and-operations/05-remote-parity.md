# Phase 05 — Remote project parity (SSH / WSL)

Status: **DONE + VERIFIED 2026-09-25.** See the implementation decisions
and verification sections at the end of this file.

Phase 5 of the web Git tab conflict/operation-recovery work, tracked from
`.opencode/plans/2026-09-25-git-conflicts-and-operations/INDEX.md`.

## What this phase delivers

Every behavior added in phases 1 through 4, working for a project whose git
repository lives on a remote SSH host or inside WSL. The Git tab is already
host-aware throughout, so a local-only feature would be a visible parity hole
for exactly the users who hit conflicts most often.

## Context you need

The server has two parallel git implementations. The local one lives in
`internal/server/handler_git.go` and `handler_git_actions.go`. The remote one
lives in `internal/server/handler_remote_git.go` and
`handler_remote_git_actions.go`, and the file header there states the intent
plainly: the remote path is a port of the local pipeline that produces the
*identical* response types so the web cannot tell them apart.

Key existing pieces:

- `remoteGitStatus` (around line 21) already batches every status probe into
  **one** shell script, with sections separated by a record-separator `echo`
  sentinel, and parses the sections locally. This batching is deliberate: each
  additional round trip is an ssh invocation, so new probes must be appended
  as new sections rather than as new calls.
- `remoteRun` and `remoteGitCommand` live in
  `internal/server/handler_remote_work.go` (around lines 90 and 389).
  `remoteGitCommand` composes `cd <quoted path> && GIT_OPTIONAL_LOCKS=0 git
  <args...>`, and the path is quoted by `shellQuotePathPOSIX`.
- `remoteGitMutation` (in the remote actions file) wraps `remoteRun` in
  `gitexec.WithLockRetry` so a momentary host-side index-lock loss is retried.
- `remoteGitDirForMutation` validates the host and project pair and rewrites
  the working path to the remote repository toplevel, so a nested remote
  project path cannot broaden the operation.
- `remoteRelCheck` and `remoteSafeSpec` are the path-safety gates. There is no
  equivalent lax mode; anything they reject must be reported as an error.

## Status detection over the transport

Append two sections to the existing batch:

1. The unmerged conflicts, from `git status --porcelain=v2 -z`, parsed by the
   same parser the local path uses.
2. The operation state, by resolving the remote git directory once with
   `git rev-parse --absolute-git-dir` and then probing the **same fixed name
   list** the local path probes, emitting each hit as a line carrying the name,
   whether it is a directory, and a size-bounded slice of its contents.

Those emitted lines are exactly the phase 1 state-entry shape, so the remote
path reuses the phase 1 parser verbatim. That is the point: the parser is
transport-neutral precisely so this phase has no second implementation of
"what does a rebase look like".

**Security requirement.** The probe block must be a fixed literal with no
interpolated caller input. The project path reaches the script only through
`remoteGitCommand`'s quoting; the probe names come from the constant list.
Do not build that block by concatenating anything from a request.

## Mutation handlers over the transport

Add remote branches for both new endpoints, following the established shape
of the existing remote handlers:

- Branch on the host query parameter at the top, exactly as
  `HandleGitPull` and friends do; if there is no host, call the local
  implementation.
- Validate with `remoteGitDirForMutation`, and validate the caller's path with
  `remoteAbsJoin`, then `remoteRelCheck`, then `remoteSafeSpec` — the same
  three-step order `remoteGitHunk` uses.
- Run the mutation through `remoteGitMutation` so the host-side lock retry
  applies.
- Return the remote workspace, from `remoteGitWorkspace`, so the response type
  is identical to the local one.
- The command construction differs: the local path passes git arguments to a
  process, while the remote path builds a shell string, so the editor- and
  path-safety environment settings have to be expressed in that string.

## Known limitation to implement honestly

`remoteSafeSpec` rejects any path containing a colon among a broad set of
punctuation, and a colon is legal in a git path. Such a conflicted file
therefore **cannot** be resolved on a remote project today. The correct
behavior is to return the validator's error to the user so the panel shows why
the action was refused — not to skip the file silently, and not to relax the
validator, which exists to stop shell injection through a path. Record this
limitation in `TODO.md` so it is not rediscovered as a bug.

## Verified findings this phase relies on

- **VERIFIED by reading the source.** `remoteGitCommand` quotes the project
  path via `shellQuotePathPOSIX`, so a path containing shell metacharacters
  cannot break out of the `cd`. The new probe block adds no new path
  interpolation.
- **VERIFIED by reading the source.** `remoteSafeSpec` rejects `:` along with
  quotes, semicolons, backticks, redirection, glob and control characters.
- **VERIFIED by reading the source.** The existing remote tests use a fake
  SSH shim (`installFakeSSH`) and a remote handler constructor in
  `internal/server/handler_remote_git_test.go`. Reuse that harness; do not
  invent a second one.

## Tests first

Add to `internal/server/handler_remote_git_test.go` and
`handler_remote_git_actions_test.go`, using the existing fake-SSH harness.

- Remote status over the fake transport reports a merge with the label and
  progress taken from the remote's own state files, proving the probe block
  and the shared parser agree.
- A remote unmerged record produces a conflict entry with the right code and
  the right ours/theirs booleans.
- A remote state block with a `rebase-merge` directory, including its
  `msgnum`, `end`, `head-name` and `onto` files, is reported as a rebase with
  the branch prefix stripped — identical to what the local path produces for
  the same state.
- The remote resolve handler issues the expected command for each resolution,
  including the delete-side `git rm` case, and refuses a path that
  `remoteSafeSpec` rejects.
- The remote operation handler issues the expected command per kind and
  returns 400 for an unsupported combination and 409 on a kind mismatch.
- A test that the probe block contains no caller-controlled text: send a path
  attempt and assert it is rejected before any command is built.

## Verification

- `go test ./internal/server/ -run 'RemoteGit' -count=1`
- `go test ./internal/server/ -count=1`
- `gofmt -l internal/server/`
- `go vet ./internal/server/`
- `go build ./...`
- Mutation-check: make the remote probe emit a fixed "merge" string instead
  of reading the state files and confirm the label test goes red; drop the
  `remoteSafeSpec` call from the resolve handler and confirm the rejection
  test goes red.

## Risks and open questions

- The batched script is now longer. If a remote host's shell chokes on it,
  the failure will look like a transport error rather than a probe error.
  Keep the appended block simple, and ensure the sections that already worked
  still parse when a later section is empty.
- A remote repository on a host with an unusual git version could report
  state files the local parser has never seen. The parser ignores unknown
  names, so this degrades to "no operation detected" rather than a crash —
  acceptable, but worth knowing.
- The colon-path limitation is permanent unless `remoteSafeSpec` is
  redesigned. Redesigning it is out of scope here and is noted in
  `TODO.md`.

## Implementation decisions (recorded 2026-09-25)

Three deviations from this plan, all forced by properties of the transport.

- **Both new probes are base64-encoded in the shell.** The plan specified a
  "size-bounded slice" of each state file and a raw `-z` conflicts section.
  Neither survives the trip as raw text: `MERGE_MSG` holds a commit message
  that can contain tabs and newlines, and `-z` emits raw paths that can
  contain the `0x1e` section separator. Either would forge a state entry or
  shift every later field in the batch. Both payloads are therefore encoded
  and decoded with the `base64Decode` convention `remoteReadFile` already
  uses. The 4 KiB bound and the fixed probe-name list are unchanged, and
  `gitOperationStateFor` is still called verbatim — the transport stays
  invisible to the parser.

- **The `-z` section is not last, so `SplitN` carries the cap instead.**
  The plan's Risks note anticipated a collision. Because the payload is
  base64 now, a separator byte can no longer appear in it at all; the
  `SplitN(out, sep, 8)` is kept as a second line of defense rather than as the
  primary mechanism.

- **`remoteSafeSpec` is not relaxed, and the limitation is reported.**
  The plan already said to return the validator's error rather than loosen the
  guard; this phase confirmed how much that costs. The rejected set is wider
  than the plan's `:` example — it also covers `[]()` and `#`, which blocks
  ordinary Next.js routes like `app/[slug]/page.tsx` and `(group)/`. The fix
  is a transport that carries the path via stdin/argv (as `remoteGitHunk`
  already does for its patch), not a weaker filter. Recorded in TODO.md with
  the user-visible workarounds.

## Verification (2026-09-25)

16 tests in `handler_remote_git_conflicts_test.go`, all driving the real remote
code path through the existing `installFakeSSH` harness against a genuine temp
repository — no mocked transport. `go test -run 'RemoteGit|GitConflict|
GitOperation'`, the full `./internal/server/` suite (179s), `gofmt -l`,
`go vet` and `go build ./...` all pass.

Four mutations were confirmed to fail the suite: a fixed `"merge"` string
instead of reading the state files; dropping `remoteSafeSpec`; dropping
`GIT_EDITOR`; and swapping the `gitOperationCommand(kind, action)` arguments —
the last was a real defect this phase introduced and three tests caught it.
