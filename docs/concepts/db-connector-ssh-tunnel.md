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
timestamp: 2026-11-27T16:03:00Z
---
# DB Connector: per-project config + SSH tunnel

Extends the global-only model in `concepts/db-connector.md` in two ways:

1. A project can carry its own connections in `.ocode/db.json`, merged over the
   global `ocodeconfig.json#db` set with **project entries winning by name**.
2. A connection may reach its database through an SSH tunnel — useful when the
   DB is only reachable from the project's own remote host or a bastion.

A tunnel can live in either file. The per-project file never carries a sealed
secret: a project connection holds templates and tunnel descriptors only, and
`HandleDBConnectAdd` clears any tunnel password before it saves one
(`handler_dbconnect.go:659-661`). A **global** connection may store a tunnel
password, sealed under the master password (`storesTunnelPassword`,
`handler_dbconnect.go:554-556`).

## Status note

MySQL support is **unit-tested only** — no live MySQL server was available in
the dev environment, so the MySQL path (`openMySQL`, `mysqlDSN`, URL guards) is
pinned by unit tests but has not run against a real server.

Both SSH auth modes are implemented: key/agent (`BatchMode=yes`) and password
(`SSH_ASKPASS`, `BatchMode=no`, OpenSSH 8.4 or newer). Neither can prompt
interactively. The web DB panel has the tunnel fields; see
`docs/concepts/db-environment-templates.md` § "Where the feature is used in the UI".

## `.ocode/db.json` shape and merge

The file is `{"connections":[ … ]}` where each entry is the same `DBConnection`
shape as the global set (`internal/config/db_connections.go:117`), including the
optional `tunnel` descriptor (`DBConnection.Tunnel`, `db_connections.go:128`).

- **Only environment templates** (`"${DATABASE_URL}"`) and tunnel descriptors are
  valid here. A literal URL would be an unencrypted credential in a repo, so
  `HandleDBConnectAdd` rejects a non-template connection with `project_path`
  (`handler_dbconnect.go:643-646`). Sealed URLs stay global.
- `ProjectDBConfigPath` → `<root>/.ocode/db.json` (`db_project.go:35`).
- `LoadProjectDBConnections` (`db_project.go:46`): a missing or empty file returns
  `(nil, nil)`; a malformed file is an error (`db_project.go:63`). It then
  reconciles the env-file sidecar onto the connections. `db.json` is
  authoritative and a torn or unreadable sidecar is logged and ignored (see
  `docs/concepts/db-environment-templates.md`, § "Project connections: the sidecar").
- `SaveProjectDBConnections` (`db_project.go:142`): `MkdirAll .ocode` at `0700`,
  then, under one `5s` `filelock` on `<path>.lock` (`db_project.go:169`), writes
  `db.json` **first** and the env-file sidecar second. Each file goes through a
  temp file and a rename (`writeDBFileAtomic`, `db_project.go:96`), so a reader
  sees the old file or the new one. If the sidecar write fails after `db.json`
  is saved, the connections still load. The file/dir modes are pinned by
  `TestProjectDBConfig_FileMode`.
- `MergeDBConnections(global, project)` (`db_project.go:182`) keeps first-seen
  order and **overwrites by name from project**, so a project entry shadows a
  same-named global one (`TestProjectDBConfig_LoadSaveMerge`).

## HTTP surface and `project_path`

All routes stay behind `authMiddleware`; `project_path` is the only project
input, and it is always validated with `dbProjectRootFor`
(`handler_dbconnect.go:243`), which accepts the server's registry roots and the
host's `~` spelling (`projects.ExpandHome`). A remote project's calls are
proxied to the host's own server under `/api/remote/{host}/api/dbconnect/*`, so
that server validates against its own roots — never the local cwd or process
environment (`dbUnlockProjectRoot`, `handler_dbconnect.go:215`).

