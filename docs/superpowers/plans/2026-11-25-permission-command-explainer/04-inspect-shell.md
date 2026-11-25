---
type: Plan
timestamp: 2026-11-25T07:19:25Z
---
# Part 04 — inspect_shell and hardened Git metadata

**Goal:** Add one argv-only read-only inspection tool that resolves fixed binaries, validates every token, and executes under the mandatory sandbox with metadata-only hardened Git.

**Risk:** high — process execution under an adversarial repository; a bypass is a code-execution hole.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md`.

**Global constraints this part must honor:**
- 10s shell deadline; 8 KiB combined shell output after streaming mask with overlap; 8 KiB evidence result and 40 KiB total evidence at the runner boundary.
- Windows `inspect_shell` disabled; mandatory no-write sandbox on macOS/Linux.
- Metadata-only hardened Git; no content guard; secrets-only masking.
- Fixed PATH; no new endpoint / config / schema / migration.

**Files:**
- Create: `internal/agent/tool_digest_shell.go`, `internal/agent/tool_digest_shell_test.go`
- Modify: `internal/agent/tool_digest_loop.go` to register `inspect_shell` in `newToolDigestRunnerToolbox`
- Modify only if unavoidable: one `internal/tool` helper. Use the existing `tool.NewProcessSupervisor` / `tool.StartSupervised` / `Shutdown` / `TerminateAll` / `MarkExited` / `MarkKilled` unchanged; no `NewProcessRegistry`.

**Interfaces — consumed (exact, restated):**
```go
type toolDigestToolbox struct {
    defs    []map[string]interface{}
    tools   map[string]tool.Tool
    cleanup []func()
}
func (b *toolDigestToolbox) add(t tool.Tool)
func (b *toolDigestToolbox) addCleanup(fn func())
func (b *toolDigestToolbox) Close()

type toolDigestSnapshot struct {
    WorkDir           string
    SessionID         string
    Profile           string
    Config            config.Config
    RedactionHook     *redact.NetHook
    RedactionRegistry *redact.Registry
    Client            LLMClient
    OnSideUsage       func(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int64, spend *float64)
}
```
`newToolDigestRunnerToolbox` currently adds `call_parameters` and then registers the four file tools; extend it to call `registerInspectShellTool(box, snap)` after those and return the registration error.

Existing pieces: `tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})`, `tool.StartSupervised(sup, cmd, reg)`, `(*tool.ProcessSupervisor).Shutdown(ctx)`, `(*tool.ProcessSupervisor).TerminateAll(ctx)`, `(*tool.ProcessSupervisor).MarkExited(id, exitCode)`, `(*tool.ProcessSupervisor).MarkKilled(id, exitCode)`, `sandbox.New()`, `Wrapper.Available() bool`, `Wrapper.Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error)`, `redact.IsSensitiveFile`, `isSecretMaterialPath`, `isRepoMetadataPath`, `isLoginStatePath`.

**Interfaces — produced (exact):**
```go
func registerInspectShellTool(box *toolDigestToolbox, snap toolDigestSnapshot) error
func newInspectShellTool(snap toolDigestSnapshot, sup *tool.ProcessSupervisor) (tool.Tool, bool)
func inspectShellBinaryDirs() []string
func validateInspectShellArgv(workDir string, argv []string) (validatedCommand, error)
func inspectShellEnv() []string

type validatedCommand struct {
    Program      string
    Args         []string
    ProjectPaths []string
}
```
`registerInspectShellTool` creates `sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})`, registers `box.addCleanup(func(){ ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel(); _ = sup.Shutdown(ctx) })`, then calls `newInspectShellTool(snap, sup)`. `newInspectShellTool` returns `false` only when the mandatory sandbox is unavailable or no allowlisted fixed binary exists at all; when it returns `false`, `registerInspectShellTool` adds nothing and returns nil. A missing individual program is a per-call tool error, not whole-tool absence. The tool accepts only an `argv` array, never a shell-text field.

**Trusted directories, in order:** macOS `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`, `/bin`, `/opt/homebrew/sbin`, `/usr/local/sbin`, `/usr/sbin`, `/sbin`, `/usr/local/go/bin`; Linux `/usr/local/bin`, `/usr/bin`, `/bin`, `/usr/local/sbin`, `/usr/sbin`, `/sbin`, `/usr/local/go/bin`, `/usr/lib/go/bin`, `/opt/go/bin`; Windows disabled. The executed path is the fixed absolute candidate; a project-local executable can never substitute.

**Fixed Git prefix, never model-supplied:** `git --no-pager -c core.fsmonitor=false -c core.pager=cat -c diff.external= -c core.hooksPath=/dev/null`; `diff` adds `--no-ext-diff --no-textconv`. The environment sets `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, and inherits no `GIT_DIR` / `GIT_WORK_TREE` / `GIT_INDEX_FILE` / `GIT_CONFIG*`. Git output is parsed as path records (quoted names and NUL-delimited records) before sensitive filtering; substring filtering is not acceptable.

**Allowlist:** `pwd`; `ls`; `stat`; `file`; `readlink` / `realpath`; `wc`; `head` / `tail` (regular files, reject `-f` / `-F`); `md5` / `md5sum` / `shasum` / `sha1sum` / `sha256sum` / `sha512sum`; print-only `find` (`-name`, `-iname`, `-path`, `-type`, `-maxdepth`, `-mindepth`, `-print`; explicit `-print`; reject `-exec*`, `-ok`, `-delete`, `-fprint*`, `-fls`, `-L` / `-H` / `-follow`); `go version` / `go env` with `GOTOOLCHAIN=local`; Git `status`, metadata-only `diff` (`--name-only` / `--name-status` / `--stat` / `--numstat`, optional `--cached` / `--staged`, at most two refs, pathspecs, `-z`), the exact `rev-parse` list (`--show-toplevel`, `--show-prefix`, `--is-inside-work-tree`, `--is-shallow-repository`, `--show-superproject-working-tree`, `--verify` plus one revision, or one plain revision), and `ls-files` (`--cached`, `--others`, `--exclude-standard`, `--modified`, `--deleted`, `--unmerged`, `--ignored`, `-z`). Reject every `-c` / `--config-env` / `--git-dir` / `--work-tree` / `-C` / `--exec-path`, patch/content flags (`-p`, `-U`, `--word-diff`, `--raw`, `--output`, `--ext-diff`, `--textconv`, `--open-files-in-pager`), `rev:path`, `log` / `show` / object forms, shell operators, control characters, project-local binaries, unknown flags, and sensitive operands.

**Execution:** build the final argv locally, run without a shell, stdin, background mode, or inherited credentials, with `cmd.Dir` set to the project root and `PATH` set to `inspectShellBinaryDirs()`. Use `sandbox.New()` with `roots := sandbox.RootSet{WritableRoots: nil, NetworkEgress: false}`; require `Available()`, then replace `cmd` with the wrapped command returned by `Wrapper.Wrap(cmd, roots)` (exact signature `func (w Wrapper) Wrap(cmd *exec.Cmd, roots RootSet) (*exec.Cmd, error)`), with no unconfined retry. Start it with `tool.StartSupervised(sup, cmd, tool.ProcessRegistration{ID, Name, Command, Kind: tool.ProcessKindBackgroundBash, Cmd: cmd})` and drain `cmd.Wait` in a goroutine bounded by a 10s context. Mask streaming output with overlap before the 8 KiB retained-output cap. On timeout, cancellation, or the output limit, call `sup.TerminateAll(ctx)` to kill the whole process group before recording the result; a normal exit calls `sup.MarkExited(reg.ID, code)` and a killed one calls `sup.MarkKilled(reg.ID, code)`. The registered cleanup calls `sup.Shutdown` with a 5s deadline at runner close. Fail closed when the sandbox is unavailable.

**Red-green steps:**
- [ ] **Step 1: Write the failing tests.**
```go
func TestInspectShell_RejectsShellOperatorsAndUnknownFlags(t *testing.T)
func TestInspectShell_FixedBinaryAndPath(t *testing.T)
func TestInspectShell_SymlinkFIFODeviceRejected(t *testing.T)
func TestInspectShell_MissingProgramIsToolError(t *testing.T)
func TestInspectShell_GitHardenedPrefixAndEnv(t *testing.T)
func TestInspectShell_HostileRepoConfigAndAttributes(t *testing.T)
func TestInspectShell_FullDiffNegativeControl(t *testing.T)
func TestInspectShell_GitMetadataFiltersSensitivePaths(t *testing.T)
func TestInspectShell_TimeoutKillsProcessGroup(t *testing.T)
func TestInspectShell_SandboxUnavailableFailsClosed(t *testing.T)
func TestInspectShell_ToolCatalog(t *testing.T)
func TestInspectShell_WindowsDisabled(t *testing.T)
```
Cover: `a; b`, `a|b`, `$(x)`, `>f`, newlines, and unknown flags per program all fail closed with an error that does not echo secret operands; a project-local binary named `ls` is never executed, the fixed absolute path is used, `PATH` equals the fixed dirs, and credentials are absent from the env; symlinks out of the project, FIFOs, and devices are rejected before execution (no hang on a FIFO); every admitted Git form starts with the exact prefix, `diff` carries the two forced flags, and the env has the four `GIT_*` values with no routing vars; a repo whose `.git/config` sets fsmonitor/pager/diff.external/hooksPath and whose `.gitattributes` sets clean/smudge/process/textconv leaves marker files untouched; a full `git diff` without hardening runs the marker (proving the vector is live); `.env`, `id_rsa`, and `.git` paths are removed from `ls-files`, `status`, and every diff form including quoted and NUL-delimited records; the 10s deadline and cancellation kill the whole process group; a missing individual program (e.g. `go`) returns a tool error while `inspect_shell` stays registered; unavailable sandbox or no allowlisted fixed binary removes only `inspect_shell` and keeps the file tools; the catalog advertises six tools when `inspect_shell` is available and five when it is not.

- [ ] **Step 2: Run to verify failure.**
`go test ./internal/agent/ -run 'TestInspectShell' -count=1`
Expected: FAIL — undefined `newInspectShellTool` and validators.

- [ ] **Step 3: Implement the validator, fixed resolution, and supervised execution.** In `tool_digest_shell.go`, implement `inspectShellBinaryDirs`, `inspectShellEnv`, `validateInspectShellArgv`, `newInspectShellTool`, and `registerInspectShellTool` (which creates the supervisor and registers its `Shutdown` cleanup), then extend `newToolDigestRunnerToolbox` to register the shell tool after the file tools. Build the final argv locally, run under the supervisor and mandatory sandbox, mask with overlap before the cap, and kill the process group on timeout/cancel/limit.

- [ ] **Step 4: Run to verify pass.**
`go test ./internal/agent/ -run 'TestInspectShell' -count=1 -race`
Also run `gofmt` and `go vet` on `internal/agent` and any touched `internal/tool` file. Expected: PASS.
