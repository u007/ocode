---
type: Guide
title: Worktree-Based Parallel Feature Development
description: Integration pattern for parallel feature development using git worktrees to avoid collisions when multiple features touch shared files
timestamp: 2026-09-06T09:56:59Z
---
# Worktree-Based Parallel Feature Development

## Overview

This document captures the durable integration pattern for parallel feature development using git worktrees. When multiple features need to touch the same files, worktrees provide isolation while sharing the same underlying codebase. This pattern ensures collision-avoidance with specific integration protocols.

## Core Principles

### 1. Isolation via Git Worktrees
- Each feature develops on its own worktree branch
- Worktrees share the same `.git` directory but have independent working directories
- Enables simultaneous development without merge conflicts at the branch level

### 2. Phase Independence
- Phases touching shared files must be independently buildable and testable
- Each worktree must compile and pass its own test suite before integration
- Prevents cascading failures when features depend on shared infrastructure

### 3. Integration Protocols

**No Cherry-Picks:**
- Never use `git cherry-pick` to move changes between worktrees
- Cherry-picks create implicit dependencies and merge ambiguity
- Use `git merge` or `git rebase` explicitly instead

**Preserve Integration Seams:**
- When features touch shared boundaries (APIs, configs, interfaces), update all affected worktrees simultaneously
- Maintain a shared "integration checklist" that must be completed before any worktree is merged
- Document all changed interfaces in a central location

**Sequential Execution for Shared Files:**
- When two or more worktrees modify the same files, execute phases sequentially rather than in parallel
- The first worktree to complete its phase must be merged/rebased before the second begins work on overlapping areas
- Use `git worktree add` with `--lock` to temporarily prevent work on a branch during integration

### 4. Reconciliation via Deliberate Rebase

When it's time to integrate multiple worktrees:

1. **Identify the integration order** based on dependency graph (which features touch which shared files)
2. **Rebase each worktree** onto the latest main branch sequentially
3. **Resolve conflicts** at the worktree level, not through cherry-picks
4. **Run integration tests** across all merged features
5. **Only then** clean up worktrees

### 5. Worktree Lifecycle

```
Feature start:
  git worktree add ../worktrees/feature-X feature-X-branch

Feature development:
  Work in isolation within the worktree

Integration:
  1. Rebase worktree onto main
  2. Run full test suite
  3. Merge or rebase into main
  4. git worktree remove ../worktrees/feature-X

Cleanup:
  Remove worktree once merged: git worktree remove path
```

## When to Use This Pattern

- Multiple features modifying the same source files
- Complex dependency chains between features
- High-stakes integrations where collateral damage must be minimized
- Teams working across time zones needing coordinated but independent progress

## When NOT to Use This Pattern

- Simple features with no shared dependencies
- Single-feature development workflows
- When the overhead of managing worktrees exceeds the benefit

## Anti-Patterns to Avoid

- ✗ Leaving worktrees unlocked while another team member works on the same area
- ✗ Using `git stash` across worktrees as a primary sync mechanism
- ✗ Merging worktrees in non-deterministic order
- ✗ Skipping the integration checklist for "small" changes

## Examples

### Three-Feature Parallel Development

```bash
# Set up worktrees
git worktree add ../worktrees/auth-api feature/auth
git worktree add ../worktrees/payment-api feature/payment
git worktree add ../worktrees/notification-worker feature/notifications

# Each team works independently...

# Integration sequence (based on dependency graph):
# 1. Rebase and merge notifications (no deps)
git checkout main
git pull
git worktree pull ../worktrees/notification-worker
git rebase main
# Resolve conflicts...
git merge main
git worktree remove ../worktrees/notification-worker

# 2. Rebase and merge auth API (depends on notifications interface)
git rebase main
# Resolve conflicts...
git merge main
git worktree remove ../worktrees/auth-api

# 3. Rebase and merge payment API (depends on both above)
git rebase main
# Resolve conflicts...
git merge main
git worktree remove ../worktrees/payment-api
```

## Related Patterns

- [Changes Tab](changes-tab.md) - For tracking per-session file modifications
- [File-Edit Snapshot & Undo Mechanism](file-edit-snapshot.md) - For reverting individual edits