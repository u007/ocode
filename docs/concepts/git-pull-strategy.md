---
type: Concept
title: Git pull reconcile strategy — explicit flag, configurable
description: 'Why ocode always passes --rebase or --no-rebase to git pull, the pull.rebase / pull.ff problem on remote hosts, and how the setting travels through local, remote, TUI and settings surfaces.'
tags:
  - git
  - remote
  - config
  - pull
  - rebase
  - settings
timestamp: 2026-10-28T00:00:00Z
---

# Git pull reconcile strategy

- **Status:** Implemented (CHANGES.md entry references this doc at line 4). Config, server endpoints, remote command plumbing, TUI resolver, and web Settings form all ship.
- **Where:** `internal/config/ocodeconfig.go` (config model + validate + load/save), `internal/server/handler_git_actions.go` (`HandleGitPull` + `gitPullFlag` + `remoteGitCommand`), `internal/server/handler_config.go` (`HandleGetGitConfig` / `HandleSetGitConfig`), `internal/server/server.go` (routes `GET/PUT /api/config/ocode/git`), `internal/tui/git_model.go` (`gitPullStrategy`, `gitPullFlagFromConfig`), `internal/tui/git_pull_strategy_test.go`, `web/src/components/Settings/GitForm.tsx`, `web/src/components/Settings/SettingsPanel.tsx`, `web/src/api/client.ts`.

## 1. The failure it fixes

`git pull` since git 2.33 requires an explicit reconcile answer — `--rebase`, `--no-rebase`, `--ff-only`, or a repository-level `pull.rebase` / `pull.ff` / `pull.ff-only` / `branch.<name>.rebase` setting. Without it, a bare `git pull` on a divergent branch exits with:

```
fatal: Need to specify how to reconcile divergent branches
```

ocode's Git tab (web/desktop) and the TUI `ctrl+p` both used to run a bare `git pull`. For a **remote (SSH/WSL) project**, the pull runs on the host, so any decision was the host's git config — a user setting `pull.rebase = true` on their own machine had no effect on the remote repository. The fix makes the decision explicit at the command line level so the host's config can never override it.

## 2. The decision — one shared config, always explicit

The shared `git.pull_strategy` config (`internal/config/ocodeconfig.go`):

| Value | Meaning | Default |
|---|---|---|
| `rebase` | `git pull --rebase` | yes |
| `merge` | `git pull --no-rebase` | no |

- `GitConfig.Validate()` (`ocodeconfig.go`) rejects any value outside `rebase`/`merge` with an error (`fmt.Errorf` naming both allowed values) — never a silent fallback.
- `GitConfig.PullFlag()` (`ocodeconfig.go`) maps `rebase` → `--rebase`, `merge` → `--no-rebase`.
- The config block is only written to `ocodeconfig.json` (`SaveOcodeGitConfig`, `ocodeconfig.go`) when the value departs from the `rebase` default, so an untouched install gains no new key and switching back to rebase removes it.
- Endpoints (`internal/server/handler_config.go` + `server.go` routes): `GET /api/config/ocode/git` returns `{"pull_strategy":"rebase"|"merge"}`; `PUT` requires the same body, and an omitted or unknown strategy is a 400.

## 3. How the flag reaches each surface

Every pull path reads the same live config and always appends the flag explicitly — there is no case where a bare `git pull` is issued.

**Web / desktop — local project.** `HandleGitPull` (`handler_git_actions.go`) reads `gitPullFlag()` from the server's `h.cfg.Ocode.Git` (under `h.mu`), then runs `runGitNetwork` with `"pull"` + the flag (`handler_git_actions.go`, around the `remoteRunNetwork` call). The flag travels as literal argv; the local repo's git config cannot override it because `--rebase` / `--no-rebase` takes precedence over `pull.rebase`.

**Web / desktop — remote (SSH/WSL) project.** The same `gitPullFlag()` value is appended inside the remote shell command (`remoteGitCommand(rw.Path, "pull", flag)`, `handler_git_actions.go`). Because the flag is embedded in the command string that the host executes, the host's own `git config` cannot decide instead — it is always the client's `git.pull_strategy` that governs.

**TUI (`ctrl+p`).** `gitPullStrategy()` (`internal/tui/git_model.go`) is a package-level `var` that resolves from the on-disk config at action time (`LoadOcodeConfigCopy()`), so a Settings change applies to the next pull without a TUI restart. `gitPullFlagFromConfig()` (`git_model.go`) does the mapping. If the config file is missing or unreadable, the resolver falls back to the `rebase` default (`GitConfig{}.PullFlag()`), never to an empty string.

**TUI side fix.** The TUI's network operations (`cmdNetworkOp`, `git_model.go`) now invoke the stubbable `gitBinary` with `gitexec.Env()` (`GIT_OPTIONAL_LOCKS=0`), so a `git pull` no longer acquires the optional index lock that ocode's concurrent probes are designed to avoid.

## 4. Trade-offs (stated plainly)

- **Rebase rewrites commits onto upstream.** If the user's local commits were already pushed, a later push requires `--force-with-lease` (or a manual reconciliation). This is standard `git pull --rebase` behaviour, not an ocode-specific risk.
- **Rebase refuses an unclean working tree.** `git pull --rebase` exits with `fatal: cannot pull with rebase: You have unstaged changes.` There is **deliberately no `--autostash`** — a silent stash/pop cycle can leave a user in a conflicted pop state with no audit trail. The Settings form (`GitForm.tsx`) notes this; `--no-rebase` (merge) is one tick away for the unclean-tree case.
- **`pull.ff=only` in config does not conflict with an explicit `--rebase`.** Verified empirically: git treats the command-line flag as authoritative, so no extra `-c pull.ff=` override is needed. The only source of truth for the pull strategy is the client's `git.pull_strategy`.

## 5. Invariants and test evidence

- Divergent-fixture tests (`internal/server/handler_git_pull_strategy_test.go`) set `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_SYSTEM=/dev/null` so the developer machine's git config cannot mask the failure; both local and remote paths assert commit/merge counts.
- The TUI resolver mapping (`internal/tui/git_pull_strategy_test.go`) covers nil/unset/explicit config; mutation-verified (dropping `PullFlag()`, ignoring config, inverting the checkbox mapping all fail).
- The web form covers both directions (`GitForm.test.tsx`, `SettingsPanel.git.test.tsx`) and asserts the effective command line (`--rebase` / `--no-rebase`).
- Mutation-verified: removing the flag from either call site, ignoring the config, or inverting the checkbox mapping each breaks tests.
