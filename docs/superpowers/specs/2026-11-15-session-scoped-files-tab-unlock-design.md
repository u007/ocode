---
type: Design
title: Session-scoped Files-tab unlock (design)
description: Design spec for letting the web/desktop Files tab, editor and preview open any directory for the current chat session only — on local, /rc and remote SSH projects — through an editor-only, session-scoped grant store with a per-host wildcard unlock, and with no change to the agent's tool or permission scope.
tags:
  - files
  - editor
  - grants
  - sessions
  - remote-ssh
  - web
  - design-spec
timestamp: 2026-11-15T12:23:20Z
---

# Session-scoped Files-tab unlock (design)

**Date:** 2026-11-15
**Status:** Approved — gate A (click to unlock). Code anchors derived against the tree on 2026-11-15.
**Scope:** web/desktop Files tab, file editor, and preview surfaces only. The agent's tool scope and permission roots do not change.

## 1. Problem

The editor and preview may only reach: the server's project roots, `ocode.extra_allowed_paths`, and in-memory editor grants. Three gaps make that insufficient:

1. Grants are **process-wide**, so two chats in the same server share them, and they outlive the chat that created them.
2. Grants carry **no host**, so a remote project cannot use them at all.
3. **Remote projects refuse every out-of-project path** except scratch roots (`/tmp`, `/var/tmp`, `/var/folders`).

The user wants to browse and edit any directory from the Files tab, for the current chat session only, locally, over `/rc`, and on remote SSH projects.

## 2. Requirements

- **R1 Session-scoped.** A grant applies only to the session that created it. Session A's unlock never authorises session B.
- **R2 Lifetime.** A grant dies with the server process, with the session switch, or on explicit revoke. Nothing is persisted.
- **R3 Surfaces.** Local projects, `/rc` (TUI-hosted server), and remote SSH projects.
- **R4 Editor-only.** The unlock never widens the agent's file-tool or `bash` scope, nor the permission manager's allowed roots.
- **R5 Gate A.** Unlock is a button click. No password, no OS prompt. Options B (OS prompt / Touch ID) and C (browser password) are deferred, not built.
- **R6 No session, no unlock.** If the Files tab is open with no chat session in that project, the unlock is refused and the dialog explains why.

**Non-goals:** root-owned paths (a privileged helper is a separate design), persistent grants across restarts, per-window scoping, and any agent-tool reach.

## 3. Current behaviour (verified 2026-11-15)

- `fileGrantStore` (`internal/server/handler_file_grants.go:44`), lazily created by `fileGrants()` (`:151`). Referenced only from `handler.go`, `handler_file_grants.go` and `handler_files.go` — all editor surfaces.
- `fileAccessRoots` (`internal/server/handler_files.go:1069`) = project roots + `ocode.ExtraAllowedPaths`. Called only from `handler_files.go:1107` and `:1139`.
- `resolveProjectFilePathScoped` (`handler_files.go:996`) and `resolveEditorFilePath` (`:992`) implement the containment rules. The editor scope adds grants through `grantedRootFor` (`:1050`).
- Routes: `POST/DELETE /api/files/grant` and `GET /api/files/grants` (`server.go:487-489`). None accepts a session id or a host.
- Remote reads: `remoteFileContent` (`handler_remote_files.go:477`) calls `remoteWorkForFileRead` (`handler_remote_work.go:121`). Out-of-project roots are admitted only by `isAllowedScratchRoot` (`:161`).
- Remote saves: `remoteSaveFileContent` (`handler_remote_files.go:589`) calls `remoteWorkFor` (`handler_remote_work.go:101`), which requires a registered project or a tree root, then `remoteRelCheck` (`:837`). `remoteMutationWorkFor` (`:178`) serves git, `/api/fs/*` and uploads; it is not the editor save path.
- **Every remote file operation branches to the SSH handlers** (`handler_files.go:39`, `:710`, `:1152`, `:1274`, `:1341`, `:1391`, `:1577`, `:1739`; `remoteFileTree` at `handler_remote_files.go:199`). None of them consults a grant list.
- **The SPA already sends grants to the remote host's own server** (`web/src/api/client.ts:3757-3787`, via `remoteApiBase(host)`; documented at `docs/concepts/file-editor-grants.md:164`). Because the SSH read path never consults that list, **a remote grant is stored and then unusable today** — a pre-existing defect this design resolves.
- File reads and saves on a remote project travel over the local server's SSH transport, not through the `/api/remote/{host}/api/{rest}` proxy (`server.go:579`).
- `/rc` starts the same Handler and binds the TUI session through `RegisterExternalSession` (`server.go:2647`) and `BindNewOrVerify`. Session ids are transport-independent.
- `SessionManager.Rekey` (`session_manager.go:418`) moves the registry entry only; it has no hook for other session-keyed maps. `handler_reset_id.go` orders a reset as: 409 while a turn is active, `session.FlushForDir(..., 10s)`, `sessions.ReleaseAgent(id)`, then the rekey.
- Media tokens: `HandleMediaToken` (`handler_files.go:1500`) admits via `resolveEditorFilePath`, then issues a token that `/api/files/raw` re-validates at use time.
- The agent-scope separation is already pinned by `TestFileGrant_DoesNotWidenAgentScope` (`internal/server/handler_file_grants_test.go`).

## 4. Design

### 4.0 One authority rule

**The server that performs the file operation holds the grant.** For an SSH-proxied remote project that is the local server; for a browser connected directly to a remote server it is that server. The SPA sends grant, unlock, revoke and list to the server that performs the operation.

Rejected alternative: consult the remote server's own grant list on every read. It costs a network hop per file read and makes revocation coherence a cache problem, on a path that already pays an SSH round trip. It also contradicts the rule that remote file operations stay per-request.

### 4.1 Store

A new `sessionFileGrants` holds `map[sessionID]*sessionGrant`, where each `sessionGrant` keeps `roots map[host]map[root]string` and `wildcard map[host]bool`. The empty host key means the local machine.

- The store has its own mutex, for the same reason `fileGrantStore` has one (`handler_file_grants.go:41-46`): the grant list sits on the hot path of every file read, and `h.mu` is the map lock every endpoint takes. `h.mu` is taken only to create and publish the store, never while a lookup runs. Lock order is `h.mu` first, released, then the grant mutex; the reverse must never exist.
- Lookups are pure reads: `allows(sessionID, host, path)` and `rootsFor(sessionID, host)`.
- A wildcard for a host short-circuits containment for that host only.
- The store never emits an SSE event, so no `sessionScopedEvents` or `liveFrameEvents` entry is added; a reconnecting tab re-reads the list.

### 4.2 Endpoints

- `POST /api/files/unlock` with `{session_id, host?}` sets the wildcard for that session and host.
- `POST /api/files/grant` keeps its current body and gains `session_id` and optional `host`.
- `DELETE /api/files/grant` gains `session_id` and optional `host`.
- `GET /api/files/grants` gains `session_id` and optional `host`.

A missing or unknown `session_id` is a 400. Existence is checked through the session registry, which already holds RC-registered sessions.

### 4.3 Local admission

`fileAccessRootsFor(sessionID)` replaces `fileAccessRoots()`. It returns project roots, `extra_allowed_paths`, and the session's roots for the local host. `resolveProjectFilePathScoped` and `resolveEditorFilePath` gain a session parameter; the wildcard bypasses containment, and every existing rule (dot-dot rejection, write-side symlink strictness, save hash checks) stays.

Every local editor endpoint carries `session_id`: content, raw, access probe, stat, tree, search, media-token, save, save-raw, and open/reveal.

### 4.4 Remote read admission

`remoteWorkForFileRead` gains the session id. A path is admitted when the session has a wildcard for that host, or when `root` is one of that session's roots for that host. The scratch-root rule remains as the fallback for callers with no session.

### 4.5 Remote write admission

`remoteSaveFileContent` and `remoteSaveFileRaw` must use the same session-aware admission that reads use, before `remoteWorkFor` rejects the root, and must keep `remoteRelCheck` bounded by the granted root. `remoteMutationWorkFor` stays project-only: git, `/api/fs/*` and uploads do not gain foreign roots in this design.

### 4.6 Files tree

A granted root is a valid tree root for the local tree endpoint, and for the remote tree path where `remoteTreeRoot` currently recognises only registered projects. The tree and search endpoints take the same `session_id`.

### 4.7 Media tokens

The media token records the session id and the admitted root. `/api/files/raw` re-runs the session-aware admission at use time; the token stays a capability, never a substitute for the check.

### 4.8 Client

