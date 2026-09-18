//go:build darwin

package sysperm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// macOS System Settings deep links for the grants this catalog tracks.
const (
	paneFullDiskAccess   = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"
	paneFilesAndFolders  = "x-apple.systempreferences:com.apple.preference.security?Privacy_FilesAndFolders"
	paneAccessibility    = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	paneScreenRecording  = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	paneAutomation       = "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"
	accessibilityCheckJS = `ObjC.import('ApplicationServices'); $.AXIsProcessTrusted() ? 'true' : 'false'`
	// The options dictionary key must be the literal CFString value
	// "AXTrustedCheckOptionPrompt" (see internal/computer/permissions_darwin.go
	// for why the bridged Ref crashes osascript).
	accessibilityPromptJS = `ObjC.import('ApplicationServices'); $.AXIsProcessTrustedWithOptions($.NSDictionary.dictionaryWithObjectForKey($.kCFBooleanTrue, 'AXTrustedCheckOptionPrompt')) ? 'true' : 'false'`
	automationProbe       = `tell application "System Events" to get name of first process`
)

// builtinEntries is the macOS permission catalog: the TCC areas ocode's file
// tree, terminal, and computer-use tool can trip.
func builtinEntries() []Entry {
	home, _ := os.UserHomeDir()
	join := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	cat := func(id, label, detail string) Entry {
		return Entry{ID: id, Label: label, Detail: detail, Kind: KindCategory, Platform: "darwin", Supported: true, Source: SourceBuiltin}
	}
	pathEntry := func(id, label, path string) Entry {
		return Entry{ID: id, Label: label, Detail: path, Kind: KindPath, Platform: "darwin", Supported: true, Path: path, Source: SourceBuiltin}
	}
	return []Entry{
		cat("macos.full-disk-access", "Full Disk Access",
			"Read protected locations (Mail, Messages, Safari, backups, every user's files). No consent dialog — grant it in System Settings."),
		pathEntry("macos.files.desktop", "Files & Folders — Desktop", join("Desktop")),
		pathEntry("macos.files.documents", "Files & Folders — Documents", join("Documents")),
		pathEntry("macos.files.downloads", "Files & Folders — Downloads", join("Downloads")),
		pathEntry("macos.files.icloud", "Files & Folders — iCloud Drive", join("Library", "Mobile Documents", "com~apple~CloudDocs")),
		cat("macos.files.network-volumes", "Files & Folders — Network Volumes",
			"Mounted network shares (SMB/AFP/NFS) under /Volumes."),
		cat("macos.files.removable-volumes", "Files & Folders — Removable Volumes",
			"External disks and USB drives under /Volumes."),
		cat("macos.accessibility", "Accessibility",
			"Control the Mac on your behalf (the computer-use tool's input backend)."),
		cat("macos.screen-recording", "Screen Recording",
			"Capture the screen (the computer-use tool's screenshots)."),
		cat("macos.automation", "Automation — System Events",
			"Send keystrokes and control apps through AppleScript."),
	}
}

// detectEntry computes a live status without raising a consent dialog where it
// can. Folder grants are only probed once the user has recorded a request,
// because macOS cannot distinguish "never asked" from "denied" and the probe
// itself can raise the dialog.
func detectEntry(ctx context.Context, e Entry) Status {
	switch e.ID {
	case "macos.accessibility":
		out, err := runBounded(ctx, 5*time.Second, "osascript", "-l", "JavaScript", "-e", accessibilityCheckJS)
		if err != nil {
			return StatusUnknown
		}
		if strings.TrimSpace(out) == "true" {
			return StatusGranted
		}
		return StatusDenied

	case "macos.full-disk-access":
		if _, err := os.Stat(tccDBPath()); err == nil {
			return StatusGranted
		}
		return StatusDenied

	case "macos.screen-recording", "macos.automation",
		"macos.files.network-volumes", "macos.files.removable-volumes":
		// macOS exposes no scriptable grant check for these.
		return StatusUnknown
	}

	if e.Kind == KindPath && e.Path != "" {
		// A path outside the protected roots is always readable, so probing it
		// is safe and yields a real status. A protected root must only be probed
		// once the user has recorded a request, because macOS cannot distinguish
		// "never asked" from "denied" and the probe itself raises the dialog.
		if isProtectedPath(e.Path) && !e.Requested {
			return StatusNotDetermined
		}
		return dirStatus(e.Path)
	}
	return StatusUnknown
}

