# Plan: route Claude Code approvals and questions through ocode

Date: 2026-11-27. Status: decisions settled (section 5), spike done (Task 0). Next: Task 1.

## Flow context

**How it currently works**
1. The `claude-code/<model>` main model runs `claude -p` once per turn (`chatClaudeCode`, `internal/agent/claude_code_client.go`), with stdin `/dev/null` and `--permission-prompts none`.
2. Claude Code runs its own tools. ocode only sees them afterwards, as a display-only notice row.
3. The permission mode is mapped to a CLI mode: yolo → `bypassPermissions`, normal + auto-permission → `auto` (Claude Code's own classifier), normal → `manual`, locked/sandbox → refused.
4. Anything that would need a human is denied silently. With `--permission-prompts none`, the CLI removes `AskUserQuestion` entirely.

**Where it breaks**
- ocode's own checks never see Claude Code's actions: static rules, `/ban`, the network/content guards, the Jev permission judge, the approval dialog.
- In plain normal mode, every risky action is denied with no way for the user to approve it.
- Claude Code can never ask the user a question.

**Proposed fix**
- Keep one `claude` process alive for the whole turn.
- Give it an ocode-owned MCP endpoint and pass `--permission-prompts host --permission-prompt-tool mcp__ocode__approve`. Every approval request then calls back into ocode.
- ocode decides with the same checks as its own tools: static rules, `/ban`, guards, judge. If a human is needed, the MCP call stays blocked while ocode shows its normal dialog. The answer goes back as `allow` / `deny`.
- Questions use the same endpoint. See Task 6.

## Ground rules (from CLAUDE.md and docs, apply to every task)

- **No transcript ToolCalls:** Claude Code tool events must never become ocode ToolCalls. Step and `recoverOrphanedToolCalls` would execute them a second time. Do NOT use the `PERMISSION_ASK` / `QUESTION_PROMPT` transcript sentinels for these asks.
- **Model on sub-agent asks:** model the asks on sub-agent asks (`as.childAsks`, `internal/server/child_perm_asks.go`), not on main-agent sentinels. A parked ask is resolved WITHOUT `agentSession.mu`, because the blocked turn holds that lock for the whole wait.
- **Fail closed:** any error, timeout or Stop denies the pending request.
- **Remote projects:** they stay refused (the CLI runs locally).
- **No fallbacks:** if the endpoint cannot start, the turn fails with a clear error. It never silently drops back to `--permission-prompts none`.

## Backend choice: the installed `claude` CLI on the user's subscription

- **Login:** this path runs the user's installed, unmodified `claude` CLI. That CLI uses the Claude subscription login it stores itself (`/login` once, kept in the macOS keychain and refreshed by the CLI). ocode never handles the credentials. The API-key route stays the separate `anthropic` provider.
- **Not the Agent SDK:** it is TypeScript/Python only, and it runs the same Claude Code binary underneath. Anthropic's terms also restrict offering subscription login through SDK-built third-party tools.
- **Never pass `--bare`:** bare mode ignores the subscription login. The docs say it will become the default for `-p`, so pin the current behaviour in a test.
- **Strip API keys:** remove `ANTHROPIC_API_KEY` (and `ANTHROPIC_AUTH_TOKEN`) from the environment ocode gives the CLI. In `-p` mode the CLI uses a key when one is present, so an Anthropic key added through `/connect anthropic` would silently move billing from the subscription to the API.

## Task 0 — Spike (done 2026-11-27, CLI 2.1.332, stdio MCP server)

Scripts and logs are in the session scratchpad (`spike-prompt-tool/`, `spike-long-wait/`), not in the repo. With `--permission-mode manual --permission-prompts host --permission-prompt-tool mcp__x__approve`:

1. **What reaches the prompt tool:**
   - Reaches it: Edit, WebFetch (also from inside Claude Code's own `Agent` sub-agents), writing Bash (`echo … > r.txt`), and AskUserQuestion.
   - Never reaches it: read-only Bash (`date`) and Read, which the CLI allows itself, so ocode never sees them.
   - Request arguments: `{"tool_name", "input", "tool_use_id"}`, where `input` is the tool's own input.
   - Reply: one text block whose text is `{"behavior":"allow"}`, `{"behavior":"allow","updatedInput":…}` or `{"behavior":"deny","message":…}`.
2. **AskUserQuestion works through the prompt tool (Route A).**
   - Input is `{"questions":[{"question","header","options":[{"label","description"}],"multiSelect"}]}`.
   - Answering with `allow` + `updatedInput` (the original `questions` plus `answers: {question text: label}`) delivers the answer. Verified with the last of three options: Claude replied "Cherry".
3. **Long waits survive.**
   - A stdio prompt-tool call blocked for 120 s and for 660 s (11 min) both succeeded, with no timeout and no special settings.
   - Knobs: env `MCP_TOOL_TIMEOUT` (ms) or a per-server `timeout` key (ms) in the `--mcp-config` entry; the per-server key wins.
   - The binary shows a 60 s floor in its HTTP transport path. Only stdio was tested. **If Task 1 uses HTTP, set the per-server `timeout` above the ask timeout and test a call longer than 60 s.**
4. **`--resume` works:** the prompt tool is called again in a resumed session. The CLI respawns the MCP server each run.
5. **Without `--permission-prompt-tool`** (`--permission-prompts host` alone): AskUserQuestion is absent, and Edit/WebFetch are denied with "Claude requested permissions to … but you haven't granted it yet". That matches today's behaviour.
6. **Handshake:** the CLI sends `initialize` (protocolVersion `2025-11-25`) and a `server/discover` request; a -32601 reply is fine.

## Task 1 — Per-run loopback MCP endpoint

- Run an in-process MCP server (streamable HTTP) on `127.0.0.1` with a random port, for one `chatClaudeCode` run only. It is started before the CLI and torn down when the run ends.
- Require a per-run random bearer token. Pass the URL and token to the CLI through a temp `--mcp-config` file (mode 0600) that is removed at the end of the run.
- Expose `approve`, plus `ask_user` if Task 6 takes route B.
- Same code path for TUI and server, so both hosts behave the same.
- Files: new `internal/agent/claude_code_bridge.go` (or similar). Wire it in `chatClaudeCode`.

## Task 2 — Decision-only permission path

- Extract the decision part of `handleToolCallWithContext` (`internal/agent/agent.go`) into one shared function that returns allow / deny / needs-human without running the tool: static checks, `/ban`, self-escalation guard, network/content guards, `consultPermissionModel`, the asker.
- `handleToolCallWithContext` calls the extracted function, so there is one implementation and no copy.
- Keep the existing rules: judge the whole line from `req.Args`, peel wrappers with `effectiveCommandWords`, and keep `/ban` a hard deny that the judge never sees.

## Task 3 — Map Claude Code tools to ocode permission requests

- Bash → ocode `bash` request (command).
- Edit / Write / MultiEdit / NotebookEdit → ocode write request (path, checked against allowed roots).
- WebFetch / WebSearch → ocode network request (URL / host, through the network guard).
- Read / Glob / Grep / LS → allowed when the path is inside the allowed roots. Otherwise, the same rule as ocode's read tools.
- Any unknown tool and any `mcp__*` tool → human ask. Never sent to the judge.
- The exact input shape comes from Task 0.1.

## Task 4 — Server plumbing

- Park a Claude Code approval request in the `childAsks` registry, or a sibling registry with the same contract, so that:
  - `livePendingAsks` / `pending_asks` show it,
  - the web permission dialog renders it unchanged,
  - `HandleResolvePermission` delivers the answer through the registry without taking `as.mu`.
- Keep `turn_heartbeat` running while parked.
- Stop / cancel: kill the `claude` process group, deny and clear every parked ask, then return through the normal cancelled path.
- The goal loop and auto-continue must treat a parked ask as an outstanding ask. Check `sessionHasOutstandingAsk`.
- Timeout: reuse `childPermAskTimeout` (10 min). Task 0.3 showed the CLI waits at least 11 min on stdio. Set the MCP `timeout` above it.
- "Always allow" choices persist through the same `persistAlwaysAllow` path, with the same harmful-decision guard.

## Task 5 — TUI plumbing

- Use the TUI's synchronous asker (`OnPermissionAsk`) for approvals, so the normal TUI permission dialog shows while the MCP call is blocked.
- Esc / Stop kills the process and denies the pending ask.

## Task 6 — Questions (AskUserQuestion)

Task 0.2 confirmed Route A. Route B is kept only as a record of the alternative.

**Route A (preferred, if the spike passes).** Keep Claude Code's own `AskUserQuestion`. It reaches the prompt tool, and ocode:
- converts `questions` (1-4 questions, 2-4 options, `multiSelect`) into ocode's question dialog,
- parks it in a new parked-question registry (web) or uses a synchronous question asker (TUI),
- answers with `allow` + `updatedInput {questions, answers}`, where keys are question text and values are the chosen labels.

**Route B (if the question does not reach the prompt tool).**
- Pass `--disallowedTools AskUserQuestion` and expose `mcp__ocode__ask_user` on the same endpoint, with the same input shape.
- The MCP call blocks on the same parked-question registry and returns the answer as the tool result.

For both routes:
- **New server registry:** the server has no blocking question path today (questions are sentinel-only). Add a parked-question registry that mirrors `childPermAsks`: buffered response channel, `take` / `markResolved`, no `as.mu`, timeout. `HandleAnswerQuestion` and `question_dismiss` resolve it.
- **Web dialog:** the web question dialog renders it from `pending_asks` with no new UI. A dismiss returns a "user declined to answer" result to Claude.
- **Auto-continue question judge:** when auto-continue is enabled, dispatch it on a parked Claude Code question the same way as on a pending `question` ask (`autoContinueAnswerQuestion` on the server, `maybeDispatchQuestionJudge` in the TUI). A deferral leaves the question parked for the human.

## Task 7 — Tests and docs

- Unit tests:
  - tool mapping table,
  - decision path parity: the same request through `handleToolCallWithContext` and through the extracted function gives the same verdict,
  - bridge token rejection,
  - a parked ask resolved without `as.mu`,
  - Stop denies a parked ask,
  - question answer round-trip.
- Fake-CLI tests reuse the existing `fakeClaude` helper and extend it to call the MCP endpoint.
- One gated live test (real `claude`, skipped without it) for an approve round-trip.
- Docs through `okf:context-writer`:
  - `docs/concepts/claude-code-main-model.md`: permission section, no-approval-surface statement, question support,
  - plus a CLAUDE.md invariant line: Claude Code asks are parked asks, never sentinels.

## 5 — Decisions (settled by the user, 2026-11-27)

1. **Normal + auto-permission → ocode's judge.** CLI mode becomes `manual` + prompt tool. Claude Code's `auto` classifier is no longer used. One judge and one rule set serve both backends.
2. **Yolo stays `bypassPermissions`.** It is not routed through the prompt tool. `/ban` and ocode's guards do not apply to Claude Code in yolo, as today. Task 0.5 is no longer needed.
3. **Auto-continue question judge answers Claude Code questions when auto-continue is enabled.** It follows the same rules as for ocode's own `question` asks: Jev only, all-or-nothing, defer when the transcript does not determine the answer, applied through the same path as a human answer. It never runs when auto-continue is off.

Resulting mapping:

| ocode mode | CLI `--permission-mode` | Prompt tool |
|---|---|---|
| yolo | `bypassPermissions` | not passed |
| normal + auto-permission | `manual` | ocode: rules, guards, judge, then human |
| normal | `manual` | ocode: rules, guards, then human |
| locked / sandbox | refused | — |

## Out of scope

- Remote (SSH/WSL) projects.
- Word-by-word streaming (`--include-partial-messages`). Separate change.
- A multi-turn single `claude` process across user turns. Per-turn `--resume` stays.
