//go:build linux

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

// linuxDriver implements tool.ComputerDriver for Linux using
// xdotool/scrot on X11 and ydotool/grim on Wayland.
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

// Screenshot captures the primary display.
// On X11: scrot captures the screen, xdotool reports display size.
// On Wayland: grim captures the screen, dimensions come from the PNG.
// Fractional scaling is unsupported (documented limitation).
func (d *linuxDriver) Screenshot(ctx context.Context) (pngData []byte, screenW, screenH int, err error) {
	tmp, err := tempPNGPath()
	if err != nil {
		return nil, 0, 0, err
	}

	if d.backend == "wayland" {
		if _, err := d.r.run(ctx, "grim", tmp); err != nil {
			return nil, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
		}
		pngData, err = readAndRemove(tmp)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
		}
		// Screen size comes from PNG dimensions on Wayland.
		img, err := png.DecodeConfig(bytes.NewReader(pngData))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
		}
		return pngData, img.Width, img.Height, nil
	}

	// X11: scrot + xdotool getdisplaygeometry
	if _, err := d.r.run(ctx, "scrot", "-o", tmp); err != nil {
		return nil, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
	}
	pngData, err = readAndRemove(tmp)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	out, err := d.r.run(ctx, "xdotool", "getdisplaygeometry")
	if err != nil {
		return pngData, 0, 0, linuxError(fmt.Errorf("computer Screenshot: %w", err), d.backend)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return pngData, 0, 0, fmt.Errorf("computer Screenshot: unexpected screen size %q", out)
	}
	screenW, err = strconv.Atoi(fields[0])
	if err != nil {
		return pngData, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	screenH, err = strconv.Atoi(fields[1])
	if err != nil {
		return pngData, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	return pngData, screenW, screenH, nil
}

// Click performs a click at (x,y).
func (d *linuxDriver) Click(ctx context.Context, x, y int, button tool.MouseButton, count int) error {
	var buttonCode string
	switch button {
	case tool.MouseLeft:
		buttonCode = "1"
	case tool.MouseRight:
		buttonCode = "3"
	case tool.MouseMiddle:
		buttonCode = "2"
	}

	if d.backend == "wayland" {
		var code string
		switch button {
		case tool.MouseLeft:
			code = "0xC0"
		case tool.MouseRight:
			code = "0xC1"
		case tool.MouseMiddle:
			code = "0xC2"
		}
		for i := 0; i < count; i++ {
			if _, err := d.r.run(ctx, "ydotool", "click", code); err != nil {
				return linuxError(fmt.Errorf("computer Click: %w", err), d.backend)
			}
		}
		return nil
	}

	// X11: xdotool mousemove x y click --repeat count button
	if _, err := d.r.run(ctx, "xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "--repeat", strconv.Itoa(count), buttonCode); err != nil {
		return linuxError(fmt.Errorf("computer Click: %w", err), d.backend)
	}
	return nil
}

// Move moves the cursor to (x,y).
func (d *linuxDriver) Move(ctx context.Context, x, y int) error {
	if d.backend == "wayland" {
		if _, err := d.r.run(ctx, "ydotool", "mousemove", "--absolute", "-x", strconv.Itoa(x), "-y", strconv.Itoa(y)); err != nil {
			return linuxError(fmt.Errorf("computer Move: %w", err), d.backend)
		}
		return nil
	}
	if _, err := d.r.run(ctx, "xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return linuxError(fmt.Errorf("computer Move: %w", err), d.backend)
	}
	return nil
}

// Drag drags from (x1,y1) to (x2,y2).
func (d *linuxDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	if d.backend == "wayland" {
		if _, err := d.r.run(ctx, "ydotool", "mousemove", "--absolute", "-x", strconv.Itoa(x1), "-y", strconv.Itoa(y1), "click", "0x40", "mousemove", "--absolute", "-x", strconv.Itoa(x2), "-y", strconv.Itoa(y2), "click", "0x80"); err != nil {
			return linuxError(fmt.Errorf("computer Drag: %w", err), d.backend)
		}
		return nil
	}
	if _, err := d.r.run(ctx, "xdotool", "mousemove", strconv.Itoa(x1), strconv.Itoa(y1), "mousedown", "1", "mousemove", strconv.Itoa(x2), strconv.Itoa(y2), "mouseup", "1"); err != nil {
		return linuxError(fmt.Errorf("computer Drag: %w", err), d.backend)
	}
	return nil
}

// Scroll moves the wheel at (x,y).
func (d *linuxDriver) Scroll(ctx context.Context, x, y int, dir string, amount int) error {
	if d.backend == "wayland" {
		var wheelY int
		switch dir {
		case "up":
			wheelY = -amount
		case "down":
			wheelY = amount
		case "left":
			// ydotool doesn't have horizontal scroll via mousemove --wheel;
			// fall back to mousemove on the x axis with no wheel (documented limitation)
			if _, err := d.r.run(ctx, "ydotool", "mousemove", "--wheel", "-x", strconv.Itoa(-amount)); err != nil {
				return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
			}
			return nil
		case "right":
			if _, err := d.r.run(ctx, "ydotool", "mousemove", "--wheel", "-x", strconv.Itoa(amount)); err != nil {
				return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
			}
			return nil
		}
		if _, err := d.r.run(ctx, "ydotool", "mousemove", "--wheel", "-y", strconv.Itoa(wheelY)); err != nil {
			return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
		}
		return nil
	}

	// X11: button 4/5 (up/down), 6/7 (left/right) repeated amount times
	var button string
	switch dir {
	case "up":
		button = "4"
	case "down":
		button = "5"
	case "left":
		button = "6"
	case "right":
		button = "7"
	}
	for i := 0; i < amount; i++ {
		if _, err := d.r.run(ctx, "xdotool", "click", button); err != nil {
			return linuxError(fmt.Errorf("computer Scroll: %w", err), d.backend)
		}
	}
	return nil
}

// Type sends text as keystrokes.
func (d *linuxDriver) Type(ctx context.Context, text string) error {
	if d.backend == "wayland" {
		if _, err := d.r.runStdin(ctx, text, "ydotool", "type", "--file", "-"); err != nil {
			return linuxError(fmt.Errorf("computer Type: %w", err), d.backend)
		}
		return nil
	}
	if _, err := d.r.runStdin(ctx, text, "xdotool", "type", "--delay", "12", "--file", "-"); err != nil {
		return linuxError(fmt.Errorf("computer Type: %w", err), d.backend)
	}
	return nil
}

// Key presses an xdotool-style combo (e.g. "ctrl+s").
// On X11, key names pass through unchanged to xdotool key.
// On Wayland, uses ydotool with code:press/code:release pairs.
func (d *linuxDriver) Key(ctx context.Context, combo string) error {
	if d.backend == "wayland" {
		codes, err := ydotoolKeyCombo(combo)
		if err != nil {
			return err
		}
		args := []string{"key"}
		args = append(args, codes...)
		if _, err := d.r.run(ctx, "ydotool", args...); err != nil {
			return linuxError(fmt.Errorf("computer Key: %w", err), d.backend)
		}
		return nil
	}
	if _, err := d.r.run(ctx, "xdotool", "key", combo); err != nil {
		return linuxError(fmt.Errorf("computer Key: %w", err), d.backend)
	}
	return nil
}

// Cursor returns the current cursor position.
// On Wayland, cursor position is not supported (returns an error).
func (d *linuxDriver) Cursor(ctx context.Context) (x, y int, err error) {
	if d.backend == "wayland" {
		return 0, 0, fmt.Errorf("computer: cursor_position not supported on wayland")
	}
	out, err := d.r.run(ctx, "xdotool", "getmouselocation", "--shell")
	if err != nil {
		return 0, 0, linuxError(fmt.Errorf("computer Cursor: %w", err), d.backend)
	}
	fields := strings.Fields(out)
	for _, f := range fields {
		if strings.HasPrefix(f, "X=") {
			x, err = strconv.Atoi(strings.TrimPrefix(f, "X="))
			if err != nil {
				return 0, 0, fmt.Errorf("computer Cursor: %w", err)
			}
		}
		if strings.HasPrefix(f, "Y=") {
			y, err = strconv.Atoi(strings.TrimPrefix(f, "Y="))
			if err != nil {
				return 0, 0, fmt.Errorf("computer Cursor: %w", err)
			}
		}
	}
	return x, y, nil
}
