//go:build windows

package computer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// windowsDriver implements tool.ComputerDriver for Windows using
// PowerShell (SendInput) for input and GDI for screenshots.
type windowsDriver struct {
	r          commandRunner
	scriptPath string
}

// windowsHelper is the process-wide PowerShell helper file (see helperScript).
var windowsHelper = &helperScript{name: "ocode-computer-%d.ps1", data: windowsScript}

// windowsScriptFile returns the path of the written PowerShell helper,
// registering its removal with sup.
func windowsScriptFile(sup *tool.ProcessSupervisor) (string, error) {
	return windowsHelper.ensure(sup)
}

// newWindowsDriver creates a windowsDriver sharing the process-wide
// PowerShell helper script.
func newWindowsDriver(r commandRunner, sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	path, err := windowsScriptFile(sup)
	if err != nil {
		return nil, err
	}
	return &windowsDriver{r: r, scriptPath: path}, nil
}

// Screenshot captures the primary display via GDI. The script writes the PNG
// and prints the primary screen size in physical pixels, which is the OS
// input coordinate space the driver contract requires.
func (d *windowsDriver) Screenshot(ctx context.Context) (png []byte, screenW, screenH int, err error) {
	tmp, err := tempPNGPath()
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() {
		// No-op once readAndRemove has taken the file; covers every
		// earlier error path.
		if rmErr := os.Remove(tmp); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			log.Printf("[COMPUTER] remove screenshot temp %s: %v", tmp, rmErr)
		}
	}()

	out, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "screenshot", tmp)...)
	if err != nil {
		return nil, 0, 0, windowsError(fmt.Errorf("computer Screenshot: %w", err))
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: unexpected screen size %q", out)
	}
	screenW, err = strconv.Atoi(fields[0])
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: screen width: %w", err)
	}
	screenH, err = strconv.Atoi(fields[1])
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: screen height: %w", err)
	}
	png, err = readAndRemove(tmp)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	return png, screenW, screenH, nil
}

// windowsButtonName maps a tool.MouseButton to the name the script expects.
func windowsButtonName(button tool.MouseButton) (string, error) {
	switch button {
	case tool.MouseLeft:
		return "left", nil
	case tool.MouseRight:
		return "right", nil
	case tool.MouseMiddle:
		return "middle", nil
	}
	return "", fmt.Errorf("computer Click: unknown mouse button %d", button)
}

// Click moves the cursor to (x,y) and sends count press/release pairs.
func (d *windowsDriver) Click(ctx context.Context, x, y int, button tool.MouseButton, count int) error {
	name, err := windowsButtonName(button)
	if err != nil {
		return err
	}
	args := windowsArgs(d.scriptPath, "click", strconv.Itoa(x), strconv.Itoa(y), name, strconv.Itoa(count))
	if _, err := d.r.run(ctx, "powershell", args...); err != nil {
		return windowsError(fmt.Errorf("computer Click: %w", err))
	}
	return nil
}

// Move moves the cursor to (x,y).
func (d *windowsDriver) Move(ctx context.Context, x, y int) error {
	args := windowsArgs(d.scriptPath, "move", strconv.Itoa(x), strconv.Itoa(y))
	if _, err := d.r.run(ctx, "powershell", args...); err != nil {
		return windowsError(fmt.Errorf("computer Move: %w", err))
	}
	return nil
}

// Drag presses the left button at (x1,y1), moves to (x2,y2) and releases.
func (d *windowsDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	args := windowsArgs(d.scriptPath, "drag",
		strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2))
	if _, err := d.r.run(ctx, "powershell", args...); err != nil {
		return windowsError(fmt.Errorf("computer Drag: %w", err))
	}
	return nil
}

// Scroll turns the wheel at (x,y). dir is "up"|"down"|"left"|"right".
func (d *windowsDriver) Scroll(ctx context.Context, x, y int, dir string, amount int) error {
	args := windowsArgs(d.scriptPath, "scroll", strconv.Itoa(x), strconv.Itoa(y), dir, strconv.Itoa(amount))
	if _, err := d.r.run(ctx, "powershell", args...); err != nil {
		return windowsError(fmt.Errorf("computer Scroll: %w", err))
	}
	return nil
}

// Type sends text as Unicode keystrokes.
func (d *windowsDriver) Type(ctx context.Context, text string) error {
	if _, err := d.r.run(ctx, "powershell", windowsTypeArgs(d.scriptPath, text)...); err != nil {
		return windowsError(fmt.Errorf("computer Type: %w", err))
	}
	return nil
}

// Key presses an xdotool-style combo, e.g. "ctrl+s".
func (d *windowsDriver) Key(ctx context.Context, combo string) error {
	args, err := windowsKeyArgs(d.scriptPath, combo)
	if err != nil {
		return err
	}
	if _, err := d.r.run(ctx, "powershell", args...); err != nil {
		return windowsError(fmt.Errorf("computer Key: %w", err))
	}
	return nil
}

// Cursor returns the current cursor position in physical pixels.
func (d *windowsDriver) Cursor(ctx context.Context) (int, int, error) {
	out, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "cursor")...)
	if err != nil {
		return 0, 0, windowsError(fmt.Errorf("computer Cursor: %w", err))
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("computer Cursor: unexpected output %q", out)
	}
	x, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("computer Cursor: x: %w", err)
	}
	y, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("computer Cursor: y: %w", err)
	}
	return x, y, nil
}
