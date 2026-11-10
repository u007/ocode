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
timestamp: 2026-11-09T17:22:16Z
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

`handler.go:319` carries the field; `handler_file_grants.go:44` defines the
store; `handler_file_grants.go:151` lazily creates it.

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

Registered in `Server.registerRoutes()` (`server.go:419-421`), all behind `authMiddleware`:

- `POST /api/files/grant` — body `{"path": "..."}` → `{"root","path","granted","roots"}`.
  `path` is the requested file spelled under the canonical `root` (symlinks resolved;
  the root itself when the path named a directory) via `canonicalGrantFile`, so the web
  can place it relative to the root.
- `DELETE /api/files/grant` — revokes
- `GET /api/files/grants` — lists

`canonicalGrantRoot` (`handler_file_grants.go:61`) makes the granted root
**symlink-resolved**, and a path naming a **file** grants its **parent
directory** (so sibling files in that directory are reachable). Handlers:
`HandleGrantFilePath` (`:164`), `canonicalGrantFile` (`:197`, the `path` the grant
response returns), `HandleRevokeFilePath` (`:215`), `HandleListFileGrants` (`:236`).

### Access probe (`GET /api/files/access`)

`GET /api/files/access?path=…&project_root=…&host=…` (route at `server.go:411`;
`Server.handleFileAccess` calls `Handler.HandleFileAccess`, `handler_files.go:1333`)
answers one question: would the editor serve this path? It is **containment
only**. A local path runs the same resolver as the raw endpoint
(`resolveEditorFilePath`, `handler_files.go:992`), so it honors the same grants. A
remote path (`host=`) runs `remoteRawContainment` (`handler_remote_files.go:466`),
the helper `remoteFileRaw` also uses. The response is `200 {"ok":true}`, or `400`
with the resolver's message. The probe never stats, reads or type-checks the file.

It exists because preview-only kinds (image, pdf, office, media) skip the content
fetch, so the tab must ask the server before it appears. `HEAD` on
`/api/files/raw` is not used: refusals are 400 bodies, and `HEAD` has no body. The
client is `api.checkFileAccess` (`web/src/api/client.ts`), wrapped by
`isOutsideProjectRoot` (`web/src/components/Preview/outsideRootAccess.ts`), which
returns true only for an outside-root refusal.

## One resolver, two scopes

`resolveProjectFilePath` (`handler_files.go:981`) delegates to
`resolveProjectFilePathScoped` (`:971`) with a `filePathScope`
(`handler_files.go:943`):

- **`scopeProjectRoots`** — the SQLite browser. `resolveDBPath`
  (`handler_db.go:91`) keeps its OWN unconditional `pathWithinAllowedRoots`
  (`handler_db.go:50`) check and must **not** accept grants: the DB surface gains
  write capability later, so its blast radius is different.
- **`scopeEditorRoots`** — `resolveEditorFilePath` (`handler_files.go:992`), used
  by `HandleFileContent` (`:1125`), `HandleFileRaw` (`:1240`),
  `HandleSaveFileContent` (via `writeScopeRoot`, `:1070`) and
  `HandleMediaToken` (`:1367`). `resolveOpenPath` shares the rules
  (`handler_open.go:95`, `:115`).

`grantedRootFor` (`:1025`) is the only way a request may name a directory outside
every project root. `fileAccessRoots` (`:1044`) = project roots + extra allowed
paths + grants; `pathWithinAnyFileRoot` (`:1113`) checks resolved **or** lexical
containment (the Files tab browses symlinked folders).

`HandleFileRaw` used to carry an **inline duplicate** of the containment logic;
it now calls the shared resolver, so the two cannot drift.

## The closed LFI-shaped hole

Before the scope split, an **absolute path with no `project_root`** was returned
verbatim with **no containment check at all** on `GET /api/files/content` and
`GET /api/files/raw` — a file outside every root read back 200. The web always
sent a `project_root`, so the UI never hit it, but the endpoint was reachable.
`scopeEditorRoots` now confines that case
(`handler_files.go:1041` → `pathWithinAnyFileRoot`). Do not restore the old
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

`fileTreeRootForEditor` (`handler_files.go:782`) = `fileTreeRootFor` + grants, used
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

## Opening a folder (directory grant)

A path that names a **directory** (rather than a file) is also grantable: `canonicalGrantRoot`
returns the directory itself and `canonicalGrantFile` returns the root, so the grant's relative
path is `.`. The client treats such a grant as a successful "open folder": no editor tab is
created, the granted directory is added to the Files tab's root selector (via
`/api/files/grants`), and the tree switches its active root to it. The agent's scope is
unaffected — the grant is editor-only, exactly like a file grant.

## Outside-root paths reach the grant prompt from every surface (2026-11-03)

Every surface that opens a file asks the server before it shows the file. Only an
outside-root refusal becomes a grant offer; any other failure keeps the surface's
own error. Preview-only kinds (`PREVIEW_ONLY_KINDS`: pdf, docx, pptx, excel, image,
audio, video) and legacy `.doc`/`.ppt` skip the `/api/files/content` fetch, which is
the only containment check a text file gets before its tab exists. So each surface
probes the access route first:

- **Files tab.** `useEditorTabs` `handleOpenFile`'s preview-only branch calls
  `api.checkFileAccess`. On any failure it releases the open claim, logs, and
  returns `false`. `App.tsx`'s `openFileAndShow` then calls
  `requestOpenOutsideProject(path)`, but only for an absolute path opened with
  `fallbackQuickOpen` (chat file links). Other callers get no offer. The branch
  returns `false` for every probe failure, not only containment refusals, so an
  absolute path that failed for another reason also reaches the grant dialog. Test: `web/src/hooks/useEditorTabs.outsideProject.test.ts`.
- **Sidebar `PreviewHost` and the session Preview sub-tab (`PreviewTabPage.tsx`).**
  Both probe the selected doc of any kind with `isOutsideProjectRoot`. On an
  outside-root refusal they show `OutsideRootGrantPrompt`
  (`web/src/components/Preview/OutsideRootGrantPrompt.tsx`) instead of the viewer.
- **Clicking the prompt's button is the confirmation.** It calls
  `api.grantFilePath`, then the pane re-anchors the doc to the granted root
  (`anchorForGrant`, `web/src/lib/outOfProjectFiles.ts`). Nothing is granted
  automatically.
- **Relative outside paths.** The prompt names the file's absolute path.
  `resolveAgainstRoot` resolves the request against the anchor root first, and the
  grant is made with that absolute path.
- **`grantedFrom`.** A granted sidebar doc records the root it was granted from.
  `docBelongsHere` accepts a doc whose `grantedFrom` matches the pane's root, so a
  granted doc survives a remount with the granted root.
- **Restore is silent.** A restored sidebar doc whose probe is outside-root is
  cleared with no prompt on page load. Restored editor tabs that fail the access
  check are dropped quietly by the restore effect in `useEditorTabs.ts`.

Known gaps (5a and 5b are tracked in `TODO.md`):

- If the sidebar panel unmounts while its probe is in flight, the activation is
  still consumed once. An inside-project result is saved to the session's sidebar
  state and restored on the next mount. An outside-root result is dropped, because
  the grant prompt needs a mounted panel; the file must be opened again.
- The remote branch of `HandleFileAccess` has no direct test. It shares
  `remoteRawContainment` with `remoteFileRaw`.
- Grants are in-memory, so after a server restart a granted tab or sidebar doc is
  dropped on reload and must be re-granted.

The lexical-check gaps from the earlier round (symlinked spellings such as
`/var` vs `/private/var`, and no check when `projectRoot` is undefined) are closed
by the server probe.

## Home-directory `~` links

A chat link to `~/...` opens through the same editor path as any out-of-project
file (`MarkdownLink` → `openFile` → the Files tab). The browser cannot read
`$HOME`, so the server expands the `~`:

- `resolveProjectFilePathScoped` expands it with `projects.ExpandHome` **before**
  the containment checks. An expanded path outside every project root is still
  refused (`path is outside the project root`), so a `~` link needs an editor
  grant exactly like any other absolute out-of-project path.
- `canonicalGrantRoot` and `canonicalGrantFile` expand it too. Before this,
  `filepath.Abs` resolved `~/x` against the server's cwd, so the grant step
  pointed at the wrong directory.
- `resolveOpenPath` (OS-app open and reveal) does **not** expand `~`. The desktop
  open-external bridge stays http(s)-only, so local files never reach the OS
  browser through it.

Pinned by `TestFileGrant_HomeTildePathExpandsThenContained`
(`internal/server/handler_file_grants_test.go`). Why other link shapes stay
no-op: [chat file links silent no-op](../gotchas/chat-file-links-silent-noop.md).

Prose auto-linkification is a separate path from an authored `[x](~/...)` href:
[the prose auto-linkifier](../gotchas/chat-file-links-silent-noop.md#prose-auto-links-vs-markdown-hrefs).

## Verification

`internal/server/handler_file_grants_test.go` (10 cases: read/save outside,
sibling file, containment after revoke, traversal, save cannot escape, list,
tree root, absolute-path-without-root scoping, agent-scope separation),
`web/src/lib/outOfProjectFiles.test.ts`,
`web/src/hooks/useEditorTabs.outsideProject.test.ts`,
`web/src/components/Files/OpenPathDialog.test.tsx`. Live-verified against a
running server: refuse → grant → read → save → traversal still 400 → revoke →
refuse.

## See also

- [Office add-in path presets](office-addin-path-presets.md) — the Settings-side
  consumer of `extra_allowed_paths` (the list this page deliberately keeps
  separate from editor grants), including the save-time macOS access request.
