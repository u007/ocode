---
type: Concept
title: Plugin System
description: 'Overview of ocode''s plugin system: plugin.json manifest format, custom tools, slash commands, MCP server registration, and plugin lifecycle management.'
tags:
  - plugins
  - extensibility
  - mcp
  - architecture
timestamp: 2026-08-27T15:04:53Z
---
# Plugin System

ocode's plugin system extends the agent with custom tools, slash commands, LLM instructions, and MCP server registrations — all without modifying the core binary.

## Quick Start

```bash
# Scaffold a new plugin (creates directory + plugin.json + commands/)
/plugin create my-plugin "Description of what it does"

# Install a plugin from GitHub
/plugin install github.com/username/my-plugin

# List installed plugins
/plugin list

# Enable/disable a plugin
/plugin enable my-plugin
/plugin disable my-plugin

# See plugin details
/plugin info my-plugin

# Update a plugin to the latest version
/plugin update my-plugin

# Remove a plugin
/plugin remove my-plugin
```

## How Plugins Work

A plugin is a directory on disk containing a `plugin.json` manifest file. Plugins are discovered from two search paths:

| Location | Path |
|---|---|
| **Global** | `~/.config/opencode/plugins/` (Unix) / `%APPDATA%/opencode/plugins/` (Windows) |
| **Project-local** | `.opencode/plugins/` (relative to project root) |

Plugins are additionally discovered from a **bundled/embedded** path — plugins
ships inside the ocode binary (e.g. the `orchestrator` plugin) are extracted to
the global data dir and treated as the lowest-precedence search path, so any
disk copy (global or project-local) overrides the embedded copy. All three paths
are merged when listing or loading plugins.

Each subdirectory inside these paths is treated as a plugin. A typical plugin layout:

```
plugins/my-plugin/
  plugin.json        # Manifest (required)
  tools/             # Custom tools (optional)
  commands/          # Custom slash commands (optional)
```

## The `plugin.json` Manifest

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique plugin name |
| `description` | string | no | Human-readable description |
| `version` | string | no | Plugin version string |
| `instructions` | string | no | Text injected into the LLM system prompt |
| `tools` | []string | no | Names of tools provided in `tools/` |
| `commands` | []string | no | Names of slash commands provided in `commands/` |
| `on_install` | []string | no | Commands to run after installation (no shell) |
| `mcp` | object | no | MCP server configuration (see below) |

### Example

```json
{
  "name": "my-plugin",
  "description": "Adds database inspection tools",
  "version": "1.0.0",
  "instructions": "The user has the db-inspect tool available. Use it to query database schemas and run EXPLAIN plans.",
  "tools": ["db-inspect"],
  "commands": ["db-status"],
  "on_install": ["go", "build", "-o", "{plugin_dir}/bin/tools"],
  "mcp": {
    "server": "db-server",
    "auto_register": true,
    "command": ["{plugin_dir}/bin/server"]
  }
}
```

## What Plugins Can Do

### 1. Inject LLM Context (`instructions`)

The `instructions` field is appended to the system prompt sent to the LLM every turn. Use this to teach the model about your plugin's tools, conventions, or domain knowledge.

Example:
```json
{
  "instructions": "The user has installed the 'postgres-helper' plugin. When asked about database queries, use the /pg-query tool to run EXPLAIN ANALYZE."
}
```

### 2. Provide Custom Tools (`tools/`)

Drop executable scripts, binaries, or tool implementations in a `tools/` subdirectory. Each tool is registered by name in `plugin.json` and becomes available to the agent.

The tool implementation follows whatever convention the agent supports (e.g. shell scripts, compiled Go plugins, etc.).

### 3. Provide Custom Slash Commands (`commands/`)

Each `.md` file in the `commands/` directory defines a slash command. Files use Markdown with optional YAML frontmatter:

```markdown
---
name: db-status
description: Show database connection status
---
Analyze the database connection by running `pg_isready` and report:
1. Connection status (accepting / rejecting / no route)
2. Latency in ms
3. Active connections count
```

The frontmatter fields:
- `name` — Command name (defaults to filename stem)
- `description` — Shown in help/autocomplete
- (optional `has_args: true` if the command accepts arguments)

The body is the prompt that will be sent to the agent when the user invokes `/db-status`.

### 4. Auto-Register MCP Servers (`mcp`)

If the plugin defines an `mcp` block with `auto_register: true`, the MCP server is automatically added to `opencode.json` on install and removed on uninstall.

```json
"mcp": {
  "server": "my-server",
  "auto_register": true,
  "command": ["{plugin_dir}/bin/mcp-server", "--port", "8080"]
}
```

