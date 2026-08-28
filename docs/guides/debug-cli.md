---
type: Guide
title: ocode debug CLI Reference
description: Documentation for the ocode debug CLI subcommand family
tags:
  - cli
  - debug
  - introspection
  - project-slug
  - tooling
timestamp: 2026-08-28T07:03:09Z
---
# ocode debug CLI

The `ocode debug` subcommand family provides small, stable introspection utilities for external tooling (review scripts, dashboards) that need ocode's project-scoping logic without reimplementing the hashing/resolution rules in another language.

## Usage

```bash
ocode debug project-slug [path]
```

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `project-slug` | Yes | The subcommand to run (currently only `project-slug` is implemented) |
| `[path]` | No | Directory to compute the slug for (defaults to current directory) |

## Output

The command prints a JSON object with the following fields:

```json
{
  "root": "/path/to/project",
  "slug": "dcd5a911f8bd",
  "globalDataDir": "/Users/james/.local/share/opencode",
  "sessionsDir": "/Users/james/.local/share/opencode/project/dcd5a911f8bd/sessions"
}
```

| Field | Description |
|-------|-------------|
| `root` | Absolute path to the resolved project root |
| `slug` | SHA-256 prefix of the git repo root path (project identifier) |
| `globalDataDir` | ocode's global data directory for the current platform |
| `sessionsDir` | Per-project sessions directory under the global data dir |

## Code Reference

- `internal/debugcli/debugcli.go` — `Run` dispatcher and `runProjectSlug` implementation
- `internal/paths/` — `ProjectRoot`, `ProjectSlug`, `GlobalDataDir`, `SessionsDir` resolution

## Use Cases

- **Review scripts** that need to locate a project's session files without reimplementing slug hashing.
- **Dashboards** that display project metadata (slug, paths) for monitoring.
- **CI/CD pipelines** that need to compute ocode's internal paths for automation.

## Implementation Notes

- The slug is derived from the same SHA-256 hashing used by `internal/paths`, ensuring consistency with ocode's session storage.
- The command respects the same project resolution logic as the TUI and server modes.
- Future subcommands may be added; unknown subcommands return an error listing the available ones.