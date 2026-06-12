# Part 3: Config Section, Targeted Saver, Sidebar Toggle

Spec §8, §9-sidebar. Follows existing patterns exactly: `SaveTUITheme` (internal/config/ocodeconfig.go:879) / `SaveAdvisorEnabled` (:908) for the saver; advisor sidebar toggle for the UI.

Prereq: none (parallel with Part 2).

### Task 3.1: Config struct + load

**Files:**
- Modify: `internal/config/ocodeconfig.go` — `OcodeConfig` struct (:58), `defaultOcodeConfig()` (:217), `ocodeConfigFile` struct (:181)
- Test: `internal/config/config_test.go`

- [ ] Write failing test: loading a config file containing `"security": {"redaction": {"enabled": true, "model": "lmstudio/x", "failMode": "block", "customWords": ["w"]}}` populates `cfg.Ocode.Security.Redaction` fields; absent section → defaults (`Enabled: false`, `FailMode: "block"`, empty model/words).
- [ ] Add `SecurityConfig{Redaction RedactionConfig}` and `RedactionConfig{Enabled bool, Model string, FailMode string, CustomWords []string}` with JSON tags matching spec; wire into `OcodeConfig`, defaults, and the file-load merge.
- [ ] Test PASS. Commit `feat(config): security.redaction section`.

### Task 3.2: Persistence + targeted saver

**Files:**
- Modify: `internal/config/ocodeconfig.go` — `writeOcodeConfigFile` payload map (:566), new saver next to `SaveAdvisorEnabled` (:908)
- Test: `internal/config/config_test.go`

- [ ] Write failing tests: `SaveSecurityRedaction(mutate func(*RedactionConfig))` does load-modify-write and **preserves unrelated keys** (pre-seed file with theme + permissions, mutate redaction, assert others intact — mirror `TestSaveTUIThemePreservesExistingOcodeConfig` at :294); round-trip: save then load returns saved values (this implicitly catches the :566 payload-map gotcha — `security` must be added to the map or the test fails).
- [ ] Implement saver via `loadFullOcodeConfig()` → mutate → `SaveOcodeConfig(cfg)`; add `"security"` to the payload map in `writeOcodeConfigFile`.
- [ ] Test PASS. Commit `feat(config): SaveSecurityRedaction targeted saver`.

### Task 3.3: Sidebar "Secrets" toggle row

**Files:**
- Modify: `internal/tui/model.go` — `sidebarRenderData` (:11539), `buildSidebarRenderData` (:11799), click-hit-test helpers near `sidebarAdvisorToggleForClick` (:12432), sidebar click handler block (:4588)
- Test: `internal/tui/model_test.go` (or `sidebar_overflow_test.go` if sidebar tests live there — match existing placement)

- [ ] Read the advisor toggle chain end-to-end first; replicate every element for secrets.
- [ ] Write failing tests: `buildSidebarRenderData` includes a "Secrets" row rendering `off` when disabled and `on (<short model name>)` when enabled; `sidebarSecretsToggleForClick` hit-tests the row's recorded lines; simulated click flips `m` state and calls the saver (inject/stub via the same mechanism advisor tests use — if advisor toggle has no test, add a minimal render+hit-test test only).
- [ ] Implement: `secretsToggleTopIdx`/`secretsToggleRows` fields, row population (clamped `.Width(w).MaxHeight(1)` per CLAUDE.md TUI rule), hit-test helper, click handler calling `config.SaveSecurityRedaction` and updating the live Redactor enabled flag.
- [ ] `go test ./internal/tui/ -run Sidebar` → PASS. Commit `feat(tui): sidebar secrets toggle`.
