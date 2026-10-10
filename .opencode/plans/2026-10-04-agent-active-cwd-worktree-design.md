# Design: agent `activeCwd` + worktree support for ocode

**Status:** design only — no implementation code written.
**Date:** 2026-10-04
**Scope:** TUI + web/desktop. Local and remote (SSH/WSL) projects.

---

## 1. Problem

ocode has **no product-level worktree support**. The entire feature surface is one
prompt line:

```go
// internal/agent/prompt.go:328
lines = append(lines, "  Git worktree directory: .worktrees/ (gitignored, project root)")
```

Every other `worktree` hit in `internal/` is a comment, a symlink-containment
branch, a permission-rubric sentence, or test fixtures. There is no
`EnterWorktree` equivalent in `internal/tool/`, no route in `internal/server/`,
and no UI in `web/src/`.

The consequence is worse than "the cwd is stale". **ocode has no persistent
shell cwd**: `BashTool` spawns a fresh process per invocation with
`cmd.Dir = workDirFromContext(ctx)` (`internal/tool/exec.go:225`) and keeps no
shell state between calls. Verified by reproduction:

```
call 1: cd /Users/james/www/ocode/.worktrees/pristine && pwd
        → /Users/james/www/ocode/.worktrees/pristine
call 2: pwd                                    (separate invocation)
        → /Users/james/www/ocode
call 2: git rev-parse --show-toplevel
        → /Users/james/www/ocode
```

So an agent that "moves itself" into a worktree silently keeps operating on the
main checkout. It fails **silently** because the worktree lives *inside*
`workDir`, so every containment check passes. This is pinned by
`internal/tool/exec_workdir_test.go`.

Concrete fallout observed in this repo: 7 stale worktrees under `.worktrees/`,
and an accreted `CLAUDE.md` section of copy-paste instructions telling each new
session to `cp` two gitignored `//go:embed` files into its worktree.

---

## 2. What the three implementations do

| | **ocode (today)** | **Claude Code** | **opencode 2.0** |
|---|---|---|---|
| Unit | — | per session | per workspace/project |
| Agent-invocable | ✗ | ✓ `EnterWorktree`/`ExitWorktree` | ✗ (no such tool) |
| Session cwd follows | ✗ never | ✓ (transcript relocates too) | ✓ bound at creation |
| Isolation enforcement | ✗ | ✓ 4 checks, 1 un-disableable | ~ implicit via `containsPath` |
| Subagent isolation | ✗ | ✓ `isolation: worktree` | ✗ |
| Location | `.worktrees/` (agent-invented) | `.claude/worktrees/` | `~/.local/share/opencode/worktree/<id>/` |
| Cleanup | never happens | exit check + periodic sweep | explicit reset/remove API |

- **Claude Code** details are from `code.claude.com/docs/en/worktrees`
  (point-in-time, not verified locally).
- **opencode** details read from `sst/opencode` @ `7a6ce05` (branch `2.0`).
  Worktree is a *project* concept: `packages/opencode/src/worktree/index.ts`,
  exposed over HTTP, no agent tool. `Project.id` is the **root commit hash**, so
  all worktrees share one project; each `Instance` carries both `worktree` (main
  checkout) and `sandbox` (its own cwd).

### Why not copy Claude Code wholesale

Claude Code's most expensive property is that entering a worktree **relocates
the session transcript** to the new directory (like `/cd`), so `--resume` finds
it there.

**ocode already has that for free.** `paths.ProjectSlug` resolves via
`git rev-parse --show-toplevel` (`internal/paths/paths.go:104`), and
`--show-toplevel` inside a linked worktree returns *the worktree path*:

```
/Users/james/www/ocode                          → slug=dcd5a911f8bd
/Users/james/www/ocode/.worktrees/pristine      → slug=c15722e4677b
/Users/james/www/ocode/.worktrees/quick-actions → slug=b4d81cd01366
```

So session storage, `snapshot.Store` (`project/<slug>/snapshots`) and
`.ocode/todo/` already split per worktree with zero code changes. The corollary
cost: a session inside a worktree does **not** appear under the main project,
and cross-worktree session discovery breaks. Decide whether that is acceptable
before building; it is a real product question, not an implementation detail.

---

## 3. The constraint that decides the architecture

**Do not reuse `Agent.SetWorkDir` for the new cwd.** It is a *project switch*,
not a cwd move (`internal/agent/agent.go:3156`):

```go
a.workDir = dir
a.clearEnvironmentPromptCache()        // rebuilds <env>
a.preloadedModelContextReady = false   // re-resolves {model}.OCODE.md
snapDir := a.projectSnapshotsDir()     // ← calls paths.ProjectSlug(a.workDir)
a.snapshotStore.SetBaseDir(snapDir)    // ← RELOCATES the snapshot store
a.mdState = nil                        // nukes md discovery cache
a.permissions.SetWorkDir(dir)
```

