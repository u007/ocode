# Desktop LSP toolchain path design

Date: 2026-09-25
Status: approved for implementation

## Goal

Make the desktop process discover installed LSP binaries consistently across macOS, Linux, and Windows, including GUI launches that do not inherit the user's full executable PATH.

## Design

- Add `desktop.EnsureExecutablePath()` and call it before the in-process server starts.
- On macOS/Linux, probe the supported login shell (`zsh`, `bash`, or `sh`) with a bounded, marker-delimited `PATH` query; merge that result after the inherited process PATH.
- On every platform, add existing conventional user tool directories that are not shell-managed: Go, Cargo, local bin, pnpm, and platform equivalents; on Windows also include npm, Scoop, Chocolatey, and WindowsApps locations.
- Deduplicate entries with Windows-aware case-insensitive comparison and never replace the inherited PATH wholesale.
- Leave LSP lookup itself unchanged: after startup hydration, `exec.LookPath` remains the single discovery mechanism for all server tools.
- Keep `config.EnsureUserBinPath()` as a defensive fallback for `~/.local/bin` and `~/bin`.
- Cold headless session status creates the project manager, and lifecycle rows
  retain `starting`/`failed` states until a server becomes ready or the manager
  is closed.
- Canonicalize roots for manager identity/filtering while preserving the
  session's established CWD spelling in the response.

## Error handling

- The login-shell probe is bounded by a short timeout.
- Probe failures are logged with the attempted shell and returned reason, then startup continues with the inherited PATH and conventional directories.
- A failure to set the process environment is logged; desktop startup continues.

## Tests

- Failing-first tests cover Unix login-shell PATH merging, conventional Go/Cargo/NVM directories, Windows user-tool directories without invoking a shell, and probe failure fallback.
- Focused `internal/desktop`, `internal/config`, and `internal/lsp` tests run on the host.
- Cross-platform compilation is checked with `GOOS=windows` and `GOOS=darwin` where feasible.
