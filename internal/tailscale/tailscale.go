// Package tailscale shares the tailscale serve/funnel lifecycle helpers used
// by both the TUI /rc path and the headless/desktop server share endpoint.
// Keeping the CLI parsing in one place avoids drift between the two callers:
// the serve/funnel output format is parsed identically, --set-path mounts are
// sanitized identically, and cleanup removes only the caller's own mount.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// exposeTimeout bounds how long any `tailscale <cmd> --bg` invocation may run
// before it is killed. A hung tailscale CLI must not wedge the caller: the
// desktop boot hook runs this in the background, and the Share dialog calls it
// inline while the user waits.
const exposeTimeout = 2 * time.Second

// FunnelHTTPSPort is the public HTTPS port every funnel mount is created on.
//
// Funnel is only permitted on 443, 8443 and 10000, and 443 is routinely already
// occupied by tailnet-only `serve` routes (the TUI /rc `/ses_...` mounts, for
// instance). Tailscale cannot expose one port as BOTH serve and funnel — "if
// the most recent command to configure the port was serve, then the port will
// be completely private" — so a funnel attempt that defaults to 443 silently
// degrades to a tailnet-only mount, which is exactly how a "public" share ended
// up reachable only over the VPN. Mounting funnel on 8443 keeps the public and
// tailnet listeners on separate ports; 8443 is always funnel-eligible.
const FunnelHTTPSPort = 8443

// knownCandidates lists common installation paths for the tailscale
// CLI, in priority order. The desktop shell launches with a minimal
// PATH that does not include /usr/local/bin, where tailscale is
// typically installed, so we search these locations as a fallback
// after PATH resolution.
var knownCandidates = []string{
	"/usr/local/bin/tailscale",
	"/opt/homebrew/bin/tailscale",
	"/usr/bin/tailscale",
	"/usr/sbin/tailscale",
}

