# Part 01 — Architecture & Shared Foundations

Self-contained. Defines the backend model, transport, provisioning,
credential sync channel, progress/error reporting, and security invariants
shared by all phases of ocode Remote.

## Backend model

A remote backend = a host where the ocode binary runs, reached by a
transport. One abstraction, two transports:

- **ssh** — any host the user's system `ssh` can reach. Host definitions
  come from `~/.ssh/config`; ocode keeps **no parallel host registry**.
  Target syntax: `[user@]host` (anything ssh accepts as a destination).
- **wsl** — on Windows, a local distro launched via `wsl.exe -d <distro>`.
  Target syntax: `wsl:<distro>` (`wsl:` alone = default distro).

New package `internal/remote`:

```
internal/remote/
  target.go      // parse "[user@]host" | "wsl:<distro>" → Target
  transport.go   // Transport interface: Exec, ExecInteractive, Copy
  ssh.go         // Transport impl shelling out to system ssh/scp
  wsl.go         // Transport impl shelling out to wsl.exe (Phase 3)
  provision.go   // detect platform, cross-compile, install, verify
  sync.go        // credential/config sync framing + push
  serve.go       // remote serve launch, state file, tunnel (Phase 2)
  multiplex.go   // tmux/screen detection + launch-command wrapping (resume)
  progress.go    // staged progress reporting (see below)
```

The `Transport` interface is the only seam between phases: Phase 3 is a
second implementation, nothing more.

## Transport: system ssh, never x/crypto/ssh

Shell out to the system `ssh`/`scp` binaries (as VS Code Remote-SSH does).
Rationale: inherits ssh_config, agent auth, hardware keys, ProxyJump,
ControlMaster, and known_hosts verification for free. Host authenticity is
system-ssh known_hosts behavior, unmodified.

All spawned processes register with the existing process supervisor
(`internal/tool/process_supervisor.go`) — no new uncoordinated spawn
sites (ocode architectural rule).

## Provisioning

**No release-artifact downloader.** Verified 2026-08-29: releases are
local `make release` builds only; no published per-platform download URLs
exist. The module is cgo-free (modernc.org/sqlite), so plain
`GOOS=<os> GOARCH=<arch> go build` cross-compiles from any dev machine —
`make build-linux` already does exactly this. Therefore:

1. Detect remote platform: `uname -sm` over the transport → GOOS/GOARCH.
   Unsupported platform → named error, stop.
2. Check `~/.ocode/bin/<version>/ocode` exists and is executable remotely
   (`test -x`). `<version>` = the local client's version string
   (dev builds included — a dev version simply gets its own dir).
3. If missing: cross-compile locally for the remote platform
   (equivalent of `GOOS=… GOARCH=… go build` with the repo's LDFLAGS
   when run from a source checkout; when the local binary already matches
   the remote platform, reuse it), then `scp` to a temp name and
   atomically `mv` into place, `chmod +x`, and re-verify by running
   `ocode --version` remotely.
4. Binary GC: after a successful install, delete all but the two newest
   version dirs under `~/.ocode/bin/`.

Installed binaries never appear on `$PATH`; the client always invokes the
full versioned path, so client and remote can never drift.

## Session resume on disconnect (TUI)

A raw `ssh -t <host> <remote-ocode> <path>` passthrough has no survival story:
a dropped connection (network blip, laptop sleep, `ssh` client killed) sends
SIGHUP to the remote process group and the in-progress TUI session — and
whatever the agent was mid-turn on — dies with it. Reconnecting starts a
brand new process with no memory of the old one.

Decision: wrap the launch stage in a terminal multiplexer already on the
remote host, keyed by the resolved remote project path so the same project
always reattaches to the same session:

- `tmux` present → `tmux new-session -A -s ocode-<sha256(remote-path)[:12]>
  <remote-ocode> <path>`. `-A` attaches if the session exists, else creates
  it — one code path for both first-connect and reattach.
- else `screen` present → `screen -xRR ocode-<sha256(remote-path)[:12]>
  <remote-ocode> <path>`. `-xRR` reattaches (multi-display if already
  attached elsewhere) or creates.
- else → fall back to the plain passthrough, and the progress output prints
  a one-line non-fatal warning: no resume on disconnect, with a hint to
  install tmux on the remote.

Why shell out to an existing multiplexer rather than build one: the
codebase already leans on system tools for exactly this reason (`ssh`/`scp`
instead of `x/crypto/ssh`) — tmux/screen already solve PTY ownership,
resize propagation (`SIGWINCH`), scrollback, and multi-client attach
correctly, and doing so needs zero changes to `internal/tui` or the
provisioning story (no new binary to ship, no custom attach protocol over a
socket). The cost is that a remote with neither tool installed gets no
resume — accepted, since it degrades to today's behavior with a visible
warning rather than a silent gap.

Detection runs once per connect (`ssh <host> 'command -v tmux ||
command -v screen'`), cheap enough not to warrant caching. Multiple local
clients reattaching to the same session concurrently is allowed (tmux
mirrors, `screen -x` mirrors) — not treated as an error.

This only applies to the TUI passthrough. Phase 2 (web) already has an
independent, stronger resume story: the remote server is a detached
long-lived process (state file + reuse, see Part 03) that outlives any one
SSH tunnel by design — the multiplexer wrapping here is additive for TUI
mode only and never applies to `--web`.

## Credential & config sync channel

At connect time: push auth profiles + core model config to the remote —
piped to `<remote-ocode> remote-receive-config` on **stdin** as a framed
JSON payload (`{version, sha256, payload}`). The remote validates framing
and checksum, then writes each file `0600` via temp-file + rename (same
protections as `internal/auth/profile_store.go`). Secrets never appear in
argv, environment listings, logs, or LLM traffic.

Skip-if-unchanged: the local client caches the last-pushed payload hash
per host in `<OcodeGlobalDataDir>/remote-sync.json` (`internal/paths.OcodeGlobalDataDir`,
i.e. `~/.local/share/ocode/remote-sync.json` — the existing ocode-only
sidecar dir, not a new config-dir helper); identical hash → skip the
push entirely. `--no-sync` disables sync for a connect.

Security tests are mandatory, one each: framing rejection, checksum
rejection, permission bits, partial-write rejection, argv/log absence.

## Progress & error reporting (user requirement)

Every connect renders **staged progress in the invoking terminal** (TUI
passthrough and web mode both start life as a CLI invocation, so the
terminal is always available before handoff):

```
Connecting to devbox…
  ✓ ssh reachable
  ✓ platform: linux/arm64
  ⠸ building ocode v0.9.3 for linux/arm64…
  ✓ uploaded (14.2 MB)
  ✓ credentials synced
  ✓ launching remote TUI
```

On failure, the output names the **failing stage** and prints the
underlying `ssh`/`scp`/`go build` stderr **verbatim** — never a swallowed
or paraphrased error — plus one actionable hint when the cause is known
(e.g. arch unsupported, `go` not found for cross-compile from a
non-source install). Non-TTY invocations print the same stages as plain
lines. In web mode the same stages print before the browser opens; a
failure means the browser never opens and the error is the terminal
output. Every stage failure exits non-zero.

## Security invariants (all phases)

1. Remote `ocode serve` binds `127.0.0.1` only; the SSH tunnel is the
   only ingress (Phase 2).
2. API token required on every server request when launched in remote
   mode; token never passed via argv or query string.
3. Synced secrets: stdin-only transfer, `0600`, temp+rename.
4. Host authenticity: system ssh known_hosts, unmodified.
5. All remote-launched processes are children of the tracked ssh/wsl
   process — killing the local supervisor entry tears down the chain
   (serve is the exception; it outlives the connect, see Part 03. The TUI
   under tmux/screen is a second, narrower exception: killing the local
   `ssh -t` only detaches the multiplexer client, the remote session and
   the ocode process inside it keep running by design — that is the
   resume mechanism, not a leak).

## Sessions & projects

Sessions are never synced; they live on the remote under the remote
`$HOME`, so per-host session storage is automatic and there is no
path-collision handling. Local `internal/projects` recent-project entries
gain an optional `host` field (empty = local, backward compatible) so
remote projects appear in pickers and reconnect in one action.
