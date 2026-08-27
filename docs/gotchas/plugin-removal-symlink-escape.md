---
type: Gotcha
title: Symlink Escape in Plugin Removal Validation
description: 'Security gotcha: filepath.EvalSymlinks must resolve both the target dir and all approved roots to prevent symlink-based path traversal in plugin removal.'
tags:
  - plugins
  - security
  - path-traversal
  - symlink
timestamp: 2026-08-27T15:04:11Z
---
# Symlink Escape in Plugin Removal Validation

## The Problem

When validating that a plugin removal target is inside an approved plugin root, a naive prefix check on the logical path is insufficient. A symlink can have its logical path inside a root while its target resolves outside it:

```
~/.config/opencode/plugins/my-plugin -> /etc/passwd
```

The logical path `~/.config/opencode/plugins/my-plugin` passes a prefix check against the global plugin root, but `os.RemoveAll` follows the symlink and deletes `/etc/passwd`.

## The Fix

`ValidateRemovableDirForProject` in `internal/plugins/manager.go` applies `filepath.EvalSymlinks` to **both** the target directory and every approved root before comparing:

1. **Resolve the target**: `filepath.EvalSymlinks(abs)` canonicalizes the removal target, following any symlink in the path.
2. **Resolve each root**: The approved roots (global install dir, project plugin dir, bundled dir) are also canonicalized via `filepath.EvalSymlinks`.
3. **Prefix-check the canonical forms**: The resolved target must be a direct child of a resolved root.

This catches two vectors:
- A symlink *at* the plugin dir whose target escapes (e.g. `plugins/foo -> /tmp/evil`).
- A symlinked *parent* directory whose target escapes (e.g. `plugins` itself is a symlink to `/tmp/evil`).

## Code Reference

- `internal/plugins/manager.go` — `ValidateRemovableDirForProject` (lines 224–290)
- `internal/plugins/loader_test.go` — `TestValidateRemovableDirSymlinkEscape`, `TestValidateRemovableDirSymlinkWithinRoot`

## Gotcha

If you add a new removal path or a new allow-listed root, you **must** also pass it through `EvalSymlinks` before the prefix check. A bare `strings.HasPrefix` on the unresolved path reintroduces the escape vector. The test `TestValidateRemovableDirSymlinkEscape` exercises this exact case — a symlink pointing outside the approved root must be rejected.
