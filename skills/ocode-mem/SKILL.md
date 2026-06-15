---
name: ocode-mem
description: Persistent memory workflow for ocode that preserves user preferences, project preferences/history, and global history with layered summaries and recovery.
when_to_use: When the user asks to remember project context, resume prior work, preserve durable decisions, recover context after a long gap, or maintain separate user, project, and global memory for ocode.
---

# ocode Memory

Use this skill to keep long-running ocode work coherent across sessions without dumping everything into the chat.

## Memory scopes

Maintain four distinct scopes and never merge them into one blob:

1. **User preferences** — stable personal defaults that should apply across every project.
2. **Project preferences and history** — decisions, conventions, and context that belong to one repository or worktree.
3. **Global history** — cross-project lessons, recurring workflows, and durable notes that are useful everywhere but are not personal defaults.
4. **Session context** — raw exploration, temporary hypotheses, and one-off investigation details.

## Storage proposal

Use separate files for each scope so updates stay clean and reversible:

- **User preferences**: `~/.local/share/opencode/memory/user.md` on macOS/Linux, or the platform equivalent under ocode's global data dir.
- **Global history**: `~/.local/share/opencode/memory/global.md` on macOS/Linux, or the platform equivalent under ocode's global data dir.
- **Project memory**: `~/.local/share/opencode/project/<slug>/memory.md`, alongside the existing project session folder for that slug.
- **Session context**: keep in the chat, then compress into one of the three persisted layers when it becomes durable.

If a project already has a local memory file, prefer the project copy over the global user copy for that repository.

## /mem command

Use `/mem` to inspect or toggle the active memory layer.

- `/mem` or `/mem status` prints the current status plus file previews for user, project, and global scopes.
- `/mem on` enables memory context injection.
- `/mem off` disables memory context injection.

## Precedence order

When reading memory, prefer the most specific source first:

1. Project memory
2. User preferences
3. Global history
4. Current session

When writing memory, do the opposite: only promote information upward when it is stable enough to matter again.

## Startup checklist

When this skill is active, recover context in this order:

- Read the repo instructions (`CLAUDE.md`, `AGENTS.md`, or other top-level guidance if present).
- Read project memory for the current repository if it exists.
- Read user preferences memory if it exists.
- Read global history if it exists and the task benefits from cross-project context.
- Review the bundled skill inventory with `/skills` or the repo's skill docs if the task depends on existing workflows.
- If the user refers to earlier work and the context is missing, do targeted file reads instead of guessing.

## Capture loop

After meaningful work, compress what happened into the correct memory layer:

- **User preferences**: repeated personal defaults, UI/workflow choices, phrasing preferences, or stable cross-project habits.
- **Project memory**: repository-specific decisions, architecture notes, conventions, recurring commands, and project history.
- **Global history**: reusable lessons, durable agent workflows, and cross-project discoveries.

Keep each update short. Prefer bullets over paragraphs.

## What to record

Write memory when the information is likely to matter again:

- a repeated preference or workflow
- a project architecture decision
- a command or check that keeps coming up
- a constraint discovered the hard way
- a path, file, or tool that is central to the task
- a debugging lesson that would help in future projects

Do not write:

- speculative ideas
- dead ends
- noisy transcripts
- duplicate copies of the current chat

## How to compress

When the current context is too large, condense it into a reusable summary with this structure:

```markdown
## Current focus
- ...

## Stable preferences
- ...

## Project decisions
- ...

## Global lessons
- ...

## Open threads
- ...

## Next time
- ...
```

Favor wording that can be pasted directly into a memory file or project instruction file.

## Promotion rules

- If a rule should always apply, move it into `CLAUDE.md` or the closest equivalent project instructions.
- If it is useful but only for one repository, keep it in project memory.
- If it is useful everywhere but not a personal default, keep it in global history.
- If it is only relevant for one task, leave it in the session.

## Recovery rules

If the user asks to continue prior work and memory is stale or missing, rebuild the minimum viable context from source files, recent changes, and the last validated commands.

Never invent remembered facts. If memory and source disagree, trust the source and update memory afterward.

## Output format

When asked for a memory summary, return:

1. Current state
2. User preferences
3. Project decisions
4. Global lessons
5. Open questions
6. Next actions

Keep it concise and directly reusable.
