package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
)

const (
	windowDefaultLimit = 50
	windowMaxLimit     = 200
)

// WindowTool lists and manipulates top-level desktop windows. It is opt-in
// with the computer tool (cfg.Ocode.ComputerUse.Enabled) and shares its
// platform driver; see attachComputerDriver in internal/agent.
type WindowTool struct {
	Config    *config.Config
	Driver    WindowDriver
	DriverErr error
}

func (t *WindowTool) Name() string { return "window" }

func (t *WindowTool) Description() string {
	return "List and manipulate desktop windows: list, focus, move_resize, minimize, restore, maximize, close."
}

func (t *WindowTool) Parallel() bool { return false }

func (t *WindowTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "window",
		"description": "Manage desktop windows across all apps. Always `list` first: window ids are opaque and only valid " +
			"until a window opens or closes. Bounds are OS screen coordinates (macOS points, Windows/X11 pixels), " +
			"NOT screenshot-image pixels. `focus` un-minimizes and raises a window so `computer` screenshots/input reach it. " +
			"Linux support is X11 only (wmctrl, xdotool, xprop).",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{"list", "focus", "move_resize", "minimize", "restore", "maximize", "close"},
				},
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Window id from `list` (required except for list).",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "list only: case-insensitive substring filter on app name and title.",
				},
				"x":      map[string]interface{}{"type": "integer", "description": "move_resize: left edge. Omitted values keep the current one."},
				"y":      map[string]interface{}{"type": "integer", "description": "move_resize: top edge."},
				"width":  map[string]interface{}{"type": "integer", "description": "move_resize: width, > 0."},
				"height": map[string]interface{}{"type": "integer", "description": "move_resize: height, > 0."},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("list only: page size (default %d, max %d).", windowDefaultLimit, windowMaxLimit),
				},
				"offset": map[string]interface{}{"type": "integer", "description": "list only: rows to skip."},
			},
			"required": []string{"action"},
		},
	}
}

