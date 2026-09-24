---
type: Concept
title: Advisor Claude Code CLI backend on web/desktop
description: How the web/desktop advisor model picker exposes Claude Code CLI models via a sentinel provider, how claude_code is derived from the provider on PUT /api/advisor, and the picker parity with the TUI.
tags:
  - advisor
  - claude-code
  - model-picker
  - web
  - desktop
  - provider
  - backend
timestamp: 2026-09-21T05:45:25Z
resource: web/src/components/Layout/ModelDialog.tsx; internal/tui/picker.go; web/src/components/Layout/modelSelection.ts; internal/server/handler_config.go; internal/agent/advisor_tool.go
---
## Summary

The web/desktop advisor model picker (`web/src/components/Layout/ModelDialog.tsx`, `purpose="advisor"`) now offers a "Claude Code (Read-Only CLI)" section, mirroring the TUI (`internal/tui/picker.go` `prependClaudeCodeSection`). Selection semantics are now TUI parity: `PUT /api/advisor` receives `{provider, model, claude_code: provider === "claude-code"}`, and "Clear (not set)" clears both provider and `claude_code`.

## Picker section

`CLAUDE_CODE_SECTION_TITLE` and `CLAUDE_CODE_ADVISOR_MODELS` (`web/src/components/Layout/modelSelection.ts`) define the section label and the model list. The models are **synthetic** `ModelInfo` rows prepended only for the advisor purpose — they don't appear in the global model catalog.

**Current model list (keep in sync with TUI `CLAUDE_CODE_ADVISOR_MODELS`):**
- `claude-sonnet-4-6`
- `claude-sonnet-5`
- `claude-opus-4-8`
- `claude-opus-4-7`
- `claude-opus-5`
- `claude-opus-5-5`
- `claude-haiku-4-5`
- `claude-fable-5`

The sentinel provider is `"claude-code"` (`CLAUDE_CODE_PROVIDER`). When the user picks any model in this section, the picker records `provider: "claude-code"`.

## `claude_code` derivation

`PUT /api/advisor` (`internal/server/handler_config.go`) derives `claude_code` from the provider: `claude_code = provider === "claude-code"`. An explicit `claude_code` field in the request body wins if present.

**Why this matters:** previously the web dialog carried the stored `claude_code` value through every pick. Selecting a registry provider (e.g. `anthropic`) after a Claude Code pick could leave `claude_code=true`, causing the advisor to run `claude -p --model <non-claude-model>` instead of calling the picked provider. Now the flag is derived from the provider at request time, so it is always consistent.

**"Clear (not set)"** now clears `provider` too, which makes `claude_code=false` deterministically.

## Backend execution

When `claude_code=true`, the advisor executes via `executeClaudeCodeAdvisor` (`internal/agent/advisor_tool.go`), which uses `claudeCLIPath()` + `withLoginShellPath()` to resolve and pass the user's login-shell `PATH` (see `gotchas/desktop-subprocess-path-trap.md`). TUI and shell-launched server paths use bare `"claude"` with the existing process PATH.

## Tests

- Web: `web/src/components/Layout/modelSelection.test.ts`, `web/src/components/Layout/ModelDialog.test.tsx`
- Go: `internal/agent/claude_cli_test.go`
