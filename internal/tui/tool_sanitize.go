package tui

import (
	"strings"
	"unicode/utf8"
)

// sanitizeForTUI strips or replaces characters in tool output that can corrupt
// the TUI's alt-screen rendering. The TUI runs in Bubble Tea's alt-screen
// mode: any byte in the content that the terminal interprets as a control
// sequence (cursor movement, screen clear, line feed, etc.) will overwrite
// the rendered frame and break layout. In particular, a stray carriage
// return (`\r`) is the canonical cause of "scrolling down becomes scrolling
// up" symptoms — the cursor snaps to column 0 and the next line overwrites
// from the left, scrambling the viewport's internal line index.
//
// What this function preserves:
//   - SGR ("Select Graphic Rendition") color/style escapes: `\x1b[<n>m`. These
//     are emitted by chroma syntax highlighting and `git diff --color` and
//     are the only ANSI codes the TUI relies on for visual styling.
//   - Newlines (`\n`) and tabs (`\t`), so layout and indentation survive.
//
// What this function strips or replaces:
//   - OSC sequences (`\x1b]...BEL` / `\x1b]...ST`): can rewrite the window
//     title, write to the system clipboard, or trigger desktop notifications.
//   - CSI sequences whose final byte is NOT `m`: cursor up/down/left/right,
//     cursor home, erase display, erase line, scroll up/down, etc. These
//     are the moves that corrupt the alt-screen frame.
//   - DCS / SOS / PM / APC sequences: device-specific payloads of unknown
//     length that some terminals act on.
//   - Two-character ESC sequences (`\x1b<single byte>`): the second byte is
//     almost always a terminal control (e.g. `\x1b=` keypad mode).
//     We strip the whole pair.
//   - C0 control codes (0x00–0x1F) other than `\n` and `\t`: NUL, BEL, BS,
//     VT, FF, SO/SI, LF, CR, SUB. `\r` is replaced with `\n` (the scroll
//     fix); everything else is dropped.
//   - C1 control codes (0x80–0x9F): the 8-bit control range (8-bit CSI,
//     8-bit OSC, etc.). Always terminal-active, always bad here.
//   - Zero-width and bidi-override characters: ZWSP, ZWNJ, ZWJ, LRM, RLM,
//     LRE/RLE/PDF/LRO/RLO, LSEP/PSEP, soft hyphen, BOM. These don't move
//     the cursor but they break column-to-rune math in the selection
//     highlight code and can produce visual reversals with surrounding
//     RTL text.
//
// This function is intentionally a single linear walk (no regex). ANSI
// sequences have variable-length parameter lists and a final byte that may
// itself be in the printable range; regexes on ANSI are a known source of
// bugs. Walking by rune gives byte-correct results and lets us decide
// per-sequence whether to keep, drop, or replace.
//
// A byte-level fast path was tried and rejected: the zero-width / bidi
// runes we drop encode as ordinary multi-byte UTF-8 lead bytes (0xC2,
// 0xE2, 0xEF, …), not as control bytes, so a control-byte pre-scan
// would miss them. Going through the full walk unconditionally is
// simpler and the allocator cost is paid only once per tool result.
func sanitizeForTUI(content string) string {
	if content == "" {
		return content
	}

	var b strings.Builder
	b.Grow(len(content))
	i := 0
	for i < len(content) {
		c := content[i]

		// Handle ESC (\x1b) — start of every ANSI-family sequence.
		if c == 0x1b {
			if i+1 < len(content) {
				next := content[i+1]
				switch next {
				case '[':
					// CSI: ESC [ <params> <final 0x40-0x7E>
					end := scanCSI(content, i+2)
					if end > i+2 {
						final := content[end-1]
						if final == 'm' {
							// SGR: keep — this is just colors.
							b.WriteString(content[i:end])
						}
						// else: cursor/erase/scroll — drop.
						i = end
						continue
					}
					// Malformed CSI; drop the ESC and keep scanning.
					i++
					continue

				case ']':
					// OSC: ESC ] ... BEL  or  ESC ] ... ESC \
					end := scanOSC(content, i+2)
					if end > i+2 {
						// Drop the entire OSC (window title, etc.).
						i = end
						continue
					}
					i++
					continue

				case 'P', 'X', '^', '_':
					// DCS / SOS / PM / APC: variable-length payload
					// terminated by ESC \ (ST) or, for DCS, by a cancel
					// byte. We don't trust unknown device commands —
					// scan for ST and drop the whole sequence.
					end := scanStringTerminator(content, i+2)
					if end > i+2 {
						i = end
						continue
					}
					// No terminator: drop ESC only, keep going.
					i++
					continue

				default:
					// Two- or three-character ESC sequence.
					//
					// For most second bytes (e.g. `=`, `>`, `<`, `c`,
					// `7`, `8`, `D`, `E`, `H`, `M`, `Z`, `\\`) the
					// sequence is exactly 2 bytes: ESC + final. Drop
					// both.
					//
					// For SCS / G0-G3 character-set selects (`(`, `)`,
					// `*`, `+`, `-`, `.`, `/`) and DEC line attributes
					// (`#`) the sequence is 3 bytes: ESC + intro +
					// charset/attribute. Drop all three — the third
					// byte is otherwise a normal printable character
					// (e.g. `'B'`, `'0'`, `'8'`) and would survive and
					// confuse the column math if we left it in.
					//
					// Special case: ESC ESC. Consume both so the
					// second can begin a fresh sequence.
					if next == 0x1b {
						i += 2
						continue
					}
					if isSCSOrLineAttrIntro(next) {
						// Only consume the third byte if it's actually
						// present; otherwise drop just the opener and
						// let the next iteration re-classify the
						// partial sequence.
						if i+2 < len(content) {
							i += 3
						} else {
							i += 2
						}
						continue
					}
					i += 2
					continue
				}
			}
			// ESC at end of input — drop it.
			i++
			continue
		}

		// C0 control range (0x00–0x1F). Keep LF (\n) and TAB (\t).
		// Convert CR (\r) to LF — this is the explicit fix for the
		// "scrolling down becomes scrolling up" symptom in alt-screen.
		// CR-only line endings (old Mac) and CRLF are both normalized
		// to LF so lipgloss wraps consistently.
		if c < 0x20 {
			switch c {
			case '\n', '\t':
				b.WriteByte(c)
			case '\r':
				// Swallow the LF half of a CRLF if it follows.
				if i+1 < len(content) && content[i+1] == '\n' {
					b.WriteByte('\n')
					i++
				} else {
					b.WriteByte('\n')
				}
			default:
				// Drop: NUL, BEL, BS, VT, FF, SO, SI, SUB, ESC
				// (handled above), etc.
			}
			i++
			continue
		}

		// DEL (0x7F). Sometimes emitted by line editors; always a
		// control code as far as the terminal is concerned.
		if c == 0x7F {
			i++
			continue
		}

		// C1 control range (0x80–0x9F). These are the 8-bit forms of
		// the C0 controls (CSI, OSC, etc.) — drop them; a fresh CSI
		// is handled separately above. Bare 8-bit control bytes
		// without an opener are not legal text. Note the explicit
		// `>= 0x80` lower bound: without it, `c < 0xA0` would also
		// catch all printable ASCII (0x20–0x7E) and silently drop
		// every letter and digit in the input.
		if c >= 0x80 && c < 0xA0 {
			i++
			continue
		}

		// At this point we have a multi-byte UTF-8 sequence. Decode
		// the rune to check for problematic zero-width / bidi chars.
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8: drop the byte to avoid feeding the
			// terminal garbage. The selection highlight code
			// assumes valid UTF-8 too.
			i++
			continue
		}
		if isZeroWidthOrBidi(r) {
			i += size
			continue
		}
		// Plain (or at least non-problematic) text: copy the whole
		// rune's bytes verbatim. We don't normalize combining marks
		// or strip variation selectors — chroma and selection
		// highlight expect them, and they don't move the cursor.
		b.WriteString(content[i : i+size])
		i += size
	}
	return b.String()
}

