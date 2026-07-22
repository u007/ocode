---
type: Gotcha
title: Skill Tool Test Fixture Gap — expectedBuiltinTools Missing load_skill
description: expectedBuiltinTools in tool_test.go only lists "skill" but InitBuiltinTools also registers "load_skill" as a second alias, causing a stale test failure.
tags:
  - test
  - tool-registration
  - skill
  - expectedBuiltinTools
  - fixture-gap
timestamp: 2026-07-22T08:00:57Z
---
# Skill Tool Test Fixture Gap — `expectedBuiltinTools` Missing `load_skill`

## Problem

The test fixture `expectedBuiltinTools` in `internal/tool/tool_test.go:21` lists `"skill"` but is missing `"load_skill"`, which `InitBuiltinTools` registers as a second tool alias.

## Root Cause

- `SkillTool` (defined in `internal/tool/misc.go:17`) has `Name() string { return "skill" }` — **present** in the expected list.
- `SkillAliasTool` (defined in `internal/tool/misc.go:66`) embeds `SkillTool` and overrides `Name()` to return `"load_skill"` — **absent** from the expected list.
- Both are registered unconditionally in `InitBuiltinTools` (`tool.go:121-122`).
- The test at `tool_test.go:83-98` iterates every tool returned by `InitBuiltinTools` and flags any name not in `expectedBuiltinTools` as unexpected (line 96: `t.Errorf("InitBuiltinTools returned unexpected tool %q", g)`).

## Symptom

Running `TestBuiltinToolsUnconditionalNames` produces:

```
InitBuiltinTools returned unexpected tool "load_skill"
```

## Why This Exists

`SkillAliasTool` was added later (as a pragmatic alias so models that guess the name `"load_skill"` instead of `"skill"` don't trigger the unregistered-tool hallucination guard). The `expectedBuiltinTools` fixture was not updated at the same time.

## Fix

Add `"load_skill"` to `expectedBuiltinTools` in `internal/tool/tool_test.go`. The order follows `InitBuiltinTools`: insert it immediately after `"skill"`.

## References

- `internal/tool/tool_test.go` — `expectedBuiltinTools` list (line 21) and the two test loops (lines 70, 89)
- `internal/tool/tool.go` — `InitBuiltinTools` registration order (lines 121-122)
- `internal/tool/misc.go` — `SkillTool.Name()` (line 19) and `SkillAliasTool.Name()` (line 68)
