---
type: Concept
title: Claude Code as a main LLM model and agent backend
description: How ocode runs the claude CLI as its main model (stream-json) instead of an HTTP provider, retaining session ids, mapping ocode permission modes to the CLI, routing its tool approvals and AskUserQuestion prompts through an in-process MCP approval bridge, and how that compares to embedding a PTY (tmux/rmux).
tags:
  - claude-code
  - provider
  - streaming
  - pty
  - permissions
  - approval-bridge
timestamp: 2026-11-27T14:25:30Z
---

# Claude Code as a main LLM model and agent backend

## Two integration shapes

### A. Structured stream-json (implemented)

Run `claude -p --verbose --output-format stream-json --resume <sid>` per turn.
Parse JSONL events: `system/init` (capture session_id), `assistant` (text blocks →
OnDelta), `result` (final text + usage). Only the latest user message is forwarded;
Claude Code keeps its own history under the captured session id.

Pros: typed deltas, token usage, clean cancellation via exec.CommandContext,
no PTY/websocket terminal plumbing, works in TUI and web identically.
Cons: Claude Code runs its own tool loop, so ocode executes none of the tool calls.
In `normal` mode ocode still decides each tool call and each AskUserQuestion, through
the approval bridge (see "Permission mode" and "Approval bridge" below). `locked` and
`sandbox` refuse; a nil permission callback also refuses. In `yolo` the CLI runs
unprompted and `/ban` does not apply. Session id is persisted in session metadata under
`agent.ClaudeCodeSessionMetadataKey` = `claude_code_session_id`, via
`internal/session/claude_code_bridge.go` and the TUI/server save paths. The
field is unexported `claudeSessionID`, guarded by `sessionMu`; it is restored on
the next turn; `/new` and `/reset-id` clear it; a missing id with prior conversation
is refused rather than silently dropping history. `--append-system-prompt` is not used. ocode system messages are NOT forwarded;
Claude Code loads CLAUDE.md from `cmd.Dir`; ocode tool definitions are not sent.
Missing `claude` CLI and auth failures produce explicit errors. Remote (SSH/WSL)
projects are refused because WorkDir is a remote path. Process handling is
platform-split (`claude_code_process_unix.go` / `_windows.go`); the wait
goroutine uses crashguard. Tool events and token usage are reported (below).
Writes are tracked as non-undoable external changes (below).

### B. Embedded PTY (tmux/rmux-style)

Spawn `claude` in a pseudo-terminal and stream raw bytes over the existing
terminal websocket. ocode already does this for `!shell` via creack/pty +
gorilla/websocket (`internal/server/terminal_session.go`).

Pros: full interactive Claude Code (its own permission dialogs, slash commands,
skills) with zero semantic parsing.
Cons: output is terminal bytes, not chat messages; cannot be rendered as a
transcript or compacted; session state lives only in the PTY.

## Go + web stack comparison

- **creack/pty (already used)**: cross-platform enough for macOS/Linux; Windows
  needs ConPTY. Integrates directly with the existing terminal websocket and
  replay buffer. Best fit for ocode's web stack because the frontend already
  renders a terminal emulator (ghostty-web).
- **tmux**: external binary dependency; driving it programmatically from Go means
  shelling out to `tmux` commands or using a Go client (libtmux-style). Adds a
  runtime dependency and a second source of truth for session state. Heavier
  than creack/pty for our use case.
- **rmux**: Rust implementation; no mature Go binding. Would require a sidecar
  process or cgo/FFI, increasing build complexity. Not recommended.

Recommendation: keep creack/pty for terminal-style embedding (shape B) and use
stream-json (shape A) for chat-transcript integration. The two can coexist:
shape A for the main model, shape B for an optional "Claude Code terminal" tab.

## Session id retention

`GenericClient.claudeSessionID` (unexported, guarded by `sessionMu`) stores the
Claude session id captured from the `system/init` event and reuses it on later
turns via `--resume`. It is persisted in session metadata under
`agent.ClaudeCodeSessionMetadataKey` (`claude_code_session_id`) through
`internal/session/claude_code_bridge.go` and the TUI/server save paths, then
restored on the next turn. `/new` and `/reset-id` clear it. A missing id with
prior conversation is refused rather than silently dropping history. Only human
prompts count as prior conversation (`hasPriorClaudeConversation` checks
`isHumanPromptMessage`); synthetic user-role `[ocode:…]` tail blocks do not, so
a new chat's first turn is not mistaken for a resumed one.

## Tool and usage reporting

Claude Code owns its internal tool loop, so its `tool_use` / `tool_result`
events are **display-only**. They are joined into `Message.Notice` (capped at
40 lines, 400 characters each) and never become `Message.ToolCalls`; creating
real calls would make ocode execute the tools a second time and trip the
orphan/cancellation rules. Input summaries pass through the session's
secrets-only redaction registry before they are persisted. The web and TUI
render that notice above the answer. Lines recording an auto-continue judge
answer to an AskUserQuestion (see "Approval bridge") are added to the same
notice; they are display-only too.

