package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

// artifactTailscalePath is the single global mount for ALL published artifacts
// in Mode A (self-hosted). One mount, dispatched by artifact id inside the
// server's /p/{id}/... routes — never N mounts (one per artifact).
const artifactTailscalePath = "p"

// tailscaleShareStatus is the wire shape shared by the status endpoint and the
// auto-share config response. `running` means an exposure is PROVEN live (a URL
// came back from funnel or serve, not a bare DNS-name guess); `kind` says which
// one, because "public on the internet" and "tailnet-only" must never be
// conflated in the UI. An unproven URL is deliberately not reported as running
// and its url is omitted — a dead link presented as a share is worse than an
// honest "not sharing".
type tailscaleShareStatus struct {
	Running   bool   `json:"running"`
	Available bool   `json:"available"`
	URL       string `json:"url,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Hint      string `json:"hint,omitempty"`
	BrowseURL string `json:"browse_url,omitempty"`
}

// tailscaleShare caches one tailscale exposure per server process. Starting is
// an explicit action (the Share dialog's Start button, or the auto-share boot
// hook); a status read never starts one, so opening Settings or polling cannot
// publish the instance by accident.
type tailscaleShare struct {
	// opMu serializes start/stop. It exists because the mount is a single
	// GLOBAL mount per node: two concurrent starts would both run
	// `tailscale ... --set-path /desktop` and the second would silently
	// overwrite the first's target. It is deliberately separate from mu so a
	// status read (which takes only mu) never blocks behind a slow exposure.
	opMu sync.Mutex

	// mount is the raw --set-path prefix this instance exposes. Empty means
	// desktopTailscalePath (the historical desktop share). The artifact
	// exposure instantiates its own tailscaleShare with mount="p" so it gets
	// a separate, single global /p/* mount that never clobbers /desktop.
	mount string

	// upstreamPath is appended to the local target (localhost:<port>) that
	// tailscale proxies to. It is set ONLY on the artifact instances, to
	// "/p" (see newArtifactTailscaleShare). Without it, `--set-path /p` strips
	// the mount prefix and forwards to the server ROOT, so a public request for
	// /p/api/... reaches the owner's authenticated API. With it the upstream is
	// localhost:<port>/p, so only /p/* artifact routes are reachable. Empty for
	// the desktop share, whose target is unchanged.
	upstreamPath string

	mu      sync.Mutex
	url     string
	hint    string
	kind    string // "funnel" (public) | "serve" (tailnet-only); "" = not exposed
	started bool
	proc    *exec.Cmd
	path    string

	// Browse proxy exposure (second origin). browseProxyPort is the local port
	// the browse proxy listener binds; browseURL is the shared HTTPS URL on
	// the second Tailscale HTTPS port (8444 by default). browseProc/browsePath
	// track the second mount for cleanup.
	browseProxyPort int
	browseURL       string
	browseProc      *exec.Cmd
	browsePath      string

	// tabPorts is the set of tab-origin listener ports exposed on the tailnet by
	// exposeTab. stopLocked removes their mounts. Guarded by mu.
	tabPorts map[int]bool

	// Test seams. Both are nil in production and are only ever set by tests in
	// this package, so the exposure cannot be faked from outside it. They exist
	// because the real entry points shell out to the tailscale binary, which a
	// unit test must never do: `tailscale serve --bg --set-path` mutates the
	// developer's live node-wide serve config.
	//
	// exposeFn is called with the requested exposure kind ("serve" or "full")
	// and returns the URL plus the PROVEN kind ("funnel"/"serve", or "" for an
	// unproven DNS-name fallback). removeFn replaces RemoveSetPath.
	exposeFn func(kind string) (url, resolvedKind string)
	removeFn func(pathPrefix string)
	// exposeTargetFn observes the final upstream target expose hands to
	// tailscale (after upstreamTarget). Nil in production; tests use it to pin
	// the target string for each instance without a CLI.
	exposeTargetFn func(target string)

	// Browse-proxy test seams (nil in production). browseExposeFn returns the
	// shared browse URL (or "" on failure); browseRemoveFn tears the mount down.
	browseExposeFn func(target string) (url string)
	browseRemoveFn func()

	// Tab-origin test seams (nil in production). tabExposeFn reports whether
	// `tailscale serve` exposed target on the HTTPS port; tabRemoveFn tears that
	// port's mount down.
	tabExposeFn func(port int, target string) bool
	tabRemoveFn func(port int)
}

// setBrowseProxyPort records the local port the browse proxy listener binds,
// so the share flow can expose it on the second Tailscale HTTPS port.
func (t *tailscaleShare) setBrowseProxyPort(port int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.browseProxyPort = port
}

// newArtifactTailscaleShare builds an artifact exposure instance. Every artifact
// instance MUST be built here: it is the only place upstreamPath is set. A bare
// &tailscaleShare{mount: artifactTailscalePath} would proxy the public /p mount
// to the server root (see upstreamPath).
func newArtifactTailscaleShare() *tailscaleShare {
	return &tailscaleShare{mount: artifactTailscalePath, upstreamPath: "/" + artifactTailscalePath}
}

// upstreamTarget returns the local target tailscale proxies to. For the
// artifact instances this appends upstreamPath, so `--set-path /p` keeps the
// /p prefix upstream (localhost:<port>/p). Instances without an upstreamPath
// get target back unchanged.
func (t *tailscaleShare) upstreamTarget(target string) string {
	if t.upstreamPath == "" {
		return target
	}
	return strings.TrimRight(target, "/") + t.upstreamPath
}

// expose runs the tailscale exposure for the given kind and returns the URL,
// the background process for cleanup, any one-time setup hint, and the PROVEN
// exposure kind. kind "serve" is tailnet-only; "full" tries funnel (public)
// first and falls back to serve. The resolved kind is "" when neither command
// proved an exposure (a bare DNSName guess), which the caller treats as "not
// running".
func (t *tailscaleShare) expose(kind, target string) (string, *exec.Cmd, string, string) {
	target = t.upstreamTarget(target)
	if t.exposeTargetFn != nil {
		t.exposeTargetFn(target)
	}
	if t.exposeFn != nil {
		u, resolved := t.exposeFn(kind)
		return u, nil, "", resolved
	}
	prefix := t.mountPath()
	if kind == "serve" {
		u, proc, hint := tailscale.StartServeExpose(target, prefix)
		if u == "" {
			return "", proc, hint, ""
		}
		// StartServeExpose only ever runs `serve`, so a URL proves tailnet-only.
		return u, proc, hint, "serve"
	}
	return tailscale.StartExposeWithKind(target, prefix)
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

// mountPath returns the raw --set-path prefix this instance exposes.
func (t *tailscaleShare) mountPath() string {
	if t.mount != "" {
		return t.mount
	}
	return desktopTailscalePath
}

// statusLocked builds the status from cached state. Caller must hold t.mu.
func (t *tailscaleShare) statusLocked() tailscaleShareStatus {
	running := t.started && t.url != "" && t.kind != ""
	st := tailscaleShareStatus{
		Running:   running,
		Available: t.url != "" || tailscale.Installed(),
		Hint:      t.hint,
		BrowseURL: t.browseURL,
	}
	if running {
		st.URL = t.url
		st.Kind = t.kind
	}
	return st
}

// status reports the current exposure without starting or stopping anything.
// Only mu is taken, so it stays responsive while a start/stop is in flight.
//
// The state is cached rather than probed: querying `tailscale serve status` on
// every read would spawn a subprocess per poll. The consequence is that an
// exposure reset OUTSIDE ocode (e.g. `tailscale serve reset` by hand) still
// reads as running until this process restarts or the user hits Stop/Start.
func (t *tailscaleShare) status() tailscaleShareStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusLocked()
}

// start makes one exposure for port if none is live, and reuses the cached
// state on later calls. The FIRST caller wins, which is what keeps the single
// global mount coherent: auto-share at boot warms it tailnet-only, and a later
// manual Start reuses it rather than racing for the same mount.
//
// The one exception is an UPGRADE: an explicit "full" request replaces a warm
// tailnet-only mount with a public funnel. Without it, auto-share warming serve
// at boot would permanently pin the share to the tailnet and the Share dialog
// could never deliver the public URL its Start button advertises.
//
// opMu is held across the (slow, up to ~2x exposeTimeout) exposure on purpose:
// it serializes start against stop, so a Stop can never be resurrected by an
// in-flight start and two starts can never race for the one global mount. A
// Stop issued while a start is running therefore waits for that start to
// finish and then tears it down — bounded and correct, and status reads (which
// take only mu) stay responsive throughout.
func (t *tailscaleShare) start(kind string, port int) tailscaleShareStatus {
	t.opMu.Lock()
	defer t.opMu.Unlock()

	t.mu.Lock()
	upgrade := t.started && kind == "full" && t.kind == "serve"
	if t.started && !upgrade {
		st := t.statusLocked()
		t.mu.Unlock()
		return st
	}
	t.mu.Unlock()

	// An UPGRADE deliberately does NOT tear the warm mount down first. Funnel
	// mounts on FunnelHTTPSPort while serve uses 443, so the two listeners do not
	// collide, and expose("full") falls back to serve on the same --set-path (an
	// idempotent re-mount). Tearing down first meant a failed funnel attempt
	// destroyed a working share; now the previous exposure survives unless the
	// new one is PROVEN.
	u, proc, hint, resolved := t.expose(kind, fmt.Sprintf("localhost:%d", port))
	proven := u != "" && resolved != ""

	t.mu.Lock()
	if upgrade && !proven {
		// Keep the working mount and its cached state; only surface the hint.
		t.hint = hint
		st := t.statusLocked()
		t.mu.Unlock()
		killProc(proc)
		return st
	}
	old := t.proc
	t.url = u
	t.hint = hint
	t.kind = resolved
	t.proc = proc
	t.started = proven
	if proven {
		t.path = tailscale.SanitizePath(t.mountPath())
		// Expose the browse proxy on the second HTTPS port (best-effort).
		// A failure here does not fail the main share.
		if t.browseProxyPort != 0 {
			browseURL := t.browseExpose(t.browseProxyPort)
			if browseURL != "" {
				t.browseURL = browseURL
			}
		}
	}
	st := t.statusLocked()
	t.mu.Unlock()
	// Reap the superseded process outside mu: it is never left running once
	// replaced, whether or not the new exposure was proven.
	if old != nil && old != proc {
		killProc(old)
	}
	return st
}

// killProc kills a background tailscale process. Nil-safe.
func killProc(c *exec.Cmd) {
	if c != nil && c.Process != nil {
		_ = c.Process.Kill()
	}
}

// ensureServe starts a TAILNET-ONLY exposure for auto-share, sharing the one
// cache slot with ensure.
//
// Sharing the slot is load-bearing, not an optimisation: `tailscale serve
// --bg --set-path /desktop` is a single global mount per node, so a second
// exposure started later (by the Share dialog) would silently OVERWRITE this
// one's target. Because the first caller wins, auto-share at boot means the
// dialog's later Start reuses this mount — or UPGRADES it to a public funnel,
// which start() handles.
//
// A warm serve mount is never downgraded: ensureServe reuses a public funnel
// rather than replacing it with a tailnet-only mount, so an explicit share is
// not silently withdrawn by a later auto-share call.
func (t *tailscaleShare) ensureServe(port int) (url, hint string) {
	st := t.start("serve", port)
	return st.URL, st.Hint
}

// ensure starts a FUNNEL-FIRST exposure (public if the tailnet allows it) for
// the explicit manual share action, and returns the cached result on later
// calls. It also upgrades a warm TAILNET-ONLY mount to funnel: the explicit
// Share action must be able to deliver the public URL it advertises even when
// auto-share already warmed the slot with `serve` at boot.
func (t *tailscaleShare) ensure(port int) (url, hint string) {
	st := t.start("full", port)
	return st.URL, st.Hint
}

// stop tears down the live exposure: it kills the background process and
// removes this instance's --set-path mount, which is what actually revokes
// reachability. It returns true when something was stopped.
//
// It never touches the persisted auto_share_on_start setting: stopping the
// RUNNING share must not silently change what happens on the next launch. The
// UI is responsible for telling the user that an enabled auto-share will start
// it again.
func (t *tailscaleShare) stop() bool {
	t.opMu.Lock()
	defer t.opMu.Unlock()
	return t.stopLocked()
}

// stopLocked requires opMu. The state is detached under mu and the slow work
// (process kill, the two `tailscale ... off` subprocesses) runs AFTER mu is
// released, so a status read never blocks behind a hung CLI. opMu still
// serializes it against start.
func (t *tailscaleShare) stopLocked() bool {
	t.mu.Lock()
	had := t.started || t.proc != nil || t.path != ""
	proc, path := t.proc, t.path
	tabPorts := t.tabPorts
	t.proc = nil
	t.path = ""
	t.url = ""
	t.hint = ""
	t.kind = ""
	t.started = false
	t.tabPorts = nil
	t.mu.Unlock()

	killProc(proc)
	if path != "" {
		t.removeSetPath(path)
	}
	// Tear down the browse proxy mount.
	t.mu.Lock()
	browseURL := t.browseURL
	t.browseURL = ""
	t.mu.Unlock()
	if browseURL != "" {
		t.browseRemove()
	}
	// Tear down every tab-origin mount, in port order.
	ports := make([]int, 0, len(tabPorts))
	for p := range tabPorts {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	for _, p := range ports {
		t.removeTabPort(p)
	}
	return had
}

// cleanup kills the background process and removes only our --set-path mount.
// It never calls a global reset, which would tear down TUI /rc sessions.
func (t *tailscaleShare) cleanup() {
	t.stop()
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
// Safe to call concurrently with the Share dialog's Start: both funnel through
// the same op mutex, and whichever runs first wins the single exposure slot.
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

// tailscaleShareStatus returns the current exposure state for this server.
func (s *Server) tailscaleShareStatus() tailscaleShareStatus {
	if s.tsShare == nil {
		return tailscaleShareStatus{Available: tailscale.Installed()}
	}
	return s.tsShare.status()
}

// handleGetTailscaleShare reports the share status WITHOUT changing it.
// Starting is a POST: a GET that publishes the instance on open is exactly the
// surprise the opt-in default exists to prevent, and browsers/health checks
// issue GETs speculatively. Always 200 — availability is in the body so the
// dialog can fall back to the LAN URL instead of treating "no tailnet" as an
// error.
func (s *Server) handleGetTailscaleShare(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tailscaleShareStatus())
}

// handleStartTailscaleShare starts the manual (funnel-first / public-when-
// allowed) exposure and returns the resulting status. Idempotent: a second
// call reuses the live exposure.
func (s *Server) handleStartTailscaleShare(w http.ResponseWriter, r *http.Request) {
	if s.tsShare == nil {
		writeJSON(w, http.StatusOK, tailscaleShareStatus{Available: tailscale.Installed()})
		return
	}
	port := serverPort(s.Addr())
	if port == 0 {
		writeError(w, http.StatusServiceUnavailable, "server port unavailable")
		return
	}
	s.tsShare.ensure(port)
	writeJSON(w, http.StatusOK, s.tsShare.status())
}

// handleStopTailscaleShare tears down the live exposure and returns the
// resulting status. Stopping when nothing is running is a no-op that reports
// "not running" (idempotent).
func (s *Server) handleStopTailscaleShare(w http.ResponseWriter, r *http.Request) {
	if s.tsShare != nil {
		s.tsShare.stop()
	}
	writeJSON(w, http.StatusOK, s.tailscaleShareStatus())
}

// browseHTTPSProxyPort is the Tailscale HTTPS port for the browse-proxy mount.
const browseHTTPSProxyPort = 8444

// serveHTTPSPort exposes target on the tailnet at httpsPort with
// `tailscale serve --bg --https=<httpsPort> <target>`. It returns the first URL
// tailscale prints, with any trailing "/" trimmed (callers join paths onto it),
// or "" when the CLI is absent or printed none. Best-effort: a failure is logged.
func serveHTTPSPort(httpsPort int, target string) string {
	tailscalePath := tailscale.CLIPath()
	if tailscalePath == "" {
		return ""
	}
	args := []string{"serve", "--bg", fmt.Sprintf("--https=%d", httpsPort), target}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tailscalePath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		log.Printf("tailscale serve --https=%d failed to start: %v", httpsPort, err)
		return ""
	}
	if err := cmd.Wait(); err != nil {
		// Best-effort: the mount may still have been created.
		log.Printf("tailscale serve --https=%d wait: %v (output: %s)", httpsPort, err, out.String())
	}
	output := out.String()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return strings.TrimRight(line, "/")
		}
	}
	return ""
}

