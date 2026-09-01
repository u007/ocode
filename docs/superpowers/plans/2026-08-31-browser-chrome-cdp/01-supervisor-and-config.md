# Part 01 — Supervisor helper, server-owned supervisor, `browser` config

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** Give the browse subsystem a shared, supervised way to spawn Chrome and a config section to point at the binary.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Launch, § Config.

**Global constraints:** no raw `exec.Command` outside `internal/tool`; `--no-sandbox` forbidden (not relevant here but the helper must not add flags); Go tests via `go test ./internal/tool/ ./internal/server/ ./internal/config/`.

## Context an implementer needs

- `internal/tool/process_supervisor.go`: `ProcessSupervisor` with `Register(ProcessRegistration)`, `RegisterShutdownCallback(fn)`, `MarkExited`, `MarkKilled`, `MarkFailedToStart`, `Shutdown(ctx)`, `Snapshot()`. `ProcessRegistration` has unexported `waitFn/gracefulFn/forceFn`; only in-package code can set them. `ProcessKind` constants live at the top of the file (`background_bash`, `subagent_bash`, `interactive_shell`, `editor`).
- `internal/tool/proc_unix.go`: `setProcGroup(cmd)` (Setsid), `killProcessGroup`, `terminateProcess`. Windows variant exists alongside.
- `internal/server/server.go`: `New(addr, username, password, webFS)` builds `Server`; `Shutdown(ctx)` at ~line 863 closes the main listener, then the browse HTTP server, then the browse listener. `StartBrowse(srv, token, spaOrigin)` at ~line 506 wires the browse server.
- `internal/config/ocodeconfig.go`: `OcodeConfig` struct (~line 30–60, has `Extra map[string]json.RawMessage`), `loadOcodeConfigFile` (~line 955) decodes to `raw map[string]json.RawMessage`, consumes known keys with `if _, ok := raw["compact"]; ok { … delete(raw,"compact") }` blocks, and dumps leftovers into `cfg.Extra`. `LoadOcodeConfig(cfg *Config)` (~line 833) is the entry point; check how its error is handled by callers (`cmd/`, `internal/desktop/boot.go`).

---

### Task 1: `tool.StartSupervised` + `ProcessKindBrowser`

**Files:**
- Modify: `internal/tool/process_supervisor.go` (kind enum; new exported helper next to `Register`)
- Test: `internal/tool/process_supervisor_test.go` (extend existing file)

**Interfaces produced:**
- `ProcessKindBrowser ProcessKind = "browser"`
- `StartSupervised(sup *ProcessSupervisor, cmd *exec.Cmd, reg ProcessRegistration) (ProcessRecord, error)` — applies `setProcGroup(cmd)`, calls `cmd.Start()`, fills `reg.Cmd`, `reg.PID`, `reg.StartedAt`, `reg.OwnsProcessGroup = true`, registers, returns the record. On `Start` failure it calls `sup.MarkFailedToStart(reg.ID, err)` after registering so the failure is visible in `Snapshot()`, and returns the error. It does **not** call `cmd.Wait` — the caller owns `Wait` and must call `sup.MarkExited`.

- [ ] Step 1: Write failing tests: (a) `StartSupervised` with `/bin/sleep 30` → record present in `Snapshot()` with `Kind == ProcessKindBrowser`, `PID > 0`, `OwnsProcessGroup` true, and the child's `SysProcAttr.Setsid` is set; (b) `Shutdown(ctx)` afterwards kills the sleep (process no longer alive within the grace period); (c) a non-existent binary → error returned and record status `ProcFailedToStart`.
- [ ] Step 2: Run `go test ./internal/tool/ -run StartSupervised` → fails (undefined).
- [ ] Step 3: Implement kind constant + helper; reuse `setProcGroup`; keep the default `waitFn/gracefulFn/forceFn` wiring that `Register` already installs for a `Cmd`-bearing registration (read `Register` to confirm defaults exist; if `Register` requires `waitFn`, set it to `cmd.Wait` **only** if the caller passed no Wait ownership — decide: the helper sets `waitFn` to a no-op that returns nil, because the manager owns `Wait`; document this in the doc comment).
- [ ] Step 4: Run tests → pass. Run `go vet ./internal/tool/`.
- [ ] Step 5: Commit `feat(tool): StartSupervised helper + browser process kind`.

