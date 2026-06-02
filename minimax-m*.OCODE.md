# Model-Specific Instructions for minimax-m* models

This file uses a **wildcard stem** (`minimax-m*`) and applies to every model
in the `minimax-m*` family — `minimax-m2`, `minimax-m2.1`, `minimax-m2.5`,
`minimax-m2.7`, `minimax-m3`, and any future `minimax-m*` release. The loader
treats a trailing `*` as a prefix wildcard; everything else in the stem is
literal. See `internal/agent/context.go::LoadModelContext` for the matching
rules and precedence (project root > .opencode/ > global config; exact match
beats wildcard in the same dir).

## Role
You are a `minimax-m*` model (the MiniMax M-series) used by ocode. The M2.x
family is routed through the Anthropic-format `/v1/messages` endpoint
(see `internal/agent/client.go::usesAnthropicMessagesAPI`), so it has stricter
tool-call semantics than the OpenAI-compatible providers: assistant messages
with `tool_calls` must be paired with matching `tool` result messages, and
`tool_use_id` / `tool_call_id` must round-trip exactly. Reasoning models
should output `reasoning_content` separately from visible text.

## Coding Style
- Follow the repository's existing conventions (Go 1.23, modular `internal/` packages, `gofmt` / standard formatting).
- Prefer small, focused changes. Avoid drive-by formatting, refactors, or renames unrelated to the task.
- Be explicit about the diff you intend to make before running multi-step shell commands.
- M2.x sampling defaults: `temperature = 1.0`, `top_p = 0.95`, `top_k = 40` (see `internal/agent/sampling_params_test.go`). Do not silently override these without a reason.

## Git & File Mutation Policy (default — opt-in only)

**Do not** use any of the following as a default coping strategy when something goes wrong or a change is hard to apply:

- `git stash` / `git stash push` / `git stash pop` / `git stash apply` / `git stash drop`
- `git checkout -- <file>`, `git restore <file>`, `git reset --hard`, `git reset <file>` to discard local edits
- `git revert <commit>` or `git revert <range>` to "undo" changes made during the current task
- `git clean -fd` / `git clean -fx` to wipe untracked files
- Rewriting or dropping the user's working-tree state in any other way (e.g. `git reset --hard HEAD~N` to "start over")

These operations destroy state the user may not be able to recover. Treat the user's working tree as sacred.

### What to do instead (by default)
- If a change conflicts, fails, or the approach is wrong: **stop, explain the conflict, and ask the user how to proceed.** Do not silently rewind.
- If an edit is misapplied: prefer re-applying the edit forward (write the correct content) over reverting it via git.
- If the working tree is dirty in a way that blocks the task: surface the dirty paths and ask before touching them. Do not stash on the user's behalf.
- If you need context about prior work: read it (`git log`, `git show`, `git diff`) — do not unwind it.

### When these operations ARE allowed
Only when the user **explicitly and unambiguously** asks, in the current turn, for one of the following:
- "stash", "save my changes to stash", "put this aside"
- "revert", "undo my last change", "discard my edits to X", "restore X", "reset", "throw away the working tree", "clean untracked files"
- Names a specific commit / range / file to revert / reset / restore

Verbal hedges like "maybe we should revert" or "could you stash this?" count as explicit only if they are a clear instruction, not a question to discuss. When in doubt, ask.

### How to phrase the safeguard in your output
When you are about to run one of the destructive commands above (because the user asked), say so plainly first, e.g.:
> "Heads up: this will discard your uncommitted changes to `<file>`. Confirming you want me to proceed."

Never run a destructive git command and then describe it as "I cleaned things up" — name what was destroyed.

## Constraints
- Do not use `git stash` or any git-based file revert / restore / reset / clean operation as a default. See the policy above.
- Do not invent file paths, APIs, or test names. Read the code first.
- Do not spawn long-running background processes without telling the user.
- Do not write to `os.Stdout` / `os.Stderr` directly from any code path the TUI may invoke; use `agent.emitDebug` / `agent.DebugAppendf` (or `log.Printf` outside the `agent` package, which the TUI routes to its debug panel).
- Keep status / activity rows single-line and clamped with `.Width(w).MaxHeight(1)` so they cannot wrap and push the bottom chrome past the terminal height.
- Tool calls: when you emit an `assistant` message with `tool_calls`, the next `tool` message must carry the matching `tool_call_id` exactly. Mismatched ids cause Anthropic-format providers (the route used for `minimax-m*`) to reject the turn — see `internal/agent/client.go` and the strict-mode notes around it.

## Domain Knowledge
- Repo: `ocode` — a Go 1.23 terminal coding agent built on Charm TUI (Bubble Tea / Lipgloss). See `AGENTS.md` and `CLAUDE.md` for repo-wide rules.
- LLM providers: OpenAI, Anthropic, Google, Z.AI, Alibaba, plus `opencode-go` for DeepSeek V4 routing. The `minimax-m*` family is reached as `minimax/minimax-m*` in this codebase (see `internal/auth/providers.go`, `internal/agent/sampling_params_test.go`). The bare model id (the value returned by `client.GetModel()`) is what the `.OCODE.md` stem must match — so this file's stem is `minimax-m*`, not `minimax/minimax-m*`.
- Subprocesses must capture stdout/stderr (`cmd.Stdout = &buf`); never inherit the terminal.
- This file uses a wildcard stem so it covers the whole `minimax-m*` family. To override the policy for one specific model in the family, add an exact-match file (e.g. `minimax-m3.OCODE.md`) at the same priority; the exact match wins in the same directory.
