---
type: Gotcha
title: 'Auto-Permission — Harmful Segment Masked by an Earlier Ask Segment'
description: 'IsHarmfulRequest gated the auto-judge on Request.Command, which Decide fills with only the first asking segment of a compound bash line; a benign asking segment ahead of git reset --hard / git checkout -- let the harmful segment reach the judge, which approved it. Fixed 2026-09-22: the gate judges the whole line from req.Args and also runs on the Deny branch.'
tags:
  - security
  - permissions
  - auto-permission
  - gotcha
timestamp: 2026-09-22T12:00:00Z
resource: internal/agent/permissions.go
---

# Auto-Permission — Harmful Segment Masked by an Earlier Ask Segment

## Symptom

With auto-permission on, a compound bash line such as

```
curl -s https://example.com/x && git reset --hard HEAD~1
cd /some/other/dir && git checkout -- pnpm-lock.yaml
```

was handed to the LLM/Jev judge instead of the human, and the judge could
approve it (Jev judged `git checkout -- file` **allow 0.99** in the
2026-09-22 Laya evaluation). `git reset`, `git checkout`, `git stash`, … are
in `harmfulBashPrefixes` and are documented as "never auto-allowed".

## Cause

`Decide()` walks a compound line segment by segment and returns `PermissionAsk`
from the **first** segment that needs a human, with `Request.Command` set to
that segment only. `IsHarmfulRequest` (`internal/agent/permissions.go`) judged
`req.Command`, so when the asking segment was benign (`curl` to a public host,
`cd` outside the workdir, an out-of-root read) the harmful later segment was
never seen by the gate at `handleToolCall` (`internal/agent/agent.go`,
`tier=auto_fallback_harmful`). The Deny → auto-judge branch had no harmful gate
at all.

A bare `git checkout -- file` was always safe: its own segment is the asking
one, so `req.Command` was the harmful segment. Users with a Claude
`Bash(git checkout *)` deny rule in `~/.claude/settings.json` were also
protected by the hard deny.

## Fix

- `IsHarmfulRequest` now extracts the full command from `req.Args`
  (`bashCommand`) and falls back to `req.Command` only when args carry none.
  Every caller (auto-judge gate, TUI/server always-allow refusals) therefore
  sees all segments.
- The Deny → auto-judge branch in `handleToolCall` runs the same gate before
  consulting the model; a harmful denied line stays denied.

## Tests

- `TestIsHarmfulRequestUsesFullCommandFromArgs` (`permissions_test.go`)
- `TestHandleToolCallAutoPermissionHarmfulSegmentNotMaskedByEarlierAsk`
  (`agent_test.go`, ask-path and deny-path with a judge that says ALLOW).
  Both isolate `HOME` so the developer's own `~/.claude` deny rules cannot
  mask the gate under test.
