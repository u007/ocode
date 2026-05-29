# CocoIndex Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Prerequisite:** The plugin system from `2026-05-28-plugin-system.md` must be implemented first — specifically Tasks 1–4 (config, loader, manager, /plugin command).

**Goal:** Ship a bundled CocoIndex plugin that indexes the current project's codebase and gives the agent a semantic `search` tool via an MCP server.

**Architecture:** The real package is `cocoindex-code` (PyPI), its CLI binary is `ccc`. Running `ccc mcp` starts an MCP server that exposes a `search(query, limit, languages, paths)` tool. The plugin: (1) installs `cocoindex-code` via `pipx`, (2) runs `ccc init` once in the project root to build the index, (3) registers `ccc mcp` as a local MCP server in ocode config. No custom Python flow is needed — `cocoindex-code` handles indexing internally.

**Tech Stack:** Python (`cocoindex-code` pip/pipx package), `ccc` CLI, MCP protocol, ocode plugin system.

**Verified API** (from https://github.com/cocoindex-io/cocoindex-code):
- Install: `pipx install cocoindex-code`
- Init project: `ccc init` (run once in project root)
- Update index: `ccc index`
- MCP server: `ccc mcp`
- MCP `search` tool params: `query: str, limit: int = 5, offset: int = 0, refresh_index: bool = True, languages: list[str] | None, paths: list[str] | None`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `plugins/cocoindex/plugin.json` | Create | Plugin metadata, on_install, MCP auto-register config |
| `plugins/cocoindex/README.md` | Create | Usage instructions with verified `ccc` commands |

No `flow.py`, no `start_mcp.sh` — `cocoindex-code` handles everything internally through `ccc`.

---

### Task 1: Create plugin manifest

**Files:**
- Create: `plugins/cocoindex/plugin.json`

- [ ] **Step 1: Create `plugins/cocoindex/plugin.json`**

```json
{
  "name": "cocoindex",
  "description": "Semantic code search via CocoIndex. Indexes the project with ccc and exposes a search MCP tool.",
  "version": "0.1.0",
  "author": "ocode",
  "commands": [],
  "tools": ["search"],
  "instructions": "You have access to semantic code search via the MCP 'search' tool from the cocoindex server. Use it when you need to find files, functions, or concepts by meaning rather than exact text — especially for fuzzy or conceptual queries where grep would miss results. The tool accepts: query (str), limit (int, default 5), languages (list of language names, optional), paths (list of glob patterns, optional).",
  "on_install": ["pipx", "install", "cocoindex-code"],
  "mcp": {
    "server": "cocoindex",
    "auto_register": true,
    "command": ["ccc", "mcp"]
  }
}
```

Note: `on_install` uses `pipx` for isolated install. If `pipx` is not available the user will see a clear error from the `pipx` binary — they can install it with `brew install pipx` or `pip install pipx`. The `command` in `mcp` is `["ccc", "mcp"]` — no `{plugin_dir}` needed since `ccc` is installed globally by pipx.

- [ ] **Step 2: Create `plugins/cocoindex/README.md`**

```markdown
# CocoIndex Plugin for ocode

Semantic code search via [CocoIndex Code](https://github.com/cocoindex-io/cocoindex-code).

## Requirements

- Python 3.10+
- `pipx` on PATH (`brew install pipx` or `pip install pipx`)

## Install

From the ocode TUI:

```
/plugin install ./plugins/cocoindex
```

Or once published to GitHub:

```
/plugin install github.com/jamesmercstudio/ocode-cocoindex-plugin
```

The plugin installs `cocoindex-code` via pipx and registers `ccc mcp` as a
local MCP server in your ocode config automatically.

## First-time setup

After installing, run this once in your project root to build the index:

```bash
ccc init
```

Then reload ocode — the agent will have access to the `search` tool.

## Update the index

```bash
ccc index
```

Or the MCP `search` tool accepts `refresh_index: true` to update on demand.

## MCP tool reference

The `search` tool (exposed via `ccc mcp`):

```
search(
    query: str,                           # natural language or code snippet
    limit: int = 5,                       # max results (1–100)
    offset: int = 0,                      # pagination
    refresh_index: bool = True,           # refresh before querying
    languages: list[str] | None = None,   # filter e.g. ["go", "python"]
    paths: list[str] | None = None,       # filter e.g. ["internal/agent/*"]
)
```

Returns file path, language, code chunk, line numbers, and similarity score.

## Verify MCP server is loaded

```
/mcp list
```

You should see `cocoindex` listed as enabled.
```

- [ ] **Step 3: Commit**

```bash
git add plugins/cocoindex/plugin.json plugins/cocoindex/README.md
git commit -m "feat(plugins/cocoindex): add CocoIndex plugin manifest using verified cocoindex-code API"
```

---

### Task 2: Smoke-test the plugin install flow manually

This task verifies the plugin system (from Plan 1) and cocoindex work end-to-end. Run after Plan 1 is implemented.

- [ ] **Step 1: Build ocode**

```
cd /Users/james/www/ocode && go build -o /tmp/ocode . 2>&1
```

- [ ] **Step 2: Install the plugin from TUI**

Launch `/tmp/ocode`, then type:

```
/plugin install ./plugins/cocoindex
```

Expected flow:
1. Plugin cloned/copied to `~/.config/opencode/plugins/cocoindex/`
2. `on_install` command shown: `pipx install cocoindex-code`
3. Prompt: `Type /plugin confirm to proceed`

- [ ] **Step 3: Confirm**

```
/plugin confirm
```

Expected: `pipx install cocoindex-code` runs, MCP server `cocoindex` auto-registered.

- [ ] **Step 4: Verify MCP registered**

```
/mcp list
```

Expected: `cocoindex  local  enabled  0 tools` (0 tools until `ccc init` is run).

- [ ] **Step 5: Init CocoIndex in project root**

Outside ocode, in a terminal:

```bash
ccc init
```

Expected: index built in `.cocoindex/` directory.

- [ ] **Step 6: Restart ocode and verify search tool available**

Launch ocode again, run:

```
/mcp list
```

Expected: `cocoindex  local  enabled  1 tools` (the `search` tool).

Ask the agent:

```
Use cocoindex search to find where the agent step loop is implemented
```

Expected: agent calls the `search` MCP tool and returns relevant file locations.

---

## Self-Review

**Blocker 2 fully addressed:**
- Previous plan invented `cocoindex.flow_def`, `cocoindex.functions.SplitRecursively`, `cocoindex-mcp` pip package — all fabricated ✓ (removed)
- New plan uses only verified API: `pipx install cocoindex-code`, `ccc init`, `ccc mcp`, `search` tool params ✓
- `flow.py` removed entirely — `cocoindex-code` handles indexing internally, no custom flow needed ✓

**Dependencies:**
- Requires `2026-05-28-plugin-system.md` Tasks 1–4 fully implemented
- `on_install` confirmation flow (Plan 1 Task 4) gates the `pipx install` behind user approval ✓
- MCP auto-register (Plan 1 Task 3 `AutoRegisterMCP`) writes the `["ccc", "mcp"]` entry ✓
- No `{plugin_dir}` substitution needed in MCP command since `ccc` is a global pipx binary ✓
