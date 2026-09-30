// Package remote implements ocode Remote: connecting to and provisioning
// ocode on a remote host over SSH or a local WSL distro. See
// docs/superpowers/specs/2026-08-29-remote-ssh/ for the design.
package remote

import (
	"fmt"
	"strconv"
	"strings"
)

// Kind identifies which transport a Target uses.
type Kind int

const (
	// KindSSH targets any host the system ssh binary can reach.
	KindSSH Kind = iota
	// KindWSL targets a local Windows Subsystem for Linux distro, launched
	// via wsl.exe. Only valid when ocode itself is running on Windows —
	// see validateTargetOS.
	KindWSL
)

// Target is a parsed connect destination.
type Target struct {
	Kind Kind
	// User, Host: KindSSH only. User may be empty (ssh_config/agent decides).
	User string
	Host string
	// Port is an optional SSH port. It is ignored for WSL targets.
	Port int
	// Distro: KindWSL only. Empty means the default distro.
	Distro string
	// Raw is the original, unparsed target string — used as the stable
	// identity for caches and recent-project entries keyed by host.
	Raw string
}

// Validate checks the fields that are supplied separately by saved project
// configuration. ParseTarget validates the target string form; this method
// additionally validates an optional SSH port and rejects it for WSL.
func (t Target) Validate() error {
	if t.Kind == KindWSL {
		if t.Port != 0 {
			return fmt.Errorf("WSL targets cannot specify an SSH port")
		}
		if strings.ContainsAny(t.Distro, " \t\n/") {
			return fmt.Errorf("invalid WSL distribution")
		}
		return nil
	}
	if t.Host == "" {
		return fmt.Errorf("SSH host is required")
	}
	if strings.ContainsAny(t.User, "@/ \t\n") || strings.ContainsAny(t.Host, "@/ \t\n") {
		return fmt.Errorf("invalid SSH user or host")
	}
	// A host (or user) starting with "-" would be parsed by ssh as an OPTION,
	// not a destination: every ssh invocation here passes the target as a bare
	// argv element with no "--" separator. Reject it — see
	// ParseTargetRejectsLeadingDashHost for the verified ProxyCommand proof.
	// The user half matters too, because the user@host form is one token: a
	// leading-dash user makes the WHOLE token start with "-".
	if strings.HasPrefix(t.Host, "-") || strings.HasPrefix(t.User, "-") {
		return fmt.Errorf("invalid SSH user or host: must not begin with '-'")
	}
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("SSH port must be between 1 and 65535")
	}
	return nil
}

// SSHArgs returns the target portion of an ssh command: the optional port
// flag, then the "--" option terminator, then the [user@]host destination.
// Callers prepend command-specific flags such as -t.
//
// The "--" belongs HERE rather than in each caller so the ordering invariant
// lives in one place: ssh reads the first non-option element as the hostname,
// so the terminator must sit after -p but immediately before the destination.
// Getting that backwards makes ssh read "-p" as a hostname. Validate and
// ParseTarget reject a leading "-" on the target; the separator is the
// independent second barrier at the point the argv is built.
func (t Target) SSHArgs() []string {
	args := make([]string, 0, 4)
	if t.Port > 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	return append(args, "--", t.String())
}

// String returns the canonical [user@]host form for an SSH target, or
// wsl:<distro> for a WSL target — suitable as a cache/lookup key.
func (t Target) String() string {
	if t.Kind == KindWSL {
		return "wsl:" + t.Distro
	}
	if t.User != "" {
		return t.User + "@" + t.Host
	}
	return t.Host
}

// ParseTarget parses a connect destination: "[user@]host" for SSH, or
// "wsl:<distro>" / "wsl:" for WSL. This is pure syntax parsing — it does not
// check whether WSL targets are usable on the current OS; see
// validateTargetOS for that.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{}, fmt.Errorf("remote target is required (usage: ocode remote <[user@]host> [path])")
	}

	if rest, ok := strings.CutPrefix(s, "wsl:"); ok {
		return Target{Kind: KindWSL, Distro: rest, Raw: s}, nil
	}

	if strings.Contains(s, "/") || strings.ContainsAny(s, " \t\n") {
		return Target{}, fmt.Errorf("invalid remote target %q: expected [user@]host", s)
	}

	user, host, hasUser := strings.Cut(s, "@")
	if !hasUser {
		host = user
		user = ""
	}
	if host == "" {
		return Target{}, fmt.Errorf("invalid remote target %q: missing host", s)
	}
	if hasUser && user == "" {
		return Target{}, fmt.Errorf("invalid remote target %q: empty user before '@'", s)
	}
	// Reject a leading "-" on the host AFTER the user@ split, and on the user:
	// ssh receives "[user@]host" as one bare argv element with no "--"
	// separator, so a leading dash makes ssh parse it as an OPTION. Checking
	// the whole string instead of the host would miss "user@-oProxyCommand=id",
	// whose token only starts with "-o" after the user is prepended.
	// Validate repeats this because callers that build a Target field-by-field
	// from a request body never reach here.
	if strings.HasPrefix(host, "-") || strings.HasPrefix(user, "-") {
		return Target{}, fmt.Errorf("invalid remote target %q: must not begin with '-'", s)
	}

	return Target{Kind: KindSSH, User: user, Host: host, Raw: s}, nil
}

// validateTargetOS enforces "wsl: targets are only valid when the local OS
// is Windows" without hard-coding runtime.GOOS, so it's unit-testable on
// any platform (there is no Windows CI — see 04-phase3-wsl.md's Testing
// section). Callers pass runtime.GOOS; only tests pass a literal.
func validateTargetOS(kind Kind, goos string) error {
	if kind == KindWSL && goos != "windows" {
		return fmt.Errorf("wsl targets are only supported when ocode is running on Windows (this machine is %s)", goos)
	}
	return nil
}