// isProtectedPath reports whether the path is (or is under) one of the macOS
// TCC-protected locations whose first access raises the Files & Folders
// consent dialog.
func isProtectedPath(path string) bool {
	home, _ := os.UserHomeDir()
	roots := []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Library", "Mobile Documents"),
		"/Volumes",
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		if clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// requestEntry raises the OS consent dialog (or opens the relevant System
// Settings pane where macOS has no dialog), then re-detects.
func requestEntry(ctx context.Context, e Entry) RequestResult {
	res := RequestResult{ID: e.ID}

	switch e.ID {
	case "macos.accessibility":
		_, _ = runBounded(ctx, 10*time.Second, "osascript", "-l", "JavaScript", "-e", accessibilityPromptJS)

	case "macos.full-disk-access":
		// No consent dialog exists for FDA; the user must toggle it in
		// System Settings, so open that pane directly.
		res.OpenedSettings = openPane(ctx, paneFullDiskAccess)
		res.Message = "Full Disk Access has no consent dialog — enable ocode in System Settings → Privacy & Security → Full Disk Access, then relaunch ocode."
		res.Status = detectEntry(ctx, e)
		return res

	case "macos.screen-recording":
		res.Message = probeScreenRecording(ctx)
		res.OpenedSettings = openPane(ctx, paneScreenRecording)

	case "macos.automation":
		if _, err := runBounded(ctx, 10*time.Second, "osascript", "-e", automationProbe); err != nil {
			res.Message = "Automation: the System Events prompt was not approved — approve it when the computer-use tool types, or allow it under System Settings → Privacy & Security → Automation."
			res.OpenedSettings = openPane(ctx, paneAutomation)
		} else {
			res.Message = "Automation (System Events): granted."
		}

	case "macos.files.network-volumes", "macos.files.removable-volumes":
		res.OpenedSettings = openPane(ctx, paneFilesAndFolders)
		res.Message = "Enable the volume type under System Settings → Privacy & Security → Files and Folders."

	default:
		if e.Kind == KindPath && e.Path != "" {
			// The read attempt is what makes macOS show the Files & Folders
			// dialog for this location.
			_, _ = os.ReadDir(e.Path)
			probed := e
			probed.Requested = true
			if detectEntry(ctx, probed) != StatusGranted {
				res.OpenedSettings = openPane(ctx, paneFilesAndFolders)
				res.Message = "Approve the macOS dialog, or enable the location under System Settings → Privacy & Security → Files and Folders."
			} else {
				res.Message = "Access granted."
			}
		}
	}

	probed := e
	probed.Requested = true
	res.Status = detectEntry(ctx, probed)
	return res
}

// dirStatus opens a directory to trip (or confirm) the macOS Files & Folders
// grant. EPERM and ENOENT are both reported as denied/not-determined rather
// than guessed.
func dirStatus(path string) Status {
	f, err := os.Open(path)
	if err == nil {
		_ = f.Close()
		return StatusGranted
	}
	if os.IsNotExist(err) {
		// The protected root does not exist on this machine (e.g. no iCloud
		// Drive); there is nothing to grant.
		return StatusNotDetermined
	}
	return StatusDenied
}

// tccDBPath is the canonical Full Disk Access probe: the TCC database is
// unreadable without FDA and readable with it.
func tccDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db")
}

// probeScreenRecording attempts a capture, which is the only way to raise the
// Screen Recording consent dialog. A successful capture is not a grant check
// (a denied grant still yields a wallpaper-only image), so the caller reports
// unknown status with an explanatory message.
func probeScreenRecording(ctx context.Context) string {
	f, err := os.CreateTemp("", "ocode-sysperm-capture-*.png")
	if err != nil {
		return "Screen Recording: could not prepare a capture probe."
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)

	if _, err := runBounded(ctx, 20*time.Second, "screencapture", "-x", "-D", "1", "-t", "png", path); err != nil {
		return "Screen Recording: capture probe failed — enable ocode under System Settings → Privacy & Security → Screen Recording, then relaunch."
	}
	return "Screen Recording: consent dialog requested (macOS has no scriptable grant check) — if screenshots come back blank, enable ocode in System Settings → Privacy & Security → Screen Recording and relaunch."
}

// openPane opens a System Settings URL and reports whether the open command
// started successfully.
func openPane(ctx context.Context, target string) bool {
	_, err := runBounded(ctx, 5*time.Second, "open", target)
	return err == nil
}

// PlatformSupported reports whether the OS has real per-application grants.
func PlatformSupported() bool { return true }