// findCLIImpl is the testable implementation. candidates is the list
// of known installation paths to check after PATH lookup fails.
// lookPath is injected so tests can simulate PATH resolution without
// touching the real filesystem.
func findCLIImpl(candidates []string, lookPath func(string) (string, error)) string {
	if lookPath != nil {
		if p, err := lookPath("tailscale"); err == nil {
			return p
		}
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if info.Mode().Perm()&0111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

// findCLI resolves the tailscale CLI path. It first tries exec.LookPath
// (PATH lookup), then checks knownCandidates. Each candidate is
// verified as executable before being returned; a non-executable or
// stale file is skipped.
func findCLI() string {
	return findCLIImpl(knownCandidates, exec.LookPath)
}

// CLIPath resolves the tailscale CLI for every helper in this package, and is
// the injection seam that keeps test binaries away from the developer's live
// node. `tailscale funnel --https=8443 --set-path /desktop off` edits the real
// node-wide serve config, and a full `go test ./...` once silently deleted a
// running share. A test binary that can reach an exposure helper (a server that
// is built and shut down, the TUI /rc path) must therefore replace this in its
// TestMain with a resolver returning "" (or a fake binary). Production never
// reassigns it.
var CLIPath = findCLI

// SanitizePath returns a tailscale-safe --set-path component derived from an
// id (session ID, or "desktop" for the server share). It strips characters
// tailscale treats specially ("/" as a path separator, "." can break path
// normalization) and falls back to "ocode" when the input is empty, so
// sessions never collapse onto the root mount.
func SanitizePath(id string) string {
	const fallback = "ocode"
	cleaned := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 {
		cleaned = []rune(fallback)
	}
	return "/" + string(cleaned)
}

// URLWithPathPrefix mounts the serve/funnel base URL at exactly pathPrefix.
// Tailscale output can carry a sibling session's existing --set-path entry in
// the parsed URL line; appending would yield a doubled, unroutable prefix
// that tailscale longest-prefix-matches to the stale route. Replace instead.
func URLWithPathPrefix(baseURL, pathPrefix string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	if pathPrefix == "" {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + pathPrefix
	}
	u.Path = pathPrefix
	return u.String()
}

// BuildSessionURL appends /session/<id>?token=<token> to a tailscale base URL.
func BuildSessionURL(baseURL, sessionID, token string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/session/%s?token=%s", baseURL, sessionID, token)
}

// DNSName returns the tailnet DNS base URL (e.g. "https://host.ts.net")
// from `tailscale status --json`, or "" on failure.
func DNSName(tailscalePath string) string {
	statusCmd := exec.Command(tailscalePath, "status", "--json")
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	if err := statusCmd.Run(); err != nil {
		return ""
	}
	var status struct {
		SELF struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if json.Unmarshal(statusOut.Bytes(), &status) != nil {
		return ""
	}
	dnsName := strings.TrimSuffix(status.SELF.DNSName, ".")
	if dnsName != "" {
		return fmt.Sprintf("https://%s", dnsName)
	}
	return ""
}

// Installed reports whether a tailscale CLI exists, without asking the daemon
// anything. It is the cheap "could this machine serve at all" check.
func Installed() bool {
	return CLIPath() != ""
}

// Running reports whether the tailscale CLI exists and the daemon answers
// `tailscale status`.
func Running() (string, bool) {
	p := CLIPath()
	if p == "" {
		return "", false
	}
	if err := exec.Command(p, "status").Run(); err != nil {
		return "", false
	}
	return p, true
}

// normalizeExposeTarget guarantees the target carries a URL scheme.
//
// tailscale's CLI accepts a bare "localhost:port" but REJECTS any other bare
// host:port with `non-localhost target "http://host:port" must include a
// scheme`. /rc passes the server's bound address (a LAN IP), so without this
// the serve/funnel command exits non-zero, no mount is created, and the
// caller can end up advertising a URL that resolves to the tailnet's root
// mount (a dead backend -> 502). An already-schemed target is left untouched.
func normalizeExposeTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "://") {
		return target
	}
	if strings.Contains(target, ":") && !strings.HasPrefix(target, "/") && !isAllDigits(target) {
		return "http://" + target
	}
	return target
}

// isAllDigits reports whether s is a non-empty run of ASCII digits (a bare port).
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Expose tries `tailscale <cmd> --bg [--set-path] <target>` (cmd is "funnel"
// or "serve") and returns the advertised URL plus the background process.
// The process must be killed when no longer needed; the --set-path mount must
// additionally be removed via RemoveSetPath because --bg detaches the config.
//
// wait receives a command that is already deadline-bound by exposeTimeout
// (exec.CommandContext), so a hung CLI is killed rather than leaking a
// long-lived child, and a caller passing cmd.Wait() cannot block forever.
func Expose(tailscalePath, cmd, target, pathPrefix string, wait func(cmd *exec.Cmd) error) (string, *exec.Cmd, string) {
	url, proc, hint, _ := exposeAttempt(tailscalePath, cmd, target, pathPrefix, wait)
	return url, proc, hint
}

// exposeAttempt is Expose plus the CLI's combined output. A failed attempt
// returns a nil process, and its output is the only place the reason lives
// when the CLI prints neither a URL nor an enable link.
func exposeAttempt(tailscalePath, cmd, target, pathPrefix string, wait func(cmd *exec.Cmd) error) (url string, proc *exec.Cmd, hint, output string) {
	args := []string{cmd, "--bg"}
	if cmd == "funnel" {
		// Funnel must NOT share 443 with the tailnet-only serve routes: the same
		// port cannot be both, so a default-443 funnel silently stays private.
		// 8443 is funnel-eligible and keeps the two listeners separate. The CLI
		// echoes the full URL (including ":" + port) that Expose scrapes below,
		// so the caller gets the :8443 URL rather than the private 443 one.
		args = append(args, fmt.Sprintf("--https=%d", FunnelHTTPSPort))
	}
	if pathPrefix != "" {
		args = append(args, "--set-path", pathPrefix)
	}
	args = append(args, normalizeExposeTarget(target))
	ctx, cancel := context.WithTimeout(context.Background(), exposeTimeout)
	serveCmd := exec.CommandContext(ctx, tailscalePath, args...)
	var out bytes.Buffer
	serveCmd.Stdout = &out
	serveCmd.Stderr = &out

	if err := serveCmd.Start(); err != nil {
		cancel()
		log.Printf("tailscale %s failed to start: %v", cmd, err)
		return "", nil, "", err.Error()
	}

	var runErr error
	if wait != nil {
		done := make(chan error, 1)
		go func() { done <- wait(serveCmd) }()
		runErr = <-done
		// The context kills the child at exposeTimeout, so this is bounded. If
		// the caller's wait never reaped the process, this Wait does; if it did,
		// this returns immediately with "already called". Either way the child
		// has exited before the output buffer is read below.
		_ = serveCmd.Wait()
	} else {
		runErr = serveCmd.Wait()
	}
	cancel()

	output = out.String()
	// Only a zero exit proves a mount. tailscale prints its one-time enable link
	// (an https:// line) on a FAILED run, so the URL scan must not run on a
	// non-zero exit, or that link is reported as a live exposure.
	if runErr == nil {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
				return line, serveCmd, "", output
			}
		}
	}

	// A failed run: the one-time enable hint, when funnel/serve isn't enabled yet.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			return "", nil, line, output
		}
	}
	return "", nil, "", output
}

