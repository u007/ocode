//go:build linux

package computer

import "os"

func backendName() string {
	b := linuxBackend(os.Getenv)
	if b == "wayland" {
		return "Linux: ydotool/grim (Wayland, wlroots-only)"
	}
	return "Linux: xdotool/scrot (X11)"
}
