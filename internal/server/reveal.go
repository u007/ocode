package server

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// revealPathFn is the indirection the "reveal" open mode calls. It is a
// package-level var so tests can stub it instead of launching a real file
// manager (the same seam pattern as notifyGitAction in handler_git_actions.go).
var revealPathFn = revealPathInFileManager

// revealPathInFileManager opens the OS-native file manager at absPath:
// a directory opens directly, a file is revealed/selected inside its
// containing folder where the platform supports it (macOS `open -R`, Windows
// `explorer /select,`, Linux FileManager1.ShowItems), otherwise the
// containing folder is opened instead.
func revealPathInFileManager(absPath string, isDir bool) error {
	name, args := revealCommand(runtime.GOOS, absPath, isDir)
	if name == "dbus-send" {
		// FileManager1 is only present on some desktops and only on a session
		// bus. Probe it with a short, bounded wait so a headless/minimal host
		// falls back to opening the containing folder instead of silently
		// doing nothing (a detached dbus-send's failure is invisible).
		if _, err := exec.LookPath("dbus-send"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := exec.CommandContext(ctx, name, args...).Run(); err == nil {
				return nil
			}
		}
		// Fallback: open the containing directory (matches the TUI's
		// openInFileExplorer behavior when a file manager can't select).
		name, args = systemOpener(filepath.Dir(absPath))
	}
	return startDetached(name, args)
}

// revealCommand returns the file-manager command that reveals absPath. It is
// pure (no exec) and takes goos explicitly so the per-platform argv shape can
// be unit-tested without running a file manager.
//
//   - darwin: `open <dir>` / `open -R <file>` (select the file in Finder)
//   - windows: `explorer <dir>` / `explorer /select,<file>`
//   - linux/other: `xdg-open <dir>`; for a file a `dbus-send` call to
//     org.freedesktop.FileManager1.ShowItems (the freedesktop "reveal"
//     interface used by Nautilus/Thunar/Dolphin/Nemo), with xdg-open of the
//     parent folder as the fallback applied by revealPathInFileManager.
func revealCommand(goos, absPath string, isDir bool) (string, []string) {
	switch goos {
	case "darwin":
		if isDir {
			return "open", []string{absPath}
		}
		return "open", []string{"-R", absPath}
	case "windows":
		if isDir {
			return "explorer", []string{absPath}
		}
		return "explorer", []string{"/select," + absPath}
	default:
		if isDir {
			return "xdg-open", []string{absPath}
		}
		uri := (&url.URL{Scheme: "file", Path: absPath}).String()
		return "dbus-send", []string{
			"--session",
			"--dest=org.freedesktop.FileManager1",
			"--type=method_call",
			// --print-reply makes dbus-send WAIT for the reply, so a missing
			// FileManager1 service (no file manager, or a bare session bus)
			// surfaces as a non-zero exit the caller can fall back from.
			// Without it dbus-send is fire-and-forget and always exits 0.
			"--print-reply",
			"/org/freedesktop/FileManager1",
			"org.freedesktop.FileManager1.ShowItems",
			"array:string:" + uri,
			"string:",
		}
	}
}
