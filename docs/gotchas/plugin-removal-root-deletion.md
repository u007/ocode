---
type: Gotcha
title: Plugin Removal — Root Directory Deletion Risk
description: 'Security gotcha: removal validation must reject deletion of an entire approved plugin root directory, not just validate child paths.'
tags:
  - plugins
  - security
  - removal
  - path-containment
  - gotcha
timestamp: 2026-08-28T07:02:03Z
---
# Plugin Removal — Root Directory Deletion Risk

## The Problem

When `HandleRemovePlugin` resolves the plugin directory for removal, a naive implementation may allow deletion of an entire approved plugin root directory itself — not just a child plugin within it. If the plugin name resolves to the root (e.g. due to a misconfigured `Dir` field in `PluginConfig`, or a name that matches the root directory), `os.RemoveAll` would delete the entire root, destroying all plugins in that scope.

## Impact

- **Loss of all plugins** in the affected scope (global or project-local).
- **Security risk** — an attacker who can influence the persisted `cfg.Plugins[name].Dir` path could target the root for mass deletion.

## The Fix

`ValidateRemovableDirForProject` in `internal/plugins/manager.go` enforces that the resolved removal target must be a **direct child** of an approved root — never the root itself:

1. Resolve both the target and each approved root via `filepath.EvalSymlinks`.
2. Check that the resolved target's parent matches a resolved root.
3. Reject path-traversal sequences (`..`) and symlinks that escape the root.
4. Explicitly reject the case where the target **equals** the root.

Note: Valid installed plugin directories *are* absolute paths (e.g. `~/.config/opencode/plugins/my-plugin`). The validator does not reject absolute paths outright — it verifies that the resolved absolute path is a direct child of an approved root directory.

## Code Reference

- `internal/plugins/manager.go` — `ValidateRemovableDirForProject` (lines 224–290)
- `internal/plugins/manager_test.go` — `TestValidateRemovableDirRejectsRootAndNestedTargets`

## Gotcha

The root-deletion rejection is **independent** of the symlink-escape check. Both must be validated:
- Symlink escape: logical path is inside root, but symlink target escapes.
- Root deletion: target IS the root — no traversal needed, just a direct `RemoveAll`.

Removing either check reintroduces its specific vector. The tests exercise both independently.

## Related

- [Symlink Escape in Plugin Removal Validation](plugin-removal-symlink-escape.md) — the sibling check for symlink-based path traversal.
- [Plugin Install Rollback Bug](plugin-install-rollback-bug.md) — incomplete rollback on failed installs.