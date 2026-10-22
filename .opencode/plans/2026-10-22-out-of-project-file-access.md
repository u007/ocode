# Out-of-project file editor + preview access (web/desktop)

## Goal

The web/desktop **file editor** and **file preview** must be able to open files at
absolute paths **outside the active project root** (e.g. `~/Downloads/notes.md`,
`/etc/hosts`, a sibling repo) — while keeping the existing security boundary and
**without widening the AGENT's** file scope.

## Verified current behaviour (ground truth)

Established with a probe test (`internal/server/zz_probe_outofproject_test.go`,
temporary) against `internal/server/handler_files.go`:

| Request | Result |
|---|---|
| `GET /api/files/content?path=<ABS outside>` (no `project_root`) | **200 — reads the file** |
| `GET /api/files/content?path=<ABS outside>&project_root=<allowed>` | 400 "path is outside the project root" |
| `GET /api/files/raw?path=<ABS outside>` (no `project_root`) | **200 — serves bytes** |
| `GET /api/files/raw?path=<ABS outside>&project_root=<allowed>` | 400 |
| `PUT /api/files/content` `<ABS outside>` | 400 "path is outside the workspace" |
| `POST /api/open` `<ABS outside>` | 400 "path is outside the working directory" |
| `extra_allowed_paths` entry used as `project_root` | read **200**, save **200**, tree **200** |

**Key finding:** an absolute path with **no** `project_root` is currently
**unscoped** on the read endpoints. The web UI always sends `project_root`, so it
never hits that path. So "no project_root" already *behaves* as "unscoped" — the
problem is that the UI has no way to express it, and the write path is strict.

### Existing containment machinery

- `internal/server/handler_files.go`
  - `resolveProjectFilePath(projectRoot, path)` (:887) — shared by `HandleFileContent` (GET `/api/files/content`).
  - `HandleFileRaw` (:1031) — **inline duplicate** of the same check (:1044-1065).
  - `HandleSaveFileContent` (:1218) — own stricter check (symlink-resolved parent, `filepath.Rel` containment).
  - `HandleMediaToken` (:1137) — lighter pre-check; raw handler re-validates at use time.
  - `fileContentRootFor(root)` (:1337) — root must **exactly equal** an allowed root.
  - `fileTreeRootFor(root)` (:761) — root must be **contained in** an allowed root (tree/search).
  - `containedIn` (:801, resolved) / `containedLexical` (:839, lexical) — the symlink pair.
- `internal/server/handler_fs.go` — `resolveFSPath` (:28) for mutations (copy/move/delete/rename): strict `containedIn` only + `.git` exclusion.
- `internal/server/handler_open.go` — `resolveOpenPath` (:80) / `resolveWithinWorkdir` (:126).
- `internal/server/handler.go:774` — `allowedProjectRoots()`: workDir + saved **LOCAL** project paths. Remote (SSH/WSL) entries deliberately excluded.
- `internal/server/handler_git.go:184` — `mutationProjectDir` (registered root **or** extra allowed path).
- `internal/server/handler_remote_files.go` — `remoteFileContent` / `remoteFileRaw` use `remoteWorkFor(host, root)` + `remoteRelCheck`.

### Web surfaces

- `web/src/hooks/useEditorTabs.ts` — `handleOpenFile(path, projectRoot, host)` → `/api/files/content?path=&project_root=&host=`.
- `web/src/components/Files/FileTree.tsx` — browses `activeRoot` (project path or an extra path) → `onOpenFile(node.path, activeRoot)`. Root `<Select>` shown when `rootOptions.length > 1`.
- `web/src/components/Files/FilePicker.tsx` (Cmd+P) — lists only the active project's tree.
- `web/src/components/Preview/*` — viewers call `api.getFileContent(path, projectRoot, projectHost)` / `api.fetchFileRaw(path, projectRoot, projectHost)`.
- `web/src/components/Preview/PreviewHost.tsx` — anchors a doc to `request.projectRoot ?? projectRoot`.
- `web/src/App.tsx` — `openFileAndShow` resolves host, calls `handleOpenFile`.
- `web/src/components/Settings/PathsForm.tsx` — edits `extra_allowed_paths`.

### Trust-boundary facts (verified)

- `extra_allowed_paths` is **global + persistent** and is **shared with the AGENT**:
  `applyExtraAllowedPathsToRuntime` → `tool.SetPersistentExtraAllowedPaths` → `tool.ExtraAllowedRoots()`
  → `PermissionManager.AllowedRoots()`. So granting a path for the editor would
  **also** let the agent auto-read/write it. **Must not be reused for this feature.**
- The agent's scope (`internal/agent/permissions.go`) does **not** use
  `allowedProjectRoots()` — the two scopes are independent. Good: a server-side
  grant that only the file endpoints consult does **not** touch the agent.
- `authMiddleware` gates the file endpoints; `mediaAuthMiddleware` additionally
  accepts a single-file media capability.

## Design

