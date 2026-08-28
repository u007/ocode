---
type: Gotcha
title: Plugin Install Rollback Bug
description: 'Security gotcha: failed plugin installs leave stale directories and orphaned MCP registrations due to incorrect path handling in deferred cleanup.'
tags:
  - plugins
  - security
  - rollback
  - cleanup
  - mcp
  - gotcha
timestamp: 2026-08-28T07:00:33Z
---
# Plugin Install Rollback Bug

## The Problem

When a plugin installation fails partway through (e.g. `on_install` command fails, MCP registration fails, or config save fails), the deferred cleanup (`defer` block in `HandleInstallPlugin`) may not properly remove the cloned directory or undo MCP registration, leaving stale artifacts on disk.

The root cause is incorrect path handling in the deferred cleanup: the path used for rollback may not match the actual cloned location, especially when the install target is resolved through project-scoped discovery (`LoadPluginsForProject` / `FindPluginDirForProject`) rather than a raw filesystem path.

## Impact

- **Stale plugin directories** remain on disk after a failed install, consuming space and potentially being discovered as partial plugins on the next load.
- **Orphaned MCP registrations** from `AutoRegisterMCP` persist even when the plugin install was rolled back, causing ghost server entries.
- **Inconsistent config state** — `config.SavePlugin` may succeed before a later step fails, leaving the plugin registered in config but without its files.

## The Fix

`HandleInstallPlugin` was restructured into a transactional pattern:

1. Clone the repo to a temporary location.
2. Run `on_install` commands.
3. Register MCP if configured.
4. Save plugin config.
5. On **any** failure in steps 2–4, a deferred rollback:
   - Removes the cloned directory (using the canonical resolved path, not the pre-resolution path).
   - Calls `UnregisterMCP` if MCP was registered.
   - Does **not** attempt to undo `config.SavePlugin` (config write is idempotent — re-running the install will overwrite).

The key path-handling fix: the cleanup path must be `filepath.EvalSymlinks`-resolved to match what was actually created, and the directory removal must target the specific plugin subdirectory, not a parent.

## Code Reference

- `internal/server/handler_plugins.go` — `HandleInstallPlugin` (transactional rollback)
- `internal/plugins/manager.go` — `RefreshAgentSessionsForPluginChange`

## Gotcha

If you add a new post-clone step (e.g. a new hook or registration), you **must** add corresponding rollback logic in the same deferred cleanup block. Missing rollback for a new step reintroduces the stale-artifact problem. The existing test `TestHandleInstallPluginRollbackOnFailure` verifies the pattern.

## Related

- [Symlink Escape in Plugin Removal Validation](plugin-removal-symlink-escape.md) — a different security issue in the removal path.