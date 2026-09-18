//go:build darwin

package computer

import (
	"context"
	"os"
	"strings"
	"time"
)

// macOS System Settings deep links for the grants the computer tool needs.
const (
	accessibilityPane   = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	screenRecordingPane = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	automationPane      = "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"
	privacyRoot         = "x-apple.systempreferences:com.apple.preference.security"
)

// accessibilityPromptJS asks macOS to show the Accessibility consent dialog.
//
// The options dictionary key must be the literal CFString value
// "AXTrustedCheckOptionPrompt". Passing the bridged kAXTrustedCheckOptionPrompt
// Ref as the key crashes osascript (the JXA bridge hands it to
// CFDictionaryGetValue as a non-CFStringRef and it segfaults), so the literal is
// deliberate. AXIsProcessTrustedWithOptions returns immediately and prompts
// asynchronously, so the boolean it returns is the trust state *before* the user
// answers the dialog.
const accessibilityPromptJS = `ObjC.import('ApplicationServices'); $.AXIsProcessTrustedWithOptions($.NSDictionary.dictionaryWithObjectForKey($.kCFBooleanTrue, 'AXTrustedCheckOptionPrompt')) ? 'true' : 'false'`

// platformRequestPermissions triggers the macOS grants the computer tool needs
// and reports each one. It is advisory and never fatal.
func platformRequestPermissions(ctx context.Context) PermissionReport {
	rep := PermissionReport{Platform: "darwin"}

	axGranted, axLine := requestAccessibility(ctx)
	srProbeOK, srLine := requestScreenRecording(ctx)
	autoGranted, autoLine := requestAutomation(ctx)

	rep.Lines = []string{axLine, srLine, autoLine}
	// Granted reflects only what is detectable: Accessibility and Automation
	// are verifiable grants, and a Screen Recording probe that errors is a real
	// problem. A *successful* probe is not a grant check (macOS has no
	// scriptable one), so Granted=true must never be read as "Screen Recording
	// is granted" — the report line says so and the settings UI shows it.
	rep.Granted = axGranted && srProbeOK && autoGranted

	if rep.Granted {
		rep.Lines = append(rep.Lines, "All detectable permissions are in place. Screen Recording has no scriptable check — if screenshots come back blank, enable your terminal or ocode-desktop in System Settings → Privacy & Security → Screen Recording and relaunch ocode.")
		return rep
	}

	// Open the settings pane for whatever is missing so the user can grant it
	// after the consent dialog. One missing grant gets its deep link; if more
	// than one is missing, open the Privacy & Security root so all are reachable.
	missing := 0
	if !axGranted {
		missing++
	}
	if !srProbeOK {
		missing++
	}
	if !autoGranted {
		missing++
	}
	pane := privacyRoot
	switch {
	case missing > 1:
		pane = privacyRoot
	case !axGranted:
		pane = accessibilityPane
	case !srProbeOK:
		pane = screenRecordingPane
	case !autoGranted:
		pane = automationPane
	}
	if openSettingsPane(ctx, pane) {
		rep.Lines = append(rep.Lines, "Opened System Settings → Privacy & Security.")
	} else {
		rep.Lines = append(rep.Lines, "Open System Settings → Privacy & Security to grant the missing permission(s).")
	}
	return rep
}

// requestAccessibility raises (or checks) the Accessibility grant.
func requestAccessibility(ctx context.Context) (bool, string) {
	out, err := runPermissionJXA(ctx, accessibilityPromptJS)
	if err != nil {
		return false, "Accessibility: could not open the automatic prompt — enable your terminal or ocode-desktop in System Settings → Privacy & Security → Accessibility."
	}
	if strings.TrimSpace(out) == "true" {
		return true, "Accessibility: granted."
	}
	return false, "Accessibility: not granted yet — approve the macOS dialog if shown, then enable your terminal or ocode-desktop in System Settings → Privacy & Security → Accessibility and relaunch."
}

// requestScreenRecording triggers the Screen Recording consent dialog by
// attempting a capture. macOS exposes no way to poll this grant from a script,
// so the report can only say the prompt was requested: a denied grant produces a
// wallpaper-only image without an error.
func requestScreenRecording(ctx context.Context) (bool, string) {
	path, err := tempPNGPath()
	if err != nil {
		return false, "Screen Recording: could not prepare a capture probe — enable your terminal or ocode-desktop in System Settings → Privacy & Security → Screen Recording."
	}
	defer os.Remove(path)

	rctx, cancel := timeoutContext(ctx, 20*time.Second)
	defer cancel()
	if _, err := permissionRun(rctx, "screencapture", "-x", "-D", "1", "-t", "png", path); err != nil {
		return false, "Screen Recording: capture probe failed — enable your terminal or ocode-desktop in System Settings → Privacy & Security → Screen Recording, then relaunch ocode."
	}
	return true, "Screen Recording: consent dialog requested (macOS has no scriptable grant check) — if screenshots come back blank, enable your terminal or ocode-desktop in System Settings → Privacy & Security → Screen Recording and relaunch ocode."
}

// requestAutomation raises the one-time Automation → System Events prompt by
// running a harmless System Events query. macOS shows this prompt the first time
// a script controls System Events (which is how the computer tool types text).
func requestAutomation(ctx context.Context) (bool, string) {
	rctx, cancel := timeoutContext(ctx, 10*time.Second)
	defer cancel()
	if _, err := permissionRun(rctx, "osascript", "-e", `tell application "System Events" to get name of first process`); err != nil {
		return false, "Automation (System Events): prompt not approved — macOS asks the first time the computer tool types; approve it then, or allow it under System Settings → Privacy & Security → Automation."
	}
	return true, "Automation (System Events): granted."
}

// runPermissionJXA runs an inline JXA script under a bounded context.
func runPermissionJXA(ctx context.Context, script string) (string, error) {
	rctx, cancel := timeoutContext(ctx, 10*time.Second)
	defer cancel()
	return permissionRun(rctx, "osascript", "-l", "JavaScript", "-e", script)
}

// openSettingsPane opens a System Settings URL and reports whether the open
// command started successfully.
func openSettingsPane(ctx context.Context, target string) bool {
	rctx, cancel := timeoutContext(ctx, 5*time.Second)
	defer cancel()
	_, err := permissionRun(rctx, "open", target)
	return err == nil
}