The `result` event's `usage` is parsed with the same Anthropic-shape parser as
the API providers (`input_tokens`, `output_tokens`, `cache_read_input_tokens`,
`cache_creation_input_tokens`), and the model is `claude-code/<alias>`. The
normal usage ledger and context gauge therefore see a provider-reported
"actual" token reading, not an estimate. `total_cost_usd` is deliberately NOT
booked as `Message.Spend`: it is an API-price estimate, not what a Pro/Max
subscription is billed.

## Write tracking

Each turn is wrapped in the same pre/post `StatBashRecorder` used for bash.
Detected paths enter the Changes tab as **non-undoable** external writes, and
`snapshot.Store.RecordExternalWrite` advances `LastWriteSeq` / `ChangedSince`
for the advisor write watermark. No snapshot is created because there is no
pre-image, so `undo_file_change` refuses these rows; a later undo of an older
file-tool snapshot is also refused when a Claude Code write came after it.
**Limit:** the stat diff scans the whole `WorkDir` and cannot distinguish
Claude Code's writes from a concurrent user, formatter or other-session write;
those also advance the watermark and block undo. Files outside `WorkDir` are
not tracked.

## Model picker

When the `claude` CLI resolves, `GET /api/models` appends the
`claude-code/<alias>` rows and both pickers show a `Claude Code (external
harness)` section: the TUI `/models` picker and the web main-model picker. The
web advisor picker keeps its own synthetic read-only section and filters the
duplicate rows out. Other pickers (permission/small/recap/etc.) never offer
the external harness.

## Skills and slash commands

Claude Code discovers skills from `~/.claude/skills` and `.claude/skills`, the
same roots ocode scans, so installed skills are shared. Project `CLAUDE.md` is
read by Claude Code automatically because the CLI runs with `cmd.Dir = workDir`.
ocode's TUI slash commands are not forwarded; Claude Code's own slash commands
work in terminal mode (shape B).

## Permission mode

`claudeCodePermissionMode` (`internal/agent/claude_code_client.go`) maps ocode's
mode to CLI flags. `chatClaudeCode` refuses `locked` and `sandbox`, and refuses
when the permission-mode callback is nil.

| ocode mode | CLI flags | Who decides a tool call |
|---|---|---|
| `yolo` | `--permission-mode bypassPermissions --permission-prompts none` | Nobody: no bridge is started, Claude Code runs its tools itself. `/ban` and ocode's guards do not apply. |
| `normal` (auto-permission on or off) | `--permission-mode manual --permission-prompts host --mcp-config <0600 temp file> --permission-prompt-tool mcp__ocode__approve` | ocode, through the approval bridge. |
| `locked`, `sandbox` | refused | Nothing runs. |

The auto-permission setting does not change the CLI flags. In `normal`, ocode's
own pipeline (static rules, `/ban`, the auto-permission judge, then the human)
decides each mapped tool call, and Claude's own `auto` classifier is never used.
`acceptEdits` is not used either.

In `yolo` (`bypassPermissions`) Claude Code also skips its own protections: it
does not prompt for writes to protected paths such as `.git` and `.claude`
(settings and tool config). Use `yolo` only in trusted projects.

## Approval bridge

`startClaudeCodeBridge` (`internal/agent/claude_code_bridge.go`) starts one
in-process MCP streamable-HTTP server per `normal`-mode run, and `chatClaudeCode`
closes it when the run ends.

- It listens on `127.0.0.1:0` and serves `/mcp` with one tool, `approve`. The
  CLI calls it as `mcp__ocode__approve`.
- Every request must carry `Authorization: Bearer <token>`. The token is 32 random
  bytes, compared in constant time.
- The MCP config is written to an `os.CreateTemp` file with mode `0600` and
  removed when the run ends.
- Each approval runs on its request context. Closing the bridge shuts the server
  down, which cancels any approval still waiting on a human, so a stopped run
  leaves no dialog behind.
- The config sets `"timeout": 1200000` (`claudeCodeBridgeToolTimeoutMS`, 20
  minutes). Without it the CLI aborts an HTTP tool call after 60 seconds, which is
  shorter than a human answer. Verified 2026-11-27 with CLI 2.1.332: 660-second
  waits succeeded with the key set.
- `chatClaudeCode` returns an error and does not fall back when no approval
  callback is bound (`ClaudeCodeApproveFn`). `bindClaudeCodeClientHooks` binds it
  on construction and on every client swap.

`ClaudeCodeApproveFn` sends `AskUserQuestion` to `answerClaudeCodeQuestion` and
every other tool to `decideClaudeCodeToolUse`.

### Tool decisions

