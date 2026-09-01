# Part 03 — UI toggles (TUI + web) + cron guard

**Self-contained constraints recap:** `sandbox` is a permission mode toggled via the existing permission-mode surfaces — **not** `shift+tab` (that cycles agent focus/type via `cycleAgentMode`, `model.go:6034` — preserve it). Sandbox must **not** be persisted as the durable default (clamp/skip in the persist path). Interactive PTY and web `!shell` stay out of scope. No code snippets.

**Consumes from Parts 01–02:** `PermissionModeSandbox` (validated by `SetMode`); Decide already routes sandbox.

---

### Task 1: TUI — permission-mode cycle + `/sandbox` + non-persistence

**Files:**
- Modify: `internal/tui/model.go` — the status-bar/allowed-header permission-mode click cycle (`:7010-7031`, mirrored at `:7174-7198`). Current machine: `normal(auto off) → normal(auto on) → YOLO → Locked → normal`. Insert sandbox so the machine becomes `normal(auto off) → normal(auto on) → YOLO → Locked → Sandbox → normal`. Entering/leaving sandbox does **not** change `AutoPermissionEnabled` (Part 01 Task 1). Extend the mode-indicator label switch to render `SANDBOX`.
- Modify: `internal/tui/model.go:8103-8111` instant-command allowlist — add `cmd == "/sandbox"` so it applies immediately like `/yolo`.
- Modify: `internal/tui/commands.go` — register `/sandbox` (beside `/yolo` at `:145`) with a `runSandboxCmd` handler (`on|off|status`); `on` sets `PermissionModeSandbox`, `off` sets `normal`.
- Modify: the persist path (`persistPermissions()` / the config saver it calls) — when the live mode is `sandbox`, persist `normal` for the durable mode field (Decision 2). The live agent stays in sandbox; only the on-disk default is clamped.
- Test: `internal/tui/model_test.go`, `internal/tui/commands_test.go`

**Interfaces:**
- Consumes: `agent.PermissionModeSandbox`, `Permissions().SetMode/Mode()`.
- Produces: click cycle including Sandbox; `/sandbox on|off|status`; `SANDBOX` label; non-persisted default.

- [ ] **Step 1:** Write failing tests: `TestPermClickCycleIncludesSandbox` (cycle reaches `sandbox` in the defined order, returns to `normal`); `TestSandboxCommandSetsMode` (`/sandbox on` ⇒ `Mode()==sandbox`; `/sandbox off` ⇒ `normal`); `TestSandboxNotPersistedAsDefault` (live mode sandbox ⇒ the persisted config mode is `normal`); `TestShiftTabStillCyclesAgentMode` (regression: `shift+tab` still calls `cycleAgentMode`, unaffected).
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement the cycle insertion (both sites), the `/sandbox` command + instant-allowlist entry, the indicator label, and the persist clamp. Keep YOLO/Locked/auto-permission behavior intact.
- [ ] **Step 4:** Run tests + full `internal/tui` suite; expect PASS.
- [ ] **Step 5:** Commit: `feat(tui): sandbox in permission cycle + /sandbox (non-persisted)`.

---

### Task 2: Web server — dedicated mode endpoint + propagation

**Files:**
- Modify: `internal/server/server.go` (routes ~`:318-324`) — add `PUT /api/permissions/mode`. Leave `POST /api/permissions` as the tool-rule handler and `PUT /api/permissions/yolo` intact.
- Modify: `internal/server/handler_permissions.go` — add `HandleSetPermissionMode`: parse `{mode}`, reject anything but `normal|yolo|locked|sandbox`, `SetMode` on **all live agents** (`h.allAgents()`, matching `HandleSetYolo`), do **not** persist (session-scoped, Decision 2). `GET /api/permissions` already reports `string(pm.Mode())` (`:62`) as the authoritative live status — no change.
- Test: `internal/server/handler_permissions_test.go`

**Interfaces:**
- Produces: `PUT /api/permissions/mode {"mode":"sandbox"}` → 200; `GET /api/permissions` then reports `"mode":"sandbox"`; invalid mode → 400.