// StartExpose makes the local target (host:port) reachable via tailscale
// funnel (public) then serve (tailnet-only), mounted at the sanitized id path
// so instances coexist. The target must be the address the server actually
// listens on: a server bound to the LAN IP only is unreachable at
// localhost:<port>, and tailscale would silently proxy to whatever else owns
// that loopback port. A target without a scheme (e.g. "192.168.1.5:4096") is
// normalized to http:// by Expose; tailscale rejects a bare non-localhost
// host:port, which silently produced no mount. Returns the public URL including
// the path prefix, the background process for cleanup, and a one-time setup
// hint when the tailnet needs enabling.
func StartExpose(target, id string) (url string, proc *exec.Cmd, setupHint string) {
	u, p, h, _ := StartExposeWithKind(target, id)
	return u, p, h
}

// StartExposeWithKind is StartExpose plus the subcommand that produced the URL:
// "funnel" means the instance is reachable on the PUBLIC internet, "serve"
// means tailnet-only. A caller that surfaces the exposure to the user must use
// this variant, because otherwise it cannot honestly say which of the two is
// live — and reporting a public share as tailnet-only (or vice versa) is a
// security-relevant misstatement.
//
// The kind is empty when the URL came from the DNSName fallback (no process was
// started, so nothing is provably exposed) or when tailscale is unavailable.
func StartExposeWithKind(target, id string) (url string, proc *exec.Cmd, setupHint, kind string) {
	tailscalePath, ok := Running()
	if !ok {
		return "", nil, "", ""
	}
	pathPrefix := SanitizePath(id)
	wait := func(cmd *exec.Cmd) error { return cmd.Wait() }

	u, p, funnelHint, _ := exposeAttempt(tailscalePath, "funnel", target, pathPrefix, wait)
	if u != "" {
		return URLWithPathPrefix(u, pathPrefix), p, funnelHint, "funnel"
	}
	// A failed funnel attempt can still have written its PUBLIC mount before the
	// CLI failed to print a URL. Remove it before serve runs: otherwise serve is
	// reported while a funnel listener is live on the same path. Only the funnel
	// listener is removed; see the serve note below.
	if pathPrefix != "" {
		removeMount(tailscalePath, "funnel", pathPrefix)
	}
	u, p, serveHint, serveOutput := exposeAttempt(tailscalePath, "serve", target, pathPrefix, wait)
	if u != "" {
		return URLWithPathPrefix(u, pathPrefix), p, serveHint, "serve"
	}
	// No removal after a failed serve: tailscale_share upgrades a live serve
	// mount to funnel on this same path, and removing it here would kill a
	// working share when the re-mount fails.
	// The caller still gets a best-guess URL for the manual dialog, but kind is
	// "" because NOTHING was actually mounted. A caller that opens the URL (as
	// /rc does) must require a non-empty kind: the guess resolves to the
	// tailnet root mount, and a root mount whose backend is gone answers 502.
	return URLWithPathPrefix(DNSName(tailscalePath), pathPrefix), nil, failureHint(serveHint, serveOutput, funnelHint), ""
}

// failureHint is the setup hint when neither listener exposed anything. The
// serve attempt is the primary failure: its enable link, or else its own
// output, which is the only place its reason appears. The funnel hint is
// appended, never substituted, so a funnel enable link cannot hide the real
// serve error.
func failureHint(serveHint, serveOutput, funnelHint string) string {
	primary := serveHint
	if primary == "" {
		primary = strings.TrimSpace(serveOutput)
	}
	switch {
	case funnelHint == "":
		return primary
	case primary == "":
		return "funnel: " + funnelHint
	default:
		return primary + "\nfunnel: " + funnelHint
	}
}

// StartServeExpose starts a TAILNET-ONLY `tailscale serve` mount for target
// at the sanitized id path, and is the exposure used by auto-share-on-start.
//
// It deliberately does NOT try `funnel`, unlike StartExpose. Funnel publishes
// the instance on the public internet; auto-share fires at boot with nobody
// watching, so an unattended exposure must stay inside the tailnet. The
// manual Share dialog keeps StartExpose's funnel-first behaviour because that
// click is an explicit, informed request to share.
//
// Unlike StartExpose it returns "" when serve exposes nothing, rather than
// falling back to a bare DNSName() guess: with no dialog to render a setup
// hint next to it, an unproven URL is worse than an honest "unavailable" — the
// caller logs the hint instead.
//
// Returns the URL including the path prefix, the background process for
// cleanup, and a one-time setup hint when serve is not enabled on the tailnet.
func StartServeExpose(target, id string) (url string, proc *exec.Cmd, setupHint string) {
	tailscalePath, ok := Running()
	if !ok {
		return "", nil, ""
	}
	pathPrefix := SanitizePath(id)
	wait := func(cmd *exec.Cmd) error { return cmd.Wait() }

	if u, p, hint := Expose(tailscalePath, "serve", target, pathPrefix, wait); u != "" {
		return URLWithPathPrefix(u, pathPrefix), p, hint
	}
	return "", nil, ""
}

// RemoveSetPath removes a single --set-path mount (best-effort). Both funnel
// and serve are tried because the caller doesn't track which one succeeded.
func RemoveSetPath(pathPrefix string) {
	if pathPrefix == "" {
		return
	}
	tailscalePath := CLIPath()
	if tailscalePath == "" {
		return
	}
	// Both listeners are tried: a funnel mount and a serve mount can share a
	// path, and removing only one would orphan the other.
	removeMount(tailscalePath, "funnel", pathPrefix)
	removeMount(tailscalePath, "serve", pathPrefix)
}

// removeMount runs one best-effort `tailscale <cmd> --set-path <prefix> off`
// for a single listener. Each listener needs its own --https: funnel lives on
// FunnelHTTPSPort while serve uses the 443 default, so one shared argv would
// target only one of them and orphan the other's mount — leaving a PUBLIC
// funnel live after the user pressed Stop. A failure is logged, not returned:
// the caller has no mount to fall back to.
func removeMount(tailscalePath, cmd, pathPrefix string) {
	if pathPrefix == "" {
		return
	}
	args := []string{cmd}
	if cmd == "funnel" {
		args = append(args, fmt.Sprintf("--https=%d", FunnelHTTPSPort))
	}
	args = append(args, "--set-path", pathPrefix, "off")
	ctx, cancel := context.WithTimeout(context.Background(), exposeTimeout)
	c := exec.CommandContext(ctx, tailscalePath, args...)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	cancel()
	if err != nil {
		log.Printf("tailscale %s: %v\n  output: %s", strings.Join(args, " "), err, strings.TrimRight(out.String(), "\n"))
	}
}