`decideClaudeCodeToolUse` (`internal/agent/claude_code_approve.go`) maps the CLI
tool to an ocode tool (`mapClaudeCodeTool`):

| Claude Code | ocode |
|---|---|
| `Bash` | `bash` |
| `Write` | `write` |
| `Edit` | `edit` |
| `MultiEdit` | `multiedit` |
| `NotebookEdit` | `write` (on the notebook path) |
| `WebFetch` | `webfetch` |

- A mapped call runs `runToolCallWithContext` with a decide-only flag.
  `executeToolCallWithContext` checks the flag after `isToolAllowed`, records that
  the pipeline would allow the call, and returns without running it. Claude Code
  runs the tool itself, so ocode records no `ToolCall`.
- The blocking human asker is the attributed `subAgentPermAsker`, labelled
  `claude-code`. With no such asker, the call is denied with "approval required,
  but no approval surface is available in this session".
- An unmapped tool (`WebSearch`, `mcp__*`, `Agent`, ...) skips the rules, the
  judge and the pipeline. `askClaudeCodeUnmapped` asks the human directly under
  the name `claude-code:<tool>`. An allow is not persisted.
- A read-only `Bash` or `Read` call never reaches the bridge: the CLI allows
  those itself in `manual` mode, so ocode's rules and `/ban` do not see them.

### Questions (AskUserQuestion)

`answerClaudeCodeQuestion` (`internal/agent/claude_code_approve.go`) converts the
CLI's `questions` into `tool.QuestionPrompt`s and then:

1. If auto-continue or an evaluable goal is on, the auto-continue question judge
   is offered the prompts first (`judgeClaudeCodeQuestion`). It answers only when
   the transcript already determines the answer. A run takes at most
   `AutoContinueChainCap` judge answers, and a deferral does not count against
   that cap. Each judge answer adds a line to the message's display-only `Notice`:
   "AskUserQuestion answered by the auto-continue judge: ...".
2. Otherwise, or when the judge defers, the blocking `Agent.SetQuestionAsker`
   callback shows the host's question dialog.

The reply is `allow` with `updatedInput` set to the original input plus an
`answers` object keyed by question text (`claudeCodeQuestionUpdatedInput`). Each
value is the typed text if one was given, otherwise the chosen label. Multi-select
labels are joined with `", "`. A dismissed question is denied with "the user
dismissed the question without answering".

Hosts:

- **Server:** the parked-question registry `childQuestionAsks`
  (`internal/server/child_question_asks.go`), one per session. Parked questions
  are listed in `pending_asks` (`handler_session_state.go`) and emitted as
  `question` SSE frames. `HandleAnswerQuestion` and `HandleDismissQuestion`
  (`handler_questions.go`) resolve them through `parkedQuestionFor` without taking
  `as.mu`. A question nobody answers times out after 10 minutes
  (`childPermAskTimeout`) and is dismissed.
- **TUI:** `newParkedQuestionAsker` (`internal/tui/question_parked.go`) is
  installed on the model and reuses the normal question dialog.

### Errors and authentication

The CLI reports most failures, such as `Not logged in`, as an `is_error` result
on stdout with an empty stderr. `chatClaudeCode` surfaces that result text.
Text that looks like an auth failure (`claudeCodeAuthError`) becomes an error
that says `claude-code: not authenticated`, tells the user to run `claude` and sign
in again, and includes the CLI's text.

The CLI runs on the user's own subscription login. `cleanEnvForTerminal`
(`internal/agent/advisor_tool.go`) drops `ANTHROPIC_API_KEY` and
`ANTHROPIC_AUTH_TOKEN` from the environment, because a key would switch billing to
the API. `chatClaudeCode` and the advisor's CLI call both use it.

### Testing

`OCODE_LIVE_CLAUDE=1 go test ./internal/agent -run TestClaudeCodeBridgeLive` runs
a live bridge test (`claude_code_bridge_live_test.go`). It skips unless that
variable is set and `claude` is on `PATH`. The package `TestMain` points `HOME` at
a temp tree where the CLI finds no login, so the test restores the real home with
`setHomeTree`.

### Known gaps

The open items are listed in `TODO.md` under "Open: Claude Code approval bridge
follow-ups (2026-11-27)": the web `SET_MESSAGES` reducer can close a parked
question dialog, Pulse and the permission badge do not count parked questions,
`sudo` elevation through the bridge, "always allow" on unmapped tools acting as
allow-once, RC-bridged sessions and the question judge, and read-only tools
bypassing ocode's rules.

## System prompt

ocode's system-role messages (CLAUDE.md briefing, env block, etc.) are NOT
forwarded, and `--append-system-prompt` is not used. Claude Code loads
`CLAUDE.md` from `cmd.Dir`. ocode's tool definitions are NOT sent; Claude Code
owns its own tool set and executes them internally.
