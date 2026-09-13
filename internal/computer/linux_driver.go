package computer

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// xdotoolTypeDelayMS is the per-keystroke delay xdotool uses when
// typing text, in milliseconds.
const xdotoolTypeDelayMS = "12"

// xdotoolScrollDelayMS is the delay between repeated wheel clicks on
// X11, in milliseconds.
const xdotoolScrollDelayMS = "20"

// linuxDriver implements tool.ComputerDriver for Linux using
// xdotool/scrot on X11 and ydotool/grim on Wayland.
//
// This file carries no build tag and no _linux filename suffix on
// purpose: the driver only uses portable stdlib, so its argv
// construction is unit-testable from any GOOS. Selection of the
// driver stays in new_linux.go, which is Linux-only.
type linuxDriver struct {
	r       commandRunner
	backend string // "wayland" or "x11"
}

// newLinuxDriver creates a linuxDriver with the
// detected backend (xdotool/scrot on X11, ydotool/grim on Wayland).
func newLinuxDriver(r commandRunner, _ *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return &linuxDriver{
		r:       r,
		backend: linuxBackend(os.Getenv),
	}, nil
}

// run executes a full argv built by xdotoolArgs/ydotoolArgs.
func (d *linuxDriver) run(ctx context.Context, argv []string) (string, error) {
	return d.r.run(ctx, argv[0], argv[1:]...)
}

// runStdin executes a full argv built by xdotoolArgs/ydotoolArgs,
// feeding stdin to the process.
func (d *linuxDriver) runStdin(ctx context.Context, stdin string, argv []string) (string, error) {
	return d.r.runStdin(ctx, stdin, argv[0], argv[1:]...)
}

// Screenshot captures the primary display.
// On X11: scrot captures the screen, xdotool reports display size.
// On Wayland: grim captures the screen, dimensions come from the PNG.
// Fractional scaling is unsupported (documented limitation).
func (d *linuxDriver) Screenshot(ctx context.Context) (pngData []byte, screenW, screenH int, err error) {
	tmp, err := tempPNGPath()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	// intentionally not logged: best-effort cleanup of the capture temp
	// file, which readAndRemove already deletes on the success path.
	defer func() { _ = os.Remove(tmp) }()

	if d.backend == "wayland" {
		if _, err := d.r.run(ctx, "grim", tmp); err != nil {
			return nil, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
		}
		pngData, err = readAndRemove(tmp)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
		}
		// Screen size comes from PNG dimensions on Wayland.
		cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
		}
		return pngData, cfg.Width, cfg.Height, nil
	}

	// X11: scrot + xdotool getdisplaygeometry
	if _, err := d.r.run(ctx, "scrot", "-o", tmp); err != nil {
		return nil, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
	}
	pngData, err = readAndRemove(tmp)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	out, err := d.run(ctx, xdotoolArgs("getdisplaygeometry"))
	if err != nil {
		return nil, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: unexpected screen size %q", out)
	}
	screenW, err = strconv.Atoi(fields[0])
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: screen width %q: %w", fields[0], err)
	}
	screenH, err = strconv.Atoi(fields[1])
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: screen height %q: %w", fields[1], err)
	}
	return pngData, screenW, screenH, nil
}

// xdotoolButton maps a mouse button to the X11 button number
// used by `xdotool click`: 1 left, 2 middle, 3 right.
func xdotoolButton(button tool.MouseButton) (string, error) {
	switch button {
	case tool.MouseLeft:
		return "1", nil
	case tool.MouseRight:
		return "3", nil
	case tool.MouseMiddle:
		return "2", nil
	}
	return "", fmt.Errorf("computer: unknown mouse button %d", int(button))
}

// ydotoolButton maps a mouse button to the ydotool click bitmask for a
// full press+release: bit 0x40 down, bit 0x80 up, low nibble the button
// (0 left, 1 right, 2 middle).
func ydotoolButton(button tool.MouseButton) (string, error) {
	switch button {
	case tool.MouseLeft:
		return "0xC0", nil
	case tool.MouseRight:
		return "0xC1", nil
	case tool.MouseMiddle:
		return "0xC2", nil
	}
	return "", fmt.Errorf("computer: unknown mouse button %d", int(button))
}

