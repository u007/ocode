# Part 06: `internal/computer` shared runner and per-GOOS constructors

**Files:**
- Modify: `internal/computer/driver.go` (replace the Part 04 stub: keep `New`, move the platform selection behind build tags)
- Create: `internal/computer/runner.go`
- Create: `internal/computer/runner_test.go`
- Create: `internal/computer/runner_stub_test.go`
- Create: `internal/computer/new_darwin.go`, `internal/computer/new_windows.go`, `internal/computer/new_linux.go`, `internal/computer/new_other.go` (build tag `!darwin && !windows && !linux`)
- Create: `internal/computer/tempfile.go`

**Interfaces:**
- Consumes: `tool.StartSupervised`, `tool.ProcessRegistration`, `tool.ProcessKindComputer`, `tool.ComputerDriver`.
- Produces, in package `computer`:
  - `func New(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error)` — returns an error when `sup` is nil (`computer: process supervisor required`), otherwise the GOOS driver via `newPlatformDriver(sup)` defined in each `new_<os>.go`; `new_other.go` returns the unsupported-platform error.
  - `type commandRunner interface { run(ctx context.Context, name string, args ...string) (stdout string, err error); runStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) }` — drivers in later parts hold a `commandRunner` so tests can substitute a recording stub.
  - `type execRunner struct { sup *tool.ProcessSupervisor }` implementing `commandRunner` via `tool.StartSupervised`.
  - `type stubRunner` lives in `runner_stub_test.go` (test-only): records `[]string{name, args...}` per call and returns a configurable `(stdout, err)`; shared by the driver tests in later parts.
  - `func tempPNGPath() (string, error)` creating a unique path under `os.TempDir()` with prefix `ocode-computer-` and `.png` suffix.
  - `func readAndRemove(path string) ([]byte, error)`.

## Behaviour of `execRunner.run`

- Builds `exec.CommandContext`, captures stdout into a `tool.BoundedBuffer`-style limit of 1 MiB (reuse `internal/tool/bounded_buffer.go` if exported; otherwise a local `io.LimitReader` into a `bytes.Buffer`), stderr into a separate buffer.
- Registers via `tool.StartSupervised` with `ID: "computer-<name>-<unix nano>"`, `Name: "computer " + name`, `Command: name + " " + strings.Join(args, " ")`, `Kind: tool.ProcessKindComputer`.
- Waits, then `sup.MarkExited` / `sup.MarkKilled` following the `piperSynth` pattern in `internal/tts/piper.go:322-359`.
- Non-zero exit → `fmt.Errorf("computer %s: %w: %s", name, err, trimmed stderr)`. Context deadline → return `ctx.Err()`.
- `errors.Is(err, exec.ErrNotFound)` is preserved through wrapping so drivers can detect a missing binary.

## Steps

- [ ] **Step 1: Write failing tests** in `runner_test.go` (skip on windows with `t.Skip` where the command differs, or branch on GOOS to use `cmd /c echo`):
  - `TestRunner_CapturesStdout`: `run(ctx, "echo", "hi")` → `"hi\n"`, supervisor `Snapshot()` shows one record with kind `computer` and status exited.
  - `TestRunner_NonZeroExitIncludesStderr`: `sh -c "echo bad >&2; exit 3"` → error text contains `bad`.
  - `TestRunner_MissingBinary`: `run(ctx, "ocode-definitely-missing-binary")` → `errors.Is(err, exec.ErrNotFound)`.
  - `TestRunner_Timeout`: `sleep 5` with a 100ms context → returns within 1s with a context error.
  - `TestNew_NilSupervisor`: error.
  - `TestTempPNGPath_UnderTempDir`.

- [ ] **Step 2: Run** `go test ./internal/computer -v`. Expected: FAIL.

- [ ] **Step 3: Implement** runner, tempfile helpers, `New`, and the four `new_*.go` files. For this part each `newPlatformDriver` may return the unsupported error; Parts 07-09 replace them.

- [ ] **Step 4: Run** `go test ./internal/computer` and `go vet ./internal/computer`; cross-compile `GOOS=windows go build ./internal/computer/ && GOOS=linux go build ./internal/computer/ && GOOS=freebsd go build ./internal/computer/`.

- [ ] **Step 5: Commit** `git add internal/computer && git commit -m "feat(computer): supervised runner and platform constructor scaffold"`.
