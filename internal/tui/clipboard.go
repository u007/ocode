package tui

import (
	"log"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// copyToClipboard returns a command that puts text on the user's clipboard.
//
// It emits an OSC 52 sequence AND writes through the local clipboard utility.
// Both are attempted because they cover different situations:
//
//   - OSC 52 travels back over the terminal connection and is executed by the
//     terminal emulator on the user's own machine. It is the only path that
//     reaches the *local* clipboard when the TUI runs on a remote host over
//     SSH, which is why a selection copied in a remote workspace previously
//     never appeared on the user's machine.
//   - The local utility (pbcopy / xclip / xsel / wl-copy) covers terminal
//     emulators that do not implement OSC 52. It can only touch the machine the
//     TUI runs on, so over SSH it either writes the remote host's clipboard or
//     fails with "No clipboard utilities available".
//
// Empty text returns nil so callers can simply `return m, copyToClipboard(text)`.
func copyToClipboard(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), writeLocalClipboard(text))
}

// writeLocalClipboard is the non-OSC-52 half of copyToClipboard. A failure is
// logged rather than surfaced as a copy error: OSC 52 may already have copied
// the text, and it has no acknowledgement to combine the result with, so a
// local utility error is not proof the copy failed.
func writeLocalClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(text); err != nil {
			log.Printf("tui: local clipboard write failed (OSC 52 may still have copied): %v", err)
		}
		return nil
	}
}
