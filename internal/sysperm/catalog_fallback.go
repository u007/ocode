package sysperm

// genericEntries is the informational catalog for platforms with no
// per-application consent gate. It takes the OS name instead of reading
// runtime.GOOS so the Windows and Linux rows stay testable on any host: the
// compiled catalog only exists on those platforms, and CI runs Linux while
// development happens on macOS.
func genericEntries(goos string) []Entry {
	switch goos {
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
			ID:        goos + ".unsupported",
			Label:     "Not supported on " + goos,
			Detail:    "This platform has no OS permission manager.",
			Kind:      KindCategory,
			Platform:  goos,
			Supported: false,
			Status:    StatusNotRequired,
			Source:    SourceBuiltin,
		}}
	}
}
