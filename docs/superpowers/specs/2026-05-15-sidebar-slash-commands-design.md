# Sidebar, Slash Commands, and Telemetry Design

**Goal:** Add an OpenCode-style right sidebar, fix slash-command discovery/execution, and surface live session telemetry.

## Architecture

Use one command registry as the source of truth for slash commands, aliases, help text, execution, and autocomplete. The TUI will render a two-column layout on wide terminals and fall back to a single-column layout on narrow terminals.

The sidebar is a live view over app state: session id, model, context usage, spend, MCP status, LSP status, changed files, and the current session TODO list. File rows are clickable and open the configured external editor on the selected file path.

## User-Facing Behavior

- `Ctrl+B` toggles the sidebar.
- `/sidebar` toggles the sidebar.
- `Ctrl+P` still opens command palette.
- Slash commands autocomplete in the main input.
- `/model ` suggests available model names.
- Sidebar auto-hides below the configured narrow-width threshold.
- Clicking a changed file opens that file in the external editor.

## Data Sources

- Session id: `model.sessionID`.
- Context usage: exact token usage recorded from provider responses when available.
- Spend: computed from exact token usage using models.dev pricing data bundled in the app.
- MCP status: enabled MCP servers from config plus loaded MCP tools.
- LSP status: active LSP tool/client state.
- Changed files: session-local file ledger for the current chat session.
- TODOs: current session TODO state, updated when `todowrite` succeeds.

## Command System

Commands are described once with:

- canonical name
- aliases
- help text
- whether they take a model argument
- handler

That registry drives:

- `/help`
- command palette
- slash autocomplete
- command execution

Matching is simple and deterministic: prefix matches first, then substring matches.

## Telemetry Rules

- Use exact token counts from provider responses whenever the provider returns usage fields.
- Compute spend from those exact counts using models.dev pricing for the active model.
- If a provider does not expose usage data, the sidebar should not invent a number.
- If a model is not in the pricing table, show unknown spend rather than fabricating a rate.

## Sidebar Layout

Order of sections:

1. Session / model / context / spend
2. MCP
3. LSP
4. Changed files
5. TODO
6. Commands / shortcuts

The sidebar should stay compact and readable on small wide terminals, with auto-hide on cramped widths.

## Status Labels

- MCP should show enabled servers and loaded tool counts, not a fake heartbeat.
- LSP should show configured/initialized language servers and any active clients.
- TODO should mirror the latest session todo state written by the tool.

## Session Scope

- Changed-file tracking resets when a new session starts.
- TODO state is session-scoped and does not read from `TODO_OCODE.md`.
- The sidebar never reads TODOs from disk.

## Testing

- Command registry dispatch tests.
- Palette filtering tests.
- Slash autocomplete tests.
- `/model` argument completion tests.
- Sidebar render tests for wide and narrow terminals.
- Telemetry calculation tests for token usage and pricing lookup.
- Changed-file click path tests.
