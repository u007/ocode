---
type: Gotcha
title: 'Remote connection identity is user+host+port, never Target.String() - port-blind cache keys, the per-connection exec-slot pool, and the 6-of-8 foreground reserve'
description: 'Gotcha: remote.Target.String() omits the SSH port, so every cache keyed on it collapses distinct connections onto one entry - the ssh ControlPath hash (a command aimed at port 2222 can execute on port 22 multiplexed master), the exec-slot pool, and the still-unfixed host registry. The pool now keys on user+host+port and reserves 6 of its 8 slots for short-lived foreground work so a host full of long remote shell commands cannot starve the 10s git_status poll; long commands are refused at the sub-cap rather than queued, and the refusal message reaches the client verbatim.'
resource: internal/server/handler_remote_work.go; internal/remote/execcmd.go; internal/remote/target.go; internal/server/remote_hosts.go
tags:
  - gotcha
  - remote
  - ssh
  - concurrency
  - exec-slots
  - controlpath
  - git
timestamp: 2026-09-29T11:26:38Z
---

# Remote connection identity is user+host+port, never `Target.String()`

`remote.Target.String()` returns `[user@]host` (or `wsl:<distro>`) and
deliberately **omits the SSH port**. That is correct for the *project identity*
the store persists — `Project.Host` plus a separate `RemotePort` field — and
every ssh command site compensates by passing `-p` explicitly.

It is the **wrong** key for anything that caches a live connection, because a
connection is identified by user + host + **port**, and `String()` collapses
distinct connections onto one string.

## What went wrong

Three caches keyed on the port-free string, so two projects on the same host at
different ports (or two different users) shared one entry:

1. `sshControlSocket` (`internal/remote/execcmd.go`) hashed `t.String()` into
   the `ControlPath`. ssh multiplexes onto an existing master **without checking
   it matches the requested host/port**, so a command aimed at port 2222 could
   execute on port 22's connection. Now hashes `sshConnectionIdentity`
   (user+host+port).
2. The exec-slot pool (`internal/server/handler_remote_work.go`) was keyed on
   `t.String()`. Each connection has its own sshd `MaxSessions` budget, so one
   pool covering several connections let unrelated projects throttle each other.
   Now keyed by `remoteSlotKey` → `remoteConnectionKey`.
3. The host registry (`remoteHostRegistry`, `internal/server/remote_hosts.go`)
   keyed entries on the bare host string. The port was applied only when the
   entry was *created*, so the first port to connect served every later request
   for that host — including that connection's API URL, bearer token and remote
   project registration. Fixed: entries are keyed by `remoteConnectionKey`, and
   `drop`, `status`, `snapshotForRestart`, `proxyFor`, `isRegistered` and
   `markRegistered` all take the port. `registeredPaths` is port-scoped too,
   because registration happens on one specific remote server.

Note the registry fix is not merely about isolation. Sharing an entry across
ports meant port 2222's git/chat traffic was proxied to the port-22 server with
the wrong token, and a path registered on port 22 was never registered on port
2222 — the project would silently not exist there.

## Rule

Any map, mutex, semaphore, socket path or "is this the same connection?" check
keyed on a `remote.Target` must use a **connection** identity, not the project
identity. `remoteSlotKey` (user+host+port) is the reference implementation. When
adding a new per-connection cache, copy that rather than reaching for
`Target.String()`.

A useful tell: if the code asks "have I already connected to *this* target?"
rather than "is this *the same project* as the one on screen?", it wants the
connection key.

`remoteConnectionKey` (`internal/server`) and `sshConnectionIdentity`
(`internal/remote`) are the two implementations; they are in separate packages
because `internal/server` imports `internal/remote`, never the reverse.
`remoteSlotKey` delegates to `remoteConnectionKey`, and
`TestSSHControlSocketIdentityMatchesServerKey` pins that the two packages agree —
so change both or neither.

**An unspecified port is deliberately NOT the same key as an explicit 22.** With
no `-p`, ssh resolves the port from `~/.ssh/config`, which may remap the host's
default away from 22; treating "unspecified" as "22" would let a config-dialed
connection share a master with an explicit `-p 22` command. The safe direction is
more keys: splitting duplicates a master, whereas merging runs commands on the
wrong connection.

## The 8-slot pool, and why 6 of 8

The pool caps concurrent execs per connection because sshd's `MaxSessions`
(default 10) otherwise refuses new sessions outright. 8 leaves headroom.

A sub-cap reserves 6 of the 8 for short-lived **foreground** work (git status,
diff, file reads, the shell probe) and caps **long-running** work (remote shell
commands, git network ops) at 6. Rationale: the `git_status` emitter polls every
10s per viewed project, so a pool held entirely by long commands stales the whole
Git panel for that host while every starved poll burns its own 30s bound.

Two properties matter, and both are mutation-verified:

- The reserve is a **sub-cap on the same pool**, not a second pool. Total
  concurrency still cannot exceed 8, so sshd's `MaxSessions` is still respected;
  the reserve only decides who may take the last slots.
- A long-running command is **refused** at the sub-cap, not queued behind
  foreground work. Queueing would let N long commands reclaim the whole pool the
  moment foreground work drained — the starvation would just be delayed, not
  prevented. Refusing keeps the reserve intact. The refusal is a normal outcome,
  not a transport fault, and its message reaches the client verbatim in the
  `error` field of the `/api/shell` response.

Mutation-testing caveat: a third variant that only *reorders* the `all`-pool wait
while leaving the `long` sub-cap gate in front of it is an **equivalent mutant**
— the sub-cap still refuses, so the observed behaviour is identical and no test
can catch it. The two properties above are each pinned by their own case; do not
read a surviving reordering as an untested branch.

## Regression coverage

`internal/server/remote_exec_slots_test.go`:
`TestRemoteExecSlotsSeparatePorts`, `TestRemoteExecSlotsSeparateUsers`,
`TestRemoteExecSlotsReserveForForeground`, plus the pre-existing
`TestRemoteExecSlotsCapPerHost` and `TestRunBoundedSlotWaitCountsTowardTimeout`.

## Related

- `docs/gotchas/remote-project-path-trust-boundary.md` — the `host` component
  must be valid ssh *destination syntax* (no leading `-`), which is a
  local-command-execution primitive, not just a targeting rule.
- CHANGES.md 2026-09-29 — "Remote target hardening".