// removeHTTPSPort tears down the tailscale serve mount on httpsPort. A failed
// `off` is logged rather than returned: its callers are teardown paths with
// nowhere to report it.
func removeHTTPSPort(httpsPort int) {
	tailscalePath := tailscale.CLIPath()
	if tailscalePath == "" {
		return
	}
	cmd := exec.Command(tailscalePath, "serve", fmt.Sprintf("--https=%d", httpsPort), "off")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("tailscale serve --https=%d off: %v (output: %s)", httpsPort, err, out)
	}
}

// browseExpose exposes the browse proxy on the second Tailscale HTTPS port.
// It runs `tailscale serve --bg --https=<port> http://localhost:<browseProxyPort>`
// (root mount, no --set-path) and returns the advertised URL.
func (t *tailscaleShare) browseExpose(browseProxyPort int) string {
	if t.browseExposeFn != nil {
		return strings.TrimRight(t.browseExposeFn(fmt.Sprintf("localhost:%d", browseProxyPort)), "/")
	}
	if browseProxyPort == 0 {
		return ""
	}
	return serveHTTPSPort(browseHTTPSProxyPort, fmt.Sprintf("http://localhost:%d", browseProxyPort))
}

// browseRemove tears down the browse proxy mount.
func (t *tailscaleShare) browseRemove() {
	if t.browseRemoveFn != nil {
		t.browseRemoveFn()
		return
	}
	removeHTTPSPort(browseHTTPSProxyPort)
}

