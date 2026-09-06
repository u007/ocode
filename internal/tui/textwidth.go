package tui

import (
	"github.com/rivo/uniseg"
)

// visualWidth returns the display width (in terminal cells) of a plain-text
// string, counting grapheme clusters rather than individual runes.
//
// go-runewidth's RuneWidth undercounts emoji-presentation sequences whose base
// code point is East-Asian width "Neutral" (e.g. U+2328 ⌨, U+2699 ⚙) followed
// by U+FE0F VARIATION SELECTOR-16: it reports a width of 1, but terminals
// honoring VS16 render them 2 cells wide. This is the same grapheme math
// charmbracelet/lipgloss v2 uses internally (via charmbracelet/x/ansi), so any
// TUI cell computation that leads with runewidth disagrees with lipgloss box
// padding by one column on such sequences — the "double-width emoji" misalignment.
//
// Use visualWidth (and the iterator below) wherever TUI display-width math on
// arbitrary content is performed. Do not introduce go-runewidth for new code.
//
// NOTE: visualWidth is only valid on plain (ANSI-strip) lines. Call sites that
// may receive ANSI escape codes must skip escapes themselves, then measure the
// remaining printable grapheme clusters.
func visualWidth(s string) int {
	if s == "" {
		return 0
	}
	return uniseg.StringWidth(s)
}

// nextVisualCluster returns the next grapheme cluster of s, its byte length,
// and its display width in terminal cells. It is the grapheme-aware replacement
// for the common "decode one rune → runewidth.RuneWidth → advance" pattern: a
// single call consumes one full cluster (including VS16 sequences and
// ZWJ-joined emoji families) with the correct width so column math stays in
// sync with what the terminal renders.
//
// Callers that must skip ANSI escape sequences should do so before/around this
// function and advance by length; printable output should never contain ANSI
// when measured here.
func nextVisualCluster(s string) (cluster string, width int) {
	c, _, w, _ := uniseg.FirstGraphemeClusterInString(s, -1)
	if w <= 0 {
		// uniseg reports 0 for control/format chars and -1 for unresolved
		// widths; treat those as a single cell so the caller still advances
		// over the cluster instead of looping forever.
		w = 1
	}
	return c, w
}
