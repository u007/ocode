# Computer Use Tool — Design

Date: 2026-09-12
Status: approved

## Goal

Give the ocode agent a cross-platform `computer` tool that can see the host
desktop (screenshot) and drive it (mouse, keyboard) on macOS, Windows and
Linux without adding CGO to the build.

## Non-goals (v1)

- Multi-display selection (primary display only).
- Zoom / region capture, `hold_key`, `triple_click`.
- Driving a remote SSH workspace's desktop. The driver acts on the machine
  running the ocode process.
- Web settings UI toggle (tracked in TODO.md; slash command + config only).

## Approach

One tool, `computer`, whose `action` enum mirrors the Anthropic
computer-use schema so models trained on it work unprompted. Other providers
see an ordinary JSON tool. Platform work is isolated in a new
`internal/computer` package behind a `Driver` interface with one
implementation per GOOS selected by build tags. All OS interaction is a
shell-out to tools present on a stock install (macOS, Windows) or installable
via the package manager (Linux), spawned through the shared
`tool.StartSupervised` path.

## Package `internal/computer`

```go
type Button int // Left, Right, Middle

type Driver interface {
    // Screenshot captures the primary display as PNG. screenW/screenH are the
    // display size in the OS input coordinate space (macOS points, Windows
    // DPI-aware pixels, X11 pixels), which may differ from the PNG's pixel
    // size on HiDPI displays.
    Screenshot(ctx context.Context) (png []byte, screenW, screenH int, err error)
    Click(ctx context.Context, x, y int, button Button, count int) error
    Move(ctx context.Context, x, y int) error
    Drag(ctx context.Context, x1, y1, x2, y2 int) error
    // Scroll moves the wheel at (x,y). dir is "up"|"down"|"left"|"right".
    Scroll(ctx context.Context, x, y int, dir string, amount int) error
    Type(ctx context.Context, text string) error
    // Key presses an xdotool-style combo, e.g. "ctrl+s", "Return", "cmd+shift+4".
    Key(ctx context.Context, combo string) error
    Cursor(ctx context.Context) (x, y int, err error)
}

func New(sup *tool.ProcessSupervisor) (Driver, error) // build-tag selected
```

Every driver call gets a 10s context timeout (60s for `Type`) and a capped
stdout buffer. All coordinates crossing the Driver boundary are in the OS
input coordinate space reported by `Screenshot`.

### darwin

- Screenshot: `screencapture -x -D 1 -t png <tmpfile>` under `os.TempDir()`
  (the sandbox already guarantees that dir is writable). Display size in
  points comes from `$.NSScreen.mainScreen.frame` in the same JXA helper.
  A denied Screen Recording grant does not fail: macOS silently captures
  the wallpaper only. There is no reliable signal, so the grant is
  documented in `docs/computer-use.md` and printed by `/computer status`
  on darwin (System Settings → Privacy & Security → Screen Recording, for
  the terminal or ocode-desktop app).
- Input: `osascript -l JavaScript` executing an embedded JXA script that uses
  the ObjC bridge (`$.CGEventCreateMouseEvent`, `$.CGEventPost`,
  `$.CGEventCreateKeyboardEvent`, `$.CGEventKeyboardSetUnicodeString`,
  `$.CGEventCreateScrollWheelEvent`). One script, parameters passed as argv.
  Event-post failures map to a `NoticedError` naming the Accessibility
  permission. Struct arguments (`CGPoint`) through the JXA bridge
  work: spike 2026-09-12 on macOS 26.6.2 confirmed `$.CGPointMake`
  as the point-construction form, cursor round-trips correctly,
  no TCC/accessibility error.
- Cursor: same JXA path via `$.CGEventGetLocation($.CGEventCreate(null))`.
- Key names: table maps xdotool-style names (`Return`, `Tab`, `Escape`,
  `ctrl`, `cmd`, `alt`, `shift`, arrows, F-keys, letters/digits) to macOS
  virtual keycodes; unknown name is an error, not a guess.

### windows

- One embedded PowerShell script run with
  `powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File`.
  `Add-Type` a C# helper: `user32.dll` `SendInput`, `SetCursorPos`,
  `GetCursorPos`; screenshot via `System.Drawing.Graphics.CopyFromScreen`
  on the primary screen bounds, saved as PNG to a temp file.
- Keys via `SendInput` scan codes; text via `KEYEVENTF_UNICODE`.
- DPI: the script calls `SetProcessDPIAware()` first so coordinates and
  capture are in physical pixels.

### linux

