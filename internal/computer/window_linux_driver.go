package computer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// Like linux_driver.go this carries no build tag: argv construction and
// output parsing are portable and unit-tested from any GOOS.

var _ tool.WindowDriver = (*linuxDriver)(nil)

const errWaylandWindows = "window management is X11-only: Wayland compositors do not let clients list or move other clients' windows"

// windowRun runs one X11 window command, turning a missing binary into an
// install hint.
func (d *linuxDriver) windowRun(ctx context.Context, argv ...string) (string, error) {
	if d.backend == "wayland" {
		return "", errors.New(errWaylandWindows)
	}
	out, err := d.r.run(ctx, argv[0], argv[1:]...)
	if errors.Is(err, exec.ErrNotFound) {
		return "", &tool.NoticedError{Err: err, Notice: "apt install wmctrl xdotool x11-utils"}
	}
	return out, err
}

// parseWmctrlList parses `wmctrl -lGpx`: id desktop pid x y w h class host title...
func parseWmctrlList(out string) ([]tool.WindowInfo, error) {
	var wins []tool.WindowInfo
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 9 {
			return nil, fmt.Errorf("computer window: malformed wmctrl line %q", line)
		}
		var n [4]int
		for i := range n {
			v, err := strconv.Atoi(f[3+i])
			if err != nil {
				return nil, fmt.Errorf("computer window: bad geometry in %q: %w", line, err)
			}
			n[i] = v
		}
		// Title is everything after the host column; recover it from the raw
		// line so internal spacing survives.
		rest := line
		for i := 0; i < 9; i++ {
			rest = strings.TrimLeft(rest, " \t")
			if end := strings.IndexAny(rest, " \t"); end >= 0 {
				rest = rest[end:]
			} else {
				rest = ""
			}
		}
		title := strings.TrimSpace(rest)
		app := f[7]
		if dot := strings.LastIndex(app, "."); dot >= 0 && dot+1 < len(app) {
			app = app[dot+1:] // WM_CLASS is "instance.Class"; the class is the readable part
		}
		wins = append(wins, tool.WindowInfo{
			ID: f[0], App: cleanField(app), Title: cleanField(title),
			X: n[0], Y: n[1], W: n[2], H: n[3],
		})
	}
	return wins, nil
}

// ListWindows lists managed X11 windows. Minimized and focused flags come from
// xprop _NET_WM_STATE and xdotool getactivewindow.
func (d *linuxDriver) ListWindows(ctx context.Context) ([]tool.WindowInfo, error) {
	out, err := d.windowRun(ctx, "wmctrl", "-lGpx")
	if err != nil {
		return nil, fmt.Errorf("computer ListWindows: %w", err)
	}
	wins, err := parseWmctrlList(out)
	if err != nil {
		return nil, err
	}
	// getactivewindow exits non-zero when no window is active (empty desktop);
	// that means "nothing focused", not a failed listing.
	activeID := ""
	active, err := d.windowRun(ctx, "xdotool", "getactivewindow")
	if err != nil {
		log.Printf("[COMPUTER] ListWindows: no active window: %v", err)
	} else if n, perr := strconv.ParseUint(strings.TrimSpace(active), 10, 64); perr != nil {
		log.Printf("[COMPUTER] ListWindows: unexpected active window %q: %v", active, perr)
	} else {
		activeID = fmt.Sprintf("0x%08x", n)
	}
	// A window that closes between wmctrl and xprop is dropped, not fatal.
	live := wins[:0]
	for _, w := range wins {
		state, err := d.windowRun(ctx, "xprop", "-id", w.ID, "_NET_WM_STATE")
		if err != nil {
			log.Printf("[COMPUTER] ListWindows: skipping window %s: xprop: %v", w.ID, err)
			continue
		}
		w.Focused = activeID != "" && strings.EqualFold(w.ID, activeID)
		w.Minimized = strings.Contains(state, "_NET_WM_STATE_HIDDEN")
		live = append(live, w)
	}
	wins = live
	return wins, nil
}

func (d *linuxDriver) FocusWindow(ctx context.Context, id string) error {
	if err := checkWindowID(x11WindowID, id); err != nil {
		return err
	}
	// -a activates, which also un-minimizes and switches desktop.
	_, err := d.windowRun(ctx, "wmctrl", "-i", "-a", id)
	return err
}

func (d *linuxDriver) MoveResizeWindow(ctx context.Context, id string, x, y, w, h int) error {
	if err := checkWindowID(x11WindowID, id); err != nil {
		return err
	}
	// A maximized/fullscreen window ignores -e until those states are cleared.
	if _, err := d.windowRun(ctx, "wmctrl", "-i", "-r", id, "-b", "remove,maximized_vert,maximized_horz,fullscreen"); err != nil {
		return err
	}
	_, err := d.windowRun(ctx, "wmctrl", "-i", "-r", id, "-e",
		fmt.Sprintf("0,%d,%d,%d,%d", x, y, w, h))
	return err
}

func (d *linuxDriver) SetWindowState(ctx context.Context, id, state string) error {
	if err := checkWindowID(x11WindowID, id); err != nil {
		return err
	}
	if err := checkWindowState(state); err != nil {
		return err
	}
	var err error
	switch state {
	case tool.WindowMinimize:
		_, err = d.windowRun(ctx, "xdotool", "windowminimize", id)
	case tool.WindowRestore:
		if _, err = d.windowRun(ctx, "wmctrl", "-i", "-r", id, "-b", "remove,maximized_vert,maximized_horz"); err == nil {
			_, err = d.windowRun(ctx, "wmctrl", "-i", "-a", id)
		}
	case tool.WindowMaximize:
		_, err = d.windowRun(ctx, "wmctrl", "-i", "-r", id, "-b", "add,maximized_vert,maximized_horz")
	case tool.WindowClose:
		_, err = d.windowRun(ctx, "wmctrl", "-i", "-c", id)
	}
	return err
}
