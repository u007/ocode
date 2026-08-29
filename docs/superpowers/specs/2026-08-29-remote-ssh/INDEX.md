# ocode Remote (SSH + WSL) — Design Spec

**Date:** 2026-08-29 (revised after advisor review)
**Status:** Approved design, pre-implementation
**Owner:** james

## Problem

ocode operates only on the local filesystem. Users want to work on projects
that live on remote machines and, on Windows, inside WSL distros — the way
VS Code Remote does.

## Decision

VS Code Remote architecture: the **entire ocode backend runs on the remote
host**; the local machine is display only. Chosen over a
local-process-with-SSH/SFTP-tool-layer design because the codebase analysis
showed `internal/server` is already a complete IDE-grade API over the
machine it runs on, the web UI and desktop shell are already thin clients
over it, and the alternative requires threading an FS/Runner abstraction
through ~700 direct `os.*`/`exec.Command` call sites.

The TUI needs no client/server rewrite: remote TUI = run the remote ocode
TUI over `ssh -t`. Only web mode uses the client/server split, which the
web UI already speaks.

## Key decisions (post-advisor revision)

| Decision | Choice |
|---|---|
| Architecture | Remote server; local is display only |
| TUI | `ssh -t` passthrough, no thin client |
| Transport | System `ssh`/`scp` binaries (inherits ssh_config, agent, ProxyJump, known_hosts); never `x/crypto/ssh` |
| Provisioning | Local cross-compile (`GOOS=… go build`, cgo-free module) + `scp`. **No release-artifact downloader** — no published download URLs exist yet |
| Credential sync | Pushed on connect, **Phase 1** (moved up from 3 — remote TUI is useless without keys) |
| Web token | URL fragment → sessionStorage → Authorization header (fragment never reaches server logs) |
| Default remote path | Last remote project for that host, else remote `$HOME` |
| Server reuse | Remote serve writes a state file; `--web` reconnect discovers and reuses a live server so sessions resume |
| Progress/errors | Provisioning and connect render staged progress in the invoking terminal; failures show the failing stage + ssh/scp stderr verbatim |
| Sessions | Never synced; live on the remote under remote `$HOME` (per-host storage automatic) |

## Out of scope

Full TUI thin client, desktop-shell remote UI, SSH→Windows→WSL chaining,
settings sync beyond auth profiles + core model config, session sync,
release-artifact download infrastructure.

## Parts / execution order

| Part | File | Phase |
|---|---|---|
| Architecture & shared foundations | `01-architecture.md` | — (underpins all) |
| Phase 1 — Connect & Work | `02-phase1-connect.md` | TUI passthrough, provisioning, credential sync |
| Phase 2 — Web Mode | `03-phase2-web.md` | serve token auth, tunnel, server reuse |
| Phase 3 — WSL | `04-phase3-wsl.md` | `wsl.exe` transport swap |

Each part is self-contained. Implement strictly in order; Phase 1's
success metric (fresh Linux VM → one command → remote TUI answers an LLM
prompt with zero manual remote steps) gates Phase 2.
