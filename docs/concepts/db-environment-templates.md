---
type: Concept
title: DB Environment Templates (.env / .env.local for connections and SQLite paths)
description: |-
  A Postgres connection URL or a SQLite file path may contain ${NAME} or $NAME
  references, resolved per project against the project root (.env, then
  .env.local, then process env). Only a braced ${NAME} makes the value a template;
  a bare $NAME never does, so a literal password such as "pa$HOME" or "p$ssword"
  is sealed, not saved in the clear. The feature spans the dotenv
  parser (`internal/envfile`), template expansion (`internal/dbconnect/expand`),
  the saved-connection shape (`config.DBConnection`), the server unlock flow
  (`dbEnvLookup` / `dbResolveTemplate`), SQLite path expansion (`dbExpandPath`),
  and the web DB panel (`DBPanel.tsx`).
tags:
  - dbconnect
  - sqlite
  - env
  - dotenv
timestamp: 2026-10-30T00:00:00Z
resource: internal/envfile/envfile.go; internal/dbconnect/expand.go; internal/config/db_connections.go; internal/server/dbconnect_env.go; internal/server/handler_dbconnect.go; internal/server/handler_db.go; web/src/components/Layout/DBPanel.tsx
---

# DB Environment Templates

A connection URL (`postgres://...`) or a SQLite file path (`data/*.sqlite`) may
contain environment-variable references. The value is expanded at the point where
the feature needs it — load time for a Postgres connection, path-resolution time
for a SQLite file — against the environment of the project the request names. The
DB connector is a PROJECT tab, so it passes the active project's path; a legacy
session-id caller still resolves against that session's project.

Full description: `docs/concepts/db-environment-templates.md` (this page). Cross-links
in the connector and SQLite browser pages (`docs/concepts/db-connector.md`,
`docs/concepts/sqlite-browser.md`) point here for the full mechanism.

## Reference syntax (`internal/dbconnect/expand.go:37` `ExpandEnv`)

Both forms are accepted (`internal/dbconnect/expand.go:85` `readReference`):

- `${NAME}` — braced. Always a reference.
- `$NAME` — bare. Only a reference when `NAME` is a shell-like identifier (`a`-`z`,
  `A`-`Z`, `_`, followed by letters/digits/`_`). A lone `$`, `$-`, `$ `, or a
  trailing `$` stays literal text (`expand.go:52`).

A reference is substituted with the value returned by the lookup function. The
substitution happens on the **raw string before any URL or file parsing**
(`expand.go:27`): `net/url` rejects a `${` in userinfo (`"invalid userinfo"`),
so a URL with an embedded reference cannot be parsed first. Substituting into an
already-parsed URL is also unsafe — a value carrying `/`, `@`, or `:` would silently
re-interpret the URL structure (`expand.go:31`).

## Template vs literal — the load-bearing distinction (`internal/dbconnect/expand.go:151` `IsTemplate`)

`IsTemplate` decides whether a value must be expanded:

- A **braced** reference (`${NAME}`) ALWAYS makes the value a template, even when
the variable is not set. An unresolved braced reference is an **error** naming the
missing variable (`expand.go:63`): `environment variable "DATABASE_URL" is not set`.
It never expands to an empty string, because a template that silently lost its
password (`postgres://user:${DB_PASS}@...`) would connect to the wrong place with
the wrong credentials and look like a success.
- A **bare** reference (`$NAME`) never makes the value a template, whether or not it
resolves. `IsTemplate` takes no environment lookup, so the process environment cannot
change the outcome. A value with no braced reference stays **literal text**:
`postgres://app:p$ssword@host/db`, `postgres://app:pa$HOME@db/prod` and `we$ird.db`
are all kept as written.

Classifying by what the environment happens to define was the bug this rule fixes.
`pa$HOME` in a password is a common literal, and `HOME` is always set. Treating it as a
template saved the password in plaintext instead of sealing it.

