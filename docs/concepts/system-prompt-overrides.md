---
type: Concept
title: System prompt overrides — per-prompt enable switch and custom instructions
description: 'Per-prompt control of the built-in system prompts: an enable switch (on by default) and optional custom instructions appended to the built-in prompt. Covers the prompts key in ocodeconfig.json, legacy entry migration, the GET and PUT /api/config/prompts endpoints, the Settings System Prompts group, the 22 catalog keys, what disabling a prompt does by kind, what is deliberately excluded, and the live-store invariant.'
tags:
  - prompts
  - config
  - server
  - api
  - prompt-cache
timestamp: 2026-11-03T05:13:13Z
---
# System prompt overrides — per-prompt enable switch and custom instructions

## Purpose

Each built-in system prompt in the catalog has two user settings: whether it runs
at all, and optional free text appended to it. A prompt runs by default. A key
with no stored entry is enabled with no add-on text.

While a prompt is enabled, `text` is appended after the built-in prompt,
separated by a blank line. Blank text leaves the built-in prompt byte-identical.

Switching a prompt off omits the built-in prompt entirely. It does not send an
empty or shortened version. For a prompt that is the only thing a caller has (a
judge rubric, a helper instruction), that turns the capability off; see
[What disabling a prompt does](#what-disabling-a-prompt-does).

## Config

Overrides live under the `prompts` key in `ocodeconfig.json`, keyed by catalog key:

```json
{
  "prompts": {
    "title": { "text": "Keep titles under six words." },
    "md_summary": { "disabled": true }
  }
}
```

The stored shape is `PromptOverride` in `internal/config/prompt_overrides.go`
(line 30): `disabled` plus `text`, both omitted when empty. There is no `enabled`
field. `SavePromptOverride` (line 155) removes an entry only when the prompt is
enabled and its text is blank — that is the default. A disabled entry is
persisted even with no text, because "off" is a real setting.

### Legacy entries

The previous schema was append-only and stored `{"enabled": bool, "text": "..."}`.
`UnmarshalJSON` (line 51) migrates it on load:

- `enabled: true` keeps the add-on text; the prompt stays enabled.
- `enabled: false` keeps the prompt ENABLED and drops the text. The old schema
  meant "keep the built-in prompt, ignore the add-on", and the new schema has no
  state for text that is saved but not applied. Starting to apply that text
  silently would change the prompt without the user asking.
- `disabled` wins when both fields are present, so a file written by this version
  round-trips exactly.

## Endpoints

- `GET /api/config/prompts` returns `{"prompts": [...]}` in catalog order. Each
  item has `key`, `label`, `description`, `kind`, `enabled` and `text`. `enabled`
  means "the prompt is switched on" and is true for a key with no stored entry
  (`newPromptOverrideRow`, `internal/server/handler_prompts.go` line 25). The
  built-in prompt text is not sent; the UI does not need it.
- `PUT /api/config/prompts/{key}` takes `{"enabled": bool, "text": string}` and
  returns the updated item. `enabled: false` is stored as `{"disabled": true}`
  (line 69). An unknown key is 404. An invalid body is 400. Text over
  `config.MaxPromptOverrideBytes` (32 KiB) is 400, and `SavePromptOverride`
  rejects it too.

Handlers live in `internal/server/handler_prompts.go`. Routes are registered
in `Server.registerRoutes()` in `internal/server/server.go`.

## Settings UI

The web Settings panel shows these prompts in a group titled "System Prompts".
Each row shows the label, a badge with the prompt's `kind`, an
"Enable this prompt" checkbox, a custom-instructions textarea, and Save and
Reset buttons. `kind` is one of `Sub-agent`, `Judge`, `Helper task` (a background
model call such as title, compaction or summaries) or `Main agent prompt` (a
fragment added to the main agent's context). It is `agent.PromptKind` in
`internal/agent/prompt_catalog.go`.

The textarea is an add-on, so it is visible only while the prompt is enabled. While
the prompt is off the card says "Enable this prompt to edit its custom
instructions." and, if text is saved, "The saved text is kept but not applied."
Reset re-enables the prompt and clears the add-on, sending
`{enabled: true, text: ""}`.

## What disabling a prompt does

> **Practical warning:** switching off a judge or helper prompt disables that
> capability. It does not send a degraded request. Auto-continue stops judging,
> goals pause, the guard layers are skipped, titles stay at their default,
> summaries are not produced and the tool digest returns an error. Edit the
> prompt when the goal is to change behaviour; disable it only to remove the
> capability.

The rule is the same in every group: a disabled prompt is never sent, and a caller
that cannot work without it skips its work instead of sending an empty prompt.

### Main-agent fragments

`recap`, `plan`, `communication_style`, `doc_prompt` and `suggested_responses`
omit their marker-plus-prompt system message in `Agent.BasePromptMessages`
(`internal/agent/prompt.go`: lines 280, 287, 294, 340, 347). Each fragment is
still gated by its own feature flag, so the effective rule is
`flag && !disabled`. `discovery_contract` and `redaction_awareness` are omitted
the same way in `internal/agent/discovery_glue.go` (lines 986 and 956).

### Sub-agent prompts

`SubAgentPromptWithOverride` and `overrideBuiltinPrompt` return `""` for a
disabled `subagent_*` prompt (`internal/agent/prompt_catalog.go` lines 208 and
219). The mode fragment is skipped when that text is empty
(`internal/agent/prompt.go` line 258), so the sub-agent gets no role prompt from
the catalog.

### Judge prompts

- Auto-continue: `AutoContinueJudgeSync` makes no triage call and returns
  `Resume=false` with a detail naming the disabled prompt
  (`internal/agent/autocontinue_typesafe.go` line 686). `AutoContinueJudgeAsync`
  dispatches no async work and returns false (`internal/agent/agent.go` line
  3086).
- Goal: the goal pauses as `unavailable` with "goal judge prompt is disabled"
  (`internal/agent/goal.go` line 370). The judge never runs without its rubric.
- Content and network guards: the guard layer is skipped
  (`internal/agent/content_guard_typesafe.go` line 525,
  `internal/agent/network_guard_typesafe.go` line 375). The permission layer
  beneath still decides what actually runs.

### Helper tasks

- Title generation returns `""`, so the session keeps its default title
  (`internal/agent/title.go` line 61).
- Markdown summarisation returns `""`, the documented no-summary result; the
  caller's negative cache keeps the next scan from retrying immediately
  (`internal/agent/md_discovery.go` line 499).
- Speech summarisation returns `""`, so the caller speaks the original text,
  exactly as it does when no summariser client exists
  (`internal/agent/speech_summary.go` line 357).
- The tool digest returns an explicit error, "the tool digest prompt is disabled
  in Settings", because that call is an explicit user action
  (`internal/agent/tool_digest.go` line 139).

### Compaction and advisor

These features still run; only their catalog prompt is left out of the request
builder.

- Compaction: the system instruction is omitted in both builders, which then
  start from `summaryTemplate` and the inline instruction
  (`internal/agent/compact.go` lines 728 and 1316).
- Advisor: the plain-chat fallback adds no system message
  (`internal/agent/advisor_tool.go` line 301), the exploration spec gets an empty
  system prompt (line 349), and the Claude Code advisor omits
  `--append-system-prompt` entirely (line 557).

## Sub-agent prompts

A `subagent_*` override reaches a sub-agent in one place only: the prompt assembler,
`Agent.BasePromptMessages` in `internal/agent/prompt.go` (line 236). It calls
`agent.SubAgentPromptWithOverride` on the spec's raw prompt (line 258), on every
build. Dispatch in `internal/agent/subagent.go` (line 545) and the TUI `switchAgent`
in `internal/tui/model.go` (line 2493) store the raw built-in text and do not apply
the override. A sub-agent name (for example `/explore`) used as the main agent, and a
dispatched sub-agent, therefore both see an edit on their next turn, including an
agent that was already running when the edit was saved. A disabled prompt resolves
to empty here, so no role prompt is sent.

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

Consumers call `config.ResolvePrompt(key, builtin)` when they build a prompt and
omit the fragment or system message when it reports `ok == false`. Callers that
cannot run without their prompt check `config.PromptDisabled(key)` first and skip
the work. `config.ApplyPromptOverride(key, builtin)` remains for call sites that
only need text; it returns `""` when the prompt is disabled.

All three read the process-wide store in `internal/config/prompt_overrides.go`
(lines 101, 127 and 145). They must not read the override from the shared
`*config.Config`. The web server hands every session the same pointer, so a save
would race a running turn. The store is replaced whole on each save, never
mutated in place.

## Prompt cache

The system prompt is part of the cached prefix. The resolved text is read at
prompt-build time, so a save is visible on the next turn and costs one cache miss.
Switching a prompt off changes that same cached block, so a toggle costs the same
miss. This matches the Pulse system prompt. See
[prompt-cache-stability.md](prompt-cache-stability.md).

Cache salts (the speech summary and md summary caches) read the override through
`config.EffectivePromptOverride`, the same predicate `ResolvePrompt` uses. It
returns no add-on text when the prompt is disabled, so a toggle invalidates a
salted cache exactly when it changes the effective prompt.
