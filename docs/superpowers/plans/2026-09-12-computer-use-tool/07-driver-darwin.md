# Part 07: macOS driver

**Files:**
- Create: `internal/computer/driver_darwin.go` (build tag `darwin`)
- Create: `internal/computer/cgevent_darwin.js` (embedded with `//go:embed`; JXA script)
- Create: `internal/computer/keymap_darwin.go`
- Create: `internal/computer/driver_darwin_test.go`
- Create: `internal/computer/driver_darwin_live_test.go` (skipped unless `OCODE_COMPUTER_LIVE=1`)
- Modify: `internal/computer/new_darwin.go` (`newPlatformDriver(r commandRunner)` returns `&darwinDriver{r: r}`)

**Interfaces:**
- Consumes: `commandRunner`, `stubRunner` (test), `tempPNGPath`, `readAndRemove` (Part 06); `tool.ComputerDriver`, `tool.MouseButton` (Part 03); the spike decision recorded in the spec's darwin section (Part 01) selecting JXA CGEvent or System Events.
- Produces: `darwinDriver` implementing `tool.ComputerDriver`; `func darwinKeyCode(name string) (code int, isModifier bool, ok bool)`; `func darwinArgs(op string, params ...string) []string` returning the argv used for `osascript` so tests can assert it.

## Behaviour (JXA path; if the spike chose System Events, replace the input ops below with an embedded AppleScript that uses `tell application "System Events"` `click at {x, y}`, `keystroke`, `key code`, keeping the same Go interface and tests)

- `Screenshot`: `screencapture -x -D 1 -t png <tmp>` via `run`; then `osascript -l JavaScript <script> screen` printing `w h` of `$.NSScreen.mainScreen.frame.size`; parse ints; read and remove the temp PNG. Return `png, w, h`.
- Script `cgevent_darwin.js` reads `op` and parameters from `$.NSProcessInfo.processInfo.arguments` (index offset after the script path) and implements ops: `screen`, `cursor` (prints `x y`), `move x y`, `click x y button count` (down/up pairs with `CGEventSetIntegerValueField` `kCGMouseEventClickState` set to the count for double-click), `drag x1 y1 x2 y2` (down at start, move to end, up), `scroll x y dir amount` (warp to x,y, then `CGEventCreateScrollWheelEvent` with sign by direction; horizontal uses the second axis), `type text` (chunks of ≤ 20 chars via `CGEventKeyboardSetUnicodeString` on keydown+keyup), `key codes...` (modifier keycodes pressed in order, main key down/up, modifiers released in reverse). Use whichever CGPoint form the spike confirmed.
- `Key(combo)`: split on `+`, map each token via `darwinKeyCode` (xdotool-style names: `ctrl`→59, `cmd`/`super`→55, `alt`→58, `shift`→56, `Return`→36, `Tab`→48, `Escape`→53, `space`→49, `BackSpace`→51, `Delete`→117, arrows 123-126, `Home`115, `End`119, `Page_Up`116, `Page_Down`121, `F1`-`F12`, letters a-z and digits 0-9 by ANSI keycodes). Unknown → error `computer key: unknown key "X"`. Exactly one non-modifier is required.
- Error mapping: stderr containing `not allowed to send keystrokes` or `accessibility` → `*tool.NoticedError` with notice naming System Settings → Privacy & Security → Accessibility for the terminal or ocode-desktop app. `exec.ErrNotFound` cannot happen for stock tools, so no special case.
- All coordinates in points, as received.

## Steps

- [ ] **Step 1: Write failing tests** in `driver_darwin_test.go`:
  - `TestDarwinKeyCode_KnownAndUnknown`: `ctrl`, `cmd`, `Return`, `a`, `F5` map; `Bogus` → ok false.
  - `TestDarwinArgs_Click`: `darwinArgs("click", "10", "20", "left", "2")` equals `[]string{"-l","JavaScript","<embedded script path>","click","10","20","left","2"}` — assert the tail after the script path, since the script is written to a temp file at driver construction (or passed with `-e`; choose one and assert it).
  - `TestDarwinKeyCombo_OrdersModifiers`: `Key` on a driver built over `stubRunner` records argv `key 59 56 0` for `ctrl+shift+a`.
  - `TestDarwinKeyCombo_RejectsTwoMainKeys`: `a+b` → error.
  - `TestDarwinAccessibilityErrorIsNoticed`: `stubRunner` returns an error containing `not allowed to send keystrokes` → returned error `errors.As` a `*tool.NoticedError`.

- [ ] **Step 2: Run** `go test ./internal/computer -run 'Darwin' -v`. Expected: FAIL.

- [ ] **Step 3: Implement** the driver, keymap and script.

- [ ] **Step 4: Run** unit tests, then the live test: `OCODE_COMPUTER_LIVE=1 go test ./internal/computer -run 'DarwinLive' -v`. Live test: screenshot returns a decodable PNG with `screenW > 0`; `Cursor` returns ints; `Move` to (100,100) then `Cursor` equals (100,100). It must not click or type.

- [ ] **Step 5: Cross-compile** `GOOS=linux go build ./internal/computer/ && GOOS=windows go build ./internal/computer/`.

- [ ] **Step 6: Commit** `git add internal/computer && git commit -m "feat(computer): macOS driver via screencapture and CGEvent"`.
