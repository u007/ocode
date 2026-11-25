---
type: Design
title: Cross-Worktree Session Scope
description: Approved design for letting a saved local project browse and resume sessions from every Git worktree, binding each chat tab to its checkout while the owning saved project stays active—behind a single server-side workspace-root resolver that never adds worktrees to the project allowlist.
tags:
  - worktree
  - sessions
  - tabs
  - git
  - files
  - web
  - server
  - design-spec
timestamp: 2026-11-25T07:51:37Z
status: approved
---
# Cross-Worktree Session Scope

**Status:** Approved design, 2026-11-25.
**Anchor convention:** every `file:line` citation is as-of 2026-11-25. Re-derive each anchor by symbol before relying on it; line numbers drift when code above a citation changes.

## 1. Goal

A saved local project should browse and resume sessions from **every** Git worktree of its repository, without losing checkout identity (which checkout a session actually ran in) and without unexpectedly switching the active project.

**Non-goals:** no remote (SSH/WSL) worktrees, no selector-driven mutation of `projects.json`, and no merged session list across checkouts.

## 2. Problem

A linked checkout already has its own project identity: `paths.ProjectRoot` resolves through `git rev-parse --show-toplevel` and `ProjectSlug` hashes that root, so each checkout keeps separate sessions (`docs/concepts/git-worktrees.md` § Project scoping). Reaching a worktree's sessions today means registering that checkout as a separate project and activating it—a whole-project switch that loses the owning project's view. The same checkout also cannot be targeted by the Git or Files panels without that switch.

## 3. Approved decisions

- **Sessions dialog filters by checkout.** The default selection is the active chat's checkout; with no active chat it is the project path. A new chat inherits the selected checkout.
- **Opening a worktree session binds that chat tab to the checkout** while the owning saved project remains the active project.
- **The sidebar shows every Git worktree of the project.** Clicking one keeps the existing register-and-activate whole-project switch (`openProjectPath`); it is not a tab-level binding.
- **Git and Files have independent checkout selectors.** A selection affects only that panel.
- **Missing/prunable worktrees are disabled.** Remote behavior is unchanged.
- **The saved project owns the tab bar.** A `tabs.Tab` gets an optional `worktreePath` (persisted as `worktree_path,omitempty`); an empty value means `projectPath`. One `tabRoot` helper resolves the checkout root for every session-scoped call.
- **One server resolver is the trust boundary.** `resolveWorkspaceRoot(p) (root, savedRoot, ok)` accepts a saved root, a path inside one, or an exact linked worktree root whose main checkout is saved.
- **Worktrees are never added to `allowedProjectRoots` or `projects.json`.** A worktree is validated per request instead.
- **Worktree discovery is one shared endpoint,** `GET /api/git/worktrees?project=<owner>`, following pagination.
- **Tab restore is backward compatible.** A tab whose checkout is missing is disabled, never silently retargeted to `projectPath`.

## 4. Server: the workspace-root resolver

`resolveWorkspaceRoot` (`internal/server/workspace_root.go:68`) is the only place a client-supplied path becomes a checkout the server may act on. It returns the resolved checkout root and the saved project that owns it.

Accepted shapes, and nothing else:

1. a saved local project root (`h.workDir` or a local `projects.json` entry);
2. a path inside one of those roots;
3. an **exact** linked Git worktree root whose main checkout is saved (a path inside such a worktree resolves to the worktree root).

The Git-proof half is `validateWorktreeRoot` (`:176`): the candidate must carry a `.git` **file**, `git rev-parse --show-toplevel` must equal the candidate, the worktree's parent must be the saved root, and the `.git` file must point inside that repository's `.git/worktrees` directory. An unrelated repository, a path outside every saved root, a failed probe, and a forged `.git` file pointing anywhere else are all rejected.

Operational rules:

