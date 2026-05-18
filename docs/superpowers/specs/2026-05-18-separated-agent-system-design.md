# Separated Agent System Design

## Goal

Add OpenCode-style separated agents to ocode so custom Markdown agents, such as `git-commit-push`, can be loaded from agent files and invoked by the LLM through the `task` tool as real child-agent runs.

## Problem

ocode currently defines subagents as hard-coded entries in `internal/agent/subagent.go`. The `task` tool exposes only those hard-coded names: `general`, `explore`, and `scout`.

This means OpenCode agent files in `~/.config/opencode/agents/` or `.opencode/agents/` are invisible to ocode. A user can create `git-commit-push.md`, but the LLM cannot select it as a task subagent, and `/git-commit-push` is not a valid command because slash commands are a separate Markdown command system.

The fix should not be a one-off `git-commit-push` entry. ocode needs a proper agent registry, agent-file loading, permission-aware task execution, and separated child sessions.

## Scope

Phase 1 supports Markdown agent files and `task` tool invocation. It does not attempt complete OpenCode parity.

In scope:

- Built-in and loaded agents share one registry.
- Markdown agents load from global and project OpenCode agent directories.
- `mode: subagent` and `mode: all` agents are exposed to the `task` tool.
- Task subagent runs execute in separated child sessions with their own transcript state.
- Parent sessions receive concise task results and references to child session IDs.
- Agent permissions gate tools during child-agent execution.
- Unsupported critical config is rejected loudly instead of silently ignored.

Out of scope for Phase 1:

- JSON `opencode.json` agent definitions.
- Full `/agent` primary-agent parity.
- Manual `@agent` invocation.
- Child-session navigation UI.
- Per-agent model, temperature, top_p, and steps execution behavior.
- Slash command generation for agents.

## Agent Model

Create a unified agent definition type for both built-ins and loaded agents.

Fields:

- `Name`: stable identifier. For Markdown agents, this comes from the filename.
- `Description`: required for surfaced agents.
- `Mode`: `primary`, `subagent`, or `all`.
- `SystemPrompt`: instructions used as the child agent system prompt.
- `Permissions`: OpenCode-style permission rules mapped to ocode tool names.
- `Hidden`: hides a subagent from user-facing lists and task schema descriptions, while keeping explicit task invocations by exact name valid when permissions allow.
- `Source`: built-in, global file path, or project file path for diagnostics.

Built-in agents should move behind this model instead of remaining as separate hard-coded subagent structs.

## Registry

Add an agent registry as the single source of truth for all agent lookup.

Responsibilities:

- Register built-in agents.
- Load global Markdown agents from `~/.config/opencode/agents/*.md`.
- Load project Markdown agents from `.opencode/agents/*.md`.
- Apply precedence: project > global > built-in.
- Return agents by name.
- Return selectable subagents for the `task` tool.
- Return primary agents for future `/agent` parity.

The registry should be deterministic so tests and task schema output are stable.

## Markdown Agent Loading

Markdown files use OpenCode-style frontmatter and body content.

Supported frontmatter fields for Phase 1:

- `description`
- `mode`
- `permission`
- `hidden`

The Markdown body becomes `SystemPrompt`.

Validation rules:

- Filename without `.md` becomes `Name`.
- Missing prompt body is an error.
- Missing `description` is an error for non-hidden surfaced agents.
- Missing `mode` defaults to `all`, matching OpenCode behavior.
- `mode` must be `primary`, `subagent`, or `all`.
- Unsupported critical fields such as `model`, `temperature`, `top_p`, and `steps` produce a load diagnostic that names the ignored field and the agent file. The agent still loads if the supported fields are valid, because these fields affect model behavior rather than tool safety.

If a single custom agent file is invalid, ocode should surface that diagnostic without preventing other valid agents from loading.

## Permission Mapping

Agent permissions must not be ignored. Ignoring permissions creates fake safety.

Supported Phase 1 permission groups:

- `read` gates `read`.
- `edit` gates `write`, `edit`, and `apply_patch` when present.
- `glob` gates `glob`.
- `grep` gates `grep`.
- `bash` gates `bash`.
- `task` gates `task`.
- `webfetch` gates `webfetch`.
- `skill` gates `skill`.
- `question` gates `question`.
- `lsp` gates `lsp`.

Unknown permission groups produce a load diagnostic and are treated as `deny`. They should not default to allow.

For Phase 1, shorthand values are enough:

- `allow`
- `ask`
- `deny`

Pattern-specific bash permissions are deferred for Phase 1. If an agent uses pattern-specific bash permission objects, ocode should emit a diagnostic and treat that bash permission as `ask` rather than silently allowing it.

## Separated Child Sessions

The `task` tool should no longer behave as only an inline recursive call. It should create a child-agent run with isolated state.

Child session behavior:

- Each task call gets a child session ID.
- Child messages start with the selected agent system prompt and the task prompt.
- Child transcript is stored separately from the parent transcript.
- Parent receives a concise result and the child session ID.
- Child session metadata records parent session ID, agent name, start time, and completion status.

Phase 1 does not need full UI navigation for child sessions. It only needs durable separation so navigation can be added later without changing the execution model.

## Task Tool Integration

Update `TaskTool` to use the registry instead of `DefaultSubAgents`.

Behavior:

- Tool schema enum accepts all registry subagent names, including hidden agents, so exact explicit invocations work.
- Tool description includes only non-hidden agent names and descriptions.
- If no agent is provided, default to `general` when available.
- If an unknown agent is requested, return a clear error.
- Selected agent permissions are applied before running tools.
- Child run uses the selected agent's prompt and permission profile.

This is the main path that makes `git-commit-push` usable by the LLM when a matching Markdown agent file exists.

## Diagnostics

Agent loading should be inspectable.

Minimum diagnostics:

- Loaded agent count.
- Invalid agent files with reason.
- Source path for each loaded custom agent.
- Task tool schema includes loaded custom subagents.

Diagnostics can initially appear in debug logs or command output. They should not be hidden in silent fallback paths.

## Testing

Add focused tests for:

- Built-in agents are present in the registry.
- Global Markdown agents load.
- Project Markdown agents override global agents.
- Custom agents override built-ins by name.
- `mode: subagent` and `mode: all` appear in task schema.
- `mode: primary` does not appear in task schema.
- Invalid Markdown agent files produce diagnostics.
- Agent permissions restrict child-run tools.
- Task execution creates a child session and returns a parent-visible result.

## Success Criteria

Phase 1 is complete when a user can create an OpenCode-style Markdown file such as `~/.config/opencode/agents/git-commit-push.md`, restart ocode, and the LLM can invoke it through the `task` tool as a separate child-agent run with its own prompt, permissions, transcript, and result returned to the parent.
