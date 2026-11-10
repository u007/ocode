---
type: Concept
title: 'DB Connector: per-project config (.ocode/db.json) + SSH tunnel'
description: How a project carries its own DB connections in .ocode/db.json (merged over global, project wins), how List/Add/Remove/Unlock honor project_path, and the DBTunnel descriptor, validation, ssh invocation and lifecycle. Both postgres and mysql are supported; every read runs in a server-enforced READ ONLY transaction.
resource: internal/config/db_project.go; internal/remote/dbtunnel.go; internal/server/handler_dbconnect.go; internal/config/db_connections.go; internal/dbconnect/dbconnect.go
tags:
  - dbconnect
  - postgres
  - mysql
  - ssh
  - tunnel
  - project-config
  - security
timestamp: 2026-11-10T15:58:18Z
---
# DB Connector: per-project config + SSH tunnel

Extends the global-only model in `concepts/db-connector.md` in two ways:

1. A project can carry its own connections in `.ocode/db.json`, merged over the
   global `ocodeconfig.json#db` set with **project entries winning by name**.
2. A connection may reach its database through an SSH tunnel — useful when the
   DB is only reachable from the project's own remote host or a bastion.

Both are **project-scoped only**: the global config never carries a tunnel, and
the per-project file never carries a sealed secret (templates + tunnel
descriptors only).

## Status note

MySQL support is **unit-tested only** — no live MySQL server was available in
the dev environment, so the MySQL path (`openMySQL`, `mysqlDSN`, URL guards) is
pinned by unit tests but has not run against a real server. Password/interactive
SSH auth (the tunnel is key/agent only, see below) and the Web UI (Phase 3 of
the design plan) remain unimplemented.

## `.ocode/db.json` shape and merge

The file is `{"connections":[ … ]}` where each entry is the same `DBConnection`
shape as the global set (`internal/config/db_connections.go:64`), including the
optional `tunnel` descriptor (`DBConnection.Tunnel`, `db_connections.go:72`).

- **Only environment templates** (`"${DATABASE_URL}"`) and tunnel descriptors are
  valid here. A literal URL would be an unencrypted credential in a repo, so
  `HandleDBConnectAdd` rejects a non-template connection with `project_path`
  (`handler_dbconnect.go:451-453`). Sealed URLs stay global.
- `ProjectDBConfigPath` → `<root>/.ocode/db.json` (`db_project.go:23`).
- `LoadProjectDBConnections`: a missing or empty file returns `(nil, nil)`; a
  malformed file is an error (`db_project.go:29`).
- `SaveProjectDBConnections`: `MkdirAll .ocode` at `0700`, write temp file at
  `0600`, rename atomically, all under a `5s` `filelock` on `<path>.lock`
  (`db_project.go:53`). The file/dir modes are pinned by
  `TestProjectDBConfig_FileMode`.
- `MergeDBConnections(global, project)` keeps first-seen order and **overwrites
  by name from project**, so a project entry shadows a same-named global one
  (`db_project.go:81`; `TestProjectDBConfig_LoadSaveMerge`).

## HTTP surface and `project_path`

All routes stay behind `authMiddleware`; `project_path` is the only project
input, and it is always validated with `dbProjectRootFor`
(`handler_dbconnect.go:215`), which accepts the server's registry roots and the
host's `~` spelling (`projects.ExpandHome`). A remote project's calls are
proxied to the host's own server under `/api/remote/{host}/api/dbconnect/*`, so
that server validates against its own roots — never the local cwd or process
environment (`dbUnlockProjectRoot`, `handler_dbconnect.go:187`).

