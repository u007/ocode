package remote

import (
	"sort"
	"strings"
)

// RemoteShellInfo describes the shells available on a remote target, as
// reported by the remote host itself rather than by the local machine.
//
// This exists because every other shell-resolution path in ocode is
// necessarily machine-local: config.DefaultTerminalShell reads the local
// $SHELL and the local /etc/shells, and terminal_shell is a persisted local
// config value that also rides along in the credential-sync payload. Using any
// of those to start a shell on another host is how a Mac's /bin/zsh ends up on
// a Linux remote and fails with
// `fork/exec /bin/zsh: no such file or directory`.
type RemoteShellInfo struct {
	// Default is the shell the remote would use for a non-interactive login
	// command (`<shell> -l -c <command>`): the remote's own $SHELL when that
	// names an executable POSIX-family shell, else the first available
	// fallback. Never empty — an unparseable probe result yields /bin/sh.
	Default string
	// Available lists every executable shell found, in probe order (remote
	// $SHELL first when usable, then the fallback chain, then /etc/shells),
	// de-duplicated. It may be empty while Default is still populated.
	Available []string
}

// remoteShellFallbacks is the candidate order used by the remote probe and the
// remote launch scripts. bash comes first because ocode's command strings are
// POSIX/bash syntax, then zsh (the macOS default this codebase historically
// assumed), then sh (the POSIX baseline that exists on essentially every Unix).
var remoteShellFallbacks = []string{
	"/bin/bash", "/usr/bin/bash",
	"/bin/zsh", "/usr/bin/zsh",
	"/bin/sh", "/usr/bin/sh",
}

// posixShellBasenames restricts *non-interactive* execution (`-l -c <command>`)
// to shells that accept POSIX/bash syntax. An exotic remote login shell (fish,
// tcsh, nu) would reject `export FOO=1`, so it is skipped as the shell that
// runs the user's *command* — the same policy cmd/ocode-desktop's
// configureLoginShell applies locally. Interactive terminal sessions are NOT
// filtered: ocode hands those shells no command string, so the remote user's
// real login shell is the right choice.
//
// This filter only picks which candidate is exec'd. The selection scripts
// themselves are POSIX programs, and ssh hands the command string to the
// remote *login* shell for parsing — so every script is additionally wrapped
// by posixWrap, which is what actually keeps a tcsh/fish login shell from ever
// parsing POSIX syntax (unwrapped, tcsh loops forever on "Missing '}'").
const posixShellBasenames = "bash|zsh|sh|dash|ksh"

// posixWrap runs script under the remote's /bin/sh regardless of the remote
// user's login shell: `sh -c '<script>'`. ssh executes a remote command as
// `<login shell> -c "<string>"`, so without this wrapper a csh/fish login
// shell would parse (and choke on) the POSIX for/case/exec loops below. The
// single-quoted form is the one quoting shape csh, fish and POSIX shells all
// agree on, and shellQuote's `'\”` escape survives all three. WSL applies the
// same isolation locally via wslExecArgs.
func posixWrap(script string) string {
	return "sh -c " + shellQuote(script)
}

// remoteShellCandidates returns the POSIX word list the probe and launch
// scripts iterate: the remote's own $SHELL first, then the fallback chain. "$SHELL"
// may expand to nothing when unset, which every consumer guards against.
func remoteShellCandidates() string {
	return `"$SHELL" ` + strings.Join(remoteShellFallbacks, " ")
}

// shellPickLoop returns the opening of a POSIX for-loop that leaves $c set to
// the first candidate that is non-empty and executable, skipping a
// non-POSIX-family shell when posixOnly is set. Callers append their `exec`
// and the loop terminator (`done`).
func shellPickLoop(posixOnly bool) string {
	loop := `for c in ` + remoteShellCandidates() + `; do ` +
		`[ -n "$c" ] && [ -x "$c" ] || continue; `
	if posixOnly {
		loop += `case "${c##*/}" in ` + posixShellBasenames + `) ;; *) continue ;; esac; `
	}
	return loop
}

// shellLaunchScript is the remote-side command an interactive SSH terminal
// runs (appended to a `cd <path> && ` prefix). It execs the first usable
// candidate so the pty's process is the login shell itself, matching the
// previous `exec "${SHELL:-/bin/sh}" -l` shape.
//
// The loop is the fix for a stale remote $SHELL: `${SHELL:-/bin/sh}` only
// guards an *unset* variable, so a remote account whose $SHELL names a binary
// that does not exist (a dotfile copied from another machine, a removed
// package) could not start a terminal at all. Testing each candidate with `-x`
// and falling through means the terminal always comes up.
func shellLaunchScript() string {
	return shellPickLoop(false) + `exec "$c" -l; done; exec /bin/sh -l`
}