// exposeTab makes one tab-origin listener reachable from the tailnet. target is
// the listener's host:port on this machine, and the tailnet port is the same
// number, so no second port is allocated. It runs `tailscale serve` (never
// funnel), so the origin stays tailnet-only, and returns the https origin a
// tailnet client loads.
//
// Each tab origin is its own browser origin, so it needs its own HTTPS port.
// It requires a running share with its browse mount: the origin's host is the
// one that mount advertises. A port already exposed returns its origin without
// exposing it again. opMu serializes it against start and stop.
func (t *tailscaleShare) exposeTab(port int, target string) (string, error) {
	t.opMu.Lock()
	defer t.opMu.Unlock()

	t.mu.Lock()
	browseURL := t.browseURL
	running := t.started && browseURL != ""
	recorded := t.tabPorts[port]
	t.mu.Unlock()
	if !running {
		return "", errors.New("tailscale share is not running")
	}
	u, err := url.Parse(browseURL)
	if err != nil {
		return "", fmt.Errorf("parse browse url %q: %w", browseURL, err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("browse url %q has no host", browseURL)
	}
	origin := "https://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	if recorded {
		return origin, nil
	}
	var exposed bool
	if t.tabExposeFn != nil {
		exposed = t.tabExposeFn(port, target)
	} else {
		exposed = serveHTTPSPort(port, "http://"+target) != ""
	}
	if !exposed {
		return "", fmt.Errorf("tailscale could not expose tab origin port %d", port)
	}
	t.mu.Lock()
	if t.tabPorts == nil {
		t.tabPorts = map[int]bool{}
	}
	t.tabPorts[port] = true
	t.mu.Unlock()
	return origin, nil
}

