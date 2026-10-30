---
type: Concept
title: System prompt overrides — per-prompt text appended to built-in prompts
description: 'Per-prompt refinements of the built-in system prompts. Each catalog key carries an enabled flag and free text that is appended after the built-in prompt. Covers the prompts key in ocodeconfig.json, the GET and PUT /api/config/prompts endpoints, the Settings System Prompts group, the 20 catalog keys, what is deliberately excluded, and the live-store invariant.'
tags:
  - prompts
  - config
  - server
  - api
  - prompt-cache
timestamp: 2026-10-25T00:00:00Z
---
# System prompt overrides — per-prompt text appended to built-in prompts

## Purpose

Each built-in system prompt in the catalog can be refined by the user without
replacing it. An override is an `enabled` flag plus free `text`. When both are
set, the text is appended after the built-in prompt, separated by a blank line.
A disabled or blank override leaves the built-in prompt unchanged.

## Config

Overrides live under the `prompts` key in `ocodeconfig.json`, keyed by catalog key:

```json
{
  "prompts": {
    "title": { "enabled": true, "text": "Keep titles under six words." }
  }
}
```

An entry that is disabled and blank is removed from the file on save.

## Endpoints

- `GET /api/config/prompts` returns `{"prompts": [...]}` in catalog order. Each
  item has `key`, `label`, `description`, `kind`, `enabled` and `text`. The
  built-in prompt text is not sent; the UI does not need it.
- `PUT /api/config/prompts/{key}` takes `{"enabled": bool, "text": string}` and
  returns the updated item. An unknown key is 404. An invalid body is 400. Text
  over `config.MaxPromptOverrideBytes` (32 KiB) is 400, and `SavePromptOverride` rejects it too.

Handlers live in `internal/server/handler_prompts.go`. Routes are registered
in `Server.registerRoutes()` in `internal/server/server.go`.

## Settings UI

The web Settings panel shows these prompts in a group titled "System Prompts".
Each row shows the label, a badge with the prompt's `kind`, an enable checkbox,
a text box, and Save and Reset buttons. Reset clears the override. `kind` is one
of `Sub-agent`, `Judge`, `Helper task` (a background model call such as title,
compaction or summaries) or `Main agent prompt` (a fragment added to the main
agent's context). It is `agent.PromptKind` in `internal/agent/prompt_catalog.go`.

## Sub-agent prompts

A `subagent_*` override reaches a sub-agent in one place only: the prompt assembler,
`Agent.BasePromptMessages` in `internal/agent/prompt.go` (line 191). It calls
`agent.SubAgentPromptWithOverride` on the spec's raw prompt (line 213), on every
build. Dispatch in `internal/agent/subagent.go` (line 526) and the TUI `switchAgent`
in `internal/tui/model.go` (line 2493) store the raw built-in text and do not apply
the override. A sub-agent name (for example `/explore`) used as the main agent, and a
dispatched sub-agent, therefore both see an edit on their next turn, including an
agent that was already running when the edit was saved.

## Catalog keys

The catalog is `agent.PromptCatalog()` in `internal/agent/prompt_catalog.go`.
It currently holds 22 keys: `advisor`, `advisor_claude_code`,
`auto_continue_judge`, `communication_style`, `compaction`, `content_guard`,
`discovery_contract`, `doc_prompt`, `goal_judge`, `md_summary`,
`network_guard`, `plan`, `recap`, `redaction_awareness`, `speech_summary`,
`subagent_context`, `subagent_explore`, `subagent_general`, `subagent_scout`,
`suggested_responses`, `title` and `tool_digest`.

## Deliberately not covered

- The permission judge prompt. Configure it with `permissions.auto.prompt`.
- The Pulse assistant prompt. Configure it with `pulse_system_prompt`.
- The commit message prompt. Configure it with `commit_msg_prompt`.
- The provider main system prompts.

## Invariant: read the live store at call time

Consumers call `config.ApplyPromptOverride(key, builtin)` when they build a
prompt. That reads the process-wide store in `internal/config/prompt_overrides.go`.
They must not read the override from the shared `*config.Config`. The web
server hands every session the same pointer, so a save would race a running
turn. The store is replaced whole on each save, never mutated in place.

## Prompt cache

The system prompt is part of the cached prefix. A mid-session edit to an
override costs one cache miss on the next request. This matches the Pulse
system prompt. See [prompt-cache-stability.md](prompt-cache-stability.md).

Cache salts (the speech summary and md summary caches) read the override through
`config.EffectivePromptOverride`, the same predicate `ApplyPromptOverride` uses, so
an edit invalidates a salted cache exactly when it changes the effective prompt.
