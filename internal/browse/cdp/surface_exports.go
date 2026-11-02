package cdp

import "slices"

// Exports for the htrcli-backed agent surface in internal/server. That surface
// must report the same caps, control roles, operations and fingerprint as the
// CDP path, so the constants and rules are read from here rather than copied.

const (
	// ControlsMaxElements caps the controls one Controls read keeps.
	ControlsMaxElements = controlsMaxElements
	// ControlLabelMax clips a control's accessible name and its context name.
	ControlLabelMax = controlLabelMax
	// ControlValueMax clips a control's value.
	ControlValueMax = controlValueMax
	// ViewportTextMaxChars caps the page text Controls returns, in UTF-16 units.
	ViewportTextMaxChars = viewportTextMaxChars
	// PageTextMaxChars caps what PageText returns, in UTF-16 code units.
	PageTextMaxChars = pageTextMaxChars
	// SnapshotMaxElements and SnapshotMaxBytes cap one snapshot's tree.
	SnapshotMaxElements = snapshotMaxElements
	SnapshotMaxBytes    = snapshotMaxBytes
	// FindMaxElements is the largest max a find may ask for; FindTextMax clips
	// each found element's text.
	FindMaxElements = findMaxElements
	FindTextMax     = findTextMax
	// ConsoleTextRunes caps one console entry's text; ConsoleLocationBytes caps
	// its source location.
	ConsoleTextRunes     = consoleTextRunes
	ConsoleLocationBytes = consoleLocationBytes
	// NetworkURLBytes caps one network entry's URL.
	NetworkURLBytes = networkURLBytes
	// PageScrollFraction is the share of the viewport height one page scroll moves.
	PageScrollFraction = pageScrollFraction
)

// ControlBoxInViewport reports whether a control's box overlaps the viewport of vw
// by vh CSS pixels. A box with no area, or a zero viewport, is never in view.
func ControlBoxInViewport(box ControlBox, vw, vh int) bool {
	return vw > 0 && vh > 0 &&
		box.W > 0 && box.H > 0 &&
		box.X < float64(vw) && box.X+box.W > 0 &&
		box.Y < float64(vh) && box.Y+box.H > 0
}

// TruncateRunes cuts s to at most n runes and reports whether it cut.
func TruncateRunes(s string, n int) (string, bool) {
	return truncateRunes(s, n)
}

// TruncateBytes cuts s to at most n bytes, backing up to a rune boundary, and
// reports whether it cut.
func TruncateBytes(s string, n int) (string, bool) {
	return truncateBytes(s, n)
}

// ControlOperationsFor returns the operations a control with role offers, and
// whether role is a control role at all. A disabled control offers none.
func ControlOperationsFor(role string, disabled bool) ([]string, bool) {
	ops, ok := controlOperations[role]
	if !ok {
		return nil, false
	}
	if disabled {
		return []string{}, true
	}
	return slices.Clone(ops), true
}

// IsControlContextRole reports whether a named node of role gives the controls
// inside it their Context.
func IsControlContextRole(role string) bool {
	return controlContextRoles[role]
}

// SnapshotStateOrder is the order a snapshot line lists element states in.
func SnapshotStateOrder() []string {
	return slices.Clone(snapshotStates)
}

// ControlsFingerprint is the fingerprint Controls reports for a page: the URL,
// the visible text and the controls listing, without refs.
func ControlsFingerprint(url, text string, controls []Control, omitted int) (string, error) {
	return controlsFingerprint(url, text, controls, omitted)
}

// SnapshotLine formats one snapshot element line, as the CDP snapshot does.
func SnapshotLine(depth int, role, name, value, ref string, states []string) string {
	return snapshotLine(depth, role, name, value, ref, states)
}

// ClipText clips s to n runes, adding an ellipsis when it cuts.
func ClipText(s string, n int) string {
	return clipText(s, n)
}