The practical consequence (`expand.go:151`): a value with a braced reference, such as
`postgres://${U}:$P@h/db`, is a template, and every bare reference in it is expanded
when it resolves, so `$P` is expanded too. An unset bare `$P` in that value stays
literal. Known gap: a template that also carries a literal password with a `$` in it
still has that bare name expanded, and the value is saved in the clear. Use the braced
form (`${P}`) when a missing variable must be an error.

## Where values come from, and precedence (`internal/envfile/envfile.go`)

`envfile` defines the dotenv parser (`envfile.go:41` `Parse`) and the loader
(`envfile.go:182` `Load`). The constants (`envfile.go:24`):

- `FileName = ".env"`
- `LocalFileName = ".env.local"`

`Load(dir)` (`envfile.go:182`) reads `.env` first and `.env.local` second; a
key in `.env.local` overrides the same key in `.env`. A missing file is **not**
an error (`envfile.go:202` `loadFile`: `os.IsNotExist` yields an empty map). A file
that exists but cannot be parsed **is** an error (`envfile.go:208`): the caller logs
it and continues with the process environment only, so the connection fails with
an explicit "not set" error naming the variable (`dbconnect_env.go:45`).

The dotenv format (`envfile.go:8`): `KEY=VALUE`, optional `export ` prefix
(`envfile.go:79` `strings.CutPrefix`), `#` comments, blank lines skipped, single-quoted
values (literal, no escape processing — `envfile.go:127`), double-quoted values with
`\n`, `\t`, `\r`, `\\` unescaped (`envfile.go:145` `unescapeDouble`). The first
`=` outside quotes splits key from value (`envfile.go:101` `indexUnquoted`). A quoted
value may contain `=`. An unterminated quoted value (`DB_PASS="abc`) is a parse error that names the line and never echoes the value, so the line is not silently skipped. A duplicate key in one file is a parse error (`envfile.go:59`
`fmt.Errorf("line %d: duplicate key %q", ...)`). There is no multi-line value, no
interpolation inside a file (`envfile.md:10`), and no command substitution.

`Lookup(name, fileValues)` (`envfile.go:225`) gives the precedence:

1. Process environment (`os.LookupEnv`) — first.
2. `.env.local` (merged over `.env` by `Load`).
3. `.env`.

So: **process env > `.env.local` > `.env`**. A value exported in the shell (or
injected by CI) always wins over the file; `.env.local` still overrides the
committed `.env`. A variable that is set but EMPTY counts as set (`envfile.go:222`
"A variable that is set but EMPTY counts as set"): an empty value is a deliberate
value, not a missing one. That matters for a connection whose password is
deliberately empty (`DB_PASS=`).

## Per-project resolution (`internal/server/dbconnect_env.go`)

`dbEnvLookup(projectRoot)` (`dbconnect_env.go:27`) builds the lookup used to expand
a template. It is lazy — the `.env` files are read on the **first variable lookup**
(`dbconnect_env.go:29` `sync.Once`), not when the lookup is constructed, so a
request with a literal URL (no `$`) never touches the filesystem (`dbconnect_env.go:27`
comment). A `projectRoot` of `""` means "no project": only the process environment is
consulted (`dbconnect_env.go:19`). A malformed `.env` is logged (without values) and
the lookup falls back to the process environment only (`dbconnect_env.go:40`); the
connection then fails with an explicit "environment variable \"...\" is not set"
error (`dbconnect_env.go:55`).

`dbResolveTemplate` (`dbconnect_env.go:79`) resolves a connection's environment
template into a usable URL: it calls `dbconnect.ExpandEnv(template, dbEnvLookup(projectRoot))`,
then validates the expanded result as a Postgres URL (`dbconnect.ParseURL`).
Validating **after** expansion is required because a template (`"${DATABASE_URL}"`)
is not a URL (`dbconnect_env.go:80`). The resolved URL is a credential: it is never
logged (`dbconnect_env.go:86`), never returned to the client, and never written to disk
(`dbconnect_env.go:77` comment). The error names the missing variable or the invalid
URL shape (`ParseURL`'s error never contains the URL — `internal/dbconnect.ParseURL` doc —
so the password cannot leak through the message).

`HandleDBConnectUnlock` (`handler_dbconnect.go:472`) applies this per project:
`project_path` names the project (`handler_dbconnect.go:520`
`h.dbUnlockProjectRoot(req.ProjectPath, req.SessionID)`); the allowlist check
itself is `handler_dbconnect.go:184` (`h.fileContentRootFor(projectPath)` inside
dbUnlockProjectRoot), validated against the server's allowed project roots exactly
like a file read. An invalid path — including a remote
project's path, which belongs to another machine — is refused with 400 rather than
reading a local `.env` by accident. `session_id` remains as a compatibility
fallback for callers that predate the project-scoped panel
(`handler_dbconnect.go:191` `h.sessionProjectRoot(req.SessionID)`); it trusts the
session registry's recorded root (server-side state, never client input) as the
trust boundary.

**A template needs a project.** With neither `project_path` nor a resolvable
`session_id`, a template is reported as failed and is NEVER resolved from the
server's own process environment (`handler_dbconnect.go:553`). That fallback was
removed because a remote project would otherwise connect — and allow writes — to a
local database exported in the server's shell.

Every saved connection is iterated once (`handler_dbconnect.go:546`): if
`c.EnvTemplate != ""` it is resolved against that project; if `c.EncryptedURL != ""`
it is opened with the master password. The password is **optional for the
environment flow**: with no password, only the template connections are granted and
every sealed connection is left locked. A template has no envelope, so there is
nothing to decrypt. With a password, the sealed connections still gate the unlock:
a wrong password is rejected with 401 ("master password does not open connection
...") **before** any grant is applied.

Failures are **per connection**: a template that does not resolve is recorded in
the response's `failed` array and the others are still granted
(`handler_dbconnect.go:563`). An obsolete `${ANALYTICS_URL}` therefore no longer
blocks unlocking a sealed connection with the correct password. An unlock that
grants nothing is a 400 (`handler_dbconnect.go:596`). The grant holds the plaintext
URLs in memory only (`handler.go:229` "dbGrants holds the plaintext URLs"), keyed by
surface id (`db-panel:<host>:<projectPath>:<per-mount id>` in `DBPanel.tsx:75`). The
password-less flow MERGES into the surface grant (`handler_dbconnect.go:607`); a
password unlock replaces it (`handler_dbconnect.go:609`), so loading an environment connection never
revokes a sealed connection the user already unlocked.

The grant is bounded: a surface idle for 30 minutes (or beyond 64 remembered
surfaces) is retired with its pooled handle, and the client-supplied surface is
length-bounded (256). A template that stops resolving now LOSES its grant and
pool: the password-less unlock drops it from the surface before the "nothing
unlocked" 400 (`handler_dbconnect.go:563` `staleNames`), so a later query cannot
keep hitting the old database. `project_path` is resolved and validated only when
at least one saved connection is a template (`handler_dbconnect.go:511`
`hasTemplate`), so a sealed-only unlock works even when the project directory has
moved.

`HandleDBConnectAdd` (`handler_dbconnect.go:336`) stores the connection. The URL
field (`req.URL`) is examined with `dbconnect.IsTemplate(req.URL)`
(`handler_dbconnect.go:354`): a template is saved as `EnvTemplate` in the clear
(`handler_dbconnect.go:390`); a literal URL is sealed (`dbconnect.SealURL`) and
saved as `EncryptedURL`. The master password is **required only for a literal
URL** (`handler_dbconnect.go:362`): a template has no secret to encrypt, so the
add request may omit it and the existing `dbCheckMasterPassword` check is skipped
for the template. A literal URL with no password is a 400.

A template that ALSO carries a literal password is refused
(`handler_dbconnect.go:355`, `dbconnect.TemplateHasLiteralSecret`): a template is
stored in the clear, so `postgres://app:hunter2@host/db?sslmode=${SM}` would put
`hunter2` in `ocodeconfig.json` unencrypted. The password (and any
`password`/`sslpassword` query value) must consist of references only
(`${DB_PASS}`); a partial reference such as `pa$HOME` is refused too. Only
BRACED references count: a bare `$NAME` is refused, because an unset variable is
kept as literal text by `ExpandEnv` and would be stored in the clear. The query
scan runs to the end of the string with no `#` truncation, and runs even when
there is no `://` (scheme-less templates).

Exactly one of the two fields must be set (`internal/config/db_connections.go:57`
`AddDBConnection`): neither (`url` and `env_template` both empty) or both
(ambiguous which wins at unlock) is an error. The saved connection never holds a
plaintext URL.

`HandleDBConnectList` (`handler_dbconnect.go:291`) returns `FromEnv: c.EnvTemplate != ""`
(`handler_dbconnect.go:322`) — the "env" badge in the UI — and `Unlocked` from the
grant. The template itself (`EnvTemplate`) is **not** returned: it names variables,
but the resolved URL is a credential and stays on the server (`handler_dbconnect.go:307`
`FromEnv` doc comment).

## SQLite file paths (`internal/server/handler_db.go`)

`resolveDBPath` (`handler_db.go:91`) applies **both** checks to every DB endpoint:
first `dbExpandPath` (environment expansion), then `resolveProjectFilePath`
(file-content containment). The expansion is the **first** gate (`handler_db.go:83`
"The reference is expanded HERE, before the path is resolved"): `dbExpandPath`
(`handler_db.go:191`) calls `dbconnect.IsTemplate(path)`; if
it is not a template the original path is returned; if it is, `dbconnect.ExpandEnv`
resolves it (`handler_db.go:195`). An unresolved braced reference produces an error
message naming the variable. A bare reference never makes a path a template, so
`$DB_FILE.db` is taken literally; write `${DB_FILE}.db` to expand it.
The expanded value goes through the **same** containment and allowed-roots checks as
a literal path: it is never trusted, only checked (`handler_db.go:89` "The expanded
value is never trusted, only checked"). An expanded path pointing outside the
project is refused exactly as a literal one (`handler_db.go:119` `pathWithinAllowedRoots`).

`resolveDBPath` (`handler_db.go:103`) resolves symlinks (`filepath.EvalSymlinks`): a
symlink inside a root pointing at a file outside it must not pass containment
lexically and then be opened at its target. A dangling symlink falls back to a
lexical containment check (`handler_db.go:109` `containedLexical`), because there
is no real file to open; a missing file is reported as 404 by the caller's open/probe.

`dbWriteGuard` (`handler_db.go:131`) refuses writes to non-SQLite files or to
ocode's own data directory (`paths.GlobalDataDir`), resolved so a symlink cannot
dodge it. This is independent of the environment-template mechanism; expansion is
a path-shaping step, and the write guard is a mutation guard. They compose: a path
like `${DB_FILE}.db` expands first, then the expanded file is checked by the same
containment + write-guard pipeline.

## Security properties

- The resolved URL is never persisted (`dbconnect_env.go:77`), never returned
  (`handler_dbconnect.go:322` `FromEnv` — only the boolean flag), never logged
  (`dbconnect_env.go:86`; `ParseURL` never quotes the input), and never appears in
  an error message (`expand.go:63` names the variable, not a value; `dbconnect_env.go:87`
  validates the expanded URL with `ParseURL`, whose errors never include the URL).
- A missing `.env` is not an error; a malformed `.env` is logged without values and
the lookup falls back to the process environment (`dbconnect_env.go:40`), so the
connection fails with a clear "variable X is not set" rather than silently resolving
to a different URL.
- A sealed connection still requires the master password (`dbCheckMasterPassword`,
  `handler_dbconnect.go:307`). A template does NOT: it stores no secret, so requiring
  a password would only re-gate a value the project's own `.env` already exposes.
  The password-less unlock can therefore grant templates only; sealed connections
  stay locked (or open only when the correct password is supplied). **The master
  password protects stored literal URLs only.** A holder of the server auth token
  can add and load an environment connection without it and then read/write that
  database; token possession is the real boundary (the same token already opens a
  terminal). The `from_env` badge marks the connections that are password-free.
- A template cannot smuggle a literal credential into the clear:
  `TemplateHasLiteralSecret` (`internal/dbconnect/expand.go:207`) refuses a URL
  whose userinfo password or `password`/`sslpassword` query value is not composed
  entirely of references.
- `project_path` is validated against the server's allowed project roots, so a
  request cannot make the server read an arbitrary directory's `.env`; a remote
  project's path is refused because its `.env` lives on the other machine. A
  template with NO project is refused outright — there is no process-environment
  fallback.
- `ParseURL` (`internal/dbconnect/dbconnect.go:189`) refuses a raw `#`, parses the
  query the way pgx does (split on `&`, percent-decode the key, bad escape fails
  closed), and refuses `service`/`servicefile`/`passfile`/`sslkey`/`sslcert`/
  `sslrootcert` connection parameters (`dbconnect.go:160` redirect params,
  `dbconnect.go:189` ParseURL). A template is expanded from a repository's `.env`, so
  these could otherwise redirect the connection through a service file or make pgx
  read a local file and send its contents as the password.
- Precedence is process environment > `.env.local` > `.env`
  (`envfile.Lookup:225-230`). A variable exported where the server runs therefore
  shadows the project file; the UI says so.
- Containment is the last gate (`handler_db.go:119`): env expansion is never a way
around the allowed-roots boundary (`handler_db.go:89` comment).
- The server's key is `from_env`; the client maps it to `fromEnv` in `dbConnectList`
  (`web/src/api/client.ts`). A missing map made every env connection look sealed —
  real bug found in review.

## Forward-compatibility: the `db_env_templates` top-level key

A saved connection's environment template (`env_template`) lives in TWO places in
`ocodeconfig.json`, because the file is shared between ocode binaries of different
versions installed on the same machine. A binary that predates the `env_template`
field re-marshals the `"db"` section from its own narrower `DBConfig` struct when it
saves the config for any unrelated reason (changing a model, a pulse setting, etc.).
That silently DESTROYS the in-section field: the file goes from

```
{"name":"env","driver":"postgres","env_template":"${DATABASE_URL}"}
```

to `{"name":"env","driver":"postgres","url":""}`. The second copy is the
load-bearing one.

### What `db_env_templates` is (`internal/config/db_connections.go:35` `DBEnvTemplatesKey`)

`DBEnvTemplatesKey` (`"db_env_templates"`) is the **top-level** key in
`ocodeconfig.json`. It holds a flat name -> template map, for example:

```json
{"env":"${DATABASE_URL}"}
```

A top-level key that an older binary does not recognise lands in its `Extra` map
(the raw-JSON passthrough for unknown keys: `ocodeconfig.go:2299`) and is written
back **verbatim**, so the template survives any binary's save. The same mechanism
already preserves `last_model` (see `lastModelKey`). The in-section `env_template`
field (`DBConnection` struct, `db_connections.go:60`) does not survive by itself.

### Two copies, reconciled (`internal/config/db_connections.go:71` `dbEnvTemplates`)

The separate map is **authoritative** (`db_connections.go:98` `reconcileDBEnvTemplates`):

- **On load** (`ocodeconfig.go:2312` `reconcileDBEnvTemplates`): the map from
  `Extra` (`dbEnvTemplates(cfg.Extra)`) is copied onto the `"db"` connections
  (`reconcileDBEnvTemplates`). A connection whose `env_template` was stripped by
  an older binary is restored here. Only a connection with no `url` takes a template: a map entry under the name of a literal connection is stale (its connection was deleted by an older binary) and is ignored. A malformed map is logged and left on disk, and a save never deletes it because it failed to parse.
- **On save** (`ocodeconfig.go:2813` `dbEnvTemplatesCanonical`): the canonical
  map is rebuilt from the connections (`dbConnectionTemplates`, `db_connections.go:178`)
  and written AFTER the `Extra` loop (`ocodeconfig.go:2942`), so the canonical
  value always wins over any stale `Extra` copy (`ocodeconfig.go:2811` comment).
- When no template connection remains (`ocodeconfig.go:2956` `delete`): the key
  is removed entirely, so a stale map cannot resurrect a deleted connection.

The two copies never drift: the separate key is derived from the connections at
save time (`dbConnectionTemplates` iterates `cfg.DB.Connections` and collects
`EnvTemplate != ""`), and repaired back onto them at load time (`reconcileDBEnvTemplates`).

### Why both copies exist (`internal/config/db_connections.go:51` `DBConnection` doc)

- The **map** (`DBEnvTemplatesKey`) survives any binary's save — it is the one
  that protects the user's environment-backed connection.
- The **in-section field** (`DBConnection.EnvTemplate`) makes the file readable by
  a new binary that never learns about the separate key, so a user who only looks
  at the `"db"` section still sees the template.

Reading the template from the map at unlock time is the safe path (`DBConnection`
doc: "Read the template from the map, not from here: the map is the copy that
survives a save from an older binary.").

### Verified (`internal/config/db_connections_env_test.go`)

`db_connections_env_test.go` pins the property: a legacy binary's re-marshaled
`ocodeconfig.json` keeps the `db_env_templates` key intact (`test:125`) while
stripping `env_template` from the `"db"` section; the next load by the new binary
recovers the connection from the map (`test:203`) and removes the key when the
last template connection is deleted (`test:214`).

### Regression: mutation-verified (`scripts/mutcheck_dbenv.py`)

A mutation-testing script (`scripts/mutcheck_dbenv.py`) verifies the mechanism by
temporarily removing the canonical map assignment (`ocodeconfig.go:2947`) and
confirming the regression tests fail; removing the `reconcileDBEnvTemplates` call
(`ocodeconfig.go:2312`) also fails the repair tests. Both mutations are verified to
compile (`mutcheck_dbenv.py:137-151`).


## Where the feature is used in the UI (`DBPanel.tsx`)

The DB connector is a **project-level top tab** (beside Assets in `TopTabs.tsx`),
not a session sub-tab: its environment-backed connections resolve against the
active project, so it must not be tied to one chat session. `DBPanel`
(`DBPanel.tsx:64` `newDbPanelSurfaceID`) uses
`surface = db-panel:<host>:<projectPath>:<document tag>-<counter>`
(`DBPanel.tsx:75`). The id is now unique per browser document AND per mount: the
document tag is a per-document random (8 hex chars) and the counter is per mount.
A plain module counter was NOT enough — module scope is per document, so every
browser window restarted at 1 and two windows on one project collided on `p1`,
and one window's unmount revoked the other's grant (and closed its pools). That
was a real bug found in review. `App.tsx` also keys the panel by `host:path` and
mounts it only for projects whose DB tab was actually visited.

An environment-backed connection (`fromEnv`) gets a **"Load from .env"** action
with no password field (`DBPanel.tsx:498`): the unlock call sends the empty
password plus `project_path` (`DBPanel.tsx:285`), and the server grants only the
template connections. The button carries `aria-busy` while loading. A remote
project (non-empty host) does NOT get the action; it shows "Not available for a
remote project" (`DBPanel.tsx:479`), because this endpoint is not host-threaded and
its `.env` lives on the other machine. A sealed connection keeps the
master-password form. The add form mirrors the rule: `isEnvTemplate(url)`
(`DBPanel.tsx:47`, the server's exact `${[A-Za-z_][A-Za-z0-9_]*}` rule) disables the
password field and lets Save proceed without it (`DBPanel.tsx:800`). The connection
picker shows the `from_env` flag (`DBPanel.tsx:379`) and a tooltip explaining it.
A partial unlock lists the failed connections in a warning (`DBPanel.tsx:399`). A
failed connection-list load shows the error and a Retry instead of an endless
spinner (`DBPanel.tsx:344` error branch / `DBPanel.tsx:347` Retry button).

The panel is no longer mounted when there is no active project: it renders "Select
a project to use the database connector." instead. The add form now says an env URL
is not usable for a remote project.

Tests: `DBPanel.test.tsx` (22 cases, including the password-less environment flow,
partial failures, retry and the remote guard — and it now also asserts that the
GRANTED connection actually loads its tables), `web/src/api/client.dbConnect.test.ts`
(wire mapping: `from_env` -> `fromEnv`, `project_path`, null normalisation) and
`web/src/App.dbTab.test.tsx` (no mount before the tab is opened, mount path,
project-switch remount, no-project empty state).
