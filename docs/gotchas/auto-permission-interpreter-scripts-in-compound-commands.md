---
type: Gotcha
title: 'Auto-Permission — Interpreter Scripts in Compound Commands'
description: 'Auto-permission custom-script detection must cover interpreter and runner forms (python x.py, node x.js, bun run x.ts, uv run x.py, npx tsx x.ts); the structured interpreter path only sees the FIRST command, so compound commands otherwise reach the generic LLM path with no script content and no truncation guard.'
tags:
  - security
  - permissions
  - auto-permission
  - interpreter
  - gotcha
timestamp: 2026-08-31T05:00:00Z
---
## The Problem

Bash auto-permission has two LLM paths:

1. **Structured interpreter path** (`classifyInterpreterExecution` → `askPermissionModelInterpreter`) — reads the full script, returns a structured effect summary, denies on truncation. It classifies **only `cmds[0]`**, i.e. the interpreter must be the first command word.
2. **Generic path** (`askPermissionModel` + `buildPermissionContext`) — shows the LLM "Executed custom script" sections gathered by `detectExecutedCustomScripts`, and `verifyAutoGrant` refuses to auto-grant when any detected script is truncated.

`detectExecutedCustomScripts` originally `continue`d on any `interpreterLanguages` / `remoteRunners` binary, assuming path 1 handled them. That assumption only holds for the first command. Anything like

```
cd app && python3 migrate.py
make build && node scripts/publish.js
ls && uv run cleanup.py
true && npx tsx scripts/seed.ts
```

fell through to the generic path with **no script content in context and no truncation guard** — the LLM judged the command by its name alone.

## The Fix

`internal/agent/script_detection.go`:

- `stripScriptRunner` strips runner prefixes (`npx`, `bunx`, `uv run`, `poetry run`, `pipenv run`, `npm exec`, `pnpm exec`, `pnpm dlx`, `bun x`) and re-classifies the wrapped words.
- `interpreterScriptEntrypoint` extracts the script operand for interpreters, mirroring `classifyInterpreterExecution` rules: skips inline eval (`-c`/`-e`), `-m module`, bun/deno built-in subcommands, bare `bun run <pkg-script>`, and REPL.
- `isScriptInterpreter` = `interpreterLanguages` + `extraScriptInterpreters` (php, lua, Rscript, julia, ts-node, pwsh) for context detection only; the structured path's language list is unchanged.

Because both `buildPermissionContext` and `verifyAutoGrant` consume `detectExecutedCustomScripts`, interpreter scripts in compound commands now get shown to the LLM **and** block auto-grant when truncated.

## Rules

- Never assume the structured interpreter path covers a form; it only sees the first command. Any new "execute a local file" syntax must be added to `detectExecutedCustomScripts` too.
- Keep `interpreterScriptEntrypoint` and `classifyInterpreterExecution` in agreement — tests in `custom_script_test.go` (`TestDetectExecutedCustomScripts`, `TestVerifyAutoGrantDeniesTruncatedInterpreterScript`) pin the shared rules.
- `cd` is not tracked: relative paths resolve against the agent's cwd, not the post-`cd` directory (pre-existing behaviour, applies to shell wrappers too).