// removeTab tears down the tailnet mount exposeTab made for port. It is a no-op
// for a port that was not exposed, so a second call does nothing.
func (t *tailscaleShare) removeTab(port int) {
	t.opMu.Lock()
	defer t.opMu.Unlock()

	t.mu.Lock()
	if !t.tabPorts[port] {
		t.mu.Unlock()
		return
	}
	delete(t.tabPorts, port)
	t.mu.Unlock()
	t.removeTabPort(port)
}

// removeTabPort removes one tab-origin mount through the seam or the CLI. Its
// callers hold opMu and have already dropped the port from tabPorts. It never
// takes opMu itself, so stopLocked can call it.
func (t *tailscaleShare) removeTabPort(port int) {
	if t.tabRemoveFn != nil {
		t.tabRemoveFn(port)
		return
	}
	removeHTTPSPort(port)
}

// artifactTailscaleShareStatus reports the Mode A artifact exposure without
// starting one.
func (s *Server) artifactTailscaleShareStatus() tailscaleShareStatus {
	if s.artifactShare == nil {
		return tailscaleShareStatus{Available: tailscale.Installed()}
	}
	return s.artifactShare.status()
}

// handleGetArtifactTailscaleShare reports the artifact exposure state.
func (s *Server) handleGetArtifactTailscaleShare(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.artifactTailscaleShareStatus())
}