### 1. New concept: a **file-access grant** (server-side, separate from the agent scope)

A new, **editor-only** allowlist on the `Handler`:

```
h.fileGrants  — set of absolute directory roots the USER has explicitly
                 granted the editor/preview access to.
```

- **Never** consulted by `internal/agent`, `internal/tool`, or the bash/permission layer.
  The agent's scope stays exactly as it is.
- **Not** `extra_allowed_paths` (that one leaks into the agent's scope).
- Persisted **per project** in `.ocode/settings.json`? → **No**: keep it in-memory
  per server process, PLUS surfaced in the UI as a per-session grant. Rationale:
  a persistent global grant is a standing capability; the user's ask here is
  "open this file", not "open this directory forever". Persistence is revisited below.

**Grant API** (new endpoints, auth-guarded):

- `POST /api/files/grant` `{path}` → resolves `path` to an absolute directory
  (the file's parent when `path` is a file), adds it to `h.fileGrants`, returns
  `{root}`.
- `DELETE /api/files/grant` `{path}` → removes it.
- `GET /api/files/grants` → the current list (for the UI).

The **grant is per-server-process and in-memory only** — it dies with the server.
The user re-grants per session. This is the safest thing that still delivers the
feature, and it makes the "outside the project" nature explicit and visible.

### 2. Containment resolution — one shared function

Replace the duplicated logic with a single resolver in `handler_files.go`:

```go
// resolveFileAccess resolves a requested path against the request's anchor.
//
// Precedence:
//  1. project_root given  → must be an allowed root (fileContentRootFor)
//                            OR an explicit grant  → path contained in it
//  2. no project_root     → path contained in ANY allowed root
//                            OR in ANY grant        → resolved
//  3. otherwise           → rejected
```

Concretely, add to `handler_files.go`:

```go
// fileAccessRoots returns every directory root the file surfaces may read
// and write: the server's project roots, the configured extra allowed paths,
// and the user's explicit editor grants. Grants are editor-only — they never
// reach the agent's permission scope.
func (h *Handler) fileAccessRoots() []string
```

and

```go
// resolveFilePath resolves `path` for a file-surface request.
//
// anchorRoot is the request's project_root ("" when absent). The result is an
// absolute path. The rules:
//
//   - A relative path resolves against anchorRoot when one is given, else
//     against the server workDir.
//   - An absolute path is accepted when it is contained (resolved OR lexical,
//     for symlinked folders inside the project) in anchorRoot, in any allowed
//     project root, or in any explicit editor grant.
//
// Traversal (".." in the RAW request value) is always rejected: Join+Clean
// would otherwise collapse "link/../escape" and hide the ".." the OS resolves
// after following the symlink.
func (h *Handler) resolveFilePath(anchorRoot, path string) (string, error)
```

`resolveFilePath` becomes the single implementation used by:
- `HandleFileContent` (read)
- `HandleFileRaw` (read bytes) — **replaces the inline duplicate**
- `HandleSaveFileContent` (write)
- `HandleMediaToken` (token issuance)
- `resolveOpenPath` in `handler_open.go` (OS open/reveal)

**Write path:** the save handler keeps its symlink-resolved-parent check, but the
**root** it checks against becomes the resolved root from `resolveFilePath`
instead of always `h.workDir`. The `expected_hash` conflict guard is unchanged.

**`.git` exclusion:** `resolveFSPath` (mutations) keeps its `.git` exclusion.
For the editor's read/write surface, `.git` inside a project stays excluded from
*mutations* only — reads of `.git/config` were already possible and stay possible
(no behaviour change).

### 3. The web surfaces

**a) Path entry in the Files tab.** A new affordance in the Files tab lets the
user open a path by typing it:
- New button in the FileTree header: "Open path…" (or reuse the existing
  Cmd+P picker with an "enter a path" mode).
- The user types an absolute path → the web calls `POST /api/files/grant` for the
  path's parent directory → then calls `handleOpenFile(path, grantedRoot)`.
  The granted root becomes the tab's `projectRoot`, so **every** subsequent read
  (editor content, preview viewers, media token) is anchored to it and needs no
  special-casing.

**b) Preview.** `PreviewHost` already carries `projectRoot` per doc and passes it
to every viewer. An out-of-project doc therefore needs only its `projectRoot` to
be the granted root — no viewer change. **The preview works once the editor
opens the file** (or when a chat link resolves to an out-of-project path).

**c) Chat file links.** A chat link to an absolute out-of-project path currently
fails with "Failed to load file" (the picker's fallback opens the Cmd+P picker
seeded with the basename). With the grant flow, `openFileAndShow` should detect
the "outside the project root" failure and **offer** to grant + open, rather than
failing silently.

**d) Error UX.** `handleOpenFile` currently logs and returns `false`. It must
surface a **specific** error so the UI can offer the grant. The server's 400 body
already carries `{"error":"path is outside the project root"}` — the web must
read the body (today `fetchFileContent` throws a generic "Failed to load file").

