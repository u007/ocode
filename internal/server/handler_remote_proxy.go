package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/u007/ocode/internal/projects"
)

// HandleRemoteProxy proxies requests to a remote ocode server identified by
// the {host} wildcard. Admission is the same trust boundary as
// remoteWorkFor: the host must be the Host of a saved project, and (host,
// path) must be registered on the remote. The remote token never reaches
// the browser — it is injected by the cached reverse proxy.
func (h *Handler) HandleRemoteProxy(w http.ResponseWriter, r *http.Request) {
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return
	}

	host, rest, err := remoteProxyTarget(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve the project path for admission. Read in order:
	// 1. ?project= / ?path= query params (explicit, so they win)
	// 2. X-Ocode-Project header (set by the SPA on path-bearing calls)
	// 3. project_path from a JSON POST body (POST /api/chat)
	// Session-scoped calls carry no path; those are admitted on the host alone
	// and reuse whatever project the host's first connect used.
	projectPath := resolveProjectPath(r)

	// Track whether the ORIGINAL request had a path so we can skip
	// registration when it did not (session-scoped endpoints).
	originalPath := projectPath

	// Resolve the saved entry's RemotePort for the connect. Zero means
	// no explicit port (default SSH port). We resolve it early so the
	// path-less fallback (I1) can also use it.
	var remotePort int
	if entry, ok := h.remoteProjectEntry(host, projectPath); ok {
		remotePort = entry.RemotePort
	}

	// Admission: host must be a saved project host AND (host, projectPath)
	// must be a registered project on the remote. When projectPath is empty
	// (session-scoped endpoints), we skip the pair check but still need a
	// connected entry.
	if projectPath != "" {
		if _, ok := h.remoteProjectEntry(host, projectPath); !ok {
			log.Printf("remote proxy: denied %s /api/%s — (host=%q, path=%q) not a saved project", r.Method, rest, host, projectPath)
			writeError(w, http.StatusForbidden, "unknown host or unregistered project path")
			return
		}
	} else {
		// No path present — we still need the host to exist in the saved
		// projects store. Scan for ANY saved project on this host.
		first, ok := h.firstSavedProjectForHost(host)
		if !ok {
			log.Printf("remote proxy: denied %s /api/%s — host %q has no saved projects", r.Method, rest, host)
			writeError(w, http.StatusForbidden, "unknown host")
			return
		}
		// Use the first saved project's path and port for the connect.
		projectPath = first.Path
		if remotePort == 0 {
			remotePort = first.RemotePort
		}
	}

	// MCP OAuth cannot run through the proxy for an SSH host. The flow opens a
	// system browser where the SERVER runs and waits for a 127.0.0.1:8085
	// callback; proxied over `ssh -L`, the remote server sees RemoteAddr =
	// 127.0.0.1 (the tunnel) so its own loopback gate passes, the browser opens
	// on the WRONG machine, and the client polls for its full timeout before
	// erroring. Refuse here, before connecting, with the same explanation.
	// WSL is exempt: its server shares the Windows machine, so localhost does
	// reach the callback. RemoteKind is "ssh" for SSH and "wsl" for WSL; an
	// empty/unknown kind on a proxied host is treated as SSH.
	if isMCPAuthRestPath(rest) {
		kind := ""
		if entry, ok := h.remoteProjectEntry(host, projectPath); ok {
			kind = entry.RemoteKind
		}
		if kind != "wsl" {
			writeError(w, http.StatusForbidden,
				"MCP OAuth must run where the browser can reach the callback (127.0.0.1:8085). "+
					"This session runs on a remote SSH host, so run /mcp-auth in the TUI there "+
					"(or open the ocode desktop app on that machine).")
			return
		}
	}

	// Get or create the workspace + cached proxy. Pass the saved
	// project's RemotePort (0 = default) so non-default SSH ports work.
	ws, err := h.remoteHosts.workspaceForPort(host, projectPath, remotePort)
	if err != nil {
		log.Printf("remote proxy: connect host %s failed: %v", host, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("remote connect failed: %v", err),
			"stage": "remote-connect",
		})
		return
	}

	// Ensure the project is registered on the remote (first use only).
	// Skip when the ORIGINAL request had no path — session-scoped
	// endpoints do not carry a project to register.
	if originalPath != "" {
		if err := ensureRemoteProject(r.Context(), ws, h.remoteHosts, host, originalPath); err != nil {
			log.Printf("remote proxy: register project %s on host %s failed: %v", originalPath, host, err)
			// The cached workspace/proxy just proved unusable (e.g. the remote
			// server died or the tunnel dropped). Drop the host so the next
			// request reconnects instead of reusing the dead entry forever —
			// the same self-heal the proxy ErrorHandler performs on a
			// round-trip failure, extended to the pre-proxy registration call
			// that runs first.
			h.remoteHosts.drop(host)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("remote project registration failed: %v", err),
				"stage": "remote-register",
			})
			return
		}
	}

	// Get the cached proxy.
	proxy, ok := h.remoteHosts.proxyFor(host)
	if !ok || proxy == nil {
		log.Printf("remote proxy: no cached proxy for host %q (rest=%s)", host, rest)
		writeError(w, http.StatusBadGateway, "no cached proxy for host")
		return
	}

	// Rewrite the request path for the remote: /api/remote/{host}/api/{rest} → /api/{rest}
	r.URL.Path = "/api/" + rest

	// Forward the originating window's active profile so the remote builds its
	// chat agent on the same credentials/config. The remote has no
	// window-state.json (see applyProxiedActiveProfile), so without this a
	// remote turn falls back to the base credentials.
	h.injectProxiedActiveProfile(r)

	// Serve via the cached proxy. The Director strips the local ?token= and
	// injects the remote bearer token.
	proxy.ServeHTTP(w, r)
}

