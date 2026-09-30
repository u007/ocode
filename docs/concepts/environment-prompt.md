---
type: Concept
title: Environment Prompt
description: The env block the LLM receives at session start via internal/agent/prompt.go, including the extra Project host line for remote (SSH/WSL) projects and why there is no git branch line.
resource: CLAUDE.md
tags:
  - prompt
  - environment
  - remote
timestamp: 2026-09-30T07:38:54Z
---
# Environment Prompt

The LLM receives environment context at the start of each session via
`internal/agent/prompt.go`. The exact shape is the ` <env>...</env>` block
in that file; if you are reading the values out of the prompt at runtime,
parse the block — do not assume the example below is current. The
illustrative shape is:

```
<env>
  Working directory: /path/to/project
  Workspace root folder: /path/to/project
  Is directory a git repo: yes
  Platform: darwin
  Today's date: <resolved at session start>
</env>
```

A session bound to a **remote (SSH/WSL) project** gets one extra line —
`Project host: <[user@]host|wsl:distro> (remote project — …)` — right after
the git-repo lines (`Agent.SetProjectHost`, from `Handler.projectHostFor`).
That line was added because a locally-built agent for a remote project saw a
remote project root beside the local machine's config/session/skill/runtime
paths. Remote chat/agent traffic is now reverse-proxied to the host's
`ocode serve --remote` (see `docs/concepts/web-server-project-scoping.md`),
so the answering agent runs on the host; the line still marks the residual
case of a locally-built agent bound to a remote project (e.g. a direct
`/api/chat` call that does not go through the host prefix). Local projects
emit no such line, so their prompt is byte-identical.

There is no `Git branch` line: the git branch is not resolved or injected.