// scanCSI walks a CSI sequence starting after the `[` and returns the
// index just past the final byte. A CSI final byte is in 0x40–0x7E
// after zero or more parameter bytes (0x30–0x3F) and intermediate bytes
// (0x20–0x2F). Returns i+1 (just past the `[`) for a malformed sequence
// (no final byte), which the caller treats as "drop the ESC and resume".
func scanCSI(s string, i int) int {
	for i < len(s) {
		c := s[i]
		if c >= 0x40 && c <= 0x7E {
			return i + 1
		}
		i++
	}
	return i
}

// scanOSC walks an OSC sequence starting after the `]` and returns the
// index just past the terminator. OSC ends at BEL (0x07) or at the
// two-byte String Terminator ESC \\. Returns i (still pointing at the
// opener) for a malformed sequence.
func scanOSC(s string, i int) int {
	for i < len(s) {
		c := s[i]
		if c == 0x07 {
			return i + 1
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

// scanStringTerminator walks a DCS/SOS/PM/APC payload and returns the
// index just past the String Terminator (ESC \\). On any other byte it
// keeps scanning. Returns i (pointing at the opener) for a malformed
// sequence with no terminator.
func scanStringTerminator(s string, i int) int {
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

// isSCSOrLineAttrIntro reports whether b is the introducer byte of a
// 3-byte ESC sequence: an SCS (Select Character Set) G0-G3 select
// (ESC followed by one of ( ) * + - . /) or a DEC line attribute
// (ESC #). The terminal consumes the third byte as a parameter
// (charset name or attribute number); we must drop all three bytes
// or the third byte would otherwise leak through as a printable
// character and break column math.
func isSCSOrLineAttrIntro(b byte) bool {
	switch b {
	case '(', ')', '*', '+', '-', '.', '/', '#':
		return true
	}
	return false
}

// isZeroWidthOrBidi reports whether r is a character that does not
// advance the cursor on its own and can confuse column math in the
// selection highlight walker, or that reverses/overrides text
// direction. We don't strip combining marks in general — those are
// legitimate text content — only the truly invisible control range.
func isZeroWidthOrBidi(r rune) bool {
	switch r {
	case '\u200B', // ZERO WIDTH SPACE
		'\u200C', // ZERO WIDTH NON-JOINER
		'\u200D', // ZERO WIDTH JOINER
		'\u200E', // LEFT-TO-RIGHT MARK
		'\u200F', // RIGHT-TO-LEFT MARK
		'\u202A', // LEFT-TO-RIGHT EMBEDDING
		'\u202B', // RIGHT-TO-LEFT EMBEDDING
		'\u202C', // POP DIRECTIONAL FORMATTING
		'\u202D', // LEFT-TO-RIGHT OVERRIDE
		'\u202E', // RIGHT-TO-LEFT OVERRIDE
		'\u2028', // LINE SEPARATOR
		'\u2029', // PARAGRAPH SEPARATOR
		'\u00AD', // SOFT HYPHEN
		'\uFEFF': // ZERO WIDTH NO-BREAK SPACE / BOM
		return true
	}
	return false
}
