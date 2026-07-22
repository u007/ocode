# ocode Setup Guide

## Prerequisites

- **Go 1.26.1 or later** — [Download Go](https://go.dev/dl)
- **Git** — for cloning and version control
- **An LLM provider account** — OpenAI, Anthropic, Google, Z.AI, Alibaba, or GitHub Copilot
- **Terminal** — macOS, Linux, or Windows with WSL2

## Installation

### 1. Clone the repository

```bash
git clone https://github.com/your-org/ocode.git
cd ocode
```

### 2. Download dependencies

```bash
go mod download
```

### 3. Run locally

```bash
go run .
```

Or build a static binary:

```bash
go build -o ocode .
./ocode
```

## Configuration

### Initial Setup (First Run)

On first launch, ocode creates default config files:

- **`opencode.json`** (project root) — LLM provider credentials and model settings (shared with opencode)
- **`ocodeconfig.json`** (`~/.config/opencode/`) — ocode-only state: permissions, editor config. This is now global-only; project-level copies are no longer loaded.

You'll be prompted to authenticate with your LLM provider(s) via OAuth or API key.

### Adding Provider Credentials

Edit `opencode.json` to add API keys or provider configuration:

```json
{
  "provider": "anthropic",
  "apiKey": "sk-ant-...",
  "model": "claude-3-5-sonnet"
}
```

Or use environment variables (ocode respects `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, etc.).

**Global override:** Set `OPENCODE_AUTH_TOKEN` to use a single token for all providers, bypassing per-provider configuration and stored credentials. This is useful for CI/CD or when using a proxy that handles authentication.

> ⚠️ The override is sent as a **plain API key** (`Authorization`/`x-api-key` header), not via the OAuth flow. It is **not** appropriate for OAuth-subscription providers (Anthropic Max/Console, GitHub Copilot): setting it suppresses the stored OAuth credential and sends the token with the wrong scheme, producing 401s. Use it only with providers that accept a bare API-key token.

### Permissions & Safety

ocode starts in **normal mode** (project-confined file writes allowed, dangerous tools ask). Configure in `ocodeconfig.json`:

```json
{
  "permissions": {
    "mode": "normal",
    "tools": {
      "read": "allow",
      "write": "allow",
      "bash": "ask"
    }
  }
}
```

Or use `/permissions` in the TUI to view/edit rules interactively, or `/ban` to manage the bash deny list (default: `sed`, supports multi-word prefixes like `grep -n`, and `/ban clear` asks for confirmation first).

For hands-off operation, set an `auto_permission_model`:

```json
{
  "permissions": {
    "auto_permission_model": "deepseek:deepseek-v4-flash"
  }
}
```

## Development

### Running Tests

```bash
go test ./...
```

### Building for distribution

```bash
go build -o ocode .
# Binary is portable; ship it anywhere Go 1.26+ can run
```

### Debugging

Enable verbose logging:

```bash
DEBUG=1 go run .
```

ocode logs to stderr and a debug panel in the TUI (visible in the Files tab).

## Troubleshooting

### "Cannot find Go"

Ensure Go 1.26.1+ is installed and in your `$PATH`:

```bash
go version
```

### Auth fails silently

Check that you have the right provider credentials in `opencode.json` or environment variables. ocode falls back to keychain on macOS/Linux.

### TUI doesn't render

Ensure your terminal supports 256 colors and mouse input. Test with:

```bash
echo $TERM
```

Preferred: `xterm-256color`, `screen-256color`, `tmux-256color`, or modern terminals (iTerm2, Ghostty, Warp).

### Config file conflicts

`opencode.json` (project root) holds provider config; `ocodeconfig.json` (`~/.config/opencode/`) holds ocode-specific state. If you need to reset ocode settings:

```bash
rm -rf ~/.config/opencode/
```

ocode recreates defaults on next run.

## File Structure

```
.
├── main.go                    entry point
├── go.mod / go.sum            dependencies
├── internal/
│   ├── agent/                 LLM client, agent registry, permissions
│   ├── auth/                  OAuth and keychain auth
│   ├── config/                opencode.json / ocodeconfig.json loading
│   ├── mcp/                   MCP client
│   ├── server/                HTTP API
│   ├── tool/                  built-in tools (read, write, bash, git, etc.)
│   ├── tui/                   Bubble Tea TUI
│   └── version/               version info
├── docs/                      design specs
└── opencode.json              (created at first run) provider config
```

> Global ocode state lives at `~/.config/opencode/ocodeconfig.json` (macOS/Linux).

## Next Steps

1. Run `go run .` to start the TUI
2. Set your preferred LLM provider via `/config` or edit `opencode.json`
3. Type a message and press `Ctrl+Enter` to chat
4. Press `?` to see all keyboard shortcuts
5. Check `/help` for slash commands

For detailed documentation, see [README.md](README.md).
