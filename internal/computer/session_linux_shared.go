package computer

import (
	"errors"
	"os/exec"

	"github.com/u007/ocode/internal/tool"
)

// linuxBackend returns the desktop backend based on
// XDG_SESSION_TYPE: "wayland" or "x11" (default).
func linuxBackend(env func(string) string) string {
	if env("XDG_SESSION_TYPE") == "wayland" {
		return "wayland"
	}
	return "x11"
}

// xdotoolArgs returns the argv for xdotool with the
// given op and parameters: ["xdotool", op, params...].
func xdotoolArgs(op string, params ...string) []string {
	args := make([]string, 0, len(params)+2)
	args = append(args, "xdotool", op)
	return append(args, params...)
}

// ydotoolArgs returns the argv for ydotool with the
// given op and parameters: ["ydotool", op, params...].
func ydotoolArgs(op string, params ...string) []string {
	args := make([]string, 0, len(params)+2)
	args = append(args, "ydotool", op)
	return append(args, params...)
}

// linuxInstallHint returns the apt install hint for
// the given backend.
func linuxInstallHint(backend string) string {
	switch backend {
	case "wayland":
		return "apt install ydotool grim (note: ydotool requires its daemon and uinput access; grim requires a wlroots compositor)"
	default:
		return "apt install xdotool scrot"
	}
}

// linuxError wraps runner errors for the Linux driver.
// exec.ErrNotFound (binary missing) becomes a NoticedError
// pointing the user to install the required packages;
// other errors pass through.
func linuxError(err error, backend string) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &tool.NoticedError{
			Err:    err,
			Notice: linuxInstallHint(backend),
		}
	}
	return err
}
