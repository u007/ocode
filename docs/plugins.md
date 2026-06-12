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

| Command | Description |
|---|---|
| `/plugin list` or `ls` | List all installed plugins with status, sync state, commit hash |
| `/plugin info <name>` | Show full details for a specific plugin |
| `/plugin create <name> [desc]` | Scaffold a new plugin directory with `plugin.json` + `commands/` |
| `/plugin install <source>[@ref]` | Install a plugin (see below) |
| `/plugin enable <name>` / `disable` | Toggle a plugin on/off |
| `/plugin remove <name>` | Uninstall a plugin and clean up MCP registration |
| `/plugin update <name>` | Pull latest from git (omitting name updates all) |
| `/plugin sync` | Check sync status against remotes for all plugins |

### Installing Plugins

```bash
# From a GitHub URL (default branch)
/plugin install github.com/user/repo

# From a GitHub URL with a specific ref (tag, branch, or commit SHA)
/plugin install github.com/user/repo@v1.2.0
/plugin install github.com/user/repo@main
/plugin install github.com/user/repo@abc1234

# From a local path
/plugin install /path/to/my-plugin
```

Plugin installation flow:
1. Git URL → cloned via go-git into the global plugins directory
2. Local path → copied recursively into the global plugins directory
3. `plugin.json` is read and validated
4. `on_install` hooks execute (if any)
5. MCP server is auto-registered (if configured)
6. Plugin is saved to `opencode.json` with `enabled: true`

### Update & Sync

- **`/plugin update <name>`** — Pulls the latest commits from the remote for a specific plugin (or all plugins if no name given)
- **`/plugin sync`** — Checks each plugin's local HEAD against the remote HEAD and reports status:

| Status | Meaning |
|---|---|
| ✓ up-to-date | Local matches remote |
| ↑ behind | Remote has newer commits |
| ⊠ pinned | Plugin was installed at a specific ref (tag/commit) |
| ⚠ dirty | Local has uncommitted changes |
| ✗ error | Could not check (network, not a git repo) |

## The Built-in AST Plugin

ocode includes one built-in plugin — the **AST plugin** — which provides LSP-backed semantic navigation tools:

| Tool | Function |
|---|---|
| `goToDefinition` | Navigate to symbol definition |
| `findReferences` | Find all references to a symbol |
| `workspaceSymbol` | Search symbols by name |

```bash
/plugin enable ast
/plugin disable ast
```

This is treated specially: it's not a on-disk plugin but a toggle for built-in functionality. Its state is stored separately in the ocode config.

## Plugin Configuration

Installed plugins are tracked in `opencode.json` under the `plugins` key:

```json
{
  "plugins": {
    "my-plugin": {
      "source": "github.com/user/repo",
      "dir": "/home/user/.config/opencode/plugins/my-plugin",
      "ref": "v1.0.0",
      "enabled": true
    }
  }
}
```

- `source` — The git URL or local path used to install
- `dir` — Absolute path to the plugin directory on disk
- `ref` — The tag/branch/commit the plugin was installed at (empty = default branch)
- `enabled` — Whether the plugin is active

## HTTP API

The Web UI exposes plugin endpoints under the `/api` prefix:

| Endpoint | Method | Description |
|---|---|---|
| `/api/plugins` | GET | List all installed plugins with metadata |
| `/api/plugins/{name}` | GET | Get details for a specific plugin |
| `/api/plugins/{name}/enable` | PUT | Enable a plugin |
| `/api/plugins/{name}/disable` | PUT | Disable a plugin |
| `/api/plugins` | POST | Install a plugin (body: `{"source": "..."}`) |
| `/api/plugins/{name}` | DELETE | Remove a plugin |

## Example Plugin

Create a minimal plugin that adds a `hello` command:

**`~/.config/opencode/plugins/hello-world/plugin.json`**
```json
{
  "name": "hello-world",
  "description": "A simple greeting plugin",
  "commands": ["hello"]
}
```

**`~/.config/opencode/plugins/hello-world/commands/hello.md`**
```markdown
---
description: Say hello
---
The user invoked /hello. Respond with a friendly greeting in their native language.
```

Now `/hello` is available in the TUI. The agent will respond with a greeting.

## Creating a Plugin

A plugin is just a directory with a `plugin.json` manifest. You can create one in 5 minutes.

### Step 1: Scaffold the directory

Use the `/plugin create` command in the TUI:

```bash
/plugin create my-plugin "What your plugin does"
```

This creates `~/.config/opencode/plugins/my-plugin/` with a `plugin.json` and an empty `commands/` directory.

Or manually:

```bash
mkdir -p my-plugin/commands
cd my-plugin
```

### Step 2: Write `plugin.json`

Edit the generated `plugin.json`, or create one manually:

```json
{
  "name": "my-plugin",
  "description": "What your plugin does",
  "version": "1.0.0",
  "instructions": "Instructions injected into the LLM system prompt.",
  "commands": ["hello"]
}
```

### Step 3: Add a command

**`commands/hello.md`**
```markdown
---
description: Say hello in the user's language
---
The user invoked /hello. Respond with a warm greeting in their native language.
```

### Step 4: Test it locally

```bash
# Copy into your global plugins directory
cp -r my-plugin ~/.config/opencode/plugins/

# Or use the install command with the local path
/plugin install /absolute/path/to/my-plugin
```

Then run `/plugin list` and `/hello` in ocode.

### Step 5: Add tools (optional)

Each `.json` file in `tools/` defines a custom tool that the LLM can invoke. A tool is an executable command with a JSON schema describing its parameters.

