//go:build darwin

package computer

import (
	"context"
	"strconv"

	"github.com/u007/ocode/internal/tool"
)

var _ tool.WindowDriver = (*darwinDriver)(nil)

// ListWindows lists on-screen app windows through System Events. Needs the
// Accessibility grant and Automation → System Events.
func (d *darwinDriver) ListWindows(ctx context.Context) ([]tool.WindowInfo, error) {
	out, err := d.osa(ctx, "windows")
	if err != nil {
		return nil, err
	}
	return parseWindowList(out)
}

func (d *darwinDriver) FocusWindow(ctx context.Context, id string) error {
	if err := checkWindowID(darwinWindowID, id); err != nil {
		return err
	}
	_, err := d.osa(ctx, "window-focus", id)
	return err
}

func (d *darwinDriver) MoveResizeWindow(ctx context.Context, id string, x, y, w, h int) error {
	if err := checkWindowID(darwinWindowID, id); err != nil {
		return err
	}
	_, err := d.osa(ctx, "window-bounds", id, strconv.Itoa(x), strconv.Itoa(y), strconv.Itoa(w), strconv.Itoa(h))
	return err
}

func (d *darwinDriver) SetWindowState(ctx context.Context, id, state string) error {
	if err := checkWindowID(darwinWindowID, id); err != nil {
		return err
	}
	if err := checkWindowState(state); err != nil {
		return err
	}
	_, err := d.osa(ctx, "window-state", id, state)
	return err
}
