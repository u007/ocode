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
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("SSH port must be between 1 and 65535")
	}
	return nil
}

// SSHArgs returns the target portion of an ssh command, including the
// optional port flag. Callers prepend command-specific flags such as -t.
func (t Target) SSHArgs() []string {
	args := make([]string, 0, 3)
	if t.Port > 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	return append(args, t.String())
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