| Handler | `project_path` behaviour |
|---|---|
| **List** (`HandleDBConnectList`, `handler_dbconnect.go:428`) | With `project_path`, loads `.ocode/db.json` and merges it over global before sorting by name (`handler_dbconnect.go:455-459`). Missing/unreadable project config is silently ignored here. |
| **Add** (`HandleDBConnectAdd`, `handler_dbconnect.go:484`) | With `project_path`, saves into `.ocode/db.json` instead of global; **requires** an env template (`handler_dbconnect.go:643-646`). Duplicate name in the project file → 409. |
| **Remove** (`HandleDBConnectRemove`, `handler_dbconnect.go:706`) | With `project_path`, deletes the named entry from `.ocode/db.json`; not found → 404. Then drops the in-memory grant/pool for that name (`handler_dbconnect.go:750`) and stops its tunnel (`:752`). |
| **Unlock** (`HandleDBConnectUnlock`, `handler_dbconnect.go:786`) | With `project_path`, merges `.ocode/db.json` over global before resolving (`handler_dbconnect.go:818-825`). The project root is only required and validated when at least one connection needs a project: a template, a non-empty `env_file`, or a tunnel with `${VAR}` fields (`dbConnectionNeedsProject`, `handler_dbconnect.go:834-848`). A sealed-only unlock succeeds even if the project dir has moved. Templates with no project are reported in `failed`, never resolved from the process env (`handler_dbconnect.go:893-900`). |

`driver` on Add accepts `postgres` and `mysql`; any other value is a 400
(`handler_dbconnect.go:508-510`).

## Drivers and read-only enforcement

Both drivers are supported:

- `postgres` / `postgresql` → pgx via `stdlib`, extended protocol only
  (`QueryExecModeExec`): a Parse message carries exactly one statement, so
  `"COMMIT; DELETE …"` is rejected by the server instead of running the DELETE
  outside the read-only transaction.
- `mysql` → `go-sql-driver/mysql`, opened by `openMySQL` with a DSN built by
  `mysqlDSN` (`internal/dbconnect/dbconnect.go:160`); `Addr` defaults
  to `127.0.0.1:3306` when the URL carries no host.

`dbconnect.Open` selects the driver from the **URL scheme**, not from the
stored `driver` field (`dbconnect.go:120`). `DBDriverPostgres` and
`DBDriverMySQL` are the two accepted values (`db_connections.go:19-20`), and
`AddDBConnection` validates against the same pair (`db_connections.go:249`).

**Read-only enforcement.** Every query runs through `QueryReadOnly`
(`dbconnect.go:190`), which opens `db.BeginTx(ctx, &sql.TxOptions{ReadOnly:
true})`. Each driver turns that into its dialect's read-only transaction:

- **Postgres:** a `READ ONLY` transaction; a write fails with SQLSTATE `25006`.
- **MySQL:** `START TRANSACTION READ ONLY`; a write fails with server error
  `1290`, `1792` or `1836`.

`IsReadOnlyViolation` (`dbconnect.go:56`) recognises exactly those codes — by
SQLSTATE for Postgres, by error number for MySQL, never by message text. The
query handler (`HandleDBConnectQuery`, `handler_dbconnect.go:1074`) uses that
signal to classify a statement as a write: without `confirm` it returns 409 and
the UI asks the user, and with `confirm` the same SQL is re-run in a read-write
transaction (`ExecWrite`, `internal/dbconnect/write.go:20`).

MySQL URLs are additionally hardened: `ParseURL` refuses `allowallfiles`,
`localinfile`, `interpolateparams`, `multistatements` and
`allowcleartextpasswords` (`mysqlDangerousParams`, `dbconnect.go:271`), which
would otherwise read local files or re-enable multi-statement execution from a
repo-supplied URL. Both stay off by default in the driver.

**Driver label:** an explicit `driver` is validated (`handler_dbconnect.go:508-510`)
and stored as given. Without one, Add guesses from the URL scheme: `mysql://`
is stored as `mysql`, anything else as `postgres` (`handler_dbconnect.go:572-578`).
The scheme is also what selects the driver at open time. SQLite stays file-click
based (no saved config).

## `DBTunnel` descriptor

`internal/config/db_connections.go:27`:

