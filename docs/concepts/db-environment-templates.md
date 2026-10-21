---
type: Concept
title: DB Environment Templates (.env / .env.local for connections and SQLite paths)
description: |-
  A Postgres connection URL or a SQLite file path may contain ${NAME} or $NAME
  references, resolved per session against the session's project root (.env, then
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
timestamp: 2026-10-21T00:00:00Z
resource: internal/envfile/envfile.go; internal/dbconnect/expand.go; internal/config/db_connections.go; internal/server/dbconnect_env.go; internal/server/handler_dbconnect.go; internal/server/handler_db.go; web/src/components/Layout/DBPanel.tsx
---

# DB Environment Templates

A connection URL (`postgres://...`) or a SQLite file path (`data/*.sqlite`) may
contain environment-variable references. The value is expanded at the point where
the feature needs it — unlock time for a Postgres connection, path-resolution time
for a SQLite file — against the environment of the chat session's project.

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

`envfile` defines the dotenv parser (`envfile.go:39` `Parse`) and the loader
(`envfile.go:167` `Load`). The constants (`envfile.go:24`):

- `FileName = ".env"`
- `LocalFileName = ".env.local"`

`Load(dir)` (`envfile.go:175`) reads `.env` first and `.env.local` second; a
key in `.env.local` overrides the same key in `.env`. A missing file is **not**
an error (`envfile.go:195` `loadFile`: `os.IsNotExist` yields an empty map). A file
that exists but cannot be parsed **is** an error (`envfile.go:201`): the caller logs
it and continues with the process environment only, so the connection fails with
an explicit "not set" error naming the variable (`dbconnect_env.go:45`).

The dotenv format (`envfile.go:8`): `KEY=VALUE`, optional `export ` prefix
(`envfile.go:71` `strings.CutPrefix`), `#` comments, blank lines skipped, single-quoted
values (literal, no escape processing — `envfile.go:121`), double-quoted values with
`\n`, `\t`, `\r`, `\\` unescaped (`envfile.go:138` `unescapeDouble`). The first
`=` outside quotes splits key from value (`envfile.go:95` `indexUnquoted`). A quoted
value may contain `=`. A duplicate key in one file is a parse error (`envfile.go:53`
`fmt.Errorf("line %d: duplicate key %q", ...)`). There is no multi-line value, no
interpolation inside a file (`envfile.md:10`), and no command substitution.

`Lookup(name, fileValues)` (`envfile.go:218`) gives the precedence:

1. Process environment (`os.LookupEnv`) — first.
2. `.env.local` (merged over `.env` by `Load`).
3. `.env`.

