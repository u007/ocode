# ocode Project Instructions

This file is a thin pointer for tools that auto-load `CLAUDE.md` by name
(Claude Code, Cursor). The canonical, always-on agent briefing for ocode
lives in **[AGENTS.md](./AGENTS.md)** — do not duplicate content between
the two; update there only.

The only ocode-specific instruction kept here is the file-reading
convention, because it is genuinely a per-tool default (it changes how
`read`-style tool calls are presented, not just the prompt):

- **When reading files, show only the relevant excerpts needed for the
  current task.** Whole-file dumps waste the context window and obscure
  the signal.