// authConfigured reports whether the server requires credentials, i.e. whether
// authMiddleware actually checks them. It is the single definition: authMiddleware
// and the public-exposure gate both call it, so they cannot disagree.
func (s *Server) authConfigured() bool {
	return s.remoteMode || s.username != "" || s.password != ""
}

// handleStartArtifactTailscaleShare starts the (explicit, user-confirmed)
// artifact exposure. Defaults to tailnet-only serve; a funnel (public) upgrade
// happens only on an explicit request, never unattended. A funnel is refused
// with 403 when the server has no authentication: a public mount onto an
// unauthenticated server would publish the whole API to the internet.
func (s *Server) handleStartArtifactTailscaleShare(w http.ResponseWriter, r *http.Request) {
	if s.artifactShare == nil {
		writeJSON(w, http.StatusOK, tailscaleShareStatus{Available: tailscale.Installed()})
		return
	}
	port := serverPort(s.Addr())
	if port == 0 {
		writeError(w, http.StatusServiceUnavailable, "server port unavailable")
		return
	}
	kind := "serve"
	if r.URL.Query().Get("funnel") == "1" || r.URL.Query().Get("public") == "1" {
		kind = "full"
	}
	if kind == "full" && !s.authConfigured() {
		writeError(w, http.StatusForbidden, "public (funnel) artifact sharing is refused: the server has no authentication configured; set a username and password, or share tailnet-only")
		return
	}
	s.artifactShare.start(kind, port)
	writeJSON(w, http.StatusOK, s.artifactShare.status())
}

// handleStopArtifactTailscaleShare tears down the artifact exposure.
func (s *Server) handleStopArtifactTailscaleShare(w http.ResponseWriter, r *http.Request) {
	if s.artifactShare != nil {
		s.artifactShare.stop()
	}
	writeJSON(w, http.StatusOK, s.artifactTailscaleShareStatus())
}