**`tools/greet.json`** (a simple tool):
```json
{
  "name": "greet",
  "description": "Generate a personalized greeting",
  "parameters": {
    "type": "object",
    "properties": {
      "name": {
        "type": "string",
        "description": "The person's name"
      },
      "language": {
        "type": "string",
        "description": "ISO language code (e.g. en, es, fr)"
      }
    },
    "required": ["name"]
  },
  "command": ["{plugin_dir}/bin/greet-tool"]
}
```

The `command` uses `{plugin_dir}` as a placeholder for the plugin's absolute path. Parameters are substituted via `{{paramName}}` in the command arguments — no shell interpolation, safe from injection.

Then list the tool in your manifest:
```json
{
  "name": "my-plugin",
  "tools": ["greet"]
}
```

### Step 6: Add post-install steps (optional)

If your plugin needs to compile binaries, download dependencies, or set something up:

```json
{
  "on_install": [
    "go", "build", "-o", "{plugin_dir}/bin/greet-tool", "{plugin_dir}/src/"
  ]
}
```

The `{plugin_dir}` token is replaced at install time with the absolute plugin path. Commands run directly via `exec.Command` — no shell expansion.

### Step 7: Add MCP server registration (optional)

If your plugin provides an MCP server:

```json
{
  "mcp": {
    "server": "my-server",
    "auto_register": true,
    "command": ["{plugin_dir}/bin/mcp-server"]
  }
}
```

With `auto_register: true`, the MCP server is automatically added to `opencode.json` on install and removed on uninstall. Users can toggle it with `/mcp enable/disable` afterward.

## Sharing a Plugin

### Option A: Publish on GitHub (recommended)

Push your plugin directory to a public GitHub repo:

```bash
# Create the repo on GitHub first, then:
cd my-plugin
git init
git add -A
git commit -m "Initial release"
git remote add origin https://github.com/YOUR_USER/my-plugin.git
git tag v1.0.0
git push origin main --tags
```

Now anyone can install it:

```bash
/plugin install github.com/YOUR_USER/my-plugin
/plugin install github.com/YOUR_USER/my-plugin@v1.0.0   # pin to a version
```

**Naming convention:** The plugin's directory on disk is derived from the URL path
(`github.com/user/repo` → `user-repo`). But the `name` field in `plugin.json` is what
appears in the TUI and config — so you can choose any display name independent of the
repo URL.

### Option B: Share as a local directory

Zip your plugin and share it however you like. The recipient installs with:

```bash
/plugin install /path/to/plugin-directory
```

This copies the directory into the global plugins path.

### Option C: Project-local plugins

For team-wide plugins, place them under version control inside the project:

```
my-project/
  .opencode/
    plugins/
      team-lint-plugin/
        plugin.json
        commands/
          lint.md
```

These are automatically discovered when any team member runs ocode in that project.
No installation step needed — the plugin is tied to the repository.

## Plugin Best Practices

### Naming
- Use `kebab-case` for the plugin `name` (e.g. `db-inspector`, `git-flow`)
- Keep names short but descriptive — they appear in `/plugin list` and config
- The `plugin.json` `name` is what matters; the directory name is internal

### Versioning
- Use [semver](https://semver.org/) in the `version` field
- Tag releases in git so users can pin with `@v1.0.0`
- List breaking changes in the repo's README or changelog

### Instructions
- Keep `instructions` concise (a paragraph at most) — it's injected every turn
- State *when* the model should use your plugin, not just what it does
- Bad: "The user has the db-inspect plugin installed."
- Good: "When the user asks about database performance, use the db-inspect tool to run EXPLAIN ANALYZE on their queries."

### Commands
- Give each command a **clear, narrow purpose** — one task per command
- The `description` in frontmatter is shown in autocomplete; make it useful
- Add `has_args: true` if the command takes arguments (e.g. `/deploy staging`)
- The command body is a prompt — write it like you'd instruct a developer

### Tools
- Each `.json` file = one tool
- The `command` runs on every invocation — keep it fast
- Write clear parameter descriptions so the LLM fills them correctly
- Mark parameters as `required` only when truly mandatory
- Use `{plugin_dir}` for any file paths; never hardcode paths

### Post-Install Scripts
- Keep `on_install` idempotent — running it multiple times should be safe
- Compile binaries to `{plugin_dir}/bin/` so they're isolated
- Avoid network calls in `on_install` if possible (they block the install flow)
- Check that required system dependencies exist before your script runs

### MCP Servers
- Test the server standalone before wiring it into the plugin
- Log errors to stderr, not stdout (stdout is the MCP protocol transport)
- Set `auto_register: true` so users don't have to manually configure it

## Troubleshooting

| Symptom | Likely Cause |
|---|---|
| Plugin not showing in `/plugin list` | Directory not in a search path, or `plugin.json` is invalid JSON |
| Plugin not enabled after install | Check `opencode.json` — run `/plugin enable <name>` |
| MCP server not starting | Ensure `mcp.command` is correct and `{plugin_dir}` resolved |
| `on_install` failing | Check the captured output in debug panel (`/debug`) |
| Sync showing "error" | Network issue or plugin directory is not a git repo |
| Tool not available to LLM | Tool name in `plugin.json` must match the `.json` filename in `tools/` |
| Command not showing in TUI | `commands/` entry must match the name in `plugin.json` and frontmatter |
| `{plugin_dir}` not expanded | Only `on_install` and `mcp.command` support `{plugin_dir}` — tools use it via the `command` field in the tool JSON |