```go
type DBTunnel struct {
    Host          string `json:"host"`          // SSH bastion; may hold a "${VAR}" template
    Port          int    `json:"port,omitempty"` // ssh -p; 0 means "use PortEnv"
    PortEnv       string `json:"port_env,omitempty"` // template source for Port
    LocalPort     int    `json:"local_port,omitempty"` // 0 = pick a free loopback port
    LocalPortEnv  string `json:"local_port_env,omitempty"` // template source for LocalPort
    RemoteHost    string `json:"remote_host"`   // DB host as seen from the bastion; may hold a template
    RemotePort    int    `json:"remote_port"`   // DB port on RemoteHost
    RemotePortEnv string `json:"remote_port_env,omitempty"` // template source for RemotePort
    IdentityFile  string `json:"identity_file,omitempty"` // absolute path; may hold a template
    AuthMethod    string `json:"auth_method,omitempty"` // may hold a template
    Password      string `json:"password,omitempty"` // sealed envelope; NEVER expanded
}
```

**Templates.** String fields (`host`, `remote_host`, `identity_file`, `auth_method`)
hold `${VAR}` references directly, the same syntax as a connection URL
(`docs/concepts/db-environment-templates.md`). Integer ports cannot hold a string,
so a templated port uses the companion `*_env` field (`port_env`,
`local_port_env`, `remote_port_env`); an older binary sees `0` and rejects the
descriptor instead of failing to parse the whole file. `dbTunnelHasEnv`
(`dbconnect_tunnel_env.go:17`) reports whether any field needs expansion.

**Resolution.** At unlock, `dbResolveTunnel` (`dbconnect_tunnel_env.go:44`) expands
every field that holds a braced `${VAR}` reference against the connection's env
file (the same lookup as the URL, via `dbResolveTemplateWithLookup`), and runs
`ValidateTunnelDescriptor` on the **resolved** values before the descriptor is
cached in the grant. A literal value is copied verbatim and never run through
the expander, so a literal `$work` in a host or path stays as written
(`TestDBResolveTunnelKeepsLiteralDollarFieldsVerbatim`). `local_port` accepts `0`
(pick a free port), and a templated port that resolves outside its range fails
with `local_port must resolve to a port between 0 and 65535` (the other ports
say `between 1 and 65535`). A value from
`.env.staging` therefore cannot bypass the host, path or port checks. The `*_env`
fields are cleared in the resolved copy, so the cached grant holds only values ssh
can use directly. Tunnel passwords are deliberately excluded: they are a sealed
envelope and are never expanded.

Add-time rules in `HandleDBConnectAdd` (`handler_dbconnect.go:515-530`, `:581-589`):

- A tunnel with `${VAR}` fields under a **literal** URL is rejected with 400:
  `SSH tunnel fields that use ${VAR} need a template URL (with an env file); use literal tunnel values with a literal URL`.
- A non-empty `env_file` is rejected with 400 unless the URL is a template or the
  tunnel uses `${VAR}`.
- A tunnel with `${VAR}` fields needs an `env_file` and an allowed project
  (`a tunnel with environment references needs an env file and an allowed project`).
  The descriptor is resolved and validated at Add (`dbResolveTunnel`, `:589`).

## Validation (`ValidateTunnelDescriptor`, `internal/remote/dbtunnel.go:103`)

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

## SSH invocation shape (`ensureOnce`, `dbtunnel.go:184`)

```
ssh -N \
    -L 127.0.0.1:<localPort>:<RemoteHost>:<RemotePort> \
    -o ExitOnForwardFailure=yes \
    -o StrictHostKeyChecking=accept-new \
    -o BatchMode=yes \
    [-i <IdentityFile>] [-p <Port>] \
    -- <host>
```

The shape above is key/agent auth. Built in `ensureOnce` (`dbtunnel.go:243-277`):

- **Key/agent auth** (the default): `BatchMode=yes`, so ssh never prompts.
  `-i <IdentityFile>` is passed when `IdentityFile` is set.
- **Password auth** (`auth_method: "password"` with a password): `BatchMode=no`,
  `PreferredAuthentications=password` and `PubkeyAuthentication=no`, and no `-i`.
  The password reaches ssh through an `SSH_ASKPASS` helper
  (`SSH_ASKPASS_REQUIRE=force`, OpenSSH 8.4 or newer, `dbtunnel.go:293-309`). The
  helper reads it from a separate file, so it appears neither on the command line
  nor in the environment (`createAskpassFiles`, `dbtunnel.go:501`).
- The host is passed without a user. The user comes from the ssh configuration.
- **Local port.** With `LocalPort` 0, the previous port is kept while it is still
  free (`portIsFree`, `dbtunnel.go:248`). Otherwise the kernel picks one
  (`net.Listen("tcp", "127.0.0.1:0")`, `dbtunnel.go:251`) and the listener is
  closed. A restart therefore keeps the port a pooled DB handle already holds,
  when it can.
- The process is waited on in a `crashguard.Go` goroutine that calls `cmd.Wait()`
  (`dbtunnel.go:339-340`), so `ProcessState` and stderr are never read while exec
  is still running.

Stored passwords: a global connection's tunnel password is sealed under the
master password. At unlock it is opened with that password, and the client's
value is never substituted for it (`dbUnlockTunnelPassword`,
`handler_dbconnect.go:1134`). Only a connection with no stored password takes the
password the client supplied for this unlock.

## Lifecycle: when a tunnel starts, reuse, cooldown, teardown

**The tunnel does not start at unlock.** Unlock (`HandleDBConnectUnlock`,
`handler_dbconnect.go:786`) resolves the descriptor (`dbResolveTunnel`,
`handler_dbconnect.go:939`), reads the tunnel password (`:930`), and checks that
the URL can be routed through the tunnel (`rewriteURLForTunnel(connURL, 1)`,
`:947`). The grant keeps the resolved URL unchanged. The code comment at
`:924-929` gives the reason: a project's `.ocode/db.json` comes from the repo, so
unlocking a cloned repo must not make ocode dial a host the repo chose.

The tunnel starts the first time the connection is opened. `dbGrantDB`
(`handler_dbconnect.go:305`) calls `dbTunnelPort` (`:406`), which calls
`DBTunnelManager.Ensure` through `dbEnsureTunnel` (`:390`). Only after that is the
pooled DSN rewritten to `127.0.0.1:<port>` (`rewriteURLForTunnel`, `:331`). The
remote host is never in the grant.

- **Connecting.** `ensureOnce` waits up to `tunnelConnectWait` (2 s), polling the
  local port every `tunnelPortCheckGap` (`dbtunnel.go:375-409`). While ssh is
  still connecting, `Ensure` returns `ErrTunnelInProgress` (`dbtunnel.go:240`).
  The open retries that up to `dbTunnelOpenTries` (12) times, `dbTunnelOpenGap`
  (250 ms) apart (`handler_dbconnect.go:397-421`). A process still not ready
  after `tunnelDeadTimeout` (6 s) since its start is killed and marked failed
  (`dbtunnel.go:233-238`).
- **Reuse.** Tunnels are keyed by `tunnelKey` (`dbtunnel.go:87`): connection name,
  host, port, remote host and port, identity file, local port and auth method.
  Credentials are not part of the key. A ready tunnel with a live process returns
  its port at once (`dbtunnel.go:219-220`). A changed descriptor is a different
  key, so it gets a new tunnel, and the old one is only stopped by `Stop`,
  `StopConnection` or `StopAll`.
- **Cooldown.** After a failed start, `Ensure` returns `ErrTunnelCooldown`
  (wrapping the original error) for `tunnelCooldown` (5 s; `dbtunnel.go:204-208`).
  After the window the state resets and the start is tried again
  (`dbtunnel.go:210-211`).
- **Retries.** `Ensure` makes up to `tunnelMaxAttempts` (3) attempts on retryable
  outcomes (a port taken in the meantime, a process that died at start). Any
  other error is returned at once (`dbtunnel.go:165-179`).
- **Teardown.** `Stop(name, t)` cancels and kills one descriptor's process
  (`dbtunnel.go:454`). `StopConnection(name)` does the same for every tunnel of
  one saved connection (`dbtunnel.go:469`), and is what Remove calls
  (`handler_dbconnect.go:752`). `StopAll()` tears down every tunnel and clears the
  map (`dbtunnel.go:485`). There is no automatic reaper in this pass.

Constants (`dbtunnel.go:32-38`): connect wait `2s`, cooldown `5s`, port-check
gap `100ms`, max attempts `3`, dead timeout `6s`.

## See also

- `concepts/db-connector.md` — the base connector: sealed envelopes, unlock
  grants, env templates, read-only enforcement.
- `.opencode/plans/2026-11-10-db-ide-config-ssh-tunnel.md` — design plan and
  confirmed decisions (tunnel project-only, no confirm prompt, MySQL implemented
  via go-sql-driver/mysql with START TRANSACTION READ ONLY).
