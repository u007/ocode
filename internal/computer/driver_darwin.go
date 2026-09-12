package computer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// darwinDriver implements tool.ComputerDriver for macOS using
// screencapture for screenshots and an embedded JXA script
// (cgevent_darwin.js) for all input operations.
type darwinDriver struct {
	r commandRunner
}

// scriptPath is the path to the written JXA script file,
// set during driver construction.
var scriptPath string

// darwinArgs returns the argv for osascript that runs the
// embedded JXA script with the given op and parameters.
func darwinArgs(op string, params ...string) []string {
	return append([]string{"-l", "JavaScript", scriptPath, op}, params...)
}

// Screenshot captures the primary display via screencapture
// and reports display size in points from the JXA script.
func (d *darwinDriver) Screenshot(ctx context.Context) (png []byte, screenW, screenH int, err error) {
	path, err := tempPNGPath()
	if err != nil {
		return nil, 0, 0, err
	}
	if _, err := d.r.run(ctx, "screencapture", "-x", "-D", "1", "-t", "png", path); err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	png, err = readAndRemove(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}

	out, err := d.r.run(ctx, "osascript", darwinArgs("screen")...)
	if err != nil {
		return png, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return png, 0, 0, fmt.Errorf("computer Screenshot: unexpected screen size %q", out)
	}
	screenW, err = strconv.Atoi(fields[0])
	if err != nil {
		return png, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	screenH, err = strconv.Atoi(fields[1])
	if err != nil {
		return png, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	return png, screenW, screenH, nil
}

// Click performs a click at (x,y).
func (d *darwinDriver) Click(ctx context.Context, x, y int, button tool.MouseButton, count int) error {
	buttonStr := "left"
	switch button {
	case tool.MouseLeft:
		buttonStr = "left"
	case tool.MouseRight:
		buttonStr = "right"
	case tool.MouseMiddle:
		buttonStr = "middle"
	}
	if _, err := d.r.run(ctx, "osascript", darwinArgs("click", strconv.Itoa(x), strconv.Itoa(y), buttonStr, strconv.Itoa(count))...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Move moves the cursor to (x,y).
func (d *darwinDriver) Move(ctx context.Context, x, y int) error {
	if _, err := d.r.run(ctx, "osascript", darwinArgs("move", strconv.Itoa(x), strconv.Itoa(y))...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Drag drags from (x1,y1) to (x2,y2).
func (d *darwinDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	if _, err := d.r.run(ctx, "osascript", darwinArgs("drag", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2))...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Scroll moves the wheel at (x,y).
func (d *darwinDriver) Scroll(ctx context.Context, x, y int, dir string, amount int) error {
	if _, err := d.r.run(ctx, "osascript", darwinArgs("scroll", strconv.Itoa(x), strconv.Itoa(y), dir, strconv.Itoa(amount))...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Type sends text as keystrokes.
func (d *darwinDriver) Type(ctx context.Context, text string) error {
	if _, err := d.r.run(ctx, "osascript", darwinArgs("type", text)...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Key presses a key combo (e.g. "ctrl+s").
func (d *darwinDriver) Key(ctx context.Context, combo string) error {
	parts := strings.Split(combo, "+")
	var codes []string
	nonModifiers := 0
	for _, p := range parts {
		code, isMod, ok := darwinKeyCode(p)
		if !ok {
			return fmt.Errorf("computer key: unknown key %q", p)
		}
		if !isMod {
			nonModifiers++
		}
		codes = append(codes, strconv.Itoa(code))
	}
	if nonModifiers != 1 {
		return fmt.Errorf("computer key: exactly one non-modifier required in %q", combo)
	}
	if _, err := d.r.run(ctx, "key", codes...); err != nil {
		return darwinError(err)
	}
	return nil
}

// Cursor returns the current cursor position in points.
func (d *darwinDriver) Cursor(ctx context.Context) (int, int, error) {
	out, err := d.r.run(ctx, "osascript", darwinArgs("cursor")...)
	if err != nil {
		return 0, 0, darwinError(err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("computer Cursor: unexpected output %q", out)
	}
	x, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("computer Cursor: %w", err)
	}
	y, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("computer Cursor: %w", err)
	}
	return x, y, nil
}

func darwinError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "not allowed to send keystrokes") || strings.Contains(msg, "accessibility") {
		return &tool.NoticedError{
			Err:    err,
			Notice: "macOS requires Accessibility permission. Go to System Settings → Privacy & Security → Accessibility and allow the terminal or ocode-desktop.",
		}
	}
	return err
}
