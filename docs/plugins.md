---
type: Concept
title: Plugin System
description: 'Overview of ocode''s plugin system: plugin.json manifest format, custom tools, slash commands, MCP server registration, and plugin lifecycle management.'
tags:
  - plugins
  - extensibility
  - mcp
  - architecture
timestamp: 2026-07-06T08:35:11Z
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