Because `projectSnapshotsDir()` (`agent.go:3203`) is
`GlobalDataDir()/project/<ProjectSlug(workDir)>/snapshots`, moving `workDir`
into a worktree would relocate snapshots to a different slug than the web
Changes tab resolves from — producing an **empty Changes tab and broken undo**,
trading one silent failure for another.

Also note: `SetWorkDir` writes `a.workDir` **without holding `a.mu`** while
there are 21 read sites (`a.WorkDir()` / `a.effectiveWorkDir()`) in
`internal/agent/`. A new mutable cwd field must not inherit that.

---

## 4. Design

Add a **separate mutable `activeCwd`** to `Agent`, with its own narrow setter
(`SetActiveCwd`) that touches only what genuinely follows the cwd. `workDir`
remains the immutable session home root.

### Split

| Follows `activeCwd` | Stays pinned to home root |
|---|---|
| `tool.WithWorkDir` → bash `cmd.Dir` | `paths.ProjectSlug` / session storage |
| relative path resolution (`resolvePath`) | `snapshotStore` base dir |
| permission scope — **widened to allow both roots** | `mdState` discovery root |
| git status / log / branch anchoring | `{model}.OCODE.md` preloaded context |
| `<env>` refresh (one-time, at entry) | tabs, termtabs key, `project_path` |

### Rules

1. **Guard it.** `activeCwd` is read by tool calls, subagents and web handlers.
   Use a dedicated `sync.RWMutex` or `atomic.Value`; do not reuse the unguarded
   `a.workDir` pattern. Subagents **snapshot** the value at spawn.
2. **Never rewrite `<env>` mid-session.** It sits in the cached system prompt;
   a per-turn rewrite busts the prefix (`docs/concepts/prompt-cache-stability.md`).
   Emit a **user-role** `[ocode:worktree]` tail notice instead — the
   `injectLSPDelta` pattern — and refresh `<env>` only at entry/exit.
3. **Widen permission scope, don't move it.** While isolated, paths inside
   *either* root must resolve, mirroring opencode's `Instance.containsPath`.
4. **Persist `activeCwd`** in session metadata so resume and process restart land
   back in the worktree.
5. **Remote projects:** the tool runs on the **host**, never locally — remote
   chat is proxied to the host's `ocode serve --remote`.

### Enforcement (phase 2, not phase 1)

Claude Code's cheap 80% is check 1: **block writes into the main checkout while
isolated**. Its other three (cwd resolution, git redirects, unprovable git
target) are much harder and need a shell-AST story ocode does not have. Start
with check 1.

---

## 5. Scope of change

**Touches:** `Agent` (new field + setter + lock), `PermissionManager` (dual-root
scope), session metadata (persist cwd), `internal/server` (tool registration, a
`cwd_changed` SSE event), `web/src` (status badge), git handlers (anchor to
active cwd).

**Does NOT touch:** `internal/tabs`, `internal/termtabs`, `paths.ProjectSlug`,
session storage layout, the remote proxy.

---

## 6. Open questions

1. Should the transcript move to the worktree's slug (Claude Code / opencode
   behavior), or stay at the home root (cheaper, but cross-worktree resume and
   the Changes tab need care)? **This is the first decision to make.**
2. Does `EnterWorktree` relocate, or create-and-enter? Claude Code does both.
3. Cleanup policy: on session exit, remove if clean? opencode exposes explicit
   reset/remove; Claude Code prompts. ocode has neither.
4. Subagent isolation — wanted, or explicitly out of scope for v1?
5. Verify whether the unguarded `SetWorkDir` write is an actual live race today
   (`go test -race` has no test that calls it concurrently). Fix or at least
   document before adding a second mutable cwd.

---

## 7. Interim mitigation (already shipped)

The bundled `using-git-worktrees` skills were corrected so the fallback path at
least *works*, rather than silently building the wrong tree:

- Removed the `cd "$path"` that implied persistence; replaced with an explicit
  "**`cd` does not persist**" warning.
- Renamed the shell variable `path` → `WT` (`path` is an array bound to `$PATH`
  in zsh, so the original assignment clobbered the command search path).
- Made the path absolute, and anchored setup detection (`[ -f "$WT/go.mod" ]`)
  and baseline tests to the worktree instead of the project root — previously
  they detected the main checkout's manifests and installed into the wrong tree.

Files: `~/.agents/skills/using-git-worktrees/SKILL.md`,
`~/.config/opencode/plugins/u007-superpowers/skills/using-git-worktrees/SKILL.md`.

---

## 8. Note on docs/

`docs/` is an active OKF bundle (`docs/index.md` carries `okf_version: 0.1`) and
CLAUDE.md's sole-automated-writer invariant reserves writes to the `context`
sub-agent. This design therefore lives in `.opencode/plans/`. Publishing it into
the bundle (e.g. `docs/superpowers/specs/`) requires routing through the
`context` agent, not a direct write.