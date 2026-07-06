---
type: Guide
title: Using ocode with Zed
description: Setup guide and feature matrix for integrating ocode with the Zed editor via ACP (Agent Client Protocol).
tags:
  - zed
  - acp
  - editor-integration
  - setup
timestamp: 2026-07-06T08:34:47Z
---
# Using ocode with Zed

ocode implements the [Agent Client Protocol (ACP)](https://agentclientprotocol.com)
so it appears as a selectable agent in Zed's agent panel, alongside Claude Code,
Codex, and Gemini CLI.

## Setup

1. **Build or install ocode** so the binary is on your `PATH`:
   ```sh
   go install github.com/u007/ocode@latest
   # or: go build -o ~/bin/ocode .
   ```

2. **Configure a model** in `~/.config/opencode/opencode.json` (or the project's
   `.opencode/ocodeconfig.json`):
   ```json
   {
     "model": "anthropic/claude-sonnet-4-6"
   }
   ```
   Alternatively, export `OPENCODE_MODEL=anthropic/claude-sonnet-4-6`.

3. **Register ocode in Zed settings** (`~/.config/zed/settings.json`):
   ```json
   {
     "agent": {
       "agent_servers": [
         {
           "id": "ocode",
           "display_name": "ocode",
           "command": "ocode",
           "args": ["acp"]
         }
       ]
     }
   }
   ```

4. Restart Zed (or reload settings) and select **ocode** from the agent dropdown
   in the agent panel.

## What works in v1

| Feature | Status |
|---------|--------|
| Chat turns (text prompt → streamed response) | ✅ |
| Tool call rendering (read, edit, bash, search, fetch) | ✅ |
| Tool permission dialogs (allow once / always / reject) | ✅ |
| Cancel in-flight turn (Zed cancel button) | ✅ |
| @-mentioned file context (embedded resource blocks) | ✅ |
| File reference links (resource_link blocks) | ✅ |
| Reasoning / extended thinking output | ✅ |
| Session persistence (sessions appear in ocode's TUI picker) | ✅ |

## Known limitations (v1)

- **Unsaved buffer contents**: `fs/read_text_file` / `fs/write_text_file` are not
  implemented. ocode tools read from disk, so unsaved Zed edits are not visible
  to the agent. Save before asking ocode to read a file.
- **MCP servers**: Zed can pass `mcpServers` in `session/new`, but ocode ignores
  them and loads MCP from its own config (`opencode.json`).
- **Session history replay**: `session/load` is not supported. Each Zed session
  starts fresh (though ocode persists turns internally for the TUI session picker).
- **Image / audio inputs**: Not supported (capabilities advertise `image: false`,
  `audio: false`).
- **Slash commands / plans**: Not surfaced in v1.
