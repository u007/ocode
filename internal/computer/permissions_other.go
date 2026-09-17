//go:build !darwin

package computer

import (
	"context"
	"runtime"
)

// platformRequestPermissions reports the permission situation on platforms with
// no explicit per-application desktop-control grant. Windows and Linux need no
// consent dialog, so the report is informational; it never errors and never
// changes any state.
func platformRequestPermissions(_ context.Context) PermissionReport {
	switch runtime.GOOS {
	case "windows":
		return PermissionReport{
			Platform: "windows",
			Granted:  true,
			Lines: []string{
				"Windows requires no explicit permission for the computer tool.",
				"Input is posted through the PowerShell SendInput helper and screenshots use the built-in capture APIs.",
			},
		}
	case "linux":
		return PermissionReport{
			Platform: "linux",
			Granted:  true,
			Lines: []string{
				"Linux requires no explicit permission grant for the computer tool.",
				"Make sure the desktop input backend is installed: xdotool + scrot (X11) or ydotool + grim (Wayland, wlroots-only).",
			},
		}
	default:
		return PermissionReport{
			Platform: runtime.GOOS,
			Granted:  false,
			Lines: []string{
				"Computer use is not supported on " + runtime.GOOS + ".",
			},
		}
	}
}
