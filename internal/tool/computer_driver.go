package tool

import "context"

// MouseButton identifies which mouse button to use for a click.
type MouseButton int

const (
	MouseLeft MouseButton = iota
	MouseRight
	MouseMiddle
)

// ComputerDriver abstracts platform desktop control: screenshots and
// input (mouse, keyboard). Coordinates are in the OS input coordinate
// space reported by Screenshot (macOS points, Windows DPI-aware pixels,
// X11 pixels). Implementations must be safe for concurrent calls only
// if documented; the tool serializes calls via a mutex.
type ComputerDriver interface {
	// Screenshot captures the primary display as PNG. screenW/screenH
	// are the display size in the OS input coordinate space (which may
	// differ from the PNG's pixel size on HiDPI displays).
	Screenshot(ctx context.Context) (png []byte, screenW, screenH int, err error)
	// Click performs a click (or repeated click) at (x,y) in image
	// coordinates. The tool scales to driver coordinates before calling.
	Click(ctx context.Context, x, y int, button MouseButton, count int) error
	// Move moves the cursor to (x,y) in image coordinates.
	Move(ctx context.Context, x, y int) error
	// Drag drags from (x1,y1) to (x2,y2) in image coordinates.
	Drag(ctx context.Context, x1, y1, x2, y2 int) error
	// Scroll moves the wheel at (x,y). dir is "up"|"down"|"left"|"right".
	Scroll(ctx context.Context, x, y int, dir string, amount int) error
	// Type sends the text as keystrokes.
	Type(ctx context.Context, text string) error
	// Key presses an xdotool-style combo, e.g. "ctrl+s", "Return".
	Key(ctx context.Context, combo string) error
	// Cursor returns the current cursor position in driver coordinates.
	Cursor(ctx context.Context) (x, y int, err error)
}
