---
type: Concept
title: Claude Code as a main LLM model and agent backend
description: How ocode can use the claude CLI as its main model (stream-json) instead of an HTTP provider, retaining session ids and sharing skills, and how that compares to embedding a PTY (tmux/rmux).
tags:
  - claude-code
  - provider
  - streaming
  - pty
timestamp: 2026-11-25T00:00:00Z
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
Cons: Claude Code runs its own tool loop internally; ocode's tools/permissions do
not apply. Permission mode: `normal` + auto-permission off → Claude `manual`
with `--permission-prompts none`; `normal` + auto-permission on → `acceptEdits`;
`yolo` → `acceptEdits`; `locked` and `sandbox` refuse; a nil permission callback
also refuses. The CLI has no `default` mode; `--permission-prompts none` denies
anything that would otherwise block on a prompt inside `-p`. `/ban` deny rules are
a hard Deny, never re-considered by the judge. Session id is persisted in session metadata under
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
prior conversation is refused rather than silently dropping history.

## Tool and usage reporting

Claude Code owns its internal tool loop, so its `tool_use` / `tool_result`
events are **display-only**. They are joined into `Message.Notice` (capped at
40 lines, 400 characters each) and never become `Message.ToolCalls`; creating
real calls would make ocode execute the tools a second time and trip the
orphan/cancellation rules. Input summaries pass through the session's
secrets-only redaction registry before they are persisted. The web and TUI
render that notice above the answer.

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

`chatClaudeCode` refuses to run under `locked` or `sandbox` permission modes and
refuses when the permission callback is nil. `normal` plus auto-permission off
maps to Claude `manual` with `--permission-prompts none`; `normal` plus
auto-permission on maps to `acceptEdits`; `yolo` maps to `acceptEdits`. The CLI
has no `default` mode, and `--permission-prompts none` denies anything that would
otherwise block on a prompt inside `-p`. ocode's `/ban` deny rules are NOT
forwarded to Claude Code; treat claude-code sessions as at least `normal` mode.

## System prompt

ocode's system-role messages (CLAUDE.md briefing, env block, etc.) are NOT
forwarded, and `--append-system-prompt` is not used. Claude Code loads
`CLAUDE.md` from `cmd.Dir`. ocode's tool definitions are NOT sent; Claude Code
owns its own tool set and executes them internally.
