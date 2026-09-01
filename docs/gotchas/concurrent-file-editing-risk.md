---
type: Gotcha
title: Concurrent File Editing Risk — Multiple Writers in Same Checkout
description: 'Gotcha: multiple writers (agents + manual edits) on the same files within a single checkout can corrupt both streams. Covers the concurrent modification problem, affected scenarios, mitigation strategies, and recovery.'
tags:
  - gotcha
  - concurrency
  - agents
  - safety
  - workflows
timestamp: 2026-08-31T12:16:05Z
---
Multiple writers (e.g., background agents and manual edits) on the same files within a single checkout can corrupt both streams, leading to unsafe concurrent modifications. This is a durable gotcha for future development workflows involving parallel agents or edits.

## The Problem

When two agents (or one agent and a manual edit) write to the same file concurrently:
- Agent A reads file version 1
- Agent B reads file version 1
- Agent A writes its changes to the file (version 2)
- Agent B writes its changes to the file (version 1 → version 3, **discarding Agent A's changes**)

This results in lost work and potentially corrupted files.

## Affected Scenarios

- **Parallel background agents**: Dispatching multiple `task` tool calls in parallel that touch overlapping files
- **Agent + manual edits**: Running a background agent while manually editing the same files in the IDE
- **Multi-terminal sessions**: Two ocode instances or sessions working on the same checkout simultaneously

## Mitigation Strategies

1. **Serialize writes to the same file**: If two agents need to edit overlapping files, dispatch them sequentially rather than in parallel
2. **Use file-scoped agents**: Assign each agent a non-overlapping set of files
3. **Cancel before editing**: If you manually need to edit files that a background agent is touching, cancel the agent first (or vice versa)
4. **Use git worktrees**: For truly parallel work, use separate git worktrees so each agent has its own checkout
5. **Atomic writes with snapshots**: ocode's snapshot system (`internal/snapshot`) captures pre-edit state and provides `undo_file_change` to revert, but this is a recovery mechanism, not a prevention mechanism

## Recovery

If corruption occurs:
- Check the snapshot store for pre-edit versions
- Use `git diff` to identify lost changes
- Manually merge the intended changes from both sources

## Related

- AGENTS.md "Git Worktrees" section for isolated parallel development
- `internal/snapshot/` for the file-edit snapshot and undo mechanism
- OKF docs/file-edit-snapshot.md for the snapshot system architecture