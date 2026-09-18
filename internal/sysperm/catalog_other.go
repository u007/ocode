//go:build !darwin

package sysperm

import (
	"context"
	"runtime"
)

// builtinEntries returns an informational catalog on platforms with no
// per-application consent gate. The settings section still renders so the
// cross-platform UI has a consistent shape.
func builtinEntries() []Entry {
	switch runtime.GOOS {
	case "windows":
		return []Entry{{
			ID:        "windows.no-explicit-grant",
			Label:     "No explicit permission grants",
			Detail:    "Windows (UAC and NTFS ACLs) does not require per-application consent for file or input access.",
			Kind:      KindCategory,
			Platform:  "windows",
			Supported: false,
			Status:    StatusNotRequired,
			Source:    SourceBuiltin,
		}}
	case "linux":
		return []Entry{{
			ID:        "linux.no-explicit-grant",
			Label:     "No explicit permission grants",
			Detail:    "Linux (POSIX file permissions) does not require per-application consent. Computer use additionally needs xdotool + scrot (X11) or ydotool + grim (Wayland).",
			Kind:      KindCategory,
			Platform:  "linux",
			Supported: false,
			Status:    StatusNotRequired,
			Source:    SourceBuiltin,
		}}
	default:
		return []Entry{{
			ID:        runtime.GOOS + ".unsupported",
			Label:     "Not supported on " + runtime.GOOS,
			Detail:    "This platform has no OS permission manager.",
			Kind:      KindCategory,
			Platform:  runtime.GOOS,
			Supported: false,
			Status:    StatusNotRequired,
			Source:    SourceBuiltin,
		}}
	}
}

// detectEntry is always not_required off macOS.
func detectEntry(context.Context, Entry) Status { return StatusNotRequired }

// requestEntry is informational off macOS; there is nothing to request.
func requestEntry(_ context.Context, e Entry) RequestResult {
	return RequestResult{
		ID:      e.ID,
		Status:  StatusNotRequired,
		Message: "No explicit OS permission is required on this platform.",
	}
}

// PlatformSupported reports whether the OS has real per-application grants.
func PlatformSupported() bool { return false }