- [ ] **Step 1:** Write failing tests: `TestSetPermissionModeAcceptsSandbox` (PUT sandbox ⇒ live mode sandbox, GET reflects it); `TestSetPermissionModeRejectsInvalid` (400, mode unchanged); `TestSetPermissionModePropagatesToAllAgents` (two live agents both flip); `TestSetPermissionModeDoesNotPersist` (config default unchanged).
- [ ] **Step 2:** Run; expect FAIL (route/handler absent).
- [ ] **Step 3:** Implement route + handler.
- [ ] **Step 4:** Run tests + `internal/server` permission suite; expect PASS.
- [ ] **Step 5:** Commit: `feat(server): PUT /api/permissions/mode with live propagation`.

---

### Task 3: Web UI — dispatcher, API client, mode control, PermissionsForm

**Files:**
- Modify: `web/src/api/types.ts` — add `"sandbox"` to the live permission-mode string handling around `permission_mode` (`:444`); leave `CronPermissionMode` (`:86`) unchanged (out of scope).
- Modify: `web/src/api/client.ts` — add `setPermissionMode(mode: string)` → `PUT /api/permissions/mode` (beside `getYolo`/`setYolo` at `:1038-1041`).
- Modify: `web/src/components/Chat/commands.ts` — add the `/sandbox` descriptor (beside `:76`) **and** a `case "/sandbox":` handler in the dispatcher (beside `:391`, mirroring the `/yolo` handler at `:898-923`) that calls `api.setPermissionMode` and reports status from `GET /api/permissions.mode`.
- Modify: the permission-mode control/pill (the surface that drives YOLO today) to offer Sandbox and render the active mode label.
- Modify: `web/src/components/Settings/PermissionsForm.tsx` — it currently stores mode as a boolean (`:14`, `useState(false) // yolo on/off`) and `save()` calls `api.setYolo(mode)`, which would **revert sandbox to normal** on save. Change it to load the real mode string from `GET /api/permissions.mode` and, on save, call `api.setPermissionMode(currentMode)` (or leave mode untouched unless changed in this form) so sandbox is preserved.
- Test: the web test mirroring the YOLO-toggle test (e.g. `web/src/components/Chat/*.test.tsx`), plus a `PermissionsForm` test.

**Interfaces:**
- Consumes: `api.setPermissionMode("sandbox")`, `GET /api/permissions.mode`.
- Produces: `/sandbox` slash command (with working dispatcher handler), a mode-selector option, and a `PermissionsForm` that preserves sandbox.

- [ ] **Step 1:** Write failing tests: selecting Sandbox / running `/sandbox` calls `api.setPermissionMode("sandbox")` and the pill renders `Sandbox` when GET reports it; `TestPermissionsFormPreservesSandbox` (form loaded while mode is sandbox, saved without touching mode ⇒ does not call `setYolo(false)` / does not revert to normal).
- [ ] **Step 2:** Run (`bun test`); expect FAIL.
- [ ] **Step 3:** Implement types, `setPermissionMode`, `/sandbox` descriptor **and dispatcher handler**, the selector/label, and the PermissionsForm fix.
- [ ] **Step 4:** Run web tests + `bun run typecheck` (tsgo); expect PASS. Manually verify the pill updates in the running UI.
- [ ] **Step 5:** Commit: `feat(web): sandbox permission-mode toggle + PermissionsForm preservation`.

---

### Task 4: Cron guard

**Files:**
- Modify (if needed): `internal/*/scheduler_runner.go` (the runner at `:55-57`: `if job.Payload.PermMode != "" { SetMode(...) }`). A fresh cron agent already defaults to `normal` and only overrides when a job specifies `PermMode`. Add an explicit guard/assertion so a blank `PermMode` can never resolve to `sandbox`.
- Test: the scheduler test package

**Interfaces:**
- Produces: invariant — blank `perm_mode` cron job runs in `normal`.

- [ ] **Step 1:** Write failing/regression test `TestCronBlankPermModeResolvesNormal` (a job with empty `PermMode` yields an agent in `normal`, never `sandbox`, even if a prior TUI session set sandbox live). Add `TestCronExplicitSandboxHonored` only if explicit cron sandbox is desired — otherwise assert an explicit `sandbox` PermMode is accepted by `SetMode` (it is, post Part 01) but note the web cron UI does not offer it (out of scope).
- [ ] **Step 2:** Run; expect PASS if the invariant already holds, FAIL if not.
- [ ] **Step 3:** Add the explicit guard if the test surfaced a gap; otherwise keep the test as a pinned invariant.
- [ ] **Step 4:** Run the scheduler suite; expect PASS.
- [ ] **Step 5:** Commit: `test(cron): pin blank perm_mode resolves to normal`.
