# Part 04 — Phase 3: WSL Backend

Self-contained. Goal: **on Windows, `ocode remote wsl:<distro>` gives a
working ocode inside the distro** — TUI and web mode. Success metric:
Windows machine → `ocode remote wsl:Ubuntu` opens the remote TUI;
`--web` variant opens the browser UI. This phase is a transport swap:
the connect flow (platform detect → ensure binary → credential sync →
launch, with staged progress and verbatim error surfacing) is the one
shipped in Phase 1; only the command carrier changes.

## Target syntax

- `wsl:<distro>` — named distro (`wsl.exe -d <distro>`).
- `wsl:` — default distro (`wsl.exe` with no `-d`).
- Only valid when the local OS is Windows; elsewhere → named error.
- Distro existence is not pre-validated; `wsl.exe`'s own error output is
  surfaced verbatim, consistent with the ssh transport's
  show-the-underlying-stderr rule.

## Transport implementation (`internal/remote/wsl.go`)

Implements the same `Transport` interface as ssh:

| Operation | ssh transport | wsl transport |
|---|---|---|
| Exec | `ssh <host> <cmd>` | `wsl.exe -d <distro> -- sh -c <cmd>` |
| ExecInteractive | `ssh -t <host> <cmd>` | `wsl.exe -d <distro> -- <cmd>` (wsl.exe allocates the console natively) |
| Copy | `scp` | copy via `\\wsl$\<distro>\…` UNC path (or stream through stdin: `wsl.exe -- sh -c 'cat > dest'`) — pick one in implementation, stdin-stream is the fallback if UNC proves flaky |

All `wsl.exe` invocations register with the process supervisor, same as
ssh.

## Provisioning

WSL distros are always Linux; `uname -m` inside the distro gives the
arch (x86_64/aarch64). The local Windows ocode cross-compiles
`GOOS=linux GOARCH=<arch>` (module is cgo-free) and copies to
`~/.ocode/bin/<version>/ocode` inside the distro — identical layout,
verification (`ocode --version`), atomic install, and keep-two-newest GC
as the ssh path. The Windows binary is never executed inside WSL.

## Credential sync

Identical framed-stdin channel:
`wsl.exe -d <distro> -- <remote-ocode> remote-receive-config` with the
payload on stdin. Same `0600`, temp+rename, checksum, per-host hash
cache (cache key is `wsl:<distro>`), same `--no-sync` flag.

## Web mode

`ocode remote --web wsl:<distro>`: launch `<remote-ocode> serve --remote
--host 127.0.0.1 --port 0` inside the distro (detached, state file
`~/.ocode/remote/serve.json` inside the distro, same reuse/stale logic
as ssh). **No tunnel**: Windows forwards localhost into WSL2 natively,
so the browser opens `http://localhost:<port>/#token=<token>` directly.
Token conveyance, header-only auth, and session resume behavior are
exactly the Phase 2 design.

## Error handling

- `wsl.exe` absent or WSL not enabled → named error with enablement
  hint.
- Distro not found / not running → `wsl.exe` stderr verbatim.
- UNC copy failure → fall back to stdin-stream copy; both fail →
  provisioning stage error, nothing half-installed (temp + atomic move).
- WSL1 (no localhost forwarding differences apply to WSL2 only; WSL1
  shares the network stack directly) — both work; no detection needed.

## Testing

- Unit (run on any OS): `wsl:` target parsing, wsl command construction
  for Exec/ExecInteractive/Copy, non-Windows rejection, cache-key
  derivation.
- Manual verification matrix on a Windows machine (no Windows CI):
  TUI connect, web connect, credential sync lands `0600`, server reuse
  after closing the browser. Recorded as a checklist in the PR.
