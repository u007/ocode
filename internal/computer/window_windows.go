//go:build windows

package computer

import (
	"context"
	"fmt"
	"strconv"

	"github.com/u007/ocode/internal/tool"
)

var _ tool.WindowDriver = (*windowsDriver)(nil)

func (d *windowsDriver) ps(ctx context.Context, what, op string, params ...string) (string, error) {
	out, err := d.r.run(ctx, "powershell", windowsArgs(d.scriptPath, op, params...)...)
	if err != nil {
		return "", windowsError(fmt.Errorf("computer %s: %w", what, err))
	}
	return out, nil
}

func (d *windowsDriver) ListWindows(ctx context.Context) ([]tool.WindowInfo, error) {
	out, err := d.ps(ctx, "ListWindows", "windows")
	if err != nil {
		return nil, err
	}
	return parseWindowList(out)
}

func (d *windowsDriver) FocusWindow(ctx context.Context, id string) error {
	if err := checkWindowID(decimalWindowID, id); err != nil {
		return err
	}
	_, err := d.ps(ctx, "FocusWindow", "window-focus", id)
	return err
}

func (d *windowsDriver) MoveResizeWindow(ctx context.Context, id string, x, y, w, h int) error {
	if err := checkWindowID(decimalWindowID, id); err != nil {
		return err
	}
	_, err := d.ps(ctx, "MoveResizeWindow", "window-bounds", id,
		strconv.Itoa(x), strconv.Itoa(y), strconv.Itoa(w), strconv.Itoa(h))
	return err
}

func (d *windowsDriver) SetWindowState(ctx context.Context, id, state string) error {
	if err := checkWindowID(decimalWindowID, id); err != nil {
		return err
	}
	if err := checkWindowState(state); err != nil {
		return err
	}
	_, err := d.ps(ctx, "SetWindowState", "window-state", id, state)
	return err
}
