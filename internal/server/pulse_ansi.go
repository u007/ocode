package server

import "regexp"

// ansiEscapeRE matches CSI sequences (colours, cursor movement), OSC sequences
// (titles, hyperlinks) terminated by BEL or ST, and two-byte escapes. The
// server had no ANSI stripper of its own and internal/tui's is unexported and
// belongs to a UI package the server must not import.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

func stripANSIEscapes(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}
