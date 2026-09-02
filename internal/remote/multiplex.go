package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Multiplexer identifies which terminal multiplexer (if any) wraps the
// remote TUI launch, so that a dropped SSH connection detaches instead of
// killing the remote session. See "Session resume on disconnect (TUI)" in
// docs/superpowers/specs/2026-08-29-remote-ssh/01-architecture.md.
type Multiplexer int

const (
	// MultiplexerNone means neither tmux nor screen was found on the
	// remote — the launch falls back to a plain passthrough with no
	// resume-on-disconnect.
	MultiplexerNone Multiplexer = iota
	MultiplexerTmux
	MultiplexerScreen
)

func (m Multiplexer) String() string {
	switch m {
	case MultiplexerTmux:
		return "tmux"
	case MultiplexerScreen:
		return "screen"
	default:
		return "none"
	}
}

// detectMultiplexCmd is the remote shell command used to probe for a
// multiplexer: prints "tmux", "screen", or nothing.
const detectMultiplexCmd = `if command -v tmux >/dev/null 2>&1; then echo tmux; elif command -v screen >/dev/null 2>&1; then echo screen; fi`

// DetectMultiplexer probes the remote host for tmux or screen, preferring
// tmux. Any error from the probe itself (not just "neither found") degrades
// to MultiplexerNone per the "never fail the connect over it" rule — the
// caller is expected to render the resulting warning, not abort.
func DetectMultiplexer(t Transport) Multiplexer {
	res, err := t.Exec(detectMultiplexCmd)
	if err != nil {
		return MultiplexerNone
	}
	switch strings.TrimSpace(res.Stdout) {
	case "tmux":
		return MultiplexerTmux
	case "screen":
		return MultiplexerScreen
	default:
		return MultiplexerNone
	}
}

// SessionName derives a deterministic multiplexer session name from the
// resolved remote project path, so repeated connects to the same project
// reattach to the same session instead of piling up new ones. remotePath
// should already be the resolved path (post "~" expansion is not required —
// the string only needs to be stable across invocations, and the raw
// pre-expansion form the user/picker passed is stable enough).
func SessionName(remotePath string) string {
	sum := sha256.Sum256([]byte(remotePath))
	return "ocode-" + hex.EncodeToString(sum[:])[:12]
}

// WrapLaunch builds the remote-side command line for the launch stage,
// wrapping remoteOcodeCmd (the plain "<remote-ocode-path> <project-path>"
// invocation) in the chosen multiplexer so that losing the local `ssh -t`
// only detaches — it never kills — the remote session:
//
//   - tmux:   tmux new-session -A -s <name> <remoteOcodeCmd>
//   - screen: screen -xRR <name> <remoteOcodeCmd>
//   - none:   <remoteOcodeCmd> unchanged
//
// "-A"/"-xRR" attach-if-exists-else-create in one call, so first connect and
// reattach-after-disconnect are the same code path.
func WrapLaunch(m Multiplexer, remotePath, remoteOcodeCmd string) string {
	name := SessionName(remotePath)
	switch m {
	case MultiplexerTmux:
		return fmt.Sprintf("tmux new-session -A -s %s %s", shellQuote(name), remoteOcodeCmd)
	case MultiplexerScreen:
		return fmt.Sprintf("screen -xRR %s %s", shellQuote(name), remoteOcodeCmd)
	default:
		return remoteOcodeCmd
	}
}

// ResumeWarning returns the one-line, non-fatal warning to print at the
// multiplex-detect stage when neither tmux nor screen was found, or "" when
// m has resume support.
func ResumeWarning(m Multiplexer) string {
	if m != MultiplexerNone {
		return ""
	}
	return "tmux/screen not found on remote — this session will not survive a dropped connection; install tmux on the remote for resumable sessions"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellQuotePath quotes a remote path for use in a shell command, the same
// way shellQuote does, except a leading "~" or "~/" is translated to the
// double-quoted "$HOME" parameter expansion rather than single-quoted.
//
// A naive shellQuote("~/foo") produces '~/foo', and POSIX shells never
// expand "~" inside single quotes, so that would target a literal
// directory named "~" instead of $HOME/foo. The seemingly obvious fix —
// leave the tilde bare and quote only the rest, e.g. ~'/foo' — is also
// wrong: POSIX tilde-prefix recognition requires every character up to
// the first unquoted "/" to be unquoted too, so a quote immediately after
// "~" disqualifies the whole prefix from expansion (verified against both
// dash and bash). "$HOME" is expanded inside double quotes (only word
// splitting/globbing is suppressed there, not parameter expansion), so
// double-quoting "$HOME/<rest>" gets both correctness and safety — the
// rest just needs backslash/dollar/backtick/double-quote escaped, the only
// characters double quotes don't already neutralize.
//
// "~otheruser/..." forms are not handled (out of scope: every path built
// by this package is either the invoking user's own "~" or an absolute
// path).
func shellQuotePath(p string) string {
	if p == "~" {
		return `"$HOME"`
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return `"$HOME/` + escapeDoubleQuoted(rest) + `"`
	}
	return shellQuote(p)
}

var doubleQuoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")

func escapeDoubleQuoted(s string) string {
	return doubleQuoteEscaper.Replace(s)
}
