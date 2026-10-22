---
type: Concept
title: File Editor Grants — Editor-Only Out-of-Project Access
description: The editor/preview opens files outside the active project through per-request, in-memory grants separated from the agent's file scope; records the LFI-shaped hole the scope split fixed.
resource: internal/server/handler_file_grants.go, internal/server/handler_files.go, internal/server/handler_open.go, web/src/lib/outOfProjectFiles.ts
tags:
  - security
  - file-access
  - server
  - editor
  - agent-scope
  - grants
timestamp: 2026-10-22T12:18:01Z
---

# File Editor Grants — Editor-Only Out-of-Project Access

The web/desktop **file editor** and **preview** can open a file at an absolute
path *outside* the active project (`~/Downloads/notes.md`, a sibling repo,
`/etc/hosts`). Such a path is **granted** first, and a grant is deliberately a
different thing from every other allowlist this server keeps.

## Three separate allowlists — do not merge them

| List | Consulted by | Lifetime |
|---|---|---|
| `allowedProjectRoots()` | sessions, terminal cwd, file tree/content, `/init`, sysperm | saved `projects.json` (local entries only) |
| `cfg.Ocode.ExtraAllowedPaths` | the file surfaces **and the agent's tool scope** | global config, persistent |
| **editor grants** (`fileGrantStore`) | the file editor/preview surfaces **only** | in-memory, per server process |

`handler.go:273` carries the field; `handler_file_grants.go:41` defines the
store; `handler_file_grants.go:143` lazily creates it.

### Why grants are NOT `extra_allowed_paths`

`extra_allowed_paths` is written through
`applyExtraAllowedPathsToRuntime` → `tool.SetPersistentExtraAllowedPaths` →
`tool.ExtraAllowedRoots()` → `PermissionManager.AllowedRoots()`. Adding a path
there so the **editor** can open a file would also stop the **agent** from
asking permission for that path — a silent widening of the agent's read/write
scope. A grant never reaches `internal/agent` or `internal/tool`.

`TestFileGrant_DoesNotWidenAgentScope` pins this: after a grant, the path is in
`fileAccessRoots()` but absent from `dbRoots()` and `pathWithinAllowedRoots`.

### Lifetime

Grants are **in-memory and per server process** — they die on restart, so the
user re-grants. A persisted grant would be a standing capability the user did
not ask for; the intent is "open *this* file", not "open this directory
forever". Do not move them into config without re-opening the agent-scope
question.

## Endpoints

`server.go:394-396`, all behind `authMiddleware`:

- `POST /api/files/grant` — body `{"path": "..."}` → `{"root","path","granted","roots"}`.
  `path` is the requested file spelled under the canonical `root` (symlinks resolved;
  the root itself when the path named a directory) via `canonicalGrantFile`, so the web
  can place it relative to the root.
- `DELETE /api/files/grant` — revokes
- `GET /api/files/grants` — lists

`canonicalGrantRoot` (`handler_file_grants.go:58`) makes the granted root
**symlink-resolved**, and a path naming a **file** grants its **parent
directory** (so sibling files in that directory are reachable). Handlers:
`HandleGrantFilePath` (`:156`), `canonicalGrantFile` (`:189`, the `path` the grant
response returns), `HandleRevokeFilePath` (`:203`), `HandleListFileGrants` (`:224`).

## One resolver, two scopes

`resolveProjectFilePath` (`handler_files.go:955`) delegates to
`resolveProjectFilePathScoped` (`:970`) with a `filePathScope`
(`handler_files.go:917`):

- **`scopeProjectRoots`** — the SQLite browser. `resolveDBPath`
  (`handler_db.go:91`) keeps its OWN unconditional `pathWithinAllowedRoots`
  (`handler_db.go:50`) check and must **not** accept grants: the DB surface gains
  write capability later, so its blast radius is different.
