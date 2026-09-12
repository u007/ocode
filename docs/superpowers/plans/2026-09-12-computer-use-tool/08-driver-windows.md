# Part 08: Windows driver

**Files:**
- Create: `internal/computer/driver_windows.go` (build tag `windows`)
- Create: `internal/computer/input_windows.ps1` (embedded with `//go:embed`)
- Create: `internal/computer/keymap_windows.go`
- Create: `internal/computer/driver_windows_test.go` (runs on all GOOS: it tests argv and keymap; put it under no build tag but keep the driver type's argv builder in a tag-free file `args_windows_shared.go` so the test compiles everywhere)
- Create: `internal/computer/driver_windows_live_test.go` (build tag `windows`, skipped unless `OCODE_COMPUTER_LIVE=1`)
- Modify: `internal/computer/new_windows.go`

**Interfaces:**
- Consumes: `runner.run`, `tempPNGPath`, `readAndRemove`, `tool.ComputerDriver`, `tool.MouseButton`, `tool.NoticedError`.
- Produces: `windowsDriver`; `func windowsArgs(scriptPath, op string, params ...string) []string` returning `[]string{"-NoProfile","-NonInteractive","-ExecutionPolicy","Bypass","-File",scriptPath,op,params...}`; `func windowsVirtualKey(name string) (vk int, isModifier bool, ok bool)`.

## Behaviour

- On construction, write the embedded script once to `os.TempDir()/ocode-computer-<pid>.ps1` (0600) and remember the path. Remove it via `sup.RegisterShutdownCallback`.
- Script: `Add-Type` a C# class `OcodeInput` with P/Invoke for `SetProcessDPIAware`, `SetCursorPos`, `GetCursorPos`, `SendInput` (`INPUT` struct with mouse and keyboard unions), plus `System.Windows.Forms`/`System.Drawing` for `Screen.PrimaryScreen.Bounds` and `Graphics.CopyFromScreen`. Ops: `screen` (prints `w h` of primary bounds after DPI-aware), `screenshot <path>`, `cursor`, `move x y`, `click x y button count`, `drag x1 y1 x2 y2`, `scroll x y dir amount` (`MOUSEEVENTF_WHEEL` / `MOUSEEVENTF_HWHEEL`, 120 per notch), `type <text>` (`KEYEVENTF_UNICODE` per UTF-16 unit, text passed base64 to avoid quoting), `key vk...` (modifiers down, main down/up, modifiers up reverse).
- `Screenshot`: run `screenshot <tmp>` then `screen`; read and remove the PNG.
- Keymap: xdotool-style names to virtual-key codes (`ctrl`→0x11, `alt`→0x12, `shift`→0x10, `super`/`cmd`→0x5B, `Return`→0x0D, `Tab`→0x09, `Escape`→0x1B, `space`→0x20, `BackSpace`→0x08, `Delete`→0x2E, arrows 0x25-0x28, `Home`0x24, `End`0x23, `Page_Up`0x21, `Page_Down`0x22, `F1`-`F12` 0x70-0x7B, letters/digits by ASCII uppercase). Unknown → error.
- `powershell` missing (`exec.ErrNotFound`) → `*tool.NoticedError` "PowerShell not found on PATH".

## Steps

- [ ] **Step 1: Write failing tests**: `TestWindowsArgs_Type` (base64 of text present), `TestWindowsVirtualKey_KnownAndUnknown`, `TestWindowsKeyCombo_Order` (`ctrl+s` → `key 17 83`), `TestWindowsMissingPowershellIsNoticed` (stubbed runner returns `exec.ErrNotFound`).
- [ ] **Step 2: Run** `go test ./internal/computer -run 'Windows' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** unit tests on the host; `GOOS=windows go vet ./internal/computer/`. If a Windows machine or VM is available run `OCODE_COMPUTER_LIVE=1 go test ./internal/computer -run WindowsLive -v` (screenshot decodes; move then cursor round-trips). Otherwise state in the commit message that live verification is pending and add a TODO.md line under a "Computer use" heading.
- [ ] **Step 5: Commit** `git add internal/computer TODO.md && git commit -m "feat(computer): Windows driver via PowerShell SendInput"`.
