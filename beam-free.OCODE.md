# Model-Specific Instructions for beam-free

This file applies to the `beam-free` model (served as `opencode-go/beam-free`).
The bare model id (the value returned by `client.GetModel()`) is `beam-free`,
so this file's stem is `beam-free` (exact match, no wildcard).

## Role
You are the `beam-free` model used by ocode. Follow the repository's existing
conventions and prefer small, focused changes.

## Coding Style
- Follow the repository's existing conventions (Go 1.23, modular `internal/` packages, `gofmt` / standard formatting).
- Prefer small, focused changes. Avoid drive-by formatting, refactors, or renames unrelated to the task.
- Be explicit about the diff you intend to make before running multi-step shell commands.

## Git & File Mutation Policy (default — opt-in only)

**Do not** use any of the following as a default coping strategy when something goes wrong or a change is hard to apply:

- `git stash` / `git stash push` / `git stash pop` / `git stash apply` / `git stash drop`
- `git checkout -- <file>`, `git restore <file>`, `git reset --hard`, `git reset <file>` to discard local edits
- `git revert <commit>` or `git revert <range>` to "undo" changes made during the current task
- `git clean -fd` / `git clean -fx` to wipe untracked files
- Rewriting or dropping the user's working-tree state in any other way

These operations destroy state the user may not be able to recover. Treat the user's working tree as sacred.

### What to do instead (by default)
- If a change conflicts, fails, or the approach is wrong: **stop, explain the conflict, and ask the user how to proceed.** Do not silently rewind.
- If an edit is misapplied: prefer re-applying the edit forward (write the correct content) over reverting it via git.
- If the working tree is dirty in a way that blocks the task: surface the dirty paths and ask before touching them. Do not stash on the user's behalf.
- If you need context about prior work: read it (`git log`, `git show`, `git diff`) — do not unwind it.

### When these operations ARE allowed
Only when the user **explicitly and unambiguously** asks, in the current turn, for one of them.

## Constraints
- Do not use `git stash` or any git-based file revert / restore / reset / clean operation as a default.
- Do not invent file paths, APIs, or test names. Read the code first.
- Do not spawn long-running background processes without telling the user.
- Do not write to `os.Stdout` / `os.Stderr` directly from any code path the TUI may invoke; use `agent.emitDebug` / `agent.DebugAppendf` (or `log.Printf` outside the `agent` package).
- Keep status / activity rows single-line and clamped with `.Width(w).MaxHeight(1)`.

## Delegation and Planning (hard triggers)

Soft "you should consider delegating" guidance is NOT reliably followed by this
model family. Measured delegation rate on `beam-free` is far below
`deepseek-v4.1-flash` and `space-bunny-free`, and the model is observed reading
the context-agent rule in the system prompt and then rationalising around it.
So use the hard triggers below instead of judgement calls. They are triggers,
not a mandate to delegate everything.

- **Plan first on multi-step work.** Before your first edit on any task that will
  touch 2+ files or need 3+ tool calls, call `todowrite` to record the plan.
  Read it back with `todoread` before mutating it, and use `todo_update` for
  targeted status changes (a full `todowrite` replaces the list and is rejected
  if it drops open items).
- **Route codebase questions through the `context` sub-agent.** For any
  "where / how / what / why" question about THIS repository, dispatch
  `task` with `agent="context"` BEFORE you grep, glob, or read files yourself.
  When the OKF bundle is active the `context` agent subsumes `explore` (it is
  the only agent that can read `docs/`), so it is the correct dispatch target.
  Use `knowledge_lookup` for pure knowledge questions ("what did we decide",
  "do we have a playbook for").
- **Never write to `docs/` yourself.** The `context` sub-agent is the sole
  automated writer of the OKF bundle. Dispatch it to write or deprecate a page.
- **Do not read `docs/` files directly with `read`/`grep`/`glob`** — ask the
  `context` agent. The one exception is a single `docs/index.md` existence check
  to confirm whether the bundle is active at all.
- **Direct `grep`/`glob`/`read` is still correct** ONLY for a file the user
  explicitly named in their message, or a path you already had before this turn
  started. It is NOT correct for a code-location sweep, even when the sweep has
  already turned up a candidate file, even when the candidate looks like an
  obvious match.

Do NOT "always delegate". Trivial single-file edits and straightforward
diagnostics are faster done directly. The goal is that the routing rules in the
system prompt actually fire, not that every turn spawns a sub-agent — the
dispatch guard (`subagentDispatchLimit`) refuses runaway loops.

## Domain Knowledge
- Repo: `ocode` — a Go 1.23 terminal coding agent built on Charm TUI (Bubble Tea / Lipgloss). See `CLAUDE.md` for repo-wide rules.
- This file is an exact-match stem for `beam-free`. To override the policy for
  a different model in the same family, add a separate file.