// proxiedRemoteWindowID returns the window id a proxied request carries, using
// the same precedence the chat handlers use (header, then query). The web
// client always sends X-Window-Id on chat/send requests (web/src/api/client.ts),
// so the header is the common case; the query fallbacks cover other callers.
func proxiedRemoteWindowID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Window-Id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("windowId")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("window_id"))
}

// injectProxiedActiveProfile stamps the originating desktop window's effective
// profile onto the request. Any client-supplied profile headers are stripped
// first so a browser cannot forge a profile (the remote only trusts the
// authoritative marker this function sets). The empty/Default case uses an
// explicit reset header instead of an empty X-Ocode-Active-Profile value so no
// transport layer can silently drop the "back to base" signal.
func (h *Handler) injectProxiedActiveProfile(r *http.Request) {
	r.Header.Del(remoteActiveProfileHeader)
	r.Header.Del(remoteProfileAuthoritativeHeader)
	r.Header.Del(remoteProfileResetHeader)

	windowID := proxiedRemoteWindowID(r)
	if windowID == "" {
		return
	}
	if profile := h.getEffectiveWindowProfile(windowID); profile != "" {
		r.Header.Set(remoteActiveProfileHeader, profile)
		r.Header.Set(remoteProfileAuthoritativeHeader, "1")
		return
	}
	r.Header.Set(remoteProfileAuthoritativeHeader, "1")
	r.Header.Set(remoteProfileResetHeader, "1")
}

// remoteProxyTarget extracts the host and the API sub-path from the request
// URL. The route is /api/remote/{host}/api/{rest...} so after the prefix
// we get the host wildcard and the remainder. The forwarded path is
// /api/ + rest with the original query string.
func remoteProxyTarget(r *http.Request) (host, rest string, err error) {
	host = r.PathValue("host")
	if host == "" {
		return "", "", fmt.Errorf("missing host in URL path")
	}
	rest = r.PathValue("rest")
	if rest == "" {
		rest = ""
	}
	return host, rest, nil
}

