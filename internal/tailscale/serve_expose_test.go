package tailscale

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// isolateCLI makes the tailscale CLI lookup fully hermetic for one test.
//
// Clearing PATH alone is NOT enough: findCLIImpl falls back to the absolute
// knownCandidates paths (/usr/local/bin/tailscale and friends) when PATH
// lookup misses, so a test with an empty PATH silently shells out to the REAL
// tailscale binary and mutates the developer's live serve config. Every test
// that exercises exposure must therefore also neutralize knownCandidates.
func isolateCLI(t *testing.T) {
	t.Helper()
	savedCandidates := knownCandidates
	savedPath := CLIPath
	knownCandidates = nil
	// Opt IN to the real resolver. TestMain blocks it for the whole binary; this
	// helper is the ONLY place that re-enables it, and it points PATH at a temp
	// dir, so the real CLI stays unreachable.
	CLIPath = findCLI
	t.Cleanup(func() {
		knownCandidates = savedCandidates
		CLIPath = savedPath
	})
	t.Setenv("PATH", t.TempDir())
}

// installFakeTailscaleCLI points PATH at a fake `tailscale` and returns a
// reader for the argv of every invocation it received.
//
// The reader is a function, not a snapshot: the CLI has not run yet at install
// time, so returning the args eagerly would hand back an empty slice and make
// every assertion below vacuous.
//
// Each recorded line is one full invocation's argv, space-joined.
func installFakeTailscaleCLI(t *testing.T, stdout string) func() []string {
	t.Helper()
	isolateCLI(t)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.txt")
	// `status` must exit 0 so Running() reports tailscale as available; the
	// expose invocation prints the URL Expose() scrapes.
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + logPath + "\n" +
		"if [ \"$1\" = status ]; then exit 0; fi\n" +
		"printf '%s' '" + stdout + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() []string {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		var out []string
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
		return out
	}
}

// exposeCommands returns only the recorded invocations that are exposure
// attempts (i.e. not the `status` probe), by subcommand.
func exposeCommands(argv []string) []string {
	var out []string
	for _, l := range argv {
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "serve" || fields[0] == "funnel" {
			out = append(out, l)
		}
	}
	return out
}

// TestStartServeExposeNeverFunnels is the security regression guard for
// auto-share. StartExpose (the manual Share dialog path) tries `funnel` FIRST,
// which publishes the instance on the PUBLIC internet. Auto-share fires at boot
// with nobody watching, so it must only ever create a tailnet-only `serve` mount.
//
// The assertion is on the argv the fake CLI received, not on a returned value:
// a funnel attempt would still hand back a perfectly usable serve URL, so only
// the recorded command line can catch it.
func TestStartServeExposeNeverFunnels(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "https://host.ts.net\n")

	url, _, _ := StartServeExpose("localhost:1234", "desktop")

	expose := exposeCommands(calls())
	if len(expose) != 1 {
		t.Fatalf("want exactly one exposure attempt, got %v (full argv %q)", expose, calls())
	}
	if !strings.HasPrefix(expose[0], "serve ") {
		t.Fatalf("auto-share ran %q; funnel publishes to the public internet and must never be used for an unattended exposure", expose[0])
	}
	if !strings.Contains(expose[0], "--set-path /desktop") {
		t.Fatalf("auto-share must pin its own --set-path mount, got %q", expose[0])
	}
	if url != "https://host.ts.net/desktop" {
		t.Fatalf("url = %q, want https://host.ts.net/desktop", url)
	}
}

// TestStartServeExposeUnavailableWhenServeFails pins the no-silent-success
// rule: when `serve` yields no URL, StartServeExpose reports unavailable rather
// than falling back to a bare DNSName() guess. StartExpose deliberately does that
// fallback for the manual dialog (which shows a setup hint next to the URL), but
// auto-share has no UI to explain it, so advertising a URL that is not actually
// serving would be worse than reporting nothing.
func TestStartServeExposeUnavailableWhenServeFails(t *testing.T) {
	installFakeTailscaleCLI(t, "Serve is not enabled on your tailnet.\n")

	url, proc, _ := StartServeExpose("localhost:1234", "desktop")
	if url != "" {
		t.Fatalf("url = %q, want empty when serve exposes nothing", url)
	}
	if proc != nil {
		t.Fatal("proc should be nil when nothing was exposed")
	}
}

