---
type: Decision
title: Duplicate Project as Remote (SSH/WSL)
description: 'Design record: duplicate any project row into a new remote (SSH/WSL) entry via create-only POST /api/projects/duplicate and Store.DuplicateAsRemote.'
tags:
  - remote
  - projects
  - design
  - implemented
  - web
  - server
timestamp: 2026-09-28T05:25:38Z
---
# Duplicate Project as Remote (SSH/WSL)

Date: 2026-09-28
Status: Implemented (uncommitted working tree)

## Context / Problem

User request: *"project list, allow to duplicate for remote ssh or wsl"*. A
project already known locally (or on another host) is a common starting point
for adding the same working tree as a remote entry — retype the path, name, and
group by hand. The only existing creation path, `AddRemote`, UPSERTS on re-add,
so reusing it for "duplicate" would silently reuse/overwrite an existing
`(host, path)` entry instead of telling the user the target is already in the
list.

Related records: sibling spec
`superpowers/specs/2026-09-13-remote-project-editing.md` (remote connection
editing, `PATCH /api/projects/remote` identity rules) and gotcha
`gotchas/remote-project-path-trust-boundary.md` (the `(host, path)` identity
invariant, and the rule that *any new project-scoped endpoint must choose its
local or remote mode explicitly, never reuse a path-only allowlist*).

## Decision

Add a **create-only, remote-only** duplicate flow: a context-menu action on
every project row that opens the remote-add dialog pre-filled with the source's
path/name/group, and a dedicated `POST /api/projects/duplicate` endpoint backed
by a new `Store.DuplicateAsRemote` that never upserts.

Clarified requirements:

- Available on **any** project row (local or remote, expanded or collapsed).
- Always produces a **NEW** remote entry (never edits the source).
- Inherits the source's `path`, display `name`, and `group`.
- If the `(canonicalHost, path)` already exists → **warn** ("already in the
  list for this host"), do not silently reuse.

## Data model & identity

`internal/projects/projects.go`:

- `var ErrProjectExists = errors.New("project already exists")` — new sentinel.
- `func (s *Store) DuplicateAsRemote(host, path string, port int, name, group string) (Project, error)`:
  1. Parses/canonicalizes the host via `remote.ParseTarget` + `target.Validate()`
     **before** the conflict check, so `james@devbox` and `devbox` collide as
     one identity.
  2. Requires a non-empty `path`.
  3. Stores the path **verbatim** — no `filepath.Clean`. `~` and separator
     conventions belong to the remote shell; normalizing them locally would
     fabricate an identity the remote does not use.
  4. Refuses when `(canonicalHost, path)` is already saved → `ErrProjectExists`.
  5. Appends a new entry with the supplied `name` (fallback
     `canonicalHost + ":" + path`) and `group`.

Identity stays `(host, path)` per the trust-boundary gotcha; a local and a
remote project with the same path remain distinct.

`AddRemote` is deliberately **unchanged** — it still upserts on re-add (pinned
by `TestAddRemoteScopedByHost`), which is why the duplicate flow cannot reuse
it.

## API contract

`internal/server/handler_projects.go` → `HandleDuplicateProjectAsRemote` for
`POST /api/projects/duplicate`, body `{host, path, port, name, group}`:

| Status | Condition | Body |
| --- | --- | --- |
| 200 | created | the created `Project` |
| 409 | `ErrProjectExists` | "that project is already in the list for this host" |
| 400 | missing `host`/`path`, or invalid target | validation error |

Route registered in `internal/server/server.go` next to the other
`/api/projects/*` routes via a thin `handleDuplicateProjectAsRemote` wrapper.

**409 vs upsert**: this endpoint is create-only. The 409 is the entire point —
a duplicate that silently resolved to the existing entry would look like
success while changing nothing (or clobbering it, as `AddRemote` would).

**Trust boundary**: the endpoint is explicitly **remote-only** (non-empty
`host` required), never path-only — per
`gotchas/remote-project-path-trust-boundary.md`.

## UI surface

- `web/src/api/client.ts`: `duplicateProjectAsRemote({host,path,port?,name?,group?})`
  → `POST /api/projects/duplicate`; returns `Project` and throws `ApiError`, so
  a 409 surfaces its message.
- `web/src/stores/projectStore.tsx`: `duplicateProjectAsRemote` action
  **rethrows** on failure (same contract as `removeProject`/`renameProject`) so
  the dialog stays open and shows the reason.
- `web/src/components/Layout/ProjectSidebar.tsx`: a **"Duplicate as remote…"**
  context-menu item on EVERY row — expanded `SortableProjectRow` and collapsed
  `CollapsedProjectButton` (the mobile drawer reuses the expanded body). State
  `duplicatingRemote: Project | null`; `handleDuplicateRemote` forwards
  `source.name`/`source.group`. `AddRemoteDialog` gained
  `initialPath`/`heading`/`submitLabel` props; the duplicate dialog is rendered
  by BOTH render branches through `renderDuplicateRemoteDialog()` (the collapsed
  rail is a separate render tree with no dialogs of its own).

## Tests (all mutation-verified)

- `internal/projects/projects_test.go`:
  `TestDuplicateAsRemoteInheritsNameAndGroup`,
  `TestDuplicateAsRemoteRejectsExistingTarget`,
  `TestDuplicateAsRemoteCanonicalizesHost`,
  `TestDuplicateAsRemoteValidatesTarget`.
- `internal/server/handler_projects_test.go`: 200 inherit, 409 conflict with
  no-write, 400 validation.
- Web: `ProjectSidebar.test.tsx` ("ProjectSidebar duplicate as remote", 6
  cases) and `stores/projectStore.test.tsx` (conflict rethrow).

## Open / adjacent findings

**Not introduced by this feature — fixed 2026-09-28.**
`allowedProjectRoots()` (`internal/server/handler.go`) used to append `p.Path`
for every saved project with no `Host != ""` filter, which was exactly the
invariant violation documented in
`gotchas/remote-project-path-trust-boundary.md`. That gotcha now carries a
"Status: fixed 2026-09-28" note: `allowedProjectRoots` (`handler.go:662`)
skips remote records, and the second path-only allowlist
`isRegisteredProjectRoot` (`handler_git.go:164`) was fixed at the same time.
`AddRemote` already accepts an arbitrary caller-supplied host+path, so this
endpoint does **not** widen that surface — and the invariant is now enforced.
Regression coverage: `internal/server/remote_project_trust_boundary_test.go`
(6 tests, mutation-verified); CHANGES.md 2026-09-28 — "Security: a remote
project's path is no longer a local filesystem root".