- `useEditorTabs` appends `session_id` (the active chat session) and `host` to every file call, including save.
- `grantFilePath`, `revokeFileGrant` and `listFileGrants` send `session_id` and `host` to the performing server; `remoteApiBase(host)` is no longer used for grants.
- `OpenPathDialog` gains "Unlock any folder for this session", plus a list and revoke of the current session's roots.
- The Files tree shows the session's granted roots and clears them when the active session changes.
- With no open chat session, the dialog disables the unlock and states the reason.

### 4.9 `/rc`

No server special case. The SPA sends the session id it is viewing, which is the RC-bound TUI session id. An integration test pins that the unlock made in the RC server authorises a read for that session only.

### 4.10 `/reset-id`

`SessionManager.Rekey` moves the grant bucket from the old id to the new one, per the rule that every session-id-keyed map moves there. The move joins the existing quiesce order in `handler_reset_id.go`: refuse while a turn is active, flush, release the agent, then rekey. A grant or revoke racing the rekey must not land under the old id.

### 4.11 Lifetime

In memory only. Ended by process exit, by a session switch, or by an explicit revoke. Nothing is written to disk, so a restart re-asks.

## 5. Security

- **Separation is structural and already pinned.** The store lives in `internal/server`; `internal/tool` and `internal/agent` cannot import it, and the dependency runs server to agent, never back. `TestFileGrant_DoesNotWidenAgentScope` must gain a session-grant case: after an unlock, the root is absent from `tool.ExtraAllowedRoots()` and from `PermissionManager.AllowedRoots()`.
- **The two scope kinds stay split.** `scopeEditorRoots` accepts session grants; `scopeProjectRoots`, `resolveDBPath` and `resolveSecretPath` must not.
- **Scope.** A wildcard is per session and per host. Unlocking the local machine does not authorise any remote host, and vice versa.
- **Boundary.** The server auth token is the only client boundary, and the product is a single-user server. A client that holds the token can name any path under a granted root; that is already true for the existing grants.
- **No early return.** The closed LFI-shaped hole (an absolute path with no `project_root` returned verbatim) must not be reopened by the session branch.
- **Residual risk, accepted with gate A.** Consent is one click, so a same-origin page the user trusts could press it. No password is typed, and nothing secret can be phished. This is the trade for the simplest implementation and is recorded rather than hidden.

## 6. Tests

- Store: session isolation, host scoping, revoke, wildcard versus roots, no persistence.
- Endpoints: 400 on a missing or unknown `session_id`; unlock and grant round-trip.
- Local admission with and without an unlock; a `..` path still rejected; an absolute path with no `project_root` still refused.
- Remote read admission with and without an unlock; remote save (content and raw) admission with and without an unlock.
- Media-token mint and raw revalidation under a granted root.
- `Rekey` carries the grants and the old id stops authorising.
- Editor-only separation: session-grant case added to `TestFileGrant_DoesNotWidenAgentScope`.
- Mutation checks on the local and remote admission branches, with a build check before calling a mutant caught.
- Any test that overrides `HOME` uses the package's `setHomeTree` helper.

Packages: `go test ./internal/server/... ./internal/tool/... ./internal/agent/...` with targeted `-run` filters while iterating.

## 7. Validation

`go build ./...`, `go vet ./internal/server/...`, the package tests above, and the web lint/typecheck command. The RC integration test runs against a TUI-hosted server.

## 8. Documentation

- Update `docs/concepts/file-editor-grants.md`: session scope, wildcard unlock, and the corrected authority rule for remote grants (the line at `:164` currently describes routing that the SSH path never honours).
- Fix that page's stale anchor `handler.go:319`, which is now `handler.go:342`.
- Fix the stale comment in `handler_remote_work.go` that claims broader remote access goes through the host's own grant endpoints.
- Add an item to `skills/ocode-web/SKILL.md` and a `CHANGES.md` entry.
- Add a `TODO.md` entry for the deferred options.

## 9. Deferred

- Option B: out-of-band OS prompt (macOS administrator dialog or Touch ID, PAM on Linux, `LogonUser` on Windows).
- Option C: browser password verified then discarded.
- Persistent grants across restarts.
- Root-owned paths through a privileged helper.

## 10. Prior art

`.opencode/plans/2026-10-22-out-of-project-file-access.md` designed the original grant feature. It chose process-wide storage and required the UI to *surface* it as a per-session grant, because a persistent grant is a standing capability. This spec keeps that reasoning and narrows storage to the session, which is stricter than the plan. Its agent-scope analysis still holds.