// Click performs a click at (x,y). Both backends move the pointer to
// the target first: `ydotool click` has no coordinate argument and
// would otherwise fire wherever the cursor happens to be.
func (d *linuxDriver) Click(ctx context.Context, x, y int, button tool.MouseButton, count int) error {
	if count < 1 {
		return fmt.Errorf("computer Click: count must be >= 1, got %d", count)
	}

	if d.backend == "wayland" {
		code, err := ydotoolButton(button)
		if err != nil {
			return err
		}
		if err := d.Move(ctx, x, y); err != nil {
			return err
		}
		for i := 0; i < count; i++ {
			if _, err := d.run(ctx, ydotoolArgs("click", code)); err != nil {
				return linuxError(fmt.Errorf("computer Click: %w", err), d.backend)
			}
		}
		return nil
	}

	code, err := xdotoolButton(button)
	if err != nil {
		return err
	}
	argv := xdotoolArgs("mousemove", strconv.Itoa(x), strconv.Itoa(y),
		"click", "--repeat", strconv.Itoa(count), code)
	if _, err := d.run(ctx, argv); err != nil {
		return linuxError(fmt.Errorf("computer Click: %w", err), d.backend)
	}
	return nil
}

// Move moves the cursor to (x,y).
func (d *linuxDriver) Move(ctx context.Context, x, y int) error {
	var argv []string
	if d.backend == "wayland" {
		argv = ydotoolArgs("mousemove", "--absolute", "--", strconv.Itoa(x), strconv.Itoa(y))
	} else {
		argv = xdotoolArgs("mousemove", strconv.Itoa(x), strconv.Itoa(y))
	}
	if _, err := d.run(ctx, argv); err != nil {
		return linuxError(fmt.Errorf("computer Move: %w", err), d.backend)
	}
	return nil
}

// Drag drags from (x1,y1) to (x2,y2).
// xdotool accepts a whole chain in one invocation; ydotool takes
// exactly one subcommand per invocation, so the Wayland path issues
// four separate commands.
func (d *linuxDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	if d.backend == "wayland" {
		steps := [][]string{
			ydotoolArgs("mousemove", "--absolute", "--", strconv.Itoa(x1), strconv.Itoa(y1)),
			ydotoolArgs("click", "0x40"), // left button down
			ydotoolArgs("mousemove", "--absolute", "--", strconv.Itoa(x2), strconv.Itoa(y2)),
			ydotoolArgs("click", "0x80"), // left button up
		}
		for _, argv := range steps {
			if _, err := d.run(ctx, argv); err != nil {
				return linuxError(fmt.Errorf("computer Drag: %w", err), d.backend)
			}
		}
		return nil
	}

	argv := xdotoolArgs("mousemove", strconv.Itoa(x1), strconv.Itoa(y1),
		"mousedown", "1",
		"mousemove", strconv.Itoa(x2), strconv.Itoa(y2),
		"mouseup", "1")
	if _, err := d.run(ctx, argv); err != nil {
		return linuxError(fmt.Errorf("computer Drag: %w", err), d.backend)
	}
	return nil
}

// xdotoolScrollButton maps a scroll direction to the X11 wheel button:
// 4 up, 5 down, 6 left, 7 right.
func xdotoolScrollButton(dir string) (string, error) {
	switch dir {
	case "up":
		return "4", nil
	case "down":
		return "5", nil
	case "left":
		return "6", nil
	case "right":
		return "7", nil
	}
	return "", fmt.Errorf("computer: unknown scroll direction %q", dir)
}

// ydotoolWheelDelta maps a scroll direction and amount to the
// REL_HWHEEL / REL_WHEEL values `ydotool mousemove --wheel` passes
// straight through to uinput. Per linux/input-event-codes.h, positive
// REL_WHEEL is up and positive REL_HWHEEL is right.
func ydotoolWheelDelta(dir string, amount int) (hwheel, wheel int, err error) {
	switch dir {
	case "up":
		return 0, amount, nil
	case "down":
		return 0, -amount, nil
	case "right":
		return amount, 0, nil
	case "left":
		return -amount, 0, nil
	}
	return 0, 0, fmt.Errorf("computer: unknown scroll direction %q", dir)
}

