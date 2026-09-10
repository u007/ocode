## Design: Desktop Bash Tool — Load Full Env via Configured Login Shell

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