- **Bounded probes.** One read-only git probe per candidate under `workspaceRootProbeTimeout` (3 s), so a hung mount fails closed instead of pinning a request (`:21`).
- **Success-only short cache.** Validated worktrees are cached for `workspaceRootCacheTTL` (30 s); rejections are never cached, and the map is cleared wholesale at `workspaceRootCacheLimit` (256) rather than growing without bound (`:26`, `:32`).
- **Symlink-resolved spelling.** Returned paths are absolute and symlink-resolved when they exist, so `/var` and `/private/var` spellings of one checkout compare equal.
- **Most-specific saved root wins.** When several saved roots contain the candidate, the longest one is chosen, so a saved project nested under a broad `h.workDir` still resolves to its own repository.
- **Locking.** `h.mu` is never held across a git probe.
- **Remote entries are skipped.** Only local saved roots participate; a remote path belongs to another machine.

### Wiring

| Surface | Where | Behavior |
|---|---|---|
| Project session list | `internal/server/handler_projects.go:199` | The query path resolves to the checkout root; the list is read for that root, so a worktree returns its own sessions by slug. |
| Local Git reads | `internal/server/handler_git.go:121` (`gitProjectDir`) | Read endpoints resolve `?project=` through the resolver. |
| Git mutations | `internal/server/handler_git.go:174` (`mutationProjectDir`) | A linked worktree is a valid mutation anchor only as an **exact** checkout root; a sub-path keeps the narrower saved-root anchor. |
| File roots | `internal/server/handler_files.go:824`, `:1993` | A validated linked worktree is its own browsable root, checked before the saved root so a directory inside `worktrees/` anchors to the worktree. |
| Chat binding | `internal/server/server.go:3628` (`ChatRequest.ProjectPath`) | A new chat binds its session to the resolved checkout. |
| Session rows | `internal/server/server.go:3665` (`SessionInfo.ProjectPath`) | Optional `project_path` on each row tells the client which checkout a session was listed for. |

Routes: `GET /api/git/worktrees` (`internal/server/server.go:472`) and `GET /api/projects/sessions` (`:851`), both behind `authMiddleware`.

Worktree discovery parses `git worktree list --porcelain`, marks the main checkout first and sorts by path, and pages the result through `worktreePageParams` (`internal/server/handler_git_worktrees.go:197`). The client `gitWorktrees(project, {limit, offset})` mirrors it.

## 5. Web

### Tabs

- `tabs.Tab` carries `WorktreePath string \`json:"worktree_path,omitempty"\`` (`internal/tabs/tabs.go:47`). Empty means the owning project root, so tabs written before the field existed keep working; `TestHandleTabsLegacyWithoutWorktreePath` and `TestHandleTabsWorktreePathRoundTrip` pin both sides (`internal/server/handler_tabs_worktree_test.go`).
- The client `Tab` mirrors it as `worktreePath?` plus `worktreeMissing?` (`web/src/stores/projectStore.tsx:31`). A missing checkout keeps `worktreePath` so a reappearing checkout recovers; the tab renders disabled meanwhile.
- `tabRoot(tab)` (`web/src/lib/tabRoot.ts:9`) is the single helper for session-scoped roots: `tab.worktreePath || tab.projectPath`. It never falls back the other way—a tab naming a missing checkout stays bound to it.

### Sessions dialog

`SessionDialog` keeps its per-project list and gains a shadcn `WorktreeSelect` (`web/src/components/Worktrees/WorktreeSelect.tsx:40`) fed by the shared `useProjectWorktrees` hook. The default selection is the active chat's checkout, else the project path; a new chat inherits the selected checkout. The store keeps one `checkoutSessions` cache entry per checkout, each with its own request generation, so one checkout's spinner or failure never covers another's (`web/src/stores/projectStore.tsx:96`).

### Sidebar

`ProjectSidebar` renders a worktree sub-tree under a project (`ProjectWorktreeTree`, `web/src/components/Layout/ProjectSidebar.tsx:446`) and lists every Git worktree of the project. Selecting a row keeps the existing register-and-activate whole-project switch—`POST /api/projects` then select—rather than binding a tab.