// Scroll moves the wheel at (x,y). Both backends move the pointer to
// the target first so the wheel event lands on the intended widget.
func (d *linuxDriver) Scroll(ctx context.Context, x, y int, dir string, amount int) error {
	if amount < 1 {
		return fmt.Errorf("computer Scroll: amount must be >= 1, got %d", amount)
	}

	if d.backend == "wayland" {
		hwheel, wheel, err := ydotoolWheelDelta(dir, amount)
		if err != nil {
			return err
		}
		if err := d.Move(ctx, x, y); err != nil {
			return err
		}
		// --wheel is relative only, and mousemove requires exactly two
		// coordinates, so both axes are always supplied. Coordinates go
		// after "--" as positional arguments: the -x/-y flags only exist
		// from ydotool 1.0.4, while the positional form works on every
		// 1.x release and accepts the negative wheel deltas.
		argv := ydotoolArgs("mousemove", "--wheel", "--",
			strconv.Itoa(hwheel), strconv.Itoa(wheel))
		if _, err := d.run(ctx, argv); err != nil {
			return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
		}
		return nil
	}

	button, err := xdotoolScrollButton(dir)
	if err != nil {
		return err
	}
	argv := xdotoolArgs("mousemove", strconv.Itoa(x), strconv.Itoa(y),
		"click", "--repeat", strconv.Itoa(amount), "--delay", xdotoolScrollDelayMS, button)
	if _, err := d.run(ctx, argv); err != nil {
		return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
	}
	return nil
}

// Type sends text as keystrokes. The text travels on stdin (--file -)
// so it never appears in an argv, a process listing, or supervisor
// records.
func (d *linuxDriver) Type(ctx context.Context, text string) error {
	var argv []string
	if d.backend == "wayland" {
		argv = ydotoolArgs("type", "--file", "-")
	} else {
		argv = xdotoolArgs("type", "--delay", xdotoolTypeDelayMS, "--file", "-")
	}
	if _, err := d.runStdin(ctx, text, argv); err != nil {
		return linuxError(fmt.Errorf("computer Type: %w", err), d.backend)
	}
	return nil
}

// Key presses an xdotool-style combo (e.g. "ctrl+s").
// On X11, key names pass through unchanged to xdotool key.
// On Wayland, uses ydotool with code:press/code:release pairs.
func (d *linuxDriver) Key(ctx context.Context, combo string) error {
	var argv []string
	if d.backend == "wayland" {
		codes, err := ydotoolKeyCombo(combo)
		if err != nil {
			return err
		}
		argv = ydotoolArgs("key", codes...)
	} else {
		argv = xdotoolArgs("key", combo)
	}
	if _, err := d.run(ctx, argv); err != nil {
		return linuxError(fmt.Errorf("computer Key: %w", err), d.backend)
	}
	return nil
}

// Cursor returns the current cursor position.
// On Wayland, cursor position is not supported (returns an error):
// no wlroots protocol exposes the pointer location to a client.
func (d *linuxDriver) Cursor(ctx context.Context) (x, y int, err error) {
	if d.backend == "wayland" {
		return 0, 0, fmt.Errorf("computer: cursor_position not supported on wayland")
	}
	out, err := d.run(ctx, xdotoolArgs("getmouselocation", "--shell"))
	if err != nil {
		return 0, 0, linuxError(fmt.Errorf("computer Cursor: %w", err), d.backend)
	}
	var haveX, haveY bool
	for _, f := range strings.Fields(out) {
		switch {
		case strings.HasPrefix(f, "X="):
			x, err = strconv.Atoi(strings.TrimPrefix(f, "X="))
			if err != nil {
				return 0, 0, fmt.Errorf("computer Cursor: %q: %w", f, err)
			}
			haveX = true
		case strings.HasPrefix(f, "Y="):
			y, err = strconv.Atoi(strings.TrimPrefix(f, "Y="))
			if err != nil {
				return 0, 0, fmt.Errorf("computer Cursor: %q: %w", f, err)
			}
			haveY = true
		}
	}
	if !haveX || !haveY {
		return 0, 0, fmt.Errorf("computer Cursor: no X=/Y= in xdotool output %q", out)
	}
	return x, y, nil
}