| Handler | `project_path` behaviour |
|---|---|
| **List** (`:321`) | With `project_path`, loads `.ocode/db.json` and merges it over global before sorting by name (`:344-350`). Missing/unreadable project config is silently ignored here. |
| **Add** (`:373`) | With `project_path`, saves into `.ocode/db.json` instead of global; **requires** an env template (`:445-476`). Duplicate name in the project file → 409. |
| **Remove** (`:509`) | With `project_path`, deletes the named entry from `.ocode/db.json`; not found → 404. Then drops the in-memory grant/pool for that name (`:541`). |
| **Unlock** (`:584`) | With `project_path`, merges `.ocode/db.json` over global before resolving (`:616-621`). The project root is only *required/validated for reading* when at least one connection is a template; a sealed-only unlock succeeds even if the project dir has moved (`:631-645`). Templates with no project are reported in `failed`, never resolved from the process env. |

`driver` on Add accepts `postgres` and `mysql`; any other value is a 400
(`handler_dbconnect.go:390-393`).

## Drivers and read-only enforcement

Both drivers are supported:

- `postgres` / `postgresql` → pgx via `stdlib`, extended protocol only
  (`QueryExecModeExec`): a Parse message carries exactly one statement, so
  `"COMMIT; DELETE …"` is rejected by the server instead of running the DELETE
  outside the read-only transaction.
- `mysql` → `go-sql-driver/mysql`, opened by `openMySQL` with a DSN built by
  `mysqlDSN` (`internal/dbconnect/dbconnect.go:114`, `:131`); `Addr` defaults
  to `127.0.0.1:3306` when the URL carries no host.

`dbconnect.Open` selects the driver from the **URL scheme**, not from the
stored `driver` field (`dbconnect.go:91`). `DBDriverPostgres` and
`DBDriverMySQL` are the two accepted values (`db_connections.go:15-16`), and
`AddDBConnection` validates against the same pair (`db_connections.go:132`).

**Read-only enforcement.** Every query runs through `QueryReadOnly`
(`dbconnect.go:159`), which opens `db.BeginTx(ctx, &sql.TxOptions{ReadOnly:
true})`. Each driver turns that into its dialect's read-only transaction:

- **Postgres:** a `READ ONLY` transaction; a write fails with SQLSTATE `25006`.
- **MySQL:** `START TRANSACTION READ ONLY`; a write fails with server error
  `1290`, `1792` or `1836`.

`IsReadOnlyViolation` (`dbconnect.go:56`) recognises exactly those codes — by
SQLSTATE for Postgres, by error number for MySQL, never by message text. The
query handler (`HandleDBConnectQuery`, `handler_dbconnect.go:847`) uses that
signal to classify a statement as a write: without `confirm` it returns 409 and
the UI asks the user, and with `confirm` the same SQL is re-run in a read-write
transaction (`ExecWrite`, `internal/dbconnect/write.go:20`).

MySQL URLs are additionally hardened: `ParseURL` refuses `allowallfiles`,
`localinfile`, `interpolateparams`, `multistatements` and
`allowcleartextpasswords` (`mysqlDangerousParams`, `dbconnect.go:240`), which
would otherwise read local files or re-enable multi-statement execution from a
repo-supplied URL. Both stay off by default in the driver.

**Driver label:** Add derives `driver` from the URL scheme (`postgres` or
`mysql`, `handler_dbconnect.go:423-432`), so a `mysql://` URL is stored as
`mysql`. The scheme is also what selects the driver at open time. SQLite stays
file-click based (no saved config).

## `DBTunnel` descriptor

`internal/config/db_connections.go:19`:

```go
type DBTunnel struct {
    Host         string `json:"host"`          // SSH bastion
    Port         int    `json:"port,omitempty"` // ssh -p
    LocalPort    int    `json:"local_port,omitempty"` // 0 = pick a free loopback port
    RemoteHost   string `json:"remote_host"`   // DB host as seen from the bastion
    RemotePort   int    `json:"remote_port"`   // DB port on RemoteHost
    IdentityFile string `json:"identity_file,omitempty"` // absolute path
}
```

## Validation (`validateTunnelDescriptor`, `dbtunnel.go:69`)

Rejects before any process is spawned:

- `nil`, empty `Host`, empty `RemoteHost`, or `RemotePort == 0` → `invalid tunnel descriptor`.
- `Target{Host, Port}.Validate()` must pass (reuses the remote-connection host validator).
- `RemoteHost`: no whitespace/control chars, no leading `-`; a `:` requires a
  `[` prefix (bracketed IPv6 form).