// resolveProjectPath determines which remote project path a request belongs
// to. Returns "" if no path is present (session-scoped endpoints).
func resolveProjectPath(r *http.Request) string {
	// 1. ?project= / ?path= query params
	if p := r.URL.Query().Get("project"); p != "" {
		return p
	}
	if p := r.URL.Query().Get("path"); p != "" {
		return p
	}

	// 2. X-Ocode-Project header
	if h := r.Header.Get("X-Ocode-Project"); h != "" {
		return h
	}

	// 2b. ?project_path= query param. The terminal endpoints (WS + history +
	// list) carry the project as project_path, and a WebSocket handshake cannot
	// set the X-Ocode-Project header, so without this the proxy would admit the
	// socket on host alone and never register the project on the remote — the
	// remote's own project-root check would then 403 the shell.
	if p := r.URL.Query().Get("project_path"); p != "" {
		return p
	}

	// 3. project_path from a JSON POST body (for POST /api/chat).
	// Read the body and replace r.Body so the proxy still forwards it.
	if r.Method == http.MethodPost && r.Body != nil {
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "application/json") {
			bodyBytes, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				log.Printf("remote proxy: read body for project_path extraction: %v", err)
				return ""
			}
			var body struct {
				ProjectPath string `json:"project_path"`
			}
			if umErr := json.Unmarshal(bodyBytes, &body); umErr != nil {
				log.Printf("remote proxy: unmarshal body for project_path: %v", umErr)
			}
			if body.ProjectPath != "" {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return body.ProjectPath
			}
			// Replace body even if project_path was not found, so
			// the proxy still forwards the original content.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
	}

	return ""
}

// remoteRegisterClient is a dedicated HTTP client for ensureRemoteProject
// with a 10-second timeout. It avoids using http.DefaultClient which has
// no timeout and could hang indefinitely.
var remoteRegisterClient = &http.Client{Timeout: 10 * time.Second}

// ensureRemoteProject POSTs the project path to the remote /api/projects
// endpoint when isRegistered reports the pair is not yet registered. On
// success (200 or 409) the pair is marked registered. An already-registered
// response (409) is treated as success.
func ensureRemoteProject(ctx context.Context, ws remoteHostWorkspace, reg *remoteHostRegistry, host, path string) error {
	// Read-only check first — do NOT mark before the POST succeeds.
	if reg.isRegistered(host, path) {
		return nil
	}

	// Build a one-shot HTTP client targeting the remote's /api/projects.
	apiURL, err := url.Parse(ws.APIURL())
	if err != nil {
		return fmt.Errorf("parse API URL: %w", err)
	}
	projectsURL := apiURL.ResolveReference(&url.URL{Path: "/api/projects"})

	payload, marshalErr := json.Marshal(map[string]string{"path": path})
	if marshalErr != nil {
		log.Printf("remote proxy: marshal project path for host %s: %v", host, marshalErr)
		return fmt.Errorf("marshal project path: %w", marshalErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, projectsURL.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := ws.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := remoteRegisterClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /api/projects to %s: %w", host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		// 200 = registered, 409 = already registered — both are success.
		// Mark the pair so subsequent requests skip the POST.
		reg.markRegistered(host, path)
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("POST /api/projects returned %d: %s", resp.StatusCode, string(body))
}

// firstSavedProjectForHost returns the first saved project entry for host,
// or false if none exists. Used when a path-less request needs a connect
// target (I1 fix: avoid cd ” on a cold host).
func (h *Handler) firstSavedProjectForHost(host string) (projects.Project, bool) {
	if h.projects == nil || host == "" {
		return projects.Project{}, false
	}
	for _, p := range h.projects.List() {
		if p.Host == host {
			return p, true
		}
	}
	return projects.Project{}, false
}

// isMCPAuthRestPath reports whether a proxied sub-path ("api/" is already
// stripped) is one of the MCP OAuth routes that must not run on a remote SSH
// host: POST /api/mcp/{name}/auth and GET /api/mcp/auth/{id}. Mutating MCP
// routes (enable/disable) and the listing are not matched.
func isMCPAuthRestPath(rest string) bool {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] != "mcp" {
		return false
	}
	// GET /api/mcp/auth/{id}
	if parts[1] == "auth" {
		return true
	}
	// POST /api/mcp/{name}/auth
	return len(parts) >= 3 && parts[len(parts)-1] == "auth"
}