So: **process env > `.env.local` > `.env`**. A value exported in the shell (or
injected by CI) always wins over the file; `.env.local` still overrides the
committed `.env`. A variable that is set but EMPTY counts as set (`envfile.go:215`
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

`HandleDBConnectUnlock` (`handler_dbconnect.go:292`) applies this per session:
`projectRoot := h.sessionProjectRoot(req.SessionID)` (`handler_dbconnect.go:316`).
The session id is optional (`handler_dbconnect.go:295` `SessionID` field); without
it, only the process environment is consulted. Every saved connection is iterated
once (`handler_dbconnect.go:329`): if `c.EnvTemplate != ""` it is resolved against
that session's project; if `c.EncryptedURL != ""` it is opened with the master
password (`dbconnect.OpenURL`). The master password is still **required** for the
whole set (`handler_dbconnect.go:256` `dbCheckMasterPassword`): the flow stays
uniform, and a connection whose name a stranger could guess is still gated on a
secret. A wrong password is rejected with 401 ("master password does not open
connection ...", `handler_dbconnect.go:345`) **before** any template is resolved.
The grant (`handler_dbconnect.go:354`) holds the plaintext URL in memory only
(`handler_dbconnect.go:45` "Grants are in memory only"), keyed by surface id
(`db-panel:<sessionId>` in `DBPanel.tsx:40`).

`HandleDBConnectAdd` (`handler_dbconnect.go:190`) stores the connection. The URL
field (`req.URL`) is examined with `dbconnect.IsTemplate(req.URL)`
(`handler_dbconnect.go:224`): a template is saved as `EnvTemplate` in the clear
(`config.DBConnection`); a literal URL is sealed (`dbconnect.SealURL`) and saved
as `EncryptedURL`. Exactly one of the two must be set (`internal/config/db_connections.go:57`
`AddDBConnection`): neither (`url` and `env_template` both empty) or both
(ambiguous which wins at unlock) is an error. The saved connection never holds a
plaintext URL.

`HandleDBConnectList` (`handler_dbconnect.go:151`) returns `FromEnv: c.EnvTemplate != ""`
(`handler_dbconnect.go:178`) — the "env" badge in the UI — and `Unlocked` from the
grant. The template itself (`EnvTemplate`) is **not** returned: it names variables,
but the resolved URL is a credential and stays on the server (`handler_dbconnect.go:167`
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
  (`handler_dbconnect.go:163` `FromEnv` — only the boolean flag), never logged
  (`dbconnect_env.go:86`; `ParseURL` never quotes the input), and never appears in
  an error message (`expand.go:63` names the variable, not a value; `dbconnect_env.go:87`
  validates the expanded URL with `ParseURL`, whose errors never include the URL).
- A missing `.env` is not an error; a malformed `.env` is logged without values and
the lookup falls back to the process environment (`dbconnect_env.go:40`), so the
connection fails with a clear "variable X is not set" rather than silently resolving
to a different URL.
- The master password is still required (`dbCheckMasterPassword`, `handler_dbconnect.go:256`):
even a template-only connection set needs the password, so a stranger who guesses a
connection name cannot open it.
- Containment is the last gate (`handler_db.go:119`): env expansion is never a way
around the allowed-roots boundary (`handler_db.go:89` comment).

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

### Two copies, reconciled (`internal/config/db_connections.go:70` `dbEnvTemplates`)

The separate map is **authoritative** (`db_connections.go:87` `reconcileDBEnvTemplates`):

- **On load** (`ocodeconfig.go:2308` `reconcileDBEnvTemplates`): the map from
  `Extra` (`dbEnvTemplates(cfg.Extra)`) is copied onto the `"db"` connections
  (`reconcileDBEnvTemplates`). A connection whose `env_template` was stripped by
  an older binary is restored here.
- **On save** (`ocodeconfig.go:2809` `dbEnvTemplatesCanonical`): the canonical
  map is rebuilt from the connections (`dbConnectionTemplates`, `db_connections.go:172`)
  and written AFTER the `Extra` loop (`ocodeconfig.go:2938`), so the canonical
  value always wins over any stale `Extra` copy (`ocodeconfig.go:2801` comment).
- When no template connection remains (`ocodeconfig.go:2945` `delete`): the key
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
temporarily removing the canonical map assignment (`ocodeconfig.go:2940`) and
confirming the regression tests fail; removing the `reconcileDBEnvTemplates` call
(`ocodeconfig.go:2308`) also fails the repair tests. Both mutations are verified to
compile (`mutcheck_dbenv.py:137-151`).


## Where the feature is used in the UI (`DBPanel.tsx`)

`DBPanel` (`DBPanel.tsx:38`) uses `surface = db-panel:${sessionId}` (`DBPanel.tsx:40`).
The unlock call (`DBPanel.tsx` around the unlock handler call) passes `session_id`
so the server resolves templates against this session's project. The add form
(`DBPanel.tsx` around the add handler) still sends the session id, but the add handler no longer reads it. `IsTemplate`
looks only at braced references when deciding whether `req.URL` is a template
(`handler_dbconnect.go:224`). The connection picker (`DBPanel.tsx`) shows the
`from_env` flag (`DBPanel.tsx:264`) and a tooltip explaining it (`DBPanel.tsx:308`
"env" badge; `DBPanel.tsx:590` note about `.env` / `.env.local`).
