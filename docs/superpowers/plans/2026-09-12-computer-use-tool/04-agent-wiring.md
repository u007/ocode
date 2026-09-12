# Part 04: Agent wiring — driver attach and path-less image results

**Files:**
- Create: `internal/computer/driver.go` (only the constructor stub for this part; the real per-GOOS constructors arrive later, so this part ships `New` returning a typed "unsupported" error until Part 06 replaces the file)
- Modify: `internal/agent/agent.go` (tool install block around lines 1039-1061 where `bash_output`/`kill_shell`/`list_processes` get `Procs`; `handleToolCallWithImages` around line 2821)
- Create: `internal/agent/computer_tool_test.go`

**Interfaces:**
- Consumes: `tool.ComputerTool`, `tool.ImageProducingTool`, `tool.ComputerDriver` (Part 03); `a.procs.Supervisor()` from `tool.ProcessRegistry`.
- Produces: `computer.New(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error)` (signature fixed here; Part 06 fills it in). Agent attaches the driver at construction when the `computer` tool is registered.

## Behaviour

- In the agent's tool-install block: if `a.tools["computer"]` is a `*tool.ComputerTool`, construct the driver with `computer.New(a.procs.Supervisor())`. On error, `a.emitDebug("WARN", ...)` with the error and delete the tool from `a.tools` so it is never advertised. `a.procs` may be nil in tests: guard and treat as "no supervisor" (delete tool, debug log).
- `handleToolCallWithImages`: today it returns early when `imageReadPath(args)` fails. Change: if the tool implements `tool.ImageProducingTool` and `ProducesImage(args)` is true, skip the path lookup, call `ExecuteImage`, and use the note `"[screenshot — shown below]"` (with the same downscale note if `enc.Scaled`, replacing "image file: path" wording with "screenshot"). Otherwise keep the existing path branch untouched.

## Steps

- [ ] **Step 1: Write failing tests** in `computer_tool_test.go` (package `agent`, follow `read_image_test.go` for building an agent with a vision-capable fake model):
  - `TestHandleToolCallWithImages_ComputerScreenshotEmbedsImage`: install a `*tool.ComputerTool` with a fake driver (define a small fake in the test file) on the agent, call the images variant with `{"action":"screenshot"}`, assert one `Image` returned and the text starts with `[screenshot`.
  - `TestHandleToolCallWithImages_ComputerClickNoImage`: `{"action":"left_click","coordinate":[1,1]}` → nil images.
  - `TestAgent_DropsComputerToolWhenDriverUnavailable`: build an agent with a config where `ComputerUse.Enabled` is true but `computer.New` returns the unsupported error (this part's stub does exactly that) → `GetTool("computer")` reports absent.

- [ ] **Step 2: Run** `go test ./internal/agent -run 'ComputerScreenshot|ComputerClick|DropsComputerTool' -v`. Expected: FAIL.

- [ ] **Step 3: Implement** the stub `internal/computer/driver.go` (`package computer`, `New` returns `fmt.Errorf("computer: unsupported platform %s", runtime.GOOS)`), the attach block, and the `handleToolCallWithImages` change.

- [ ] **Step 4: Run** the tests, then `go test ./internal/agent -run 'Image|Computer'` and `go vet ./internal/agent ./internal/computer`. Expected: PASS.

- [ ] **Step 5: Commit** `git add internal/computer/driver.go internal/agent/agent.go internal/agent/computer_tool_test.go && git commit -m "feat(agent): attach computer driver, embed path-less screenshot results"`.