- Session detection: `XDG_SESSION_TYPE == "wayland"` → `ydotool` + `grim`;
  otherwise `xdotool` + `scrot`. Wayland support is wlroots-only (`grim`
  does not work on GNOME/KDE; `ydotool` needs its daemon and uinput
  access). Documented as a limitation.
- Missing binary → `NoticedError` with an install hint
  (`apt install xdotool scrot` / `apt install ydotool grim`).
- Screenshot to temp PNG, read, delete.

## Tool `internal/tool/computer.go`

```go
type ComputerTool struct {
    Config *config.Config
    Driver computer.Driver
}
```

Implements `Tool`, `ContextualTool`, `ImageResultTool`. `Parallel()` is
false.

Definition:

```
action: enum [screenshot, left_click, right_click, middle_click,
              double_click, mouse_move, left_click_drag, scroll, type,
              key, cursor_position, wait]
coordinate: [x, y]        (click/move/scroll/drag start)
start_coordinate: [x, y]  (left_click_drag)
text: string              (type, key)
scroll_direction: enum [up, down, left, right]
scroll_amount: int        (default 3)
duration: number seconds  (wait, max 10)
```

### Scaling

Screenshots are downscaled in pure Go (`golang.org/x/image/draw`, a direct
dependency) so the longest side is ≤ 1568px. The tool remembers
`scale = screenW / scaledW` from the last screenshot (screenW in the OS
input space, per the Driver contract) and multiplies incoming coordinates
by it before calling the driver. Before any screenshot has been taken,
scale is derived by a throwaway capture on the first input action. The
text result of `screenshot` reports
`screen 1440x900, image 1440x900, scale 1.00` (macOS Retina example: the
PNG arrives at 2880x1800 pixels, is scaled to 1568x980, and the reported
scale is 1440/1568 = 0.92).

### Results

- `screenshot`: `ExecuteImage` returns the scaled PNG; `Execute` returns the
  size line above.
- Input actions: one-line confirmation, e.g. `left_click at 412,300`.
- `cursor_position`: `cursor 412,300` in scaled coordinates.
- `wait`: sleeps, then returns `waited 2s`. Does not auto-screenshot.

### Registration

`LoadBuiltins` appends `ComputerTool` only when
`cfg.Ocode.ComputerUse.Enabled` is true. When disabled the tool is not
advertised at all (unlike `ocr`), because desktop control should not be
discoverable by the model unless the user opted in. Driver construction
errors (unsupported GOOS) log at warn and skip registration.

## Config and commands

- `ocode.computer_use.enabled` bool, default false, in `ocodeconfig.go`
  with a targeted load-modify-write saver (never `SaveOcodeConfig` on an
  in-memory snapshot).
- Slash command `/computer enable|disable|status` in `internal/commands`,
  mirroring `/ocr`. Enabling takes effect on the next agent tool reload
  (same behaviour as other plugin toggles; status line says so).

## Permissions

- `computer` gets a per-action decision in `internal/agent/permissions.go`:
  - `screenshot`, `cursor_position`, `wait` → allow.
  - all other actions → `PermissionAsk` with request kind `computer.input`
    and a request summary such as `computer: left_click at 412,300`.
- "Always" persists the `computer` rule to allow via the existing
  `SetRule` path, so subsequent input actions in that session (or forever,
  per the user's choice) do not prompt.
- The tool runs on the host process and is never sandboxed; the `bash`
  sandbox model is not involved.

## Error handling

- Driver errors wrap the command and stderr (`computer left_click: osascript:
  <stderr>`), logged via debuglog with action and args.
- Permission-class failures (macOS TCC, missing Linux binaries) are
  `NoticedError` so the user sees the fix and the model sees a short error.
- Invalid coordinates (negative, or beyond the last known screen size) are
  rejected before the driver is called.

## Testing

- `internal/tool/computer_test.go` with a fake `Driver`: action parsing,
  required-field validation, coordinate scaling round-trip, disabled gate,
  image result path.
- Per-OS driver tests assert the constructed argv / embedded script
  parameters without executing (darwin: JXA argv; windows: PowerShell argv;
  linux: xdotool/ydotool argv and session detection).
- Live smoke tests per OS skipped unless `OCODE_COMPUTER_LIVE=1`.
- Permission tests: action classification and "always" persistence.

## Docs

- `docs/computer-use.md`: enabling, OS prerequisites (macOS TCC grants,
  Linux packages), permission behaviour, limitations.
- AGENTS.md pointer, CHANGES.md entry, README tool table row, TODO.md entry
  for the web settings toggle and multi-display support.
