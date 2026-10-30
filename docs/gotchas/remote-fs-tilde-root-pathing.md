---
type: Gotcha
title: 'Remote fs actions on a "~"-rooted project: root-joined paths and $HOME quoting'
description: 'Why remote file operations on a project rooted at a ~ path fail: double-joined paths, unexpanded tilde in shell-quoted commands, and the root-as-paste-destination gap.'
tags: [gotcha, remote-ssh, files]
timestamp: 2026-10-30T13:33:56Z
---
# Remote fs actions on a "~"-rooted project: root-joined paths and $HOME quoting

**Type:** Gotcha  
**Description:** Why remote file operations on a project rooted at a ~ path fail: double-joined paths, unexpanded tilde in shell-quoted commands, and the root-as-paste-destination gap.  
**Tags:** gotcha, remote-ssh, files  

---

## Symptom

On a remote SSH project whose saved path is `~/www/aimsweb`, the Files tab's "New file…" answers `parent directory of "~/www/aimsweb/Makefile" does not exist`. Rename and New folder also fail; Delete appears to succeed but the file stays; download and paste-upload fail.

## Two independent causes, one family

### 1. Double-joined path

`remoteAbsJoin` (`internal/server/handler_remote_work.go`, function `remoteAbsJoin`) decided "already absolute" with a leading-`/` test. The web file tree sends ROOT-JOINED paths (`treePathForRequest` in `web/src/components/Files/FileTree.tsx` builds `~/www/aimsweb/Makefile` for the tree view; only the columns view sends root-relative ones). A tilde root is not `/`-prefixed, so the request path was prefixed onto the root a SECOND time: `~/www/aimsweb/~/www/aimsweb/Makefile`, whose parent does not exist.

Fix: a path equal to the root, or prefixed by `<root>/`, passes through unchanged. The comparison uses a slash-trimmed root (`remoteRootPrefix`), and `remoteRelCheck` trims the same way, so a root saved as `~/www/aimsweb/` behaves identically to `~/www/aimsweb`.

### 2. Tilde never expanded

Every mutation built its shell command with `remote.ShellQuote`, which single-quotes: `'~/www/aimsweb/x'`. POSIX never expands `~` inside single quotes. Worse, `rm -rf '~/…'` exits 0 on a path that does not exist, so DELETE SILENTLY NO-OPED while the API returned success.

Fix: path operands use `remote.ShellQuotePath`, which translates a leading `~/` to `"$HOME/…"` (double-quoted parameter expansion, still injection-safe because the rest is escaped). Affected sites:

- `handler_remote_fs.go` — new file (`: > `), new folder, rename/copy/move (`mv`/`cp -a`), delete (`rm -rf`)
- download `cat --` in `handler_fs_download.go`
- upload O_EXCL write script `remoteExclusiveWriteScript` plus its failure cleanup in `handler_fs_upload.go`
- git "discard untracked file" `rm -f` in `handler_remote_git_actions.go`

`remote.ShellQuote` remains correct for NON-path words (refs, commit messages, git pathspecs).

## Third rule: the project root is a valid paste destination

`remoteTargetOf` rejects a resolved `"."` so a delete/rename can never target the whole project. Copy and move need the opposite for their DESTINATION ("paste to the root folder"), which the local handler already allowed. It gained a `dest` mode: the target must exist and be a directory, and `.` is allowed. Only `dest` accepts `.`; `read` and `create` still refuse the root.

## Residuals / notes

- **The columns (Miller) view's "New file…" / "New folder…" beside a ROOT-LEVEL file is fixed in the same change.** That view passes root-RELATIVE node paths, and the menu items used `parentDir(node.path)`; for a bare name `parentDir` returns the name itself, so the request became `Makefile/newfile.txt` (parent is a file, not a directory). Both views now go through one exported helper, `newEntryDir(projectRoot, nodePath, isDir)` in `web/src/components/Files/FileTree.tsx`, which anchors the path with `treePathForRequest` FIRST and then takes the parent — the same order every other request path in the tree uses. The tree view's behaviour is unchanged. Pinned by `web/src/components/Files/FileTree.test.ts` (`newEntryDir`: root-level file, nested file, folder, trailing-slash root, tilde root; mutation-verified — reverting to `parentDir(nodePath)` fails 5).
- A path spelled with a different root anchor than the saved project (e.g. request `~/x/y` against a root stored expanded as `/home/me/x`) is still not recognised as anchored; the web client derives both from the same saved string, so this does not occur in practice.
- A remote project rooted at `/` now resolves (the trailing-slash trim makes `<root>/` the `/` prefix); before, the doubled slash made every operation refuse.

## Regression tests

`internal/server/handler_remote_fs_test.go`: the `remoteAbsJoin` table (tilde root, nested, root itself, trailing slash, tilde home, absolute root, sibling-prefix, empty path), `TestRemoteRelCheckToleratesTrailingSlashRoot`, and fake-ssh handler tests that really create/rename/delete/copy/download under `$HOME/www/aimsweb` (`setHomeTree` + `installFakeSSH`): new file at the root and in a subdirectory, trailing-slash root, copy into the root, delete, rename, new folder, the download body, the `remoteStreamFile` write script, and untracked-file discard. All mutation-verified (8 mutants, each compiled first).

On the web side, `web/src/components/Files/FileTree.test.ts` covers `newEntryDir`, the single rule both the tree and columns views use for a new entry's directory.

Client-side, EVERY `api.fs*` call now sends the browsed root as `?project=` AND anchors each path to it (`requestRoot` + `treePathForRequest` / `newEntryDir` in `FileTree.tsx`), so the pair can never disagree; selection state, the clipboard and the `ocode:fs-*` event payloads stay NODE-RELATIVE. The server still accepts a root-relative destination too (`remoteAbsJoin` joins it), which is why `TestRemoteCopyIntoRelativeSubdirectory` pins the relative form independently.

## Sub-folder scope: `?project=` is a root OR a path inside one

Read endpoints have always admitted a path inside a registered project (`?path=<root>/<child>`) by matching it through its longest containing root (`fileTreeRootFor` locally, `remoteTreeRoot` remotely). Mutations used to demand an EXACT registered root, so any caller scoping a write to a sub-folder got `400 unknown project` (local) or `host/project_path is not a remote project registered with this server`. Mutations now match the read rule, with one asymmetry that matters:

| Surface | Resolver | Symlinked subdirectory |
|---|---|---|
| reads | `fileTreeRootFor` / `remoteTreeRoot` | tolerated lexically (so linked folders can be browsed) — documented residual |
| mutations, remote | `remoteMutationWorkFor` → `remoteWorkFor` | REFUSED: one `cd <root> && pwd -P` / `cd <sub> && pwd -P` command proves the canonical directory is still inside the project |
| mutations, local | `mutationProjectDir` | REFUSED: strict symlink-resolved `containedIn`; a relative `?project=` is refused outright (it must never resolve against the server process CWD) |

The rule exists because the requested path becomes the ANCHOR: `resolveFSPath` / `remoteRelCheck` then contain every write inside that subtree, so a sub-path narrows what is reachable and never widens it. A registered root keeps the exact fast path (no extra remote round trip). Pinned by `internal/server/handler_fs_subproject_test.go` and the `TestRemoteFSSubProject*` tests in `internal/server/handler_remote_fs_test.go` (sub-directory accepted, outside rejected, symlinked sub-directory rejected with no file created, relative param rejected; all mutation-verified).

Also note in that section: `decodeFSRequest` no longer reports a refused project as `invalid request body` — it says `unknown project`, because the JSON had parsed fine.