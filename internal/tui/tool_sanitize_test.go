package tui

import (
	"strings"
	"testing"
)

// TestSanitizeForTUIKeepsSGRColors verifies that color/style SGR escapes
// are preserved verbatim. chroma syntax highlighting and `git diff --color`
// both emit these — stripping them would lose all styled output.
func TestSanitizeForTUIKeepsSGRColors(t *testing.T) {
	cases := []string{
		"\x1b[0m",                        // reset
		"\x1b[1;31m",                     // bold red
		"\x1b[38;5;208m",                 // 256-color orange
		"\x1b[38;2;255;100;0m",           // 24-bit color
		"\x1b[48;2;0;0;0;38;2;255mfoo",   // bg + fg in one
		"hello \x1b[31mred\x1b[0m world", // inline
	}
	for _, in := range cases {
		got := sanitizeForTUI(in)
		if got != in {
			t.Errorf("SGR sequence was modified: in=%q got=%q", in, got)
		}
	}
}

// TestSanitizeForTUIStripCSINonSGR is the core of the fix. Any CSI whose
// final byte is not 'm' moves the cursor, erases the screen, scrolls the
// region, or otherwise corrupts the alt-screen frame. They must go.
func TestSanitizeForTUIStripCSINonSGR(t *testing.T) {
	cases := map[string]string{
		// Cursor movement.
		"\x1b[2J":   "", // erase entire screen
		"\x1b[H":    "", // cursor home
		"\x1b[1;1H": "", // cursor home (explicit)
		"\x1b[10A":  "", // cursor up 10
		"\x1b[5B":   "", // cursor down 5
		"\x1b[3C":   "", // cursor forward 3
		"\x1b[2D":   "", // cursor back 2
		"\x1b[6n":   "", // device status report (causes echo)
		// Erase.
		"\x1b[K":  "", // erase to end of line
		"\x1b[2K": "", // erase whole line
		"\x1b[1J": "", // erase to start of screen
		// Scroll.
		"\x1b[3S": "", // scroll up
		"\x1b[2T": "", // scroll down
		// Save/restore cursor.
		"\x1b[s": "", // save cursor
		"\x1b[u": "", // restore cursor
		// Mode setting (also a CSI but not SGR).
		"\x1b[?25h": "", // show cursor
		"\x1b[?25l": "", // hide cursor
		// Mixed with surrounding text.
		"before\x1b[2Jafter": "beforeafter",
		"a\x1b[1Ab":          "ab",
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("CSI non-SGR not stripped: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIStripOSC verifies OSC sequences (window title, etc.)
// are removed. These can hijack the terminal's title bar or trigger
// desktop notifications even when the TUI is in alt-screen.
func TestSanitizeForTUIStripOSC(t *testing.T) {
	cases := map[string]string{
		"\x1b]0;evil title\x07":                      "",
		"\x1b]2;/etc/passwd\x07":                     "",
		"\x1b]0;title with \x1b] inside\x07":         "",
		"\x1b]52;c;SGVsbG8=\x07":                     "", // clipboard write
		"\x1b]0;title\x1b\\":                         "", // ST-terminated
		"\x1b]0;multi\x1b\\more\x1b]1;title2\x07end": "moreend",
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("OSC not stripped: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIHandlesCRLF is the explicit regression for the
// "scrolling down becomes scrolling up" symptom. A lone CR in tool
// output causes the cursor to jump to column 0 without advancing
// rows, and the next text overwrites the start of the same visual
// line, which the viewport reads as backward motion. CRLF should
// become LF; lone CR should become LF.
func TestSanitizeForTUIHandlesCRLF(t *testing.T) {
	cases := map[string]string{
		"line1\r\nline2":      "line1\nline2",
		"line1\rline2":        "line1\nline2",
		"\r\n\r\n":            "\n\n",
		"a\rb\rc":             "a\nb\nc",
		"trailing\r":          "trailing\n",
		"crlf\r\nmixed\rhere": "crlf\nmixed\nhere",
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("CR not normalized: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIDropsOtherC0Controls verifies the rest of the C0
// range is dropped, except \n and \t which we preserve.
func TestSanitizeForTUIDropsOtherC0Controls(t *testing.T) {
	cases := map[string]string{
		"a\x00b":       "ab",           // NUL
		"a\x07b":       "ab",           // BEL (ringing the bell is annoying)
		"a\x08b":       "ab",           // BS
		"a\x0bb":       "ab",           // VT
		"a\x0cb":       "ab",           // FF
		"a\x0eb":       "ab",           // SO
		"a\x0fb":       "ab",           // SI
		"a\x1ab":       "ab",           // SUB
		"a\x7fb":       "ab",           // DEL
		"col1\tcol2":   "col1\tcol2",   // TAB kept
		"line1\nline2": "line1\nline2", // LF kept
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("C0 handling wrong: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIDropsC1Controls verifies the 8-bit control range
// (0x80-0x9F) is dropped. In UTF-8, these are never a valid text byte
// start (UTF-8 continuation bytes are 0x80-0xBF, but those can't
// appear outside a leading byte, and a leading byte would be 0xC2+).
// In legacy 8-bit locales they map to CSI/OSC variants, which we
// never want active.
func TestSanitizeForTUIDropsC1Controls(t *testing.T) {
	for r := 0x80; r < 0xA0; r++ {
		in := "a" + string([]byte{byte(r)}) + "b"
		got := sanitizeForTUI(in)
		if got != "ab" {
			t.Errorf("C1 control U+%04X not dropped: got=%q", r, got)
		}
	}
}

// TestSanitizeForTUIDropsZeroWidthAndBidi verifies invisible directional
// and zero-width characters are removed. They don't move the cursor
// but they break column-to-rune math in the selection highlight walker
// and can produce visual reversals.
func TestSanitizeForTUIDropsZeroWidthAndBidi(t *testing.T) {
	cases := map[string]string{
		"a\u200bb":         "ab", // ZWSP
		"a\u200cb":         "ab", // ZWNJ
		"a\u200db":         "ab", // ZWJ
		"a\u200eb":         "ab", // LRM
		"a\u200fb":         "ab", // RLM
		"a\u202ab":         "ab", // LRE
		"a\u202bb":         "ab", // RLE
		"a\u202cb":         "ab", // PDF
		"a\u202db":         "ab", // LRO
		"a\u202eb":         "ab", // RLO
		"a\u2028b":         "ab", // LSEP
		"a\u2029b":         "ab", // PSEP
		"a\u00adb":         "ab", // SHY
		"a\ufeffb":         "ab", // BOM
		"hello\u200bworld": "helloworld",
		"a\u202a\u202cb":   "ab",
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("zero-width/bidi not dropped: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIPreservesMultibyteUTF8 verifies that valid multibyte
// UTF-8 (CJK, accented Latin, emoji) passes through unchanged. The TUI
// uses runewidth to lay these out; the sanitizer must not interfere.
//
// Note: ZWJ (U+200D) is in the drop set, so a ZWJ-joined family emoji
// such as 👨‍👩‍👧‍👦 becomes four separate emoji 👨👩👧👦. That's
// intentional — the alt-screen TUI's selection walker is rune-based,
// not grapheme-based, and leaving ZWJ in would break column math for
// click-and-drag text selection in such strings.
func TestSanitizeForTUIPreservesMultibyteUTF8(t *testing.T) {
	cases := []string{
		"héllo wörld",   // Latin-1 supplement
		"日本語のテキスト",      // CJK
		"emoji 🎉🚀 here", // supplementary plane
		"café — naïve",  // em dash, diaeresis
		"Привет, мир",   // Cyrillic
		"🇺🇸 flag",       // regional indicator pair
	}
	for _, in := range cases {
		got := sanitizeForTUI(in)
		if got != in {
			t.Errorf("UTF-8 mangled: in=%q got=%q", in, got)
		}
	}
}

// TestSanitizeForTUIDropsInvalidUTF8 verifies malformed UTF-8 bytes
// are dropped. The selection highlight walker assumes valid UTF-8 and
// would panic on a bad continuation byte, so the sanitizer must keep
// the input clean.
func TestSanitizeForTUIDropsInvalidUTF8(t *testing.T) {
	// 0xFF is never a valid UTF-8 byte.
	in := "a" + string([]byte{0xFF}) + "b"
	got := sanitizeForTUI(in)
	if got != "ab" {
		t.Errorf("invalid UTF-8 not dropped: got=%q", got)
	}
	// Lone continuation byte (0x80) — also invalid alone.
	in = "a" + string([]byte{0x80}) + "b"
	got = sanitizeForTUI(in)
	if got != "ab" {
		t.Errorf("lone continuation byte not dropped: got=%q", got)
	}
}

// TestSanitizeForTUIStripsDCSandFriends verifies DCS / SOS / PM / APC
// sequences are stripped. They carry device-specific payloads of
// unknown length; a Sixel graphics payload (DCS) in particular could
// repaint the entire screen.
func TestSanitizeForTUIStripsDCSandFriends(t *testing.T) {
	cases := map[string]string{
		"\x1bPpayload\x1b\\":         "", // DCS
		"\x1bPsome;data;here\x1b\\":  "", // DCS with semicolons
		"\x1bXprivate\x1b\\":         "", // SOS
		"\x1b^private-message\x1b\\": "", // PM
		"\x1b_application\x1b\\":     "", // APC (no command byte)
		"a\x1bPgraphics\x1b\\b":      "ab",
		"\x1bPno-terminator":         "", // unterminated DCS — drop the opener
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("DCS-family not stripped: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIStripsTwoByteESCSequences covers the catch-all for
// ESC + one byte (no `[`, `]`, `P`, `X`, `^`, `_`). These set terminal
// modes (keypad, character set, etc.) and never appear in legitimate
// text.
func TestSanitizeForTUIStripsTwoByteESCSequences(t *testing.T) {
	cases := map[string]string{
		"\x1b=":   "", // DECKPAM (keypad application mode)
		"\x1b>":   "", // DECKPNM (keypad numeric mode)
		"\x1b<":   "", // exit keypad mode
		"\x1b7":   "", // save cursor (xterm)
		"\x1b8":   "", // restore cursor (xterm)
		"\x1bc":   "", // full reset
		"a\x1b=b": "ab",
		// SCS / DEC line-attribute intro consumes a third byte too;
		// without dropping it the charset byte (often a printable
		// letter or digit) leaks through and lands in the rendered
		// text as garbage.
		"\x1b(B":   "", // select US ASCII into G0
		"\x1b(0":   "", // select DEC Special Graphics into G0
		"\x1b)B":   "", // select US ASCII into G1
		"\x1b)0":   "", // select DEC Special Graphics into G1
		"\x1b#8":   "", // DEC alignment test
		"a\x1b(Bb": "ab",
		// Two ESCs in a row: both consumed, the byte after is plain
		// text. (Real terminals treat ESC ESC as just two ESCs; we
		// collapse to drop both and continue from the next byte.)
		"a\x1b\x1bb": "ab",
	}
	for in, want := range cases {
		got := sanitizeForTUI(in)
		if got != want {
			t.Errorf("2-byte ESC sequence not stripped: in=%q want=%q got=%q", in, want, got)
		}
	}
}

// TestSanitizeForTUIHandlesMalformed is a defensive check: the sanitizer
// must not panic on degenerate input (unterminated sequences, ESC at
// end of string, ESC at end of buffer with no following byte, etc.).
func TestSanitizeForTUIHandlesMalformed(t *testing.T) {
	cases := []string{
		"\x1b",           // ESC at end of string
		"\x1b[",          // unterminated CSI
		"\x1b]0;title",   // OSC without terminator
		"\x1bP",          // DCS without terminator
		"\x1b[999999999", // CSI with absurd parameter count (no final byte)
		"a\x1b[1",        // CSI missing final byte
	}
	for _, in := range cases {
		// Should not panic; should produce something usable.
		_ = sanitizeForTUI(in)
	}
}

// TestSanitizeForTUIPreservesPlainASCIIFastPath documents that pure
// ASCII text (no controls, no high bytes) is returned unchanged. No
// fast-path shortcut is used — the function still walks the string
// once — but the walk is a series of byte copies with no decisions,
// so the result is identical to the input.
func TestSanitizeForTUIPreservesPlainASCIIFastPath(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog\nand some more text"
	got := sanitizeForTUI(in)
	if got != in {
		t.Errorf("plain ASCII modified: in=%q got=%q", in, got)
	}
}

// TestSanitizeForTUIRealisticAttacks assembles a realistic "nasty"
// tool output (something a malicious or buggy subprocess might emit)
// and verifies the result is safe to render in the TUI. Each element
// is chosen because it has caused real TUI bugs in the past.
func TestSanitizeForTUIRealisticAttacks(t *testing.T) {
	// Join with \n so the expected text fragments stay on their own
	// visual lines after sanitization.
	nasty := strings.Join([]string{
		"normal line 1",
		"\x1b[2J", // clear screen
		"normal line 2",
		"\x1b]0;pwned\x07", // set window title
		"normal line 3\r",  // trailing CR (scroll-reversal culprit)
		"\x1b[5A",          // cursor up
		"after cursor up",
		"\x1b]52;c;SGVsbG8=\x07", // clipboard write
		"\x07\x07\x07",           // bell spam
		"\x1b[?25l",              // hide cursor
		"normal line 4",
		"end\u200bwith\ufeffzero\u200dwidth",
	}, "\n")
	got := sanitizeForTUI(nasty)
	// No control bytes or escape sequences should survive (Lone \n
	// is the one allowed C0 byte; \t is allowed too but not expected
	// here).
	for i := 0; i < len(got); i++ {
		c := got[i]
		if c == 0x1b || (c < 0x20 && c != '\n' && c != '\t') || c == 0x7F || (c >= 0x80 && c < 0xA0) {
			t.Errorf("unsafe byte 0x%02X survived in: %q", c, got)
		}
	}
	// And the textual content should be intact (note: ZWJ is dropped,
	// so the trailing "width" no longer has its zero-width chars).
	for _, want := range []string{"normal line 1", "normal line 2", "after cursor up", "normal line 4", "endwithzerowidth"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in sanitized output, got %q", want, got)
		}
	}
}

// TestSanitizeForTUIEmptyString documents the no-op fast path.
func TestSanitizeForTUIEmptyString(t *testing.T) {
	if got := sanitizeForTUI(""); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

// TestRenderToolResultStripsDangerousControls is the end-to-end
// integration test: it confirms that the renderToolResult boundary
// actually wires the sanitizer in. Without the sanitizeForTUI call
// at the top of renderToolResult, the dangerous content below would
// reach the viewport unchanged.
//
// We test the plain-text branch (tool name "bash", content that
// does not start with "DIFF:" and does not look like a unified
// diff) so the result is just st.Text.Render(sanitized). The
// rendered output will have lipgloss SGR (color) codes wrapping
// the visible text — those are the only escape codes we keep on
// purpose. We assert that:
//   - no CSI other than SGR is present (no `J`, `H`, `A`, etc.
//     final bytes);
//   - no OSC, DCS, or two-byte ESC sequence is present;
//   - the dangerous raw CR has been normalized to LF.
//   - the visible text "line one" and "line two" is preserved.
func TestRenderToolResultStripsDangerousControls(t *testing.T) {
	content := "line one\r\x1b[2Jline two"
	got := renderToolResult("bash", content, ApplyThemeColors("tokyonight"))

	// No raw CR, no bell, no backspace, no other C0 controls
	// except LF and TAB.
	if strings.ContainsRune(got, '\r') {
		t.Errorf("expected CR to be normalized, got %q", got)
	}
	for _, r := range got {
		if r == 0x07 || r == 0x08 || r == 0x0B || r == 0x0C || r == 0x1A || r == 0x7F {
			t.Errorf("dangerous control %U leaked into rendered output: %q", r, got)
		}
	}

	// No non-SGR CSI sequences: scan for ESC [ and verify the next
	// non-digit, non-semicolon, non-`?` byte is 'm'.
	for i := 0; i+1 < len(got); i++ {
		if got[i] == 0x1b && got[i+1] == '[' {
			// Find the final byte.
			j := i + 2
			for j < len(got) {
				b := got[j]
				if b >= 0x40 && b <= 0x7E {
					break
				}
				j++
			}
			if j >= len(got) {
				t.Errorf("unterminated CSI leaked into output: %q", got)
			} else if got[j] != 'm' {
				t.Errorf("non-SGR CSI with final %q leaked into output: %q", got[j], got)
			}
		}
		// Reject OSC (ESC ]), DCS (ESC P), PM (ESC ^), SOS (ESC X),
		// APC (ESC _) — they should never reach the rendered output.
		if got[i] == 0x1b && (got[i+1] == ']' || got[i+1] == 'P' || got[i+1] == 'X' || got[i+1] == '^' || got[i+1] == '_') {
			t.Errorf("non-SGR escape sequence (ESC %q) leaked into output: %q", got[i+1], got)
		}
	}

	// Visible text survived.
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Errorf("expected visible text to survive sanitization, got %q", got)
	}
}
