package tailscale

import (
	"os"
	"path/filepath"
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
	saved := knownCandidates
	knownCandidates = nil
	t.Cleanup(func() { knownCandidates = saved })
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