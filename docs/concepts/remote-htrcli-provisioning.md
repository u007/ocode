---
type: Concept
title: Remote htrcli provisioning
description: 'How htrcli reaches a remote ssh/WSL host: the desktop bundles one htrcli per remote platform beside ocode, connect uploads it to ~/.ocode/bin/<ver>/htrcli using the ocode install path, identity is the binary SHA-256 and the reported version comes from `htrcli --version`, and the remote server puts that directory on PATH.'
tags:
  - remote
  - ssh
  - htr
  - htrcli
  - provisioning
---

# Remote htrcli provisioning

`ocode serve --remote` runs HTR automation on the remote host, and its agent shell
runs `htrcli` by name. The remote needs its own htrcli, installed the same way as
the remote ocode binary.

## Flow

1. **Bundle.** `make desktop-remote-binaries` cross-builds
   `htrcli-<os>-<arch>` from the sibling source (`HTRCLI_ROOT`, default
   `../how-to-recorder/htrcli`) with `-tags traytest`, next to `ocode-<os>-<arch>`.
   `scripts/bundle-macos.sh` copies both into `Contents/Resources/remote-binaries/<ver>/`
   and fails if either is missing.
2. **Install.** `EnsureHTRCli` (`internal/remote/htrcli.go`) runs in the same two
   connect paths as the ocode install (`RemoteWorkspace.ensureBinary` and
   `runPrepareStages`). It compares the remote digest with the bundle's digest. On a
   mismatch it uploads to `.htrcli.partial`, runs `chmod +x && mv`, and re-checks the
   digest.
3. **Link.** `linkHTRCliForShells` points `~/.local/bin/htrcli` at that install. The
   agent shell is a login shell, and Debian/Ubuntu `/etc/profile` resets `PATH`, which
   drops anything the server was launched with. `~/.profile` re-adds `~/.local/bin`
   (the convention `config.EnsureUserBinPath` uses locally). A regular file, or a
   symlink outside `~/.ocode/bin` (dangling ones included), belongs to the user and is
   left alone, with a log line.
4. **Launch.** `launchServerCmd` puts `~/.ocode/bin/<ver>` first on `PATH`. macOS login
   shells keep the inherited `PATH`, so this covers them, and the server's children
   inherit it.

## Version tracking

- Directory: `~/.ocode/bin/<ocode version>/htrcli`. It sits beside `ocode`, so
  `GCVersions` retires both together.
- Version: `htrcli --version` prints the bare version (htrcli's `commands.Version`,
  stamped by `make build` and the ocode bundle build from `git describe`). Every
  connect runs it on the remote and reports it in progress and the log. A binary
  that prints nothing fails the install.
- Identity: the SHA-256 of the bundled binary decides reinstalls. Two builds can
  share one version string (an unclean tree), so the digest is what tells them
  apart. It is verified after every activation; a mismatch fails the connect.
- Builds without a stamp (`go install`) report `dev`.

## Limits

- A platform with no bundle (a source-checkout `ocode`, or a `make install` CLI with
  no `remote-binaries` directory) logs and skips the install. The remote's HTR then
  reports `htrcli not found`. See `TODO.md`.
- A remote `ocode serve` that was already running before the upgrade keeps its old
  `PATH`, and the connect path reuses a healthy same-version server rather than
  restarting it. Restart it once with `POST /api/remote/{host}/restart`
  (`HandleRemoteRestart`, the desktop/web Restart action). That kills running turns
  and terminals on the host, so do it between sessions.
- The remote binary has no tray (`-tags traytest`). The systray backend needs cgo,
  which cannot cross-link from macOS. ocode always spawns `htrcli serve --no-tray`,
  so this only affects a hand-run `htrcli serve` on the remote host.

Related: [[htr-shared-daemon]] (local daemon and coordinates), `docs/gotchas/remote-terminal-502-provisioning.md`.