// launchScriptWithCd returns the posixWrap'd interactive launcher, prefixed
// with a `cd` into path so the terminal opens in the remote project. The cd
// is inside the wrapper so its `"$HOME/…"` expansion is also evaluated by
// /bin/sh, never by the remote user's login shell.
func launchScriptWithCd(path string) string {
	return posixWrap("cd " + shellQuotePath(path) + " && " + shellLaunchScript())
}

// LoginShellScript returns the remote-side command that runs command through the
// target's own login shell: the first usable POSIX-family candidate execs
// `<shell> -l -c <command>`, so a broken $SHELL cannot prevent execution and
// profile files are sourced (restoring the remote user's PATH).
//
// This is what makes the web `!` prefix work against a remote project: the
// command runs under a shell the *remote* has, instead of the local machine's
// $SHELL being handed to a process on another host.
//
// The returned string is posixWrap'd: the selection loop is a POSIX program
// and must not be parsed by whatever login shell the remote account has.
func LoginShellScript(command string) string {
	return posixWrap(loginShellBody(command))
}

// loginShellBody is the unwrapped POSIX selection loop behind LoginShellScript.
func loginShellBody(command string) string {
	return shellPickLoop(true) + `exec "$c" -l -c ` + shellQuote(command) + `; done; ` +
		`exec /bin/sh -l -c ` + shellQuote(command)
}

// CdedLoginShellScript is LoginShellScript prefixed with a `cd` into a remote
// project path, so a remote command runs with the project as its working
// directory (the same contract the local shell.Build honors via cmd.Dir). The
// cd sits inside the posixWrap so /bin/sh, not the login shell, evaluates it.
func CdedLoginShellScript(path, command string) string {
	prefix := ""
	if path != "" {
		prefix = "cd " + shellQuotePath(path) + " && "
	}
	return posixWrap(prefix + loginShellBody(command))
}

// ShellProbeCommand is the remote command that reports the usable shells: one
// `shell=<path>` line for the resolved non-interactive default, then one
// `available=<path>` line per usable candidate, then one per executable entry
// in /etc/shells.
//
// Every candidate must pass `-x` before it is reported, so the local side never
// receives (and therefore never caches) a path the remote cannot exec.
// /etc/shells is read line-by-line rather than with a helper so a remote
// without it (or without a readable one) still yields the fallback result.
//
// It is posixWrap'd like the launch scripts: the probe is a POSIX program and
// must be parsed by /bin/sh, not the remote account's login shell. Callers run
// it through a bounded exec (see server.remoteShellProbeFn) and hand stdout to
// ParseShellProbe.
func ShellProbeCommand() string {
	return posixWrap(`sh=''; ` + shellPickLoop(true) + `sh=$c; break; done; ` +
		`[ -n "$sh" ] || sh=/bin/sh; echo "shell=$sh"; ` +
		`for c in ` + remoteShellCandidates() + `; do ` +
		`[ -n "$c" ] && [ -x "$c" ] && echo "available=$c"; done; ` +
		`if [ -r /etc/shells ]; then while IFS= read -r line; do ` +
		`case "$line" in ''|'#'*) continue ;; esac; ` +
		`[ -x "$line" ] && echo "available=$line"; done < /etc/shells; fi`)
}

// ParseShellProbe interprets the `shell=`/`available=` lines emitted by
// ShellProbeCommand. Unknown lines are ignored so a remote shell that prints
// MOTD/banner noise ahead of the probe output cannot corrupt the result
// (mirrors the marker-isolation approach in internal/discovery/python_env.go).
// An unparseable or empty probe degrades to the /bin/sh baseline rather than
// an error: a shell choice is a convenience, and refusing to describe it must
// never block a workspace from opening.
func ParseShellProbe(out string) RemoteShellInfo {
	return parseShellProbe(out, RemoteShellInfo{Default: "/bin/sh"})
}

func parseShellProbe(out string, fallback RemoteShellInfo) RemoteShellInfo {
	info := fallback
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "shell="):
			if v := strings.TrimPrefix(line, "shell="); v != "" {
				info.Default = v
			}
		case strings.HasPrefix(line, "available="):
			v := strings.TrimPrefix(line, "available=")
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			info.Available = append(info.Available, v)
		}
	}
	if info.Default == "" {
		info.Default = "/bin/sh"
	}
	return info
}

// SortShells returns shells ordered for stable presentation, keeping bash and
// zsh ahead of the rest regardless of probe order so a picker lists the most
// likely choices first.
func SortShells(shells []string) []string {
	out := append([]string(nil), shells...)
	rank := func(p string) int {
		switch {
		case strings.HasSuffix(p, "/bash"):
			return 0
		case strings.HasSuffix(p, "/zsh"):
			return 1
		case strings.HasSuffix(p, "/sh"):
			return 3
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}
