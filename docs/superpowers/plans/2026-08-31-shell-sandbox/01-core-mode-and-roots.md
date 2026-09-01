# Part 01 — Core: sandbox mode, classified roots, unified builder

**Self-contained constraints recap:** `sandbox` is a permission mode (no config bool), session-scoped/live-only. Boundary = one capability-classified `AllowedRoots()` model; writable ⊂ all roots. Guard against roots resolving to `/`. Preserve all existing shell semantics. Wire session workdir through fg + bg + hooks. No code snippets; verify via named tests. TDD throughout.

---

### Task 1: `PermissionModeSandbox` mode + SetMode case

**Files:**
- Modify: `internal/agent/permissions.go` — add the constant beside `:33-35`; add a `PermissionModeSandbox` case to the `SetMode` switch (`:2612-2622`). Decide auto-permission interaction: entering sandbox **does not** alter `autoPermissionEnabled` (unlike YOLO, which force-disables it) — sandbox's prompt-bypass is handled in `Decide`, not by killing the LLM layer.
- Test: `internal/agent/permissions_test.go`

**Interfaces:**
- Produces: `PermissionModeSandbox PermissionMode = "sandbox"`, accepted by `SetMode`.

- [x] **Step 1:** Write failing tests: `TestSetModeSandboxAccepted` (`SetMode(PermissionModeSandbox)` ⇒ `Mode()=="sandbox"`); `TestSetModeInvalidLeavesModeUnchanged` (set to `sandbox`, then `SetMode("bogus")` ⇒ mode still `sandbox` — documents the silent-no-op contract); `TestSetModeSandboxKeepsAutoPermission` (auto-permission enabled before ⇒ still enabled after entering sandbox).
- [x] **Step 2:** Run; expect FAIL (constant undefined).
- [x] **Step 3:** Add the constant + `SetMode` case (no auto-permission mutation). No `Decide` behavior change yet (Part 02 Task 2).
- [x] **Step 4:** Run tests + full `internal/agent` permissions suite; expect PASS.
- [x] **Step 5:** Commit: `feat(permissions): add sandbox permission mode`.

---

### Task 2: Capability-classified roots + RootSet

**Files:**
- Modify: `internal/agent/permissions.go` — add `AllowedRootsClassified() []RootSpec` beside `AllowedRoots()` (`:1497`). Reuse the same union/resolve/dedupe logic, but tag each source with a `Writable` flag. **Writable:** `workDir`, `tool.ExtraAllowedRoots()` (the `extra_allowed_paths` feature), `languageDepRoots()` (npm/pip/cargo/go/maven/gradle caches — must be writable so `npm install`/`pip` work), `/tmp`, `/var/tmp`, `os.TempDir()`. **Read-only:** `tool.CacheRoots()`, `paths.GlobalDataDir()` (holds `auth.json`, sessions — protect its integrity), `paths.GlobalConfigDir()`. Keep `AllowedRoots()` returning the flat union (unchanged callers). Drop any spec whose resolved path is `/` from the writable set (boundary guard).
- Create: `internal/shell/sandbox/roots.go`
- Test: `internal/agent/permissions_test.go`, `internal/shell/sandbox/roots_test.go`

**Interfaces:**
- Produces: `RootSpec{ Path string; Writable bool }`; `(*PermissionManager).AllowedRootsClassified() []RootSpec`. And in `internal/shell/sandbox`: `RootSet{ WritableRoots []string; ReadRoots []string; NetworkEgress bool }`, `NewRootSet(specs []RootSpec) RootSet` — `WritableRoots` = paths where `Writable && Path != "/"`; `ReadRoots` = empty (whole FS readable/executable); `NetworkEgress` = true.