- `server` — Name under which the server is registered in `opencode.json`
- `auto_register` — If true, register on install, unregister on remove
- `command` — The command to start the MCP server (no shell expansion; use `{plugin_dir}` for the plugin's absolute path)

### 5. Run Post-Install Scripts (`on_install`)

The `on_install` array specifies commands to run after a plugin is installed. These run directly via `exec.Command` (no shell — safe from injection):

```json
"on_install": ["{plugin_dir}/scripts/setup.sh"]
```

The `{plugin_dir}` token is replaced with the absolute path of the installed plugin directory.

## Managing Plugins

All plugin management happens through the `/plugin` TUI command.

## Web & Desktop UI

The web SPA and desktop shell expose the same plugin management through the
**Plugins** panel (open via the sidebar) and the `/plugin` chat command:

- The plugin list shows **every plugin discovered on disk** — global,
  project-local, and bundled/embedded — not only those registered in
  `external_plugins`. Disk-only plugins (e.g. bundled ones) appear enabled by
  default and can be toggled; the first toggle auto-creates an `external_plugins`
  entry so the state persists.
- A separate **Builtin plugins** section lists opt-in built-in tools. The `ast`
  plugin (ast-grep structural search/rewrite) is toggled here, mirroring the
  TUI's `Builtin plugins` section.
- Bundled/embedded plugins ship inside the binary and are **read-only**: the UI
  returns an error if you attempt to remove one (removing it would delete the
  embedded copy). Only installed plugins (global or project-local) can be
  removed.

The backing REST endpoints are `GET /api/plugins` (merged disk + config list),
`GET /api/plugins/{name}`, `PUT /api/plugins/{name}/enable|disable`,
`POST /api/plugins`, and `DELETE /api/plugins/{name}`.

## Architecture Decisions

### Transactional Install with Deferred Rollback

Plugin installation (`POST /api/plugins`) uses a deferred-rollback pattern. After the git clone succeeds, any post-clone step that fails — `RunOnInstall`, `AutoRegisterMCP`, or `SavePlugin` — triggers a rollback that undoes all prior side effects:

1. The cloned directory is removed via `os.RemoveAll`.
2. If MCP was auto-registered, `UnregisterMCP` cleans up the entry.
3. The config entry is not persisted.

The rollback is implemented as a `defer` closure gated by a `rollbackClone` flag. On success the flag is cleared, preventing the defer from running. This guarantees the install either fully succeeds (clone + post-clone steps + config + agent refresh) or fully rolls back — no partial state is left behind.

Code reference: `internal/server/handler_plugins.go` — `HandleInstallPlugin` (lines 266–308).

### Agent Session Rebuild After Plugin Lifecycle Changes

Every plugin mutation (install, enable, disable, remove) calls
`refreshAgentSessionsForPluginChange()` to rebuild resident agent sessions so
they pick up the new plugin tool set on the next turn.

The rebuild runs outside `Handler.mu` (building an agent session touches the
filesystem and may spawn plugin/MCP processes). Session IDs are snapshotted
under `h.mu`, then each session whose project root matches is rebuilt
individually. Sessions with an active turn are skipped — the in-flight turn
finishes on the old agent, and the next turn picks up the new plugin set.

Lock order is respected: `agentSession.mu → Handler.mu`, never the reverse.

Code reference: `internal/server/handler_plugins.go` — `refreshAgentSessionsForPluginChange` (lines 394–443).

### Project-Root-Aware Plugin Discovery

`LoadPluginsForProject(enabled, projectRoot)` decouples plugin discovery from
`os.Getwd()`. When `projectRoot` is provided, project-local plugins are
discovered relative to that root instead of the process working directory. This
is critical for the web/desktop server, where a Finder/Launcher-launched
process may have a CWD of `/` — the project root comes from the session's
bound project, not the process environment.

`FindPluginDirForProject` follows the same pattern. The legacy `LoadPlugins()`
and `FindPluginDir()` wrappers pass an empty `projectRoot`, falling back to the
legacy `findProjectRoot()` path for backward compatibility.

Code reference: `internal/plugins/loader.go` — `LoadPluginsForProject` (lines 36–99), `FindPluginDirForProject` (lines 112–148).

## Browser Configuration (`browser.*`)

The embedded browser panel's headless-Chrome (CDP) mode reads its settings
from the top-level `browser` section of `ocodeconfig.json`:

```jsonc
"browser": {
  "chrome_path": "",          // optional explicit Chrome/Chromium binary
  "idle_timeout_minutes": 10  // reap the shared Chrome process after N idle minutes
}
```

- `browser.chrome_path` — when empty, discovery probes `OCODE_CHROME_PATH`,
  then platform defaults (macOS `.app` paths, Linux `$PATH` names).
- `browser.idle_timeout_minutes` — how long the Chrome process stays alive
  after the last browser surface closes, before it is reaped and relaunched
  lazily on the next navigation. Absent or `0` keeps the default `10`.
- `browser.extensions` is **reserved** and rejected at load time
  (`browser.extensions is not supported yet`) — load fails rather than
  silently ignoring it.

Private / loopback hosts never use Chrome mode; they keep the local-mode
iframe reverse proxy.
