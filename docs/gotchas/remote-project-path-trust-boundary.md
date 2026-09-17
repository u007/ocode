---
type: Gotcha
title: Remote Project Paths Must Not Enter the Local Filesystem Trust Boundary
description: 'Gotcha: saved remote project paths can enter the local filesystem allowlist — keep host-aware and out of local root validation'
resource: ""
tags:
  - security
  - remote-projects
  - filesystem
  - trust-boundary
  - server
timestamp: 2026-09-17T11:02:16Z
---
# Remote project paths must not enter the local filesystem trust boundary

A saved remote project has two identity components: `(host, path)`. Its `path` is interpreted on the remote machine and must never be treated as a local filesystem root by the local server.

## Confirmed failure mode and exploit chain

The review confirmed a concrete trust-boundary vulnerability in the current implementation. `internal/server/handler.go:416-430` builds `allowedProjectRoots` from the server workdir and every saved project's `Path`, but does not filter out saved projects whose `Host` is non-empty.

An attacker or untrusted web client can exploit that by:

1. Saving or causing a remote project record such as `{host: "example.com", path: "/"}` to exist in the project store.
2. Letting the shared local-root builder add the remote `/` path to `allowedProjectRoots`.
3. Supplying that path to a local endpoint: for example `?path=/` for the file tree/search, `project_root=/` for file content/raw/save/open handling, or `?project_path=/` for local terminal admission.
4. Passing the local allowlist check because the path is now considered a registered root, even though it belongs to another machine.
5. Reading, writing, searching the local filesystem, or starting a local shell under that path. The caller does not need a local project at the remote path; registering the remote project is enough to make the path look trusted locally.

This is a trust-boundary failure, not merely a path-normalization issue. Remote paths may use a different filesystem, path syntax, or root semantics, and the host component is what prevents a remote identity from colliding with a local path.

## Affected handlers

The shared `allowedProjectRoots` result is consumed by local filesystem and terminal paths, including:

- `internal/server/handler_files.go`: `HandleFileTree`, `HandleFileSearch` and `HandleFileSearchStream` through `fileTreeRootFor`; `HandleFileContent`, `HandleFileRaw`, and `HandleSaveFileContent` through `fileContentRootFor`.
- `internal/server/handler_open.go`: local file opening through `fileContentRootFor`.
- `internal/server/handler_secret.go`: directory-wide secret operations through `fileTreeRootFor`.
- `internal/server/handler_terminal.go`: local `HandleTerminalWS` admission, `resolveTerminalProject`, and project-scoped terminal-process filtering. The no-host branches must never accept a remote record's path as a local root.
- `internal/server/handler_remote_proxy.go`: the `/api/remote/{host}/api/{rest...}` reverse proxy route that proxies chat/agent/session traffic to a remote host's `ocode serve --remote`.

Remote project records are created and managed by `internal/server/handler_projects.go`; their identity is `(host, path)`, not `path` alone.

## Required invariant and validation

- `allowedProjectRoots` and every other local filesystem allowlist may contain only the server workdir, local saved projects (`Host == ""`), and explicitly configured local extra-allowed paths.
- Remote saved projects (`Host != ""`) must be excluded from local file-tree, file-search, file-content, raw, save, open, secret, upload, git, and local-terminal root validation.
- Local handlers must reject a remote path even when it is a broad or otherwise valid-looking local path. A path-only request cannot select a remote project.
- Remote terminal/file operations must require the remote identity and validate the exact registered `(host, path)` pair (and port where applicable) before starting an SSH/WSL operation. They must not fall back to local path validation.
- Local and remote projects with the same path must remain distinct identities.
- Any new project-scoped endpoint must choose its local or remote mode explicitly and must not reuse a path-only allowlist for both modes.
- The remote proxy route `/api/remote/{host}/api/*` (`handler_remote_proxy.go`) admits only a host that is the `Host` of at least one saved project — the same trust boundary as `remoteWorkFor`. It never consults the local path allowlist (`allowedProjectRoots`). The remote bearer token is injected server-side by the cached reverse proxy (`remote.InjectAuth`) and never reaches the browser, so the SPA cannot use the token to reach the remote directly.

## Regression coverage

Keep tests for all of these cases:

1. A saved remote project with a broad path such as `/` is rejected by local file-tree and file-content/raw/save/open/search endpoints.
2. A saved remote project path is rejected by local terminal validation when no `host` is supplied, including project-scoped process filtering.
3. The same remote project succeeds only through the registered remote `(host, path)` route, with the expected port/target validation.
4. A local project and a remote project sharing the same path do not grant or reuse each other's access.

When changing project-root resolution or adding a project-scoped endpoint, review this invariant before reusing `allowedProjectRoots`.
