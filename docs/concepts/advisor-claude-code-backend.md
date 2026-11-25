---
type: Concept
title: Advisor Claude Code CLI backend on web/desktop
description: How the web/desktop advisor picker follows the normal web section order, exposes Claude Code as its first provider group, keeps shared history read-only, and derives claude_code from the provider on PUT /api/advisor.
tags:
  - advisor
  - claude-code
  - model-picker
  - web
  - desktop
  - provider
  - backend
  - recent-models
  - favorites
  - session-scoping
timestamp: 2026-11-24T23:31:33Z
resource: "web/src/components/Layout/ModelDialog.tsx; internal/tui/picker.go; web/src/components/Layout/modelSelection.ts; internal/server/handler_config.go; internal/agent/advisor_tool.go"
---
## Summary

The web/desktop advisor model picker (`web/src/components/Layout/ModelDialog.tsx`, `purpose="advisor"`) follows the normal web picker convention: **Recently Used → ★ Favorites → Claude Code (Read-Only CLI) → registry providers**. `advisor` is now included in `supportsSections`, so the shared priority sections and their dedupe apply. The synthetic Claude Code rows are prepended to the model list, making Claude Code the first provider group without moving it above the priority sections.

This is a deliberate web/TUI difference: the TUI (`internal/tui/picker.go`, `openAdvisorPicker` → `prependClaudeCodeSection`) still prepends Claude Code above Recently Used and Favorites. Selection semantics remain unchanged: `PUT /api/advisor` receives `{provider, model, claude_code: provider === "claude-code"}`, and "Clear (not set)" clears both provider and `claude_code`.

## Picker sections and controls

`CLAUDE_CODE_SECTION_TITLE` and `CLAUDE_CODE_ADVISOR_MODELS` (`web/src/components/Layout/modelSelection.ts`) define the section label and the model list. The models are **synthetic** `ModelInfo` rows prepended only for the advisor purpose — they don't appear in the global model catalog. `partitionModelSections` removes recents/favorites from provider groups, so a model shown in a priority section is not repeated below; empty priority sections are omitted.

Advisor has **no favorite-star toggles or recent-removal controls**. `supportsFavoriteToggle` is true only for `main` and `permission`; the advisor can display and select shared favorites, but only those two purposes own the star control. Likewise, `canRemove` requires `purpose !== "advisor"`, so an advisor recent row has neither the Radix `Remove from recently used` menu nor a native context menu.

## Shared recent-model history is read-only for advisor

Per explicit user decision, advisor selections show the shared Recently Used/Favorites history but **do not call `config.SaveRecentModel`**. `HandleSetAdvisor` (`internal/server/handler_config.go`) is unchanged and does not append the picked advisor model to shared recents. `TestHandleSetAdvisorDoesNotChangeRecentModels` pins this by seeding a recent model, issuing an advisor PUT, and asserting the list is unchanged.

The selection payload and session scoping are unchanged: a real session still writes that chat's advisor pin through `api.setAdvisorFull(..., host, realSessionId)`, while a draft/no-session pick updates the default for new chats. Sharing history in the picker does not make advisor picks contribute to that history.

**Current model list (keep in sync with TUI `CLAUDE_CODE_ADVISOR_MODELS`):**
- `claude-sonnet-4-6`
- `claude-sonnet-5`
- `claude-sonnet-5-5`
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

- Web: `web/src/components/Layout/modelSelection.test.ts`, `web/src/components/Layout/ModelDialog.test.tsx` — advisor section order, dedupe, empty history, no star toggles, read-only shared recents (`keeps shared recents read-only` asserts no removal menu and no `removeModelRecent` call), and Claude Code as the first provider group.
- Go: `internal/server/handler_config_test.go` `TestHandleSetAdvisorDoesNotChangeRecentModels`; `internal/agent/claude_cli_test.go`.