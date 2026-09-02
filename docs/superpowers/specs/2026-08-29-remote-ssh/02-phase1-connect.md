# Part 02 — Phase 1: Connect & Work

Self-contained. Goal: **SSH into a host and have a fully working remote
TUI in one command.** Success metric: fresh Linux VM → one command →
remote TUI answers an LLM prompt, zero manual steps on the remote.

## Command

```
ocode remote <[user@]host> [path] [--no-sync]
```

Flow (each stage rendered as live progress in the terminal; any failure
names the stage, prints the underlying stderr verbatim, exits non-zero —
see the progress contract below):

1. **Reachability** — `ssh <host> true`. Failure surfaces ssh's own
   stderr (auth, known_hosts, DNS) untouched.
2. **Platform detect** — `ssh <host> 'uname -sm'` → GOOS/GOARCH.
3. **Ensure binary** — `test -x ~/.ocode/bin/<local-version>/ocode`
   remotely. If missing:
   - local binary already matches remote platform → use it as-is;
   - else cross-compile locally: `GOOS=… GOARCH=… go build` with repo
     LDFLAGS (requires running from a source checkout or a local Go
     toolchain; if neither is available, fail with a named error telling
     the user to install Go or run from the ocode repo);
   - `scp` to `~/.ocode/bin/<version>/.ocode.partial`, `chmod +x`,
     atomic `mv` to `ocode`, verify with remote `ocode --version`;
   - GC: keep the two newest version dirs under `~/.ocode/bin/`.
4. **Credential sync** (unless `--no-sync`) — pipe framed JSON
   (auth profiles + core model config, `{version, sha256, payload}`) to
   `<remote-ocode> remote-receive-config` on stdin; remote validates
   checksum and writes files `0600` via temp+rename. Skipped when the
   payload hash matches the per-host cache in
   `<OcodeGlobalDataDir>/remote-sync.json` (see `01-architecture.md`).
5. **Multiplex detect** — `ssh <host> 'command -v tmux || command -v
   screen'`. Determines how stage 6 wraps the launch command; see
   "Session resume on disconnect" in `01-architecture.md`.
6. **Launch** — run (not exec-replace, so the local process stays alive
   to supervise/relaunch on a dropped session — see below):
   - tmux found: `ssh -t <host> tmux new-session -A -s
     ocode-<sha256(remote-path)[:12]> <remote-ocode> <path>`
   - else screen found: `ssh -t <host> screen -xRR
     ocode-<sha256(remote-path)[:12]> <remote-ocode> <path>`
   - else: `ssh -t <host> <remote-ocode> <path>`, with a one-line warning
     already printed at stage 5 that this connection will not survive a
     disconnect.
   From here the remote TUI owns the terminal. If neither multiplexer was
   found, the local `ssh -t` exiting ends the session for good (today's
   behavior). If one was found, the local `ssh -t` exiting (network drop,
   `ssh` killed) only detaches the multiplexer client — the remote
   session and the ocode process inside it keep running. Rerunning
   `ocode remote <host> [path]` re-executes stages 1-6; stage 6 reattaches
   to the same session instead of starting a new one, because the session
   name is deterministic from the resolved remote path.

## Path resolution

- Explicit `[path]` → used verbatim (remote-side expansion of `~`).
- Omitted → the most recent `internal/projects` entry whose `host`
  matches this target; none → remote `$HOME`.
- On successful launch, upsert the recent-projects entry
  `{path, host, lastOpened}` locally so the picker can reconnect in one
  action.

## New/changed code

| Area | Change |
|---|---|
| `internal/remote` (new) | `target.go`, `transport.go`, `ssh.go`, `provision.go`, `sync.go`, `multiplex.go`, `progress.go` |
| CLI (`main.go` / `internal/runcli` pattern) | `remote` subcommand; hidden `remote-receive-config` subcommand |
| `internal/auth` | export a payload builder for sync (profiles + core model config), reusing profile-store read/write with `0600` |
| `internal/projects` | optional `host` field on `Project`; picker rows show `host:path`; selecting a remote entry re-runs the connect flow |
| Process spawning | all `ssh`/`scp`/`go build` invocations registered with the process supervisor |

`remote-receive-config` reads stdin only, validates framing + sha256,
rejects partial/oversized payloads, writes temp+rename `0600`, prints a
single machine-readable OK/error line. It must never log payload
contents.

## Progress contract (user requirement)

Stages: `reachable → platform → build → upload → verify → sync → multiplex
→ launch`.
TTY: spinner + checkmarks, updated in place. Non-TTY: one plain line per
stage. Failure output = failing stage name + verbatim underlying stderr +
one actionable hint when the cause is recognized. No stage output is ever
swallowed; exit code is non-zero on any failure.

## Error handling

- No matching toolchain for cross-compile → named error, nothing
  uploaded.
- Upload interrupted → `.ocode.partial` left behind is harmless; next
  connect overwrites it; the atomic `mv` guarantees `ocode` is never a
  truncated binary.
- `remote-receive-config` failure → connect **continues** to launch
  (the TUI may still work if the remote already has keys) but the sync
  stage is marked failed in the progress output with the remote's error
  line; the payload-hash cache is not updated.
- Remote `ocode --version` mismatch after install → treated as install
  failure (named error), never silently launched.
- Multiplex detect failure (e.g. the detect command itself errors rather
  than simply finding nothing) → treated the same as "neither found":
  degrade to plain passthrough with the warning, never fail the connect
  over it.

## Testing

- Unit: target parsing (`user@host`, `host`, rejection of `wsl:` until
  Phase 3 lands), `uname -sm` → GOOS/GOARCH mapping, install command
  construction, GC selection (keep-two-newest), sync payload
  framing/checksum/partial-write rejection, per-host hash cache,
  progress renderer TTY + non-TTY, multiplex command construction (tmux
  found / screen found / neither), session-name determinism (same remote
  path → same name across two separate connect invocations).
- Security (mandatory, one each): framing rejection, checksum rejection,
  `0600` bits, partial-write rejection, secrets absent from argv/logs.
- Integration (flag-gated): full connect → provision → sync → launch
  against a local sshd container (linux/amd64 or arm64 to also exercise
  cross-compile from macOS).
