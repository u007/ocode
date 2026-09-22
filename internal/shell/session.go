package shell

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupported is returned by NewSession on platforms without a pty (Windows
// today). Callers treat it as "fall back to a one-shot shellpkg.Run" rather
// than a hard failure, so `!` commands never regress to "nothing happens".
var ErrUnsupported = errors.New("persistent shell sessions are not supported on this platform")

// ErrClosed is returned by Session.Run after the session has been closed.
var ErrClosed = errors.New("shell session is closed")

// SessionOptions configures a persistent interactive shell.
//
// Env is APPENDED to the process environment after the session's own
// overrides (HISTFILE, TERM, …). Go's exec.Cmd keeps the last value for a
// duplicate key, so passing e.g. ZDOTDIR or HOME here overrides the inherited
// value — which is how the tests point the shell at a throwaway rc file.
type SessionOptions struct {
	Shell   string
	Dir     string
	Env     []string
	Timeout time.Duration
	Grace   time.Duration
	Cols    uint16
	Rows    uint16
}

// markerPrefix and commandPrefix name the two framing sentinels. They carry a
// per-session random nonce so a command's own output (or a stale marker from a
// previous session) can never be mistaken for a boundary.
const (
	markerPrefix  = "__OCODE_DONE_"
	commandPrefix = "__OCODE_CMD_"
	// readyPrefix tags the startup sentinel's output, so the handshake can pick
	// out the marker that follows the sentinel even though the prelude itself
	// fires the prompt hook (and therefore emits markers) before it.
	readyPrefix = "__OCODE_READY_"
)

// defaultGrace is the window a timed-out command gets between SIGINT and the
// hard pty teardown.
const defaultGrace = 2 * time.Second

// newNonce returns a random hex string used to build the session's markers.
func newNonce() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate shell session nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// commandDelimiter is the heredoc delimiter for the framed command unit.
func commandDelimiter(nonce string) string { return commandPrefix + nonce + "__" }

// doneMarker is the line the shell's prompt hook prints when it is ready for
// the next command.
func doneMarker(nonce string) string { return markerPrefix + nonce + "__" }

// readyToken is the sentinel's own output; the startup handshake waits for the
// marker that appears after it.
func readyToken(nonce string) string { return readyPrefix + nonce + "__" }

// startupCommand returns the shell snippet the handshake sends: it prints the
// ready token and is then followed by the marker from the prompt hook.
//
// The token is emitted as two adjacent quoted words so the command TEXT does
// not contain the literal token. At startup the tty may still be echoing input
// (the prelude's `stty -echo` has been read but the shell may not have executed
// it yet), and an echoed copy of the token would let the handshake match the
// prelude's own marker instead of the sentinel's — desynchronising every later
// Run by one command.
func startupCommand(nonce string) string {
	split := len(readyPrefix) - len("READY_")
	return "printf '%s\\n' '" + readyPrefix[:split] + "''" + readyPrefix[split:] + nonce + "__'"
}

// frameCommand wraps command in a single shell unit that the shell parses as
// one command regardless of its syntax:
//
//	eval "$(cat <<'<delim>'
//	<command>
//	<delim>
//	)"
//
// The quoted heredoc delimiter means the text is passed through verbatim (no
// expansion), and eval then parses it exactly as the user typed it — including
// several complete lines. That collapses a multi-line command into ONE prompt
// hook firing, so exactly one marker follows and Run returns the last line's
// exit status. An unwrapped command would fire the hook once per line and leak
// the tail into the next Run.
//
// A command that already contains the delimiter as a whole line is rejected
// before anything is written: it would terminate the heredoc early and turn the
// remainder into commands the session would then execute.
func frameCommand(command, delim string) (string, error) {
	if command == "" {
		return "", errors.New("shell command is empty")
	}
	// SplitSeq avoids materialising the slice for a command that is usually a
	// single line; the delimiter is a whole-line sentinel, so substring
	// containment is not enough.
	for line := range strings.SplitSeq(normalizeNewlines(command), "\n") {
		if line == delim {
			return "", fmt.Errorf("shell command contains the reserved delimiter %q", delim)
		}
	}
	var b strings.Builder
	b.Grow(len(command) + len(delim)*2 + 32)
	b.WriteString("eval \"$(cat <<'")
	b.WriteString(delim)
	b.WriteString("'\n")
	b.WriteString(command)
	if !strings.HasSuffix(command, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(delim)
	b.WriteString("\n)\"\n")
	return b.String(), nil
}

// findMarker scans raw for the first occurrence of marker that is followed by
// a numeric exit status, and returns:
//   - the byte offset of the marker (the output is everything before it),
//   - the exit status parsed from the first field after the marker,
//   - the current working directory (the remainder of that line, so a path
//     containing spaces survives intact),
//   - whether the marker was found at all.
//
// The marker is normally at the start of a line because the hook prints a
// leading newline; accepting it mid-line too costs nothing and makes framing
// robust when a command's output has no trailing newline.
func findMarker(raw, marker string) (offset, status int, cwd string, ok bool) {
	search := 0
	for {
		idx := strings.Index(raw[search:], marker)
		if idx < 0 {
			return 0, 0, "", false
		}
		idx += search
		after := strings.TrimPrefix(raw[idx+len(marker):], " ")
		digits := 0
		for digits < len(after) && after[digits] >= '0' && after[digits] <= '9' {
			digits++
		}
		if digits > 0 {
			endOfField := digits == len(after)
			var term byte
			if !endOfField {
				term = after[digits]
			}
			if endOfField || term == ' ' || term == '\n' || term == '\r' {
				if st, err := strconv.Atoi(after[:digits]); err == nil {
					dir := strings.TrimPrefix(after[digits:], " ")
					if nl := strings.IndexAny(dir, "\r\n"); nl >= 0 {
						dir = dir[:nl]
					}
					return idx, st, dir, true
				}
			}
		}
		search = idx + len(marker)
	}
}

// ansiPattern matches CSI/OSC/Fe escape sequences. TERM=dumb plus NO_COLOR
// should keep them out of the output entirely; this is the safety net for a
// program that emits colour unconditionally.
var ansiPattern = regexp.MustCompile(
	`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`,
)

// stripANSI removes terminal escape sequences from s.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	return ansiPattern.ReplaceAllString(s, "")
}

// normalizeNewlines converts CRLF and lone CR to LF. A pty in onlcr mode (or a
// program that prints progress with \r) would otherwise leak carriage returns
// into the transcript.
func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// cleanOutput turns the raw bytes captured before the marker into the user
// facing result: normalised newlines, no ANSI escapes, no echoed command line,
// and no boundary blank lines (the marker hook prints a leading newline).
func cleanOutput(raw, framed string) string {
	s := stripANSI(normalizeNewlines(raw))
	if framed != "" {
		f := normalizeNewlines(framed)
		// Echo should be off (stty -echo, ZLE disabled), but strip a leading
		// echo of the framed unit defensively when a shell echoed it anyway.
		if after, found := strings.CutPrefix(s, f); found {
			s = after
		} else if idx := strings.Index(s, f); idx >= 0 && strings.TrimSpace(s[:idx]) == "" {
			s = s[idx+len(f):]
		}
		// Some shells echo only the first line of a multi-line heredoc.
		first, _, _ := strings.Cut(f, "\n")
		if first != "" {
			if after, found := strings.CutPrefix(s, first+"\n"); found {
				s = after
			}
		}
	}
	return strings.Trim(s, "\r\n")
}

// Quote single-quotes s for safe interpolation into a shell command. Callers
// that need to embed a path (e.g. the registry's project-switch `cd`) use it so
// a path with spaces or quotes cannot change the command's meaning.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