type windowParams struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Query  string `json:"query"`
	X      *int   `json:"x"`
	Y      *int   `json:"y"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (t *WindowTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t *WindowTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	if t.Driver == nil {
		if t.DriverErr != nil {
			return "", fmt.Errorf("window: driver unavailable: %w", t.DriverErr)
		}
		return "", fmt.Errorf("window: driver unavailable")
	}
	var p windowParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("window: invalid arguments: %w", err)
	}
	if err := validateWindowParams(p); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	switch p.Action {
	case "list":
		wins, err := t.Driver.ListWindows(ctx)
		if err != nil {
			return "", fmt.Errorf("window: list: %w", err)
		}
		return formatWindowList(wins, p), nil
	case "focus":
		if err := t.Driver.FocusWindow(ctx, p.ID); err != nil {
			return "", fmt.Errorf("window: focus: %w", err)
		}
		return fmt.Sprintf("focused %s", p.ID), nil
	case "move_resize":
		cur, err := t.find(ctx, p.ID)
		if err != nil {
			return "", err
		}
		x, y, w, h := cur.X, cur.Y, cur.W, cur.H
		if p.X != nil {
			x = *p.X
		}
		if p.Y != nil {
			y = *p.Y
		}
		if p.Width != nil {
			w = *p.Width
		}
		if p.Height != nil {
			h = *p.Height
		}
		if err := t.Driver.MoveResizeWindow(ctx, p.ID, x, y, w, h); err != nil {
			return "", fmt.Errorf("window: move_resize: %w", err)
		}
		return fmt.Sprintf("window %s now at %d,%d size %dx%d", p.ID, x, y, w, h), nil
	case WindowMinimize, WindowRestore, WindowMaximize, WindowClose:
		if err := t.Driver.SetWindowState(ctx, p.ID, p.Action); err != nil {
			return "", fmt.Errorf("window: %s: %w", p.Action, err)
		}
		return fmt.Sprintf("%s %s", p.Action, p.ID), nil
	default:
		return "", fmt.Errorf("window: unknown action %q", p.Action)
	}
}

// find resolves id against a fresh listing so a stale id fails loudly instead
// of resizing whichever window the platform maps it to.
func (t *WindowTool) find(ctx context.Context, id string) (WindowInfo, error) {
	wins, err := t.Driver.ListWindows(ctx)
	if err != nil {
		return WindowInfo{}, fmt.Errorf("window: list: %w", err)
	}
	for _, w := range wins {
		if w.ID == id {
			return w, nil
		}
	}
	return WindowInfo{}, fmt.Errorf("window: no window with id %q (ids go stale when windows open or close; list again)", id)
}

func validateWindowParams(p windowParams) error {
	switch p.Action {
	case "list":
		if p.Limit < 0 || p.Limit > windowMaxLimit {
			return fmt.Errorf("window: limit must be between 0 and %d", windowMaxLimit)
		}
		if p.Offset < 0 {
			return fmt.Errorf("window: offset must be >= 0")
		}
		return nil
	case "focus", WindowMinimize, WindowRestore, WindowMaximize, WindowClose:
		if p.ID == "" {
			return fmt.Errorf("window: %s requires id", p.Action)
		}
		return nil
	case "move_resize":
		if p.ID == "" {
			return fmt.Errorf("window: move_resize requires id")
		}
		if p.X == nil && p.Y == nil && p.Width == nil && p.Height == nil {
			return fmt.Errorf("window: move_resize requires at least one of x, y, width, height")
		}
		if (p.Width != nil && *p.Width <= 0) || (p.Height != nil && *p.Height <= 0) {
			return fmt.Errorf("window: width and height must be > 0")
		}
		return nil
	default:
		return fmt.Errorf("window: unknown action %q", p.Action)
	}
}

func formatWindowList(wins []WindowInfo, p windowParams) string {
	q := strings.ToLower(p.Query)
	filtered := make([]WindowInfo, 0, len(wins))
	for _, w := range wins {
		if q == "" || strings.Contains(strings.ToLower(w.App+"\n"+w.Title), q) {
			filtered = append(filtered, w)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.App != b.App {
			return strings.ToLower(a.App) < strings.ToLower(b.App)
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
	limit := p.Limit
	if limit == 0 {
		limit = windowDefaultLimit
	}
	total := len(filtered)
	start := p.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	var b strings.Builder
	fmt.Fprintf(&b, "windows %d-%d of %d\n", start, end, total)
	for _, w := range filtered[start:end] {
		var flags []string
		if w.Focused {
			flags = append(flags, "focused")
		}
		if w.Minimized {
			flags = append(flags, "minimized")
		}
		fmt.Fprintf(&b, "id=%s app=%q title=%q at %d,%d size %dx%d", w.ID, w.App, w.Title, w.X, w.Y, w.W, w.H)
		if len(flags) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(flags, ","))
		}
		b.WriteByte('\n')
	}
	if end < total {
		fmt.Fprintf(&b, "more: offset=%d\n", end)
	}
	return strings.TrimRight(b.String(), "\n")
}

// newWindowTool wires the window tool to the computer driver. A driver that
// does not implement WindowDriver leaves Driver nil, which hides the tool.
func newWindowTool(cfg *config.Config, d ComputerDriver, driverErr error) *WindowTool {
	t := &WindowTool{Config: cfg, DriverErr: driverErr}
	if wd, ok := d.(WindowDriver); ok {
		t.Driver = wd
	}
	return t
}

// AttachDriver (re)wires the tool to a computer driver; see
// Agent.attachComputerDriver. It reports whether the driver supports windows.
func (t *WindowTool) AttachDriver(d ComputerDriver) bool {
	wd, ok := d.(WindowDriver)
	if !ok {
		return false
	}
	t.Driver = wd
	t.DriverErr = nil
	return true
}