- [x] **Step 1:** Write failing tests in `internal/agent`: `TestClassifiedRootsExtraPathsWritable` (a scratch dir added via `extra_allowed_paths` is present with `Writable==true`); `TestClassifiedRootsDataDirReadOnly` (the global data dir is present with `Writable==false` — proves auth/session integrity is preserved, resolving review point 5); `TestClassifiedRootsLanguageCachesWritable` (an npm/pip cache root is `Writable==true`); `TestClassifiedRootsDropsFilesystemRoot` (no writable spec has `Path=="/"`).
- [x] **Step 2:** Write failing tests in `internal/shell/sandbox`: `TestNewRootSetSplitsByCapability` (given mixed specs, `WritableRoots` contains only the writable, non-`/` paths; `ReadRoots` empty; `NetworkEgress` true).
- [x] **Step 3:** Run; expect FAIL.
- [x] **Step 4:** Implement `RootSpec`, `AllowedRootsClassified` (do not re-resolve symlinks beyond what the existing `add()` already does — the macOS `/var/folders` realpath must survive), `RootSet`, `NewRootSet`.
- [x] **Step 5:** Run tests; expect PASS.
- [x] **Step 6:** Commit: `feat(permissions,sandbox): capability-classified roots + RootSet`.

---

### Task 3: Add `~/.claude` to the classified roots (writable)

**Files:**
- Modify: `internal/agent/permissions.go` — one `add()` in `AllowedRoots()`/`AllowedRootsClassified()` for expanded `~/.claude`, classified **writable** (user explicitly requested read/write access). One-line comment noting this widens the write surface to cross-tool agent state.
- Test: `internal/agent/permissions_test.go`

**Interfaces:**
- Produces: expanded `~/.claude` as a writable classified root and a flat `AllowedRoots()` entry.

- [ ] **Step 1:** Write failing tests `TestAllowedRootsIncludesClaudeDir` (present in flat `AllowedRoots()`) and `TestClaudeDirIsWritable` (present with `Writable==true` in classified).
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Add the `add()` call in both the flat and classified builders, using the same home-dir expansion the config-dir branch uses.
- [ ] **Step 4:** Run tests + full allowed-roots suite; expect PASS.
- [ ] **Step 5:** Commit: `feat(permissions): allow ~/.claude as a writable root`.

---

### Task 4: Unified bash builder (fg + bg) + session workdir wiring

**Files:**
- Create: `internal/tool/bash_build.go`
- Modify: foreground construction `internal/tool/exec.go:174-189`; background construction `internal/tool/process.go:257-287` (`StartBackgroundDisplay`) — both call the builder. Set `cmd.Dir` to the session project root (carried in agent context, `internal/agent/agent.go:4092-4098`) for both; make the background process hooks use the session workdir instead of `os.Getwd()` (`process.go:275-287`).
- Test: `internal/tool/bash_build_test.go`, plus re-run existing `internal/tool` suites unchanged

**Interfaces:**
- Produces (this task, plain form — sandbox params added in Part 02): `buildBashCmd(ctx context.Context, command string, dir string) *exec.Cmd` encapsulating the GOOS branch (`cmd /C` Windows; `bash -c` + `setProcGroup` Unix), used by both fg and bg sites. `WrapWithParentMonitor` continues to be applied by callers exactly as today; Start-before-Register ordering preserved.

- [ ] **Step 1:** Write failing test `TestBuildBashCmd` (Unix: `Args==["bash","-c",command]`, non-nil `SysProcAttr`; Windows: `Args==["cmd","/C",command]`; `Dir` set when a non-empty dir is passed).
- [ ] **Step 2:** Write failing test `TestBashUsesSessionWorkdirNotProcessCwd`: with the session project root differing from `os.Getwd()`, a foreground command (e.g. `pwd`) runs in the session root, and the background hook environment reflects the session root — not the process cwd.
- [ ] **Step 3:** Run; expect FAIL.
- [ ] **Step 4:** Implement `buildBashCmd` by lifting the inline construction; switch both call sites; thread the session workdir into `cmd.Dir` (fg + bg) and into the background hook env. Do not change pipe/cancellation/registration behavior.
- [ ] **Step 5:** Run new tests + full `internal/tool` suite; expect PASS, no behavior regressions.
- [ ] **Step 6:** Commit: `refactor(tool): unified bash builder + session workdir wiring`.
