package server

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/tailscale"
)

// desktopTailscalePath is the stable --set-path mount for the headless/
// desktop server share. A fixed path (instead of the server root) keeps
// cleanup scoped: Shutdown removes only this mount instead of resetting the
// global serve/funnel config that TUI /rc sessions share on the same node.
// The public URL becomes https://<tailnet-host>/desktop/. The SPA base path
// logic (client.ts _basePath and the <base> script in web/index.html) treats
// any non-/session/<id> path as the mount prefix, so /desktop/ resolves
// assets and API calls under the mount instead of the tailnet root.
const desktopTailscalePath = "desktop"

// tailscaleShare caches one tailscale exposure per server process. The first
// GET /api/tailscale-url starts serve/funnel; later opens reuse the cached
// URL instead of spawning a process per dialog open (which would overwrite
// routes and leak background processes).
type tailscaleShare struct {
	mu      sync.Mutex
	url     string
	hint    string
	started bool
	proc    *exec.Cmd
	path    string

	// Test seams. Both are nil in production and are only ever set by tests in
	// this package, so the exposure cannot be faked from outside it. They exist
	// because the real entry points shell out to the tailscale binary, which a
	// unit test must never do: `tailscale serve --bg --set-path` mutates the
	// developer's live node-wide serve config.
	//
	// exposeFn is called with the exposure kind ("serve" or "full") instead of
	// running tailscale; removeFn replaces RemoveSetPath for cleanup assertions.
	exposeFn func(kind string)
	removeFn func(pathPrefix string)
}

// expose runs the tailscale exposure for the given kind and returns the URL,
// the background process for cleanup, and any one-time setup hint.
//
// The kind is threaded through so tests can count exposures without spawning a
// real binary; production always takes the live branch.
func (t *tailscaleShare) expose(kind, target string) (string, *exec.Cmd, string) {
	if t.exposeFn != nil {
		t.exposeFn(kind)
		return "https://host.ts.net/" + strings.TrimPrefix(desktopTailscalePath, "/"), nil, ""
	}
	if kind == "serve" {
		return tailscale.StartServeExpose(target, desktopTailscalePath)
	}
	return tailscale.StartExpose(target, desktopTailscalePath)
}

// removeSetPath removes this instance's mount, or reports the removal to the
// test seam when one is installed.
func (t *tailscaleShare) removeSetPath(pathPrefix string) {
	if t.removeFn != nil {
		t.removeFn(pathPrefix)
		return
	}
	tailscale.RemoveSetPath(pathPrefix)
}

// ensureServe starts a TAILNET-ONLY exposure for auto-share, sharing the one
// cache slot with ensure.
//
// Sharing the slot is load-bearing, not an optimisation: `tailscale serve
// --bg --set-path /desktop` is a single global mount per node, so a second
// exposure started later (by the Share dialog) would silently OVERWRITE this
// one's target. Because `started` makes the first caller win, auto-share at boot
// means the dialog later reuses this tailnet-only URL instead of replacing it.
//
// The trade-off is deliberate: once auto-share has warmed the cache, the
// Share dialog reports the tailnet URL rather than trying funnel. That is the
// safer of the two, and it is only reachable when the user has already opted
// into sharing at boot.
func (t *tailscaleShare) ensureServe(port int) (url, hint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return t.url, t.hint
	}
	t.started = true
	u, proc, hint := t.expose("serve", fmt.Sprintf("localhost:%d", port))
	t.url = u
	t.hint = hint
	t.proc = proc
	if u != "" {
		t.path = tailscale.SanitizePath(desktopTailscalePath)
	}
	return t.url, t.hint
}

// StartAutoShare warms the tailscale share exposure at boot for auto-share,
// using a tailnet-only `serve` mount (never funnel). It returns the share URL
// (empty when tailscale is unavailable or serve is not enabled) and a one-time
// setup hint.
//
// Best-effort by contract: auto-share must never prevent the server from
// starting, so an unavailable tailscale is reported, not raised. Callers should
// log the outcome — including the hint, which is the only place a user learns
// their tailnet has serve disabled.
//
// Safe to call concurrently with handleGetTailscaleURL: both funnel through the
// same mutex, and whichever runs first wins the single exposure slot.
func (s *Server) StartAutoShare() (url, hint string) {
	if s.tsShare == nil {
		return "", ""
	}
	port := serverPort(s.Addr())
	if port == 0 {
		return "", ""
	}
	return s.tsShare.ensureServe(port)
}

// ensure starts the exposure once for the given port and returns the cached
// result on later calls.
func (t *tailscaleShare) ensure(port int) (url, hint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return t.url, t.hint
	}
	t.started = true
	u, proc, hint := t.expose("full", fmt.Sprintf("localhost:%d", port))
	t.url = u
	t.hint = hint
	t.proc = proc
	if u != "" {
		t.path = tailscale.SanitizePath(desktopTailscalePath)
	}
	return t.url, t.hint
}

// cleanup kills the background process and removes only our --set-path mount.
// It never calls a global reset, which would tear down TUI /rc sessions.
func (t *tailscaleShare) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.proc != nil && t.proc.Process != nil {
		_ = t.proc.Process.Kill()
		t.proc = nil
	}
	if t.path != "" {
		t.removeSetPath(t.path)
		t.path = ""
	}
	t.url = ""
	t.hint = ""
	t.started = false
}

// serverPort parses the bound port from the server address.
func serverPort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return p
}

// handleGetTailscaleURL reports the cached (or newly started) tailscale share
// URL for this server. Always 200: availability is in the body so the dialog
// can fall back to the LAN URL instead of treating "no tailnet" as an error.
func (s *Server) handleGetTailscaleURL(w http.ResponseWriter, r *http.Request) {
	if s.tsShare == nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": "", "available": false})
		return
	}
	port := serverPort(s.Addr())
	if port == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"url": "", "available": false})
		return
	}
	u, hint := s.tsShare.ensure(port)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       u,
		"available": u != "",
		"hint":      hint,
	})
}

// peek returns the cached exposure WITHOUT starting one. This is what the
// config handlers read so that opening Settings can never publish the instance:
// only the boot hook (StartAutoShare) and the Share dialog (ensure) may start an
// exposure, and both do so deliberately.
func (t *tailscaleShare) peek() (url, hint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url, t.hint
}