### Git panel

`GitPanel` owns a **selected root** defaulting to its `project` prop and renders a checkout selector for itself. The selection affects only the Git panel; the Worktrees section's Open action keeps its own register-and-activate behavior.

### Files panel

`FileTree` extends its existing root selector (`FileTree` `root` prop, `web/src/components/Files/FileTree.tsx:184`) with the worktree roots. The selection affects only the Files panel.

### Worktree discovery

`useProjectWorktrees` (`web/src/hooks/useProjectWorktrees.ts:69`) loads every page of `GET /api/git/worktrees` (500 per page), caches per project, and shares one in-flight request per project. Changing the project resets the result and drops the old response, so an unknown project never substitutes another project's list.

## 6. Invariants

- **The resolver is the only trust boundary.** Worktrees are never written to `allowedProjectRoots()` or `projects.json`; adding one would widen every other endpoint at once.
- **The saved project owns the tab bar.** A tab may name a checkout, but the tab itself belongs to the project.
- **Empty `worktree_path` means `projectPath`.** Never the reverse.
- **A missing checkout is disabled, never retargeted.** Silently landing a session in the wrong checkout would put reads and writes in a tree the user did not open.
- **Git and Files selectors are independent.** Neither panel's selection changes the other, nor the active project.
- **Remote projects are unchanged.** No remote worktrees; the resolver skips remote entries.
- **No merged session list.** Each checkout keeps its own slug-keyed sessions.

## 7. Validation

- **Go:** real temp Git worktrees for the resolver and listing—`TestResolveWorkspaceRootSavedRootAndSubpath`, `TestResolveWorkspaceRootLinkedWorktree`, `TestResolveWorkspaceRootNestedWorktree`, `TestResolveWorkspaceRootRejectsUnrelatedRepo`, `TestResolveWorkspaceRootRejectsMissingPath`, `TestResolveWorkspaceRootSymlinkSpelling`, `TestResolveWorkspaceRootRejectsFailedProbe`, `TestResolveWorkspaceRootRejectsForgedGitFile`, `TestWorktreeGitFileWithinAcceptsRealWorktree`, `TestWorkspaceRootGitProjectResolution`, and `TestListProjectSessionsWorktreeRootAndSlug` (`internal/server/workspace_root_test.go`).
- **Tabs:** legacy fallback and `worktree_path` round-trip (`internal/server/handler_tabs_worktree_test.go`).
- **Web:** `useProjectWorktrees.test.tsx` (pagination, shared request, stale response), `WorktreeSelect.test.tsx` (missing/prunable disabled, no fallback), `tabRoot.test.ts` (checkout root and legacy fallback), checkout-scoped store cache and stale response, independent panel selectors, and sidebar activation.
- **Suites:** full Go and web suites, `cd web && bun check .`, and the web build.

## 8. Key files

| Area | Files |
|---|---|
| Resolver | `internal/server/workspace_root.go`, `internal/server/workspace_root_test.go` |
| Server wiring | `internal/server/handler_projects.go`, `internal/server/handler_git.go`, `internal/server/handler_files.go`, `internal/server/handler_git_worktrees.go`, `internal/server/server.go` |
| Tabs | `internal/tabs/tabs.go`, `internal/server/handler_tabs_worktree_test.go` |
| Web state/types | `web/src/api/types.ts`, `web/src/stores/projectStore.tsx`, `web/src/lib/tabRoot.ts` |
| Web UI | `web/src/components/Layout/SessionDialog.tsx`, `web/src/components/Layout/ProjectSidebar.tsx`, `web/src/components/Git/GitPanel.tsx`, `web/src/components/Files/FileTree.tsx`, `web/src/App.tsx` |
| Shared worktree UI | `web/src/hooks/useProjectWorktrees.ts`, `web/src/components/Worktrees/WorktreeSelect.tsx` |
