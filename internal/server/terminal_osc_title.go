package server

import "strings"

// terminalTitleMaxRunes bounds a stored OSC title so a runaway program cannot
// bloat the in-memory session record or the inventory response. Mirrors the
// SPA's MAX_OSC_TITLE_LEN (web/src/stores/terminalStore.tsx).
const terminalTitleMaxRunes = 80

const (
	// oscMaxCommandLen bounds the numeric OSC command accumulator; real
	// commands are 1-4 digits, so anything longer is not a title sequence.
	oscMaxCommandLen = 8
	// oscMaxTitleBytes bounds the raw payload before sanitization so a missing
	// terminator cannot grow the buffer without limit.
	oscMaxTitleBytes = 512
)

const (
	oscScanGround = iota
	oscScanEsc
	oscScanCmd
	oscScanTitle
	oscScanTitleEsc
)

// oscTitleScanner incrementally extracts the payload of OSC 0 / OSC 2
// ("set window title") sequences from a pty byte stream. Sequences can be
// split across reads, so it carries a small state machine instead of scanning
// each chunk independently. Other OSC codes are ignored.
//
// The zero value is ready to use.
type oscTitleScanner struct {
	state int
	cmd   []byte
	title []byte
}

// feed consumes p and reports the latest complete OSC 0/2 title in it. ok is
// false when the chunk did not complete a title sequence; when ok is true the
// title may be empty, which means the program explicitly cleared it.
func (sc *oscTitleScanner) feed(p []byte) (title string, ok bool) {
	for _, b := range p {
		switch sc.state {
		case oscScanGround:
			if b == 0x1b {
				sc.state = oscScanEsc
			}
		case oscScanEsc:
			switch b {
			case ']':
				sc.cmd = sc.cmd[:0]
				sc.title = sc.title[:0]
				sc.state = oscScanCmd
			case 0x1b:
				// Stay: a run of ESCs still ends with the ']' we want.
			default:
				sc.state = oscScanGround
			}
		case oscScanCmd:
			switch b {
			case ';':
				if string(sc.cmd) == "0" || string(sc.cmd) == "2" {
					sc.state = oscScanTitle
				} else {
					sc.state = oscScanGround
				}
			case 0x07, 0x1b:
				sc.state = oscScanGround
			default:
				if len(sc.cmd) < oscMaxCommandLen {
					sc.cmd = append(sc.cmd, b)
				} else {
					sc.state = oscScanGround
				}
			}
		case oscScanTitle:
			switch b {
			case 0x07: // BEL terminator
				title, ok = sanitizeTerminalTitle(string(sc.title)), true
				sc.state = oscScanGround
			case 0x1b:
				sc.state = oscScanTitleEsc
			default:
				if len(sc.title) < oscMaxTitleBytes {
					sc.title = append(sc.title, b)
				}
			}
		case oscScanTitleEsc:
			if b == '\\' { // ST terminator (ESC \)
				title, ok = sanitizeTerminalTitle(string(sc.title)), true
				sc.state = oscScanGround
			} else if b == 0x1b {
				// A second ESC restarts the terminator, still inside the title.
			} else {
				// Not ST: the ESC opened some other sequence. Drop the partial
				// title and resume scanning from just after this byte.
				sc.state = oscScanGround
			}
		}
	}
	return title, ok
}

// sanitizeTerminalTitle normalizes the raw OSC payload for display: control
// characters (including newlines and tabs) are collapsed to single spaces and
// the result is truncated to terminalTitleMaxRunes runes.
func sanitizeTerminalTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > terminalTitleMaxRunes {
		s = string(runes[:terminalTitleMaxRunes])
	}
	return s
}
