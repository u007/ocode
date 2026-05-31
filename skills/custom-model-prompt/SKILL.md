---
name: custom-model-prompt
description: Create or update a model-specific custom prompt file ({MODEL}.OCODE.md)
when_to_use: When the user asks to add, configure, customize, or update a prompt for a specific model. Also triggered by: "custom prompt for", "model-specific behavior", "tailor this model", "system prompt for {model}", "make this model behave", "model instructions"
---

# Custom Model Prompt

Create or update a file named `{MODEL}.OCODE.md` to inject a custom system prompt for a specific model.

## File naming

Name the file using the lowercase model identifier (e.g. `deepseek-v4-flash`, `claude-sonnet-4-6`) plus `.OCODE.md`:
- `deepseek-v4-flash.OCODE.md`
- `claude-sonnet-4-6.OCODE.md`

Matching is case-insensitive, but prefer lowercase for consistency.

## Search priority (first match wins)

| Priority | Location | Example |
|----------|----------|---------|
| 1 (highest) | Project root | `./deepseek-v4-flash.OCODE.md` |
| 2 | `.opencode/` | `./.opencode/deepseek-v4-flash.OCODE.md` |
| 3 (lowest) | Global config | `~/.config/opencode/deepseek-v4-flash.OCODE.md` |

When asked to add a model-specific prompt, create the file at the highest-priority location available (usually project root).

## Content template

```markdown
# Model-Specific Instructions for {MODEL}

## Role
[Describe what this model should act like — e.g. "You are a senior Go backend developer"]

## Coding Style
[Language/framework conventions, formatting preferences, testing style]

## Constraints
[Things the model should avoid doing]

## Domain Knowledge
[Project-specific context that only applies when this model is active]
```

If the user provides specific instructions, use those instead. Only use the template when the user says something general like "add a custom prompt for this model."

## Git note

If the project uses git, remind the user that `{MODEL}.OCODE.md` follows the same stable-version rule as AGENTS.md: uncommitted edits use the HEAD version. Tell them to commit after creating the file.

## Checking existing files

Before creating a new file, check if one already exists at any priority level. If updating, edit the existing file in place.