// TestStartServeExposeNoCLIWhenTailscaleAbsent proves "uses tailscale if
// available" degrades to a quiet no-op instead of an error or a hang: with no
// CLI reachable there must be no exposure attempt at all.
//
// This is also the guard that the fake-CLI tests never reach the real
// tailscale binary — see isolateCLI.
func TestStartServeExposeNoCLIWhenTailscaleAbsent(t *testing.T) {
	isolateCLI(t)

	url, proc, hint := StartServeExpose("localhost:1234", "desktop")
	if url != "" || proc != nil || hint != "" {
		t.Fatalf("absent tailscale must be a silent no-op, got url=%q proc=%v hint=%q", url, proc, hint)
	}
}

// TestStartExposeFunnelPinsPublicPort pins the port the funnel path attempts.
//
// Funnel is only permitted on 443, 8443 and 10000, and 443 is routinely already
// a tailnet-only `serve` port (the TUI /rc `/ses_...` mounts). Tailscale cannot
// expose one port as BOTH serve and funnel — "if the most recent command to
// configure the port was serve, then the port will be completely private" — so
// a funnel attempt that defaults to 443 silently stays tailnet-only, which is
// exactly how a "public" share ended up VPN-only. The funnel attempt must
// therefore carry --https=FunnelHTTPSPort.
func TestStartExposeFunnelPinsPublicPort(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "https://host.ts.net:8443/desktop\n")

	url, _, _, kind := StartExposeWithKind("localhost:1234", "desktop")

	expose := exposeCommands(calls())
	if len(expose) != 1 {
		t.Fatalf("want exactly one exposure attempt (funnel succeeded), got %v", expose)
	}
	if !strings.HasPrefix(expose[0], "funnel ") {
		t.Fatalf("want a funnel attempt, got %q", expose[0])
	}
	if !strings.Contains(expose[0], "--https=8443") {
		t.Fatalf("funnel must pin the public port, got %q", expose[0])
	}
	if kind != "funnel" {
		t.Fatalf("kind = %q, want funnel", kind)
	}
	if url != "https://host.ts.net:8443/desktop" {
		t.Fatalf("url = %q, want the public :8443 URL", url)
	}
}

// TestStartServeExposeKeeps443Default is the counterpart guard: serve is the
// tailnet-only path and must stay on its 443 default. A stray --https on it
// would move the tailnet listener and change every existing tailnet URL.
func TestStartServeExposeKeeps443Default(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "https://host.ts.net/desktop\n")

	_, _, _ = StartServeExpose("localhost:1234", "desktop")

	expose := exposeCommands(calls())
	if len(expose) != 1 {
		t.Fatalf("want exactly one exposure attempt, got %v", expose)
	}
	if strings.Contains(expose[0], "--https") {
		t.Fatalf("serve must keep the 443 default, got %q", expose[0])
	}
}

// TestRemoveSetPathClearsBothListeners pins the revocation path: funnel lives on
// FunnelHTTPSPort and serve on 443, so a single removal argv would target only
// one listener and orphan the other's mount — leaving a PUBLIC funnel live after
// Stop. Both must be attempted, and the funnel one must name its port.
func TestRemoveSetPathClearsBothListeners(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "")

	RemoveSetPath("/desktop")

	var sawFunnel, sawServe bool
	for _, argv := range calls() {
		if strings.HasPrefix(argv, "funnel ") {
			sawFunnel = true
			if !strings.Contains(argv, "--https=8443") {
				t.Fatalf("funnel removal must name the public port, got %q", argv)
			}
			if !strings.Contains(argv, "--set-path /desktop") {
				t.Fatalf("funnel removal must scope to the mount, got %q", argv)
			}
		}
		if strings.HasPrefix(argv, "serve ") {
			sawServe = true
			if strings.Contains(argv, "--https") {
				t.Fatalf("serve removal must keep the 443 default, got %q", argv)
			}
		}
	}
	if !sawFunnel || !sawServe {
		t.Fatalf("RemoveSetPath must clear both listeners, got %v", calls())
	}
}

// TestCLIPathSeamBlocksMutations guards the guard.
//
// A test binary that reaches an exposure helper must never invoke the real
// tailscale CLI. The seam is CLIPath: with it returning "", Running() reports
// tailscale unavailable and RemoveSetPath is a no-op, even though a working
// fake CLI is on PATH.
func TestCLIPathSeamBlocksMutations(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "https://host.ts.net/desktop\n")
	CLIPath = func() string { return "" }

	if u, p, h, k := StartExposeWithKind("localhost:1", "x"); u != "" || p != nil || h != "" || k != "" {
		t.Fatalf("exposure ran with the CLI seam blocked: %q/%v/%q/%q", u, p, h, k)
	}
	if u, p, h := StartServeExpose("localhost:1", "x"); u != "" || p != nil || h != "" {
		t.Fatalf("serve exposure ran with the CLI seam blocked: %q/%v/%q", u, p, h)
	}
	RemoveSetPath("/x")

	if got := calls(); len(got) != 0 {
		t.Fatalf("a seam-blocked test binary invoked the tailscale CLI: %v", got)
	}
}

func TestNormalizeExposeTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"bare LAN address", "192.168.1.5:4096", "http://192.168.1.5:4096"},
		{"bare localhost", "localhost:1234", "http://localhost:1234"},
		{"already http", "http://192.168.1.5:4096", "http://192.168.1.5:4096"},
		{"already https", "https://host.ts.net", "https://host.ts.net"},
		{"bare IPv6", "[::1]:4096", "http://[::1]:4096"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeExposeTarget(tt.target); got != tt.want {
				t.Fatalf("normalizeExposeTarget(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

// TestStartServeExposeSchemesNonLocalhostTarget is the regression guard for the
// /rc LAN-IP target. tailscale accepts a bare localhost:port but rejects any
// other bare host:port with "non-localhost target ... must include a scheme";
// the serve command then exits non-zero, no mount is created, and /rc opens a
// URL that resolves to the tailnet root (502). The assertion is on the argv the
// CLI actually received, because only that proves the scheme was added before
// the command ran.
func TestStartServeExposeSchemesNonLocalhostTarget(t *testing.T) {
	calls := installFakeTailscaleCLI(t, "https://host.ts.net/desktop\n")

	url, _, _ := StartServeExpose("192.168.1.5:4096", "desktop")

	expose := exposeCommands(calls())
	if len(expose) != 1 {
		t.Fatalf("want exactly one exposure attempt, got %v", expose)
	}
	if !strings.Contains(expose[0], "http://192.168.1.5:4096") {
		t.Fatalf("serve target lost its scheme, argv = %q", expose[0])
	}
	if url != "https://host.ts.net/desktop" {
		t.Fatalf("url = %q", url)
	}
}

// TestStartExposeWithKindMarksDNSNameGuessUnproven pins the contract /rc relies
// on: when neither funnel nor serve exposes anything, the manual-dialog path
// still returns a DNSName guess, but kind MUST be empty so a caller that opens
// the URL can tell it was never mounted. Opening an unproven URL sends every
// request to the tailnet root mount, which answers 502 when its backend is gone.
func TestStartExposeWithKindMarksDNSNameGuessUnproven(t *testing.T) {
	isolateCLI(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + logPath + "\n" +
		"if [ \"$1\" = status ] && [ \"$2\" = --json ]; then\n" +
		"  printf '%s' '{\"Self\":{\"DNSName\":\"host.ts.net.\"}}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = status ]; then exit 0; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	url, proc, _, kind := StartExposeWithKind("localhost:1234", "desktop")
	if kind != "" {
		t.Fatalf("kind = %q, want empty for a DNSName guess", kind)
	}
	if proc != nil {
		t.Fatal("proc should be nil when nothing was exposed")
	}
	if url != "https://host.ts.net/desktop" {
		t.Fatalf("url = %q, want the DNSName fallback for the manual dialog", url)
	}
}

// installScriptedTailscaleCLI is installFakeTailscaleCLI with per-subcommand
// behaviour, for failure paths. Each exposure attempt prints its own output and
// exits with its own code. A mount removal (argv ending in "off") always
// succeeds, as on a real node, and status exits 0 so Running() reports true.
// Outputs must not contain single quotes.
func installScriptedTailscaleCLI(t *testing.T, funnelOut string, funnelRC int, serveOut string, serveRC int) func() []string {
	t.Helper()
	isolateCLI(t)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + logPath + "\n" +
		"if [ \"$1\" = status ]; then exit 0; fi\n" +
		"case \"$*\" in *' off') exit 0;; esac\n" +
		"case \"$1\" in\n" +
		"  funnel) printf '%s' '" + funnelOut + "'; exit " + strconv.Itoa(funnelRC) + ";;\n" +
		"  serve) printf '%s' '" + serveOut + "'; exit " + strconv.Itoa(serveRC) + ";;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatalf("write scripted CLI: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() []string {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		var out []string
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
		return out
	}
}

// indexOfArgv returns the position of the first recorded invocation equal to
// want, or -1.
func indexOfArgv(argv []string, want string) int {
	for i, l := range argv {
		if l == want {
			return i
		}
	}
	return -1
}

const (
	funnelEnableOut = "Funnel is not enabled on your tailnet.\nTo enable, visit:\n\n\thttps://login.tailscale.com/f/funnel?node=abc\n"
	serveSchemeErr  = "invalid serve config: target must include a scheme\n"
)

// TestStartExposeWithKindKeepsServeErrorAsPrimaryHint pins the review fix: when
// both attempts fail, the serve error is the primary hint and the funnel enable
// link is only appended. Before the fix the serve reason was dropped entirely,
// and the funnel link was shown instead of it.
func TestStartExposeWithKindKeepsServeErrorAsPrimaryHint(t *testing.T) {
	installScriptedTailscaleCLI(t, funnelEnableOut, 1, serveSchemeErr, 1)

	_, proc, hint, kind := StartExposeWithKind("localhost:1234", "desktop")
	if kind != "" || proc != nil {
		t.Fatalf("nothing was mounted, got kind=%q proc=%v", kind, proc)
	}
	if !strings.HasPrefix(hint, "invalid serve config: target must include a scheme") {
		t.Fatalf("serve error must lead the hint, got %q", hint)
	}
	if !strings.Contains(hint, "funnel: https://login.tailscale.com/f/funnel?node=abc") {
		t.Fatalf("funnel enable link must be appended, got %q", hint)
	}
}

// TestStartExposeWithKindCleansFailedFunnelBeforeServe pins that a failed funnel
// attempt's public listener is removed before serve runs. It also pins the
// opposite for serve: a failed serve attempt must NOT remove the serve mount,
// because tailscale_share upgrades a live serve mount on the same path and a
// removal there would kill a working share.
func TestStartExposeWithKindCleansFailedFunnelBeforeServe(t *testing.T) {
	calls := installScriptedTailscaleCLI(t, "partial config written\n", 1, serveSchemeErr, 1)

	StartExposeWithKind("localhost:1234", "desktop")

	argv := calls()
	funnelOff := indexOfArgv(argv, "funnel --https=8443 --set-path /desktop off")
	serveAttempt := -1
	for i, l := range argv {
		if strings.HasPrefix(l, "serve --bg") {
			serveAttempt = i
		}
	}
	if funnelOff < 0 {
		t.Fatalf("failed funnel attempt was not removed, argv = %q", argv)
	}
	if serveAttempt < 0 || funnelOff > serveAttempt {
		t.Fatalf("funnel mount must be removed BEFORE the serve attempt, argv = %q", argv)
	}
	if indexOfArgv(argv, "serve --set-path /desktop off") >= 0 {
		t.Fatalf("a failed serve attempt must not remove the serve mount (upgrade path), argv = %q", argv)
	}
}

// TestStartExposeWithKindServeAfterFailedFunnelKeepsServeURL: a funnel attempt
// that fails leaves no URL, serve then succeeds, and the cleanup in between must
// not change the result the caller gets.
func TestStartExposeWithKindServeAfterFailedFunnelKeepsServeURL(t *testing.T) {
	calls := installScriptedTailscaleCLI(t, "partial config written\n", 1, "https://host.ts.net/desktop\n", 0)

	url, proc, _, kind := StartExposeWithKind("localhost:1234", "desktop")
	if kind != "serve" {
		t.Fatalf("kind = %q, want serve", kind)
	}
	if proc == nil {
		t.Fatal("serve succeeded, so its process must be returned for cleanup")
	}
	if url != "https://host.ts.net/desktop" {
		t.Fatalf("url = %q", url)
	}
	if indexOfArgv(calls(), "funnel --https=8443 --set-path /desktop off") < 0 {
		t.Fatalf("failed funnel mount not removed, argv = %q", calls())
	}
}

// TestFailureHint pins the composition rules directly: a serve enable link or
// serve output leads, and the funnel hint is only ever appended to it.
func TestFailureHint(t *testing.T) {
	tests := []struct {
		name                           string
		serveHint, serveOutput, funnel string
		want                           string
	}{
		{"serve link and funnel link", "https://serve.link", "ignored", "https://funnel.link", "https://serve.link\nfunnel: https://funnel.link"},
		{"serve output and funnel link", "", "boom", "https://funnel.link", "boom\nfunnel: https://funnel.link"},
		{"serve output only", "", "  boom \n", "", "boom"},
		{"nothing from serve, funnel link", "", "", "https://funnel.link", "funnel: https://funnel.link"},
		{"nothing at all", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failureHint(tt.serveHint, tt.serveOutput, tt.funnel); got != tt.want {
				t.Fatalf("failureHint = %q, want %q", got, tt.want)
			}
		})
	}
}
