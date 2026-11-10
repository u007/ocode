# DB IDE / Connector: Per-Project Config + Remote SSH Tunnel — Design (refined)

## Goal
Allow a project to carry its own DB connection configs (`.ocode/db.json`) instead of
relying solely on the global `ocodeconfig.json#db`, and let a connection optionally
reach its database through an SSH tunnel (project's own remote host, or a different
bastion host). Also add MySQL driver support.

## User-confirmed decisions
1. Tunnel descriptor: **project config only** (no global).
2. SSH auth: **key-based (with passphrase support) AND password**, with the option
   to save passwords. (Interactive prompts allowed.)
3. MySQL: **implemented** (mysql driver, START TRANSACTION READ ONLY).
4. SQLite IDE: **stay file-click based** (no saved configs).
5. Confirm prompt for repo-configured tunnel host: **none** (no confirm).

## Scope (phased)

### Phase 1 — Per-project config (Postgres + MySQL)
- `.ocode/db.json` with `connections` array. Same shape as global `DBConnection`
  minus secrets (templates + tunnel descriptors only; sealed URLs stay global).
- Resolution order: project config > global config; name collision → project wins.
- Driver allowlist extended: `postgres`, `mysql` (new mysql driver via go-sql-driver/mysql
  or pgx-style; reuse dbbrowse-style read-only transaction enforcement where possible).
- Backward compatible: older binaries ignore `.ocode/db.json`.
- Env templates resolve against the project's `.env` as today.

### Phase 2 — SSH tunnel manager
- New `internal/remote/dbtunnel.go`: manager that shells out to system `ssh`
  with `-N -L 127.0.0.1:<port>:<dbhost>:<dbport> <bastion>`.
- Supports key-based auth (with passphrase via SSH agent / keychain) and password
  auth (interactive + saved, stored sealed like DB URLs).
- BatchMode for non-interactive where possible; interactive fallback for
  passphrase/password.
- Supervised under process supervisor; reaped on shutdown + tunnel_reap for stale.
- Loopback bind, free-port allocation, reuse existing tunnel per connection.
- Connection URL rewritten to localhost:port at dial time.
- Project config only (per decision 1); global config never carries a tunnel.

### Phase 3 — Web UI
- DBPanel reads `.ocode/db.json` when a project is selected; merges with global.
- Tunnel badge + "Establishing tunnel…" state on unlock.
- Add form: driver selector (postgres/mysql), tunnel descriptor fields,
  auth method selector (key/password), save-password checkbox.
- MySQL-specific UI (port default 3306, etc.).

## Out of scope
No cluster/HA routing, no auto-reconnect beyond retry-on-unlock.

## Security notes
- Project config is untrusted (repo-controlled). Tunnel host must pass
  `Target.Validate()` (leading-`-` rejection). No confirm prompt (per decision 5),
  so a cloned repo can direct SSH to an attacker host — documented risk, same
  trust model as `.env` templates and git hooks.
- Saved passwords stored sealed (Argon2id + AES-GCM, per-envelope salt), never
  plaintext, never logged, never returned to client.
- Interactive password prompts must not block the request goroutine indefinitely;
  use a bounded timeout + SSE/WS prompt, or require saved password for non-interactive.

## Open follow-ups before implementation
- MySQL driver choice and read-only enforcement strategy (MySQL has no
  READ ONLY transaction mode like Postgres; need SET TRANSACTION READ ONLY or
  a read-only user role).
- SSH passphrase handling: use SSH agent or prompt via TUI/web? Recommend
  SSH agent first, fall back to interactive prompt with timeout.
- Process supervisor integration for tunnel lifecycle.

## Next step
Approve Phase 1 scope and I'll write the implementation plan (writing-plans)
and start coding Phase 1. Phases 2/3 follow.