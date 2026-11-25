---
type: Plan
timestamp: 2026-11-25T07:19:25Z
---
# Part 03 — Read-only file/search boundary and cancellation

**Goal:** Wrap `read`, `glob`, `grep`, and `list` so every path stays inside the project and excludes sensitive files, and make shared search tools honor caller cancellation.

**Risk:** high — a security boundary (project containment, secrets) plus cancellation correctness in shared search tools.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md`.

**Global constraints this part must honor:**
- 8 KiB evidence result and 40 KiB total evidence are enforced when the runner appends a tool result; these wrappers return project-safe, sensitive-filtered text for that step.
- Secrets-only masking, no PII; no content guard.
- No new endpoint / config / schema / migration.
- No `Agent.Step` / transcript / hook side effects.

**Files:**
- Create: `internal/agent/tool_digest_files.go`, `internal/agent/tool_digest_files_test.go`
- Modify: `internal/tool/search.go`, `internal/tool/rgrep.go`, `internal/tool/search_test.go`, `internal/tool/rgrep_test.go`
- Modify: `internal/agent/tool_digest_loop.go` to register these tools in `newToolDigestRunnerToolbox`

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
`newToolDigestRunnerToolbox` currently adds only `call_parameters`; extend it to call `registerToolDigestFileTools(box, snap)` after that and return the registration error.

Existing helpers and predicates: `tool.ReadTool`, `tool.GlobTool`, `tool.GrepTool`, `tool.ListTool`, `redact.IsSensitiveFile(path string) bool`, `isSecretMaterialPath(path string) bool`, `isRepoMetadataPath(path string) bool`, `isLoginStatePath(p, workDir string) bool`.

**Interfaces — produced (exact):**
```go
func registerToolDigestFileTools(box *toolDigestToolbox, snap toolDigestSnapshot) error
func newToolDigestFileTools(snap toolDigestSnapshot) []tool.Tool
func validateProjectPath(workDir, p string) (string, error)
func sensitivePathFilter(workDir, p string) bool
```
`registerToolDigestFileTools` adds wrapped `read`, `glob`, `grep`, and `list` tools to `box`; it returns an error if a wrapper cannot be built. `newToolDigestFileTools` is an internal helper and is not the consumed interface. `sensitivePathFilter` returns true when blocked and checks both the supplied path and its canonical form with `redact.IsSensitiveFile`, `isSecretMaterialPath`, `isRepoMetadataPath`, and `isLoginStatePath`.

**Boundary rules:** `validateProjectPath` resolves `p` against `workDir`, resolves symlinks on the deepest existing ancestor (appending remaining lexical components for a deleted or not-yet-created path), and rejects any result outside canonical `workDir`. `read` refuses every sensitive path; `glob` and `list` filter sensitive paths out of output before retaining it; `grep` injects ignore patterns derived from the same predicate into ripgrep before any file is read, and the fallback walker checks the predicate before opening a file. Repository metadata and login-state directories are skipped, not searched. A symlink to `/etc/passwd`, a FIFO, or a device is rejected before execution.

**Cancellation rules:** `GlobTool.ExecuteCtx`, `GrepTool.ExecuteCtx`, and `ListTool.ExecuteCtx` check `ctx.Err()` during their walks and append no result produced after cancellation. `RgrepTool.run` derives its timeout from the caller context (`context.WithTimeout(ctx, rgTimeout)`) instead of `context.Background()`, keeps the existing 60-second ceiling as an inner cap, and kills the spawned search process on cancellation; `Wait` returns promptly. Sensitive-file ignores appear in the real ripgrep argv, not only in a post-result filter.

**Red-green steps:**
- [ ] **Step 1: Write the failing tests.**
```go
func TestFileTools_ProjectRootConfinementAndSymlinkEscape(t *testing.T)
func TestFileTools_SensitivePathsFiltered(t *testing.T)
func TestFileTools_GrepSkipsSensitiveBeforeRead(t *testing.T)
func TestSearchTools_CancelPromptly(t *testing.T)
func TestRgrep_CancelTerminatesProcess(t *testing.T)
func TestRgrep_SensitiveIgnoresInArgv(t *testing.T)
```
Cover: `../x`, an absolute path outside the root, and a symlink to `/etc/passwd` are rejected; a missing nested path resolves its deepest existing ancestor; `.env`, `id_rsa`, `.git/config`, and login-state dirs never appear in `glob`/`list` output, `read` refuses them, and `grep` never opens them; the fallback walker checks the predicate before opening and grep returns no sensitive content; cancelling a walk over a large tree returns promptly with no post-cancel results; cancelling `RgrepTool.run` terminates the child instead of waiting out its 60-second timeout; injected sensitive-file ignores are present in the real ripgrep argv.

- [ ] **Step 2: Run to verify failure.**
`go test ./internal/agent/ -run 'TestFileTools' -count=1 && go test ./internal/tool/ -run 'TestSearchTools_CancelPromptly|TestRgrep_CancelTerminatesProcess|TestRgrep_SensitiveIgnoresInArgv' -count=1`
Expected: FAIL — missing wrappers; cancellation assertions fail against `context.Background()`.

- [ ] **Step 3: Implement the wrappers and cancellation.** In `tool_digest_files.go`, implement `validateProjectPath`, `sensitivePathFilter`, `newToolDigestFileTools`, and `registerToolDigestFileTools`, then extend `newToolDigestRunnerToolbox` to register the four tools after `call_parameters`. In `internal/tool/search.go` and `internal/tool/rgrep.go`, propagate `ctx` through walks, kill the spawned process on cancellation, and add the sensitive ignore patterns to ripgrep's argv. Never append partial output after `ctx.Done()`.

- [ ] **Step 4: Run to verify pass.**
`go test ./internal/agent/ -run 'TestFileTools' -count=1 && go test ./internal/tool/ -run 'TestSearchTools_CancelPromptly|TestRgrep_CancelTerminatesProcess|TestRgrep_SensitiveIgnoresInArgv' -count=1 -race`
Also run `gofmt` and `go vet` on `internal/agent` and `internal/tool`. Expected: PASS.
