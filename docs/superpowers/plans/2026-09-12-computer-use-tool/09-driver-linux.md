# Part 09: Linux driver

**Files:**
- Create: `internal/computer/driver_linux.go` (build tag `linux`)
- Create: `internal/computer/session_linux_shared.go` (tag-free: `func linuxBackend(env func(string) string) string` returning `"wayland"` or `"x11"`, and argv builders so tests compile on every GOOS)
- Create: `internal/computer/driver_linux_test.go`
- Create: `internal/computer/driver_linux_live_test.go` (build tag `linux`, skipped unless `OCODE_COMPUTER_LIVE=1`)
- Modify: `internal/computer/new_linux.go`

**Interfaces:**
- Consumes: `runner.run`, `tempPNGPath`, `readAndRemove`, `tool.ComputerDriver`, `tool.MouseButton`, `tool.NoticedError`.
- Produces: `linuxDriver{backend string}`; `func xdotoolArgs(op string, ...) []string`, `func ydotoolArgs(op string, ...) []string`; `func linuxInstallHint(backend string) string`.

## Behaviour

- Backend: `XDG_SESSION_TYPE == "wayland"` → `ydotool` + `grim`; else `xdotool` + `scrot`.
- X11: screenshot `scrot -o <tmp>`; screen size `xdotool getdisplaygeometry` (prints `w h`); cursor `xdotool getmouselocation --shell` (parse `X=` `Y=`); move `xdotool mousemove x y`; click `xdotool mousemove x y click --repeat <count> <button>` (button 1/2/3 for left/middle/right); drag `mousemove x1 y1 mousedown 1 mousemove x2 y2 mouseup 1` as one xdotool command chain; scroll: buttons 4/5 (up/down), 6/7 (left/right) repeated `amount` times; type `xdotool type --delay 12 --file -` with text on stdin (`runStdin`); key `xdotool key <combo>` (xdotool names pass through unchanged; no keymap needed).
- Wayland: screenshot `grim <tmp>`; screen size from PNG dimensions (no separate query; document that fractional scaling is unsupported); cursor unsupported → error `computer: cursor_position not supported on wayland`; move `ydotool mousemove --absolute -x X -y Y`; click `ydotool click 0xC0` (left), `0xC1` right, `0xC2` middle, repeated for count; drag via `mousemove`, `click 0x40` (down), `mousemove`, `click 0x80` (up); scroll `ydotool mousemove --wheel -y ±amount`; type `ydotool type --file -`; key `ydotool key` with `<code>:1 <code>:0` pairs — requires a keymap from xdotool-style names to Linux input event codes for the same name set used in Parts 07/08 (`ctrl`→29, `shift`→42, `alt`→56, `super`→125, `Return`→28, `Tab`→15, `Escape`→1, `space`→57, `BackSpace`→14, `Delete`→111, arrows 103/108/105/106, `Home`102, `End`107, `Page_Up`104, `Page_Down`109, `F1`-`F12` 59-68 and 87/88, letters/digits per `linux/input-event-codes.h`).
- `exec.ErrNotFound` → `*tool.NoticedError` with `linuxInstallHint`: `apt install xdotool scrot` / `apt install ydotool grim` (plus the note that ydotool needs its daemon and uinput access, and grim needs a wlroots compositor).

## Steps

- [ ] **Step 1: Write failing tests**: `TestLinuxBackend_Detection`, `TestXdotoolArgs_ClickAndScroll`, `TestYdotoolArgs_ClickCodes`, `TestLinuxMissingBinaryIsNoticed` (both backends, hint text contains the package names), `TestYdotoolKeyCombo` (`ctrl+c` → `29:1 46:1 46:0 29:0`).
- [ ] **Step 2: Run** `go test ./internal/computer -run 'Linux|Xdotool|Ydotool' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** unit tests; `GOOS=linux go vet ./internal/computer/`. If a Linux desktop is available (X11 preferred) run the live test; otherwise note pending live verification in TODO.md under "Computer use".
- [ ] **Step 5: Commit** `git add internal/computer TODO.md && git commit -m "feat(computer): Linux driver via xdotool/scrot and ydotool/grim"`.
