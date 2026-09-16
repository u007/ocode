## Design: Desktop Bash Tool — Load Full Env via Configured Login Shell

**Status: implemented 2026-09-16** (`internal/tool/bash_build.go` `SetLoginShell`
+ `cmd/ocode-desktop/main.go` `configureLoginShell`). One deviation from the
original design below: there is no `shell` key in `ocodeconfig.json` — the
desktop boot resolves the login shell at startup ($SHELL when it is
zsh/bash/sh, else /bin/zsh, else /bin/bash; exotic shells are skipped because
agent commands are POSIX/bash syntax) instead of adding a new config field.
The outcome (full user PATH inside agent bash commands) is unchanged.

### Goal
When the desktop `.app` launches (no shell profile loaded), the bash tool must load the user's full environment (`PATH`, etc.) using the configured login shell from global ocode config.

### Design
- Read `shell` from global `ocodeconfig.json` (default: `zsh`).
- Bash execution invokes `exec.Command(configuredShell, "-l", "-c", command)` instead of `bash -c`.
- This loads `.zshrc` / `.bash_profile` / `.profile`, making `~/Library/pnpm` and other user `PATH` entries available to `gws`.

### Scope
Desktop binary only; does not change server or web mode behavior.

### Trade-offs
- `-l` is slightly slower (spawns login shell) but accurate.
- Falls back to `bash -l` if config missing.
