# Session lists include uncloned Claude Code sessions

The web project session list (`HandleListProjectSessions`, `HandleListSessions`)
and the TUI session picker (`loadSessionRefsCmd`) merge in Claude Code sessions
through `session.ListRefsWithClaudeForDir` / `ListRefsPaginatedWithClaudeForDir`
(`internal/session/session.go`).

- A Claude Code transcript with no ocode clone is listed with id `claude:<id>`
  and a `claude: ` title prefix. Opening it clones it (`OpenClaudeCloneForDir`).
- Once cloned (`claude-<id>` row, or metadata `claude_original_session_id`), only
  the clone is listed.
- The merge happens before pagination, so pages stay globally ordered.
- `ListRefsForDir` / `ListRefsPaginatedForDir` stay ocode-only for callers that
  open refs with `LoadForDir` (pulse, worktree state).
- **Remote (SSH) projects:** the remote host's `ocode serve --remote` answers the
  list request, so `~/.claude/projects` is read on the host. This takes effect
  only after the host binary is upgraded (version bump re-provisions it).
