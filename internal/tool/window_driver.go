package tool

import "context"

// WindowInfo describes one top-level desktop window. Bounds are in the OS
// input coordinate space (macOS points, Windows physical pixels, X11 pixels),
// NOT screenshot-image pixels.
type WindowInfo struct {
	// ID is opaque and platform-specific. It is only valid until a window
	// opens or closes; callers re-list before acting.
	ID        string
	App       string
	Title     string
	X, Y      int
	W, H      int
	Minimized bool
	Focused   bool
}

// Window states accepted by WindowDriver.SetWindowState.
const (
	WindowMinimize = "minimize"
	WindowRestore  = "restore"
	WindowMaximize = "maximize"
	WindowClose    = "close"
)

// WindowDriver abstracts platform window management. The platform
// ComputerDriver also implements it; callers obtain it by type assertion.
type WindowDriver interface {
	ListWindows(ctx context.Context) ([]WindowInfo, error)
	// FocusWindow un-minimizes and raises the window and gives it focus.
	FocusWindow(ctx context.Context, id string) error
	// MoveResizeWindow sets the window's outer bounds.
	MoveResizeWindow(ctx context.Context, id string, x, y, w, h int) error
	// SetWindowState is one of WindowMinimize/Restore/Maximize/Close.
	SetWindowState(ctx context.Context, id, state string) error
}