**e) FilePicker (Cmd+P).** Add an "absolute path" mode: if the query looks like an
absolute path, offer it as a direct result (opening it grants the parent dir).

### 4. Remote (SSH/WSL) projects

`remoteFileContent` / `remoteFileRaw` run on the **remote host's** own server,
whose `allowedProjectRoots` is the remote project path. For out-of-project remote
files the **remote host** would need its own grant concept. Scope decision:
**implement the local path fully; for remote, the remote server's own
`/api/files/grant` is reached through the existing proxy** — the proxy forwards
`/api/files/*` to the host, so a grant issued on the host works with no extra
plumbing, PROVIDED the remote handler also implements the grant endpoints.
→ Implement the grant endpoints in the shared handler; the remote host runs the
same code, so it gets them for free. The web must send the grant request to the
right host (the proxy path `/api/remote/{host}/api/files/grant`).

### 5. What is explicitly NOT changed

- The agent's permission scope (`internal/agent/permissions.go`,
  `internal/tool/file.go`) — untouched.
- `extra_allowed_paths` — untouched (still the global, agent-shared list).
- `allowedProjectRoots()` — untouched (still the project trust boundary).
- `resolveFSPath` (copy/move/delete/rename) — untouched; mutations stay bound to
  a project root / extra path. **The editor can open a file outside the project;
  the Files-tab rename/delete of it stays project-scoped.** (Decide: should
  rename/delete follow the granted root? → **No** — keep the blast radius
  minimal; a granted file is openable and editable in place.)
- `POST /api/open` (OS open/reveal) — keeps working for granted paths, because
  `resolveOpenPath` shares the resolver. (Reveal of an arbitrary directory is
  the one place this could surprise; it is still gated by the grant.)

## Risks & gotchas to design around

1. **`fileContentRootFor` requires an EXACT root match.** A subdirectory of a
   project is NOT a valid root. The grant must therefore store the exact
   directory it granted, and the web must pass that exact string as
   `project_root`.
2. **Symlink semantics.** `containedLexical` exists so the Files tab can browse
   linked folders; reads accept lexical containment, writes must not. The resolver
   must keep the read/write asymmetry.
3. **`containsDotDot` on the RAW value** — must be checked before Join/Clean.
4. **Absolute path + no `project_root` is currently unscoped.** After this
   change it must be **scoped** (contained in an allowed root or a grant). This
   closes an existing LFI-shaped hole rather than preserving it.
5. **Media token flow** — `HandleMediaToken` must use the same resolver, and the
   raw handler re-validates at use time (keep that property).
6. **`HandleFileRaw`'s inline duplicate** — replacing it with the shared resolver
   is a behaviour change for the "absolute path, no project_root" case: it goes
   from 200-unscoped to 200-scoped. That is the intended tightening.
7. **The web must read the error body**, not just the status code, to distinguish
   "outside the project root" (offer grant) from "file not found".
8. **Grant lifetime** — in-memory per server process. The desktop app restarts
   lose grants; the user re-grants. Acceptable and safest.

## Implementation steps

1. `internal/server/handler_files.go`
   - Add `fileGrants` storage + `fileAccessRoots()`.
   - Add `resolveFilePath(anchorRoot, path)`; make `resolveProjectFilePath` use it.
   - Rewrite `HandleFileContent`, `HandleFileRaw`, `HandleSaveFileContent`,
     `HandleMediaToken` to use it. **Delete the inline duplicate in
     `HandleFileRaw`.**
   - `internal/server/handler_open.go`: `resolveOpenPath` uses it.
   - `internal/server/handler_grants.go` (new): the three grant endpoints.
   - `internal/server/server.go`: route the new endpoints.
2. `internal/server/handler_remote_files.go`: remote content/raw use the remote
   host's own resolver (the remote server has the same code) — verify the
   project_root-less absolute path case is scoped there too.
3. Web:
   - `web/src/api/client.ts`: `grantFilePath`, `revokeFileGrant`, `listFileGrants`.
   - `web/src/hooks/useEditorTabs.ts`: surface the server's error message
     (read the body); keep the `false` return contract.
   - `web/src/components/Files/FileTree.tsx`: "Open path…" affordance.
   - `web/src/components/Files/FilePicker.tsx`: absolute-path mode.
   - `web/src/App.tsx`: `openFileAndShow` — on an "outside the project root"
     failure, offer grant + retry.
4. Tests:
   - Go: grant endpoints; read/save/raw/open with a grant; absolute path with
     NO project_root is now scoped (regression for the LFI hole); traversal
     still rejected; symlink asymmetry preserved; remote path.
   - Web: api client, useEditorTabs error surfacing, FileTree open-path flow,
     FilePicker absolute mode, App grant-retry flow.
5. Docs: `CHANGES.md` entry; `skills/ocode-web/SKILL.md` (file map + gotchas);
   `docs/concepts/web-server-project-scoping.md` via the **context agent**
   (docs/ is OKF-bundle-owned — the main agent must not write there directly).
