/**
 * Session-id helpers shared by the session list surfaces.
 *
 * Child ("context") sessions are minted by the agent whenever a subagent runs:
 * `internal/agent/child_session.go` builds `<parentID>_child_<agentName>_<ts>`,
 * e.g. `ses_2026-09-24-133707-d5eda0e8_child_context_2026-09-24-133958`. They
 * are execution detail for a parent turn, not conversations a user resumes, so
 * the session picker lists only main sessions.
 *
 * Kept in sync with the Go id format; main ids are `ses_<date>-<time>-<hex>`
 * and never contain the `_child_` infix.
 */

/** True for subagent / child-context sessions (id contains the `_child_` infix). */
export function isChildSessionId(id: string): boolean {
  return id.includes("_child_");
}
