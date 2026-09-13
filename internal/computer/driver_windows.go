//go:build windows

package computer

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

//go:embed input_windows.ps1
var embedScript []byte

// windowsDriver implements tool.ComputerDriver for Windows using
// PowerShell (SendInput) for input and GDI for screenshots.
type windowsDriver struct {
	r          commandRunner
	scriptPath string
}

// newWindowsDriver creates a windowsDriver, writing the embedded
// script to a temp file and registering its removal on shutdown.
func newWindowsDriver(r commandRunner, sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	path := fmt.Sprintf("%s/ocode-computer-%d.ps1", os.TempDir(), os.Getpid())
	if err := os.WriteFile(path, embedScript, 0600); err != nil {
		return nil, fmt.Errorf("computer windows: write script: %w", err)
	}
	_ = sup.RegisterShutdownCallback(func() {
		_ = os.Remove(path)
	})
	return &windowsDriver{r: r, scriptPath: path}, nil
}

// Screenshot captures the primary display via GDI and reads
// the PNG produced by the script.
func (d *windowsDriver) Screenshot(ctx context.Context) (png []byte, screenW, screenH int, err error) {
	tmp, err := tempPNGPath()
	if err != nil {
		return nil, 0, 0, err
	}
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "screenshot", tmp)...); err != nil {
		return nil, 0, 0, windowsError(fmt.Errorf("computer Screenshot: %w", err))
	}
	png, err = readAndRemove(tmp)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("computer Screenshot: %w", err)
	}
	out, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "screen")...)
	if err != nil {
		return png, 0, 0, windowsError(fmt.Errorf("computer Screenshot: %w", err))
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
func (d *windowsDriver) Click(ctx context.Context, x, y int, button tool.MouseButton, count int) error {
	buttonStr := "left"
	switch button {
	case tool.MouseLeft:
		buttonStr = "left"
	case tool.MouseRight:
		buttonStr = "right"
	case tool.MouseMiddle:
		buttonStr = "middle"
	}
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "click", strconv.Itoa(x), strconv.Itoa(y), buttonStr, strconv.Itoa(count))...); err != nil {
		return windowsError(fmt.Errorf("computer Click: %w", err))
	}
	return nil
}

// Move moves the cursor to (x,y).
func (d *windowsDriver) Move(ctx context.Context, x, y int) error {
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "move", strconv.Itoa(x), strconv.Itoa(y))...); err != nil {
		return windowsError(fmt.Errorf("computer Move: %w", err))
	}
	return nil
}

// Drag drags from (x1,y1) to (x2,y2).
func (d *windowsDriver) Drag(ctx context.Context, x1, y1, x2, y2 int) error {
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "drag", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2))...); err != nil {
		return windowsError(fmt.Errorf("computer Drag: %w", err))
	}
	return nil
}

// Scroll moves the wheel at (x,y).
func (d *windowsDriver) Scroll(ctx context.Context, x, y int, dir string, amount int) error {
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "scroll", strconv.Itoa(x), strconv.Itoa(y), dir, strconv.Itoa(amount))...); err != nil {
		return windowsError(fmt.Errorf("computer Scroll: %w", err))
	}
	return nil
}

// Type sends text as keystrokes.
func (d *windowsDriver) Type(ctx context.Context, text string) error {
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "type", text)...); err != nil {
		return windowsError(fmt.Errorf("computer Type: %w", err))
	}
	return nil
}

// Key presses a key combo.
func (d *windowsDriver) Key(ctx context.Context, combo string) error {
	codes, err := windowsKeyCombo(combo)
	if err != nil {
		return err
	}
	strCodes := make([]string, len(codes))
	for i, c := range codes {
		strCodes[i] = strconv.Itoa(c)
	}
	if _, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, "key", strCodes...)...); err != nil {
		return windowsError(fmt.Errorf("computer Key: %w", err))
	}
	return nil
}

// Cursor returns the current cursor position.
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
		return 0, 0, fmt.Errorf("computer Cursor: %w", err)
	}
	y, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("computer Cursor: %w", err)
	}
	return x, y, nil
}