---

### Task 2: Server-owned `ProcessSupervisor`

**Files:**
- Modify: `internal/server/server.go` (`Server` struct field `procSup`, `New`, `Shutdown`)
- Test: `internal/server/server_test.go` or new `internal/server/procsup_test.go`

**Interfaces produced:**
- `(*Server).ProcessSupervisor() *tool.ProcessSupervisor` (exported accessor used by `StartBrowse` in Part 05).

- [ ] Step 1: Write failing test: `New(...)` returns a server whose `ProcessSupervisor()` is non-nil; after `StartSupervised(srv.ProcessSupervisor(), sleep)` and `srv.Shutdown(ctx)`, the child is gone and a second `Register` returns `ErrProcessSupervisorClosed`.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement: create with `tool.NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 3 * time.Second})` in `New`; in `Shutdown`, call `s.procSup.Shutdown(ctx)` **before** closing the browse HTTP server (Chrome must get `Browser.close` via the shutdown callback while the CDP pipe is still alive). Log the error, do not abort shutdown.
- [ ] Step 4: Run `go test ./internal/server/ -run ProcessSupervisor` → pass. Run the full server package once to confirm no ordering regressions (`TestFindPendingSessionLoadsEvictedSessionFromDisk` is a known pre-existing failure unrelated to this work — do not touch it).
- [ ] Step 5: Commit `feat(server): server-owned process supervisor`.

---

### Task 3: `browser` config section

**Files:**
- Modify: `internal/config/ocodeconfig.go` (`BrowserConfig` type, `OcodeConfig.Browser`, file-struct mirror, `raw["browser"]` consumption, `applyBrowserConfig`)
- Test: `internal/config/ocodeconfig_test.go`
- Modify: `docs/` config reference page (find the page that documents `permissions`/`plugins` keys — grep `docs/` for `external_plugins` — and add the `browser` keys there)

**Interfaces produced:**
- `config.BrowserConfig{ ChromePath string \`json:"chrome_path"\`; IdleTimeoutMinutes int \`json:"idle_timeout_minutes"\` }`
- `OcodeConfig.Browser BrowserConfig`; default `IdleTimeoutMinutes = 10` applied only when the key is absent or zero.

- [ ] Step 1: Write failing tests: (a) config file with `"browser": {"chrome_path": "/x/chrome", "idle_timeout_minutes": 3}` → both fields set and `browser` absent from `Extra`; (b) no `browser` key → `IdleTimeoutMinutes == 10`, `ChromePath == ""`; (c) `"browser": {"extensions": [...]}` → `loadOcodeConfigFile` returns an error containing `browser.extensions is not supported yet`; (d) negative `idle_timeout_minutes` → error.
- [ ] Step 2: Run `go test ./internal/config/ -run Browser` → fails.
- [ ] Step 3: Implement following the existing `raw["compact"]` consumption pattern; decode into a small struct that also has `Extensions json.RawMessage` purely to detect it and reject.
- [ ] Step 4: Trace `LoadOcodeConfig` error handling in `cmd/ocode/*.go` and `internal/desktop/boot.go`: if the error is logged-and-continued, that's acceptable only if the log line is at error level and mentions the path; if it is swallowed, fix that call site so the message is visible (log with the config path and the error). Record what you found in the commit message.
- [ ] Step 5: Run tests → pass. Update the docs page.
- [ ] Step 6: Commit `feat(config): browser section (chrome_path, idle_timeout_minutes)`.

## Verification for the part

- `go test ./internal/tool/ ./internal/server/ ./internal/config/` green (modulo the documented pre-existing server failure).
- `gofmt -l internal/tool internal/server internal/config` prints nothing new.