- **`scopeEditorRoots`** — `resolveEditorFilePath` (`handler_files.go:966`), used
  by `HandleFileContent` (`:1127`), `HandleFileRaw` (`:1247`),
  `HandleSaveFileContent` (via `writeScopeRoot`, `:1061`) and
  `HandleMediaToken` (`:1346`). `resolveOpenPath` shares the rules
  (`handler_open.go:95`, `:115`).

`grantedRootFor` (`:1016`) is the only way a request may name a directory outside
every project root. `fileAccessRoots` (`:1035`) = project roots + extra allowed
paths + grants; `pathWithinAnyFileRoot` (`:1104`) checks resolved **or** lexical
containment (the Files tab browses symlinked folders).

`HandleFileRaw` used to carry an **inline duplicate** of the containment logic;
it now calls the shared resolver, so the two cannot drift.

## The closed LFI-shaped hole

Before the scope split, an **absolute path with no `project_root`** was returned
verbatim with **no containment check at all** on `GET /api/files/content` and
`GET /api/files/raw` — a file outside every root read back 200. The web always
sent a `project_root`, so the UI never hit it, but the endpoint was reachable.
`scopeEditorRoots` now confines that case
(`handler_files.go:1007` → `pathWithinAnyFileRoot`). Do not restore the old
early return.

## Read vs write asymmetry

Reads accept resolved **or lexical** containment so a symlinked folder inside the
project stays browsable. Writes stay strict: `writeScopeRoot` picks the most
specific containing root, then the save path resolves the target's **parent**
symlinks and re-checks containment against the real root. `expected_hash`
conflicts still 409. A `..` component in the **raw** request value is rejected in
every scope (`containsDotDot`, checked before `Join`/`Clean`, because
`link/../escape` collapses lexically but the OS resolves `..` after following
`link`).

## Tree and search, but not the secret surface

`fileTreeRootForEditor` (`handler_files.go:756`) = `fileTreeRootFor` + grants, used
by `HandleFileTree` and `parseSearchParams`. `resolveSecretPath` deliberately
keeps plain `fileTreeRootFor`: encrypting/decrypting a directory is a far
stronger action than browsing it.

## Web contract

`web/src/lib/outOfProjectFiles.ts` owns the client half:

- `isOutsideRootError` recognises the server's containment refusals so the UI can
  offer a grant instead of failing silently — which requires
  `useEditorTabs.fetchFileContent` to throw the server's `{"error": ...}` body,
  not a generic "Failed to load file".
- `relativeToRoot` enforces the separator boundary. The granted root becomes the
  tab's `projectRoot` and the file is opened **relative to it**, so every later
  read (editor content, preview viewers, media token) is anchored without special
  cases. A bare `slice(root.length)` would turn `root=/a/b` + `/a/bc/d` into
  `c/d`. It normalises `.`/`..` lexically (a path escaping the root returns
  `null`), accepts mixed `/` and `\` for Windows-style paths, folds case only for
  Windows drive/UNC roots (POSIX stays case-sensitive), and keeps `/` and `C:\`
  as real roots (the remainder is relative to them).
- `openOutsideProject` computes that relative path from the server's `path` and
  `root` (both canonical), never from the typed path, and there is **no fallback**
  to the absolute path. A `null` from `relativeToRoot` throws
  `Cannot open <typed>: it is not inside the granted folder <root>.`, which
  `App.tsx` surfaces through `reportActionErrorMessage`.
- `OpenPathDialog` is the single entry point (Files-tab header + a chat file link
  whose absolute path the server refused, pre-filled).

Remote (SSH/WSL) grants route through `remoteApiBase(host)`, so they land in the
**remote** server's own grant list.

## Verification

`internal/server/handler_file_grants_test.go` (10 cases: read/save outside,
sibling file, containment after revoke, traversal, save cannot escape, list,
tree root, absolute-path-without-root scoping, agent-scope separation),
`web/src/lib/outOfProjectFiles.test.ts`,
`web/src/hooks/useEditorTabs.outsideProject.test.ts`,
`web/src/components/Files/OpenPathDialog.test.tsx`. Live-verified against a
running server: refuse → grant → read → save → traversal still 400 → revoke →
refuse.
