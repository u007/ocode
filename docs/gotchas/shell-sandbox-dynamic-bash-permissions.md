---
type: Gotcha
title: BashTool Dynamic Permission Manager Resolution
description: 'Gotcha: BashTool must resolve permission manager dynamically to ensure child agent enforcement follows updated policies, not stale parent pointers.'
tags:
  - gotcha
  - sandbox
  - permissions
  - shell
timestamp: 2026-08-31T17:26:30Z
---
# BashTool Must Resolve Permission Manager Dynamically

## The Problem

When BashTool (or any tool that wraps shell execution) captures a reference to the permission manager at construction time, it captures a *snapshot* of the policy. In a subagent hierarchy, the child's permission manager is bound *after* tools are constructed — meaning the tool holds a stale pointer to the parent's (or default) PM, and the child's updated policies are never enforced.

## Why This Matters

- Permission modes change mid-session (user switches from `normal` to `auto` to `yolo`).
- Subagents are constructed with a fresh PM that may have different allowlists, bash-prefix rules, or path scoping.
- If BashTool holds the old PM, the child runs with wrong permissions — either too restrictive (user frustration) or too permissive (security).

## Required Fix

BashTool must either:

1. **Resolve the PM dynamically** on each execution — query the current agent/session's PM at call time, not capture time. This is the preferred pattern.
2. **Be constructed after final PM binding** — ensure `tool.NewBashTool(pm)` is called *after* the subagent's PM is fully initialized, never before.

Option 1 is safer because it survives PM mutations (mode switches, policy updates) without requiring tool reconstruction.

## Gotcha: Closure Capture in Go

In Go, if BashTool stores `pm` as a field set at construction, it's a value copy of the *pointer* — which is fine if the PM behind the pointer is mutated in place. But if the agent *replaces* the PM pointer entirely (new PM object), the tool still points to the old one. Verify whether the PM is replaced or mutated before choosing between option 1 and option 2.