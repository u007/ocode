# Computer Use Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `computer` tool that screenshots and drives the host desktop (mouse, keyboard) on macOS, Windows and Linux, opt-in via config, gated by permissions.

**Architecture:** One tool (`internal/tool/computer.go`) with an Anthropic-style `action` enum. Platform work lives in `internal/computer` behind a `Driver` interface declared in the tool package (so `internal/computer` can import `internal/tool` for `StartSupervised` without a cycle). Each GOOS gets a build-tagged driver that shells out to stock OS tools. The agent attaches the driver, the permission manager classifies actions, TUI and web get a `/computer` command.

**Tech Stack:** Go (pure, no CGO), `golang.org/x/image/draw` (direct dep) for downscaling, `osascript -l JavaScript` (darwin), PowerShell + user32 (windows), `xdotool`/`scrot` or `ydotool`/`grim` (linux). React/TS for the web command.

**Spec:** `docs/superpowers/specs/2026-09-12-computer-use-tool-design.md`

## Global Constraints

- No CGO. Release matrix in `Makefile` cross-compiles every GOOS; every new file must build under `GOOS=darwin`, `GOOS=windows`, `GOOS=linux`.
- Every subprocess goes through `tool.StartSupervised` with a `ProcessRegistration` (new kind `ProcessKindComputer = "computer"`).
- Driver call timeout: 10s, 60s for `Type`. Stdout capped at 1 MiB.
- Coordinates crossing the `Driver` boundary are in the OS input space reported by `Screenshot` (macOS points, Windows DPI-aware pixels, X11 pixels).
- Screenshot longest side ≤ 1568px after downscale.
- Tool is registered only when `cfg.Ocode.ComputerUse.Enabled` is true.
- Config writes use a targeted `withOcodeConfigLock` saver, never `SaveOcodeConfig` on an in-memory snapshot.
- No fallbacks, no optional behaviour flags, no empty catch. Log every caught error via the package's existing logging (`log.Printf` in `internal/config`, `a.emitDebug` in `internal/agent`).
- Match existing style. Tests use the standard library `testing` package as neighbouring tests do.
- Commit after each part; never `git stash`; add only files the part touched.
- Use the `use-modern-go` skill guidance when writing Go.

## Parts

| Part | File | Deliverable |
|------|------|-------------|
| 01 | `01-darwin-spike.md` | Throwaway proof that JXA can post CGEvents; decides darwin input strategy |
| 02 | `02-config.md` | `ComputerUseConfig` + saver |
| 03 | `03-tool-core.md` | `Driver` interface, `ComputerTool`, registration, fake-driver tests |
| 04 | `04-agent-wiring.md` | Driver attach, screenshot image result without a path |
| 05 | `05-permissions.md` | Per-action permission decision |
| 06 | `06-computer-runner.md` | `internal/computer` shared runner, `New()` per GOOS |
| 07 | `07-driver-darwin.md` | macOS driver |
| 08 | `08-driver-windows.md` | Windows driver |
| 09 | `09-driver-linux.md` | Linux driver |
| 10 | `10-commands-and-web.md` | `/computer` in TUI + web, server endpoints |
| 11 | `11-docs.md` | docs, AGENTS/README/CHANGES/TODO |

Parts 07, 08, 09 are independent of each other. Everything else is sequential in the order listed.
