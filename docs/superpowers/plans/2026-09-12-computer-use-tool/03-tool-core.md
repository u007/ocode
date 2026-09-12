# Part 03: Driver interface, ComputerTool, registration

**Files:**
- Create: `internal/tool/computer_driver.go`
- Create: `internal/tool/computer.go`
- Create: `internal/tool/computer_test.go`
- Modify: `internal/tool/tool.go` (`InitBuiltinTools`, after the `ImageGenTool` append around line 157; the optional-interface block near `ImageResultTool` around line 26)
- Modify: `internal/tool/process_supervisor.go` (add `ProcessKindComputer ProcessKind = "computer"` after `ProcessKindTTS`)

**Interfaces:**
- Consumes: `config.Config` with `Ocode.ComputerUse.Enabled` (Part 02).
- Produces, in package `tool`:
  - `type MouseButton int` with constants `MouseLeft`, `MouseRight`, `MouseMiddle`.
  - `type ComputerDriver interface` with methods exactly as the spec's `Driver`: `Screenshot(ctx) (png []byte, screenW, screenH int, err error)`, `Click(ctx, x, y int, button MouseButton, count int) error`, `Move(ctx, x, y int) error`, `Drag(ctx, x1, y1, x2, y2 int) error`, `Scroll(ctx, x, y int, dir string, amount int) error`, `Type(ctx, text string) error`, `Key(ctx, combo string) error`, `Cursor(ctx) (x, y int, err error)`.
  - `type ImageProducingTool interface { ImageResultTool; ProducesImage(args json.RawMessage) bool }` — lets the agent embed an image result for a call that has no file path.
  - `type ComputerTool struct { Config *config.Config; Driver ComputerDriver; ... }` with `Name() == "computer"`, implementing `Tool`, `ContextualTool`, `ImageProducingTool`. `Parallel()` false.
  - `const ComputerMaxImageSide = 1568`.
  - `ProcessKindComputer`.

## Behaviour to implement

- Definition per spec: `action` enum `[screenshot, left_click, right_click, middle_click, double_click, mouse_move, left_click_drag, scroll, type, key, cursor_position, wait]`; `coordinate` `[x,y]`; `start_coordinate` `[x,y]`; `text`; `scroll_direction` enum `[up,down,left,right]`; `scroll_amount` int default 3; `duration` number seconds max 10. Description tells the model: coordinates are in the pixel space of the most recent screenshot; take a screenshot first; each action returns one line.
- `ExecuteCtx`: unmarshal, validate required fields per action (missing `coordinate` for click/move/scroll/drag, missing `start_coordinate` for drag, missing `text` for type/key, `duration` outside 0..10, unknown action or direction → error, not a guess). `Execute` delegates to `ExecuteCtx(context.Background(), ...)`.
- Driver nil → error `computer: driver not attached`.
- Scale state (mutex-protected): `screenW, screenH` from the last `Screenshot`, `imgW, imgH` of the scaled PNG, `lastPNG []byte`. `scaleX = screenW / imgW`, `scaleY = screenH / imgH`. If no screenshot yet and an input action arrives, call `Screenshot` once to seed the state (do not keep the PNG as `lastPNG`).
- Coordinate mapping: `driverX = round(x * scaleX)`, same for y. Reject negative or `> imgW/imgH` coordinates before calling the driver.
- Downscale in a helper `scalePNG(raw []byte, maxSide int) (png []byte, w, h int, err error)` using `image/png` decode and `golang.org/x/image/draw` (`draw.CatmullRom` or `draw.ApproxBiLinear`; pick one, no option). Returns the original bytes and dims when already within bounds.
- Results: `screenshot` → `screen %dx%d, image %dx%d, scale %.2f` (report `scaleX`); clicks → `<action> at x,y` in image coordinates; `mouse_move` → `moved to x,y`; `left_click_drag` → `dragged x1,y1 -> x2,y2`; `scroll` → `scrolled <dir> <amount> at x,y`; `type` → `typed <n> chars`; `key` → `pressed <combo>`; `cursor_position` → `cursor x,y` (driver coords divided by scale); `wait` → sleeps with `ctx` cancellation and returns `waited <d>s`.
- `ProducesImage(args)` is true only when `action == "screenshot"`. `ExecuteImage` returns `lastPNG, "image/png"` or an error if no screenshot has been taken.
- Timeouts: wrap the driver call in `context.WithTimeout(ctx, 10*time.Second)`, 60s for `type`.
- Registration in `InitBuiltinTools`: `if cfg != nil && cfg.Ocode.ComputerUse.Enabled { builtins = append(builtins, &ComputerTool{Config: cfg}) }` with a comment explaining why it is not always-advertised (unlike `ocr`).

## Steps

- [ ] **Step 1: Write failing tests** in `computer_test.go` with a `fakeDriver` struct recording every call and returning a fixed 2880x1800 PNG (generate with `image.NewRGBA` + `png.Encode` in a helper) and `screenW=1440, screenH=900`:
  - `TestComputerTool_ScreenshotScalesAndReportsScale`: result line is `screen 1440x900, image 1568x980, scale 0.92`; `ExecuteImage` returns a PNG decoding to 1568x980; `ProducesImage` true.
  - `TestComputerTool_ClickMapsCoordinates`: after a screenshot, `left_click` at `[784,490]` calls `Click` with `(720,450, MouseLeft, 1)`; `double_click` count 2; `right_click`/`middle_click` buttons.
  - `TestComputerTool_InputSeedsScaleWithoutScreenshot`: fresh tool, `mouse_move` first → driver `Screenshot` called once, then `Move` with scaled coords, `ExecuteImage` returns error (no image retained).
  - `TestComputerTool_RejectsOutOfBounds`: coordinate `[2000, 10]` after screenshot → error mentioning bounds; driver not called.
  - `TestComputerTool_ValidationErrors`: table over missing `coordinate`, missing `text`, unknown action, unknown `scroll_direction`, `duration` 11.
  - `TestComputerTool_DragScrollTypeKeyCursorWait`: each action reaches the driver with expected args and returns the documented line; `wait` 0.01s returns quickly.
  - `TestComputerTool_NoDriver`: `Driver == nil` → error `computer: driver not attached`.
  - `TestComputerTool_ProducesImageOnlyForScreenshot`.
  - `TestInitBuiltinTools_ComputerGatedByConfig`: with `Enabled` false the `computer` tool is absent; true → present. Build the `config.Config` the way `internal/tool/tool_test.go` does.
  - `TestScalePNG_WithinBoundsUnchanged` and `TestScalePNG_Downscales`.

- [ ] **Step 2: Run** `go test ./internal/tool -run 'ComputerTool|ScalePNG|ComputerGated' -v`. Expected: compile failure.

- [ ] **Step 3: Implement** `computer_driver.go` (interfaces, button consts), `computer.go` (tool), the `ImageProducingTool` interface in `tool.go`, the process kind, and the gated registration.

- [ ] **Step 4: Run** the same tests, then `go test ./internal/tool` and `go vet ./internal/tool`. Expected: PASS.

- [ ] **Step 5: Cross-compile check** `GOOS=windows go build ./internal/tool/ && GOOS=linux go build ./internal/tool/`.

- [ ] **Step 6: Commit** `git add internal/tool/computer_driver.go internal/tool/computer.go internal/tool/computer_test.go internal/tool/tool.go internal/tool/process_supervisor.go && git commit -m "feat(tool): computer tool core with driver interface"`.