- `RemotePort` must be `1..65535`; `LocalPort` may be `0` (allocate) or `1..65535`.
- `IdentityFile`, when set, must be an absolute path, not start with `-`, and
  contain no CR/LF.

**Trust note:** `.ocode/db.json` is repo-controlled and there is no confirmation
prompt, so a cloned repo can direct SSH at an attacker host. This is the same
documented trust model as `.env` templates and git hooks; validation only stops
malformed/argument-injection shapes, not a hostile but well-formed host.

## SSH invocation shape (`ensureOnce`, `dbtunnel.go:193`)

```
ssh -N \
    -L 127.0.0.1:<localPort>:<RemoteHost>:<RemotePort> \
    -o BatchMode=yes \
    -o ExitOnForwardFailure=yes \
    -o StrictHostKeyChecking=accept-new \
    [-i <IdentityFile>] [-p <Port>] \
    -- <user@host>
```

- **Key/agent auth only.** `BatchMode=yes` means no interactive
  password/passphrase prompt; password auth described in the design plan is
  **not** implemented in this pass.
- `localPort`: if `LocalPort == 0`, a free loopback port is allocated by
  `net.Listen("tcp","127.0.0.1:0")` then closing it (`:182-190`).
- Process is reaped on a `crashguard.Go` goroutine that sends `cmd.Wait()` over a
  channel, so `ProcessState`/stderr are never read concurrently with exec
  (`:234-235`).

## Lifecycle: cooldown, reuse, teardown

Constants (`dbtunnel.go:24-30`): connect wait `2s`, cooldown `5s`, port-check
gap `100ms`, max attempts `3`, dead timeout `6s`.

- **Reuse:** tunnels are keyed by `name|host|port|RemoteHost|RemotePort|IdentityFile|LocalPort`
  (`tunnelKey`, `:64`). A ready tunnel with a live process returns its port
  immediately (`:150`). A changed descriptor is a *different* key (a new tunnel);
  the old one is not reaped automatically — only `Stop`/`StopAll` does that.
- **Cooldown:** after a failed attempt, `Ensure` returns `ErrTunnelCooldown`
  (wrapping the original error) for `5s` (`:136-141`); after the window the state
  is reset and retried (`:142-147`).
- **In progress:** if the process started but the port does not yet accept,
  `Ensure` polls the local port every `100ms`; callers get `ErrTunnelInProgress`
  (`:178`) until ready. If still not ready after `6s` since start, the process is
  cancelled/killed and the attempt fails retryable (`:164-177`).
- **Retries:** `Ensure` loops up to `3` attempts on retryable outcomes (port
  stolen, dead process at start) and otherwise returns the final error
  (`:116-126`).
- **Unlock integration:** for each connection with a `Tunnel`, unlock calls
  `DefaultDBTunnelManager().Ensure` **before** resolving/decrypting (`:669-683`).
  `ErrTunnelInProgress` aborts the whole unlock with **503 + `Retry-After: 2`**
  (`:672-675`); any other tunnel error is recorded in `failed`, the connection's
  stale grant is dropped, and the loop continues. On success the resolved URL is
  rewritten to `127.0.0.1:<port>` by `rewriteURLForTunnel` (`:890`, applied at
  `:704-706` and `:723-725`) — the remote host never appears in the granted URL.
- **Teardown:** `Stop(name, tun)` cancels the context and kills the process for
  one key (`:304`); `StopAll()` tears down every tunnel and clears the map
  (`:328`). There is no automatic reaper in this pass.

## See also

- `concepts/db-connector.md` — the base connector: sealed envelopes, unlock
  grants, env templates, read-only enforcement.
- `.opencode/plans/2026-11-10-db-ide-config-ssh-tunnel.md` — design plan and
  confirmed decisions (tunnel project-only, no confirm prompt, MySQL implemented
  via go-sql-driver/mysql with START TRANSACTION READ ONLY).
