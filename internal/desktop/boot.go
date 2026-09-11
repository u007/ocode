// Package desktop provides the server boot helper and run-state watcher for
// the ocode desktop shell. It is pure Go and MUST NOT import Wails, keeping
// unit tests cgo-free and the boundary clean.
package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/server"
)

// Handle is the result of a successful server boot.
type Handle struct {
	URL   string // e.g. "http://127.0.0.1:52341" (no trailing slash)
	Token string // hex-encoded 16-byte random token (32 hex chars)
	Srv   *server.Server
}

// StartServer boots an ocode HTTP/SSE API server with a fresh auth token,
// and returns the handle the desktop shell needs to open its webview window.
// The server runs in a background goroutine; on desktop quit the shell calls
// handle.Srv.Shutdown(ctx) (bounded by a TTL) to drain agent sessions and
// gracefully terminate any running terminal ptys before the process exits.
// See desktopShutdownTimeout in cmd/ocode-desktop/main.go.
//
// The listener binds all interfaces (0.0.0.0), not just loopback, so the
// Share dialog's advertised URLs actually connect: the tailscale serve
// target and — when tailscale is unavailable — the LAN URL
// (http://<lan-ip>:<port>) must be reachable from other devices. Exposure is
// token-gated (128-bit random token per launch, auth required on every API
// route, rate-limited), the same posture as the TUI's /rc server which also
// binds the LAN IP. The webview itself still opens http://127.0.0.1:PORT,
// preserving the localStorage origin the sticky port exists for.
//
// The port is sticky across launches: the webview's localStorage (terminal
// tabs, editor tabs, session tabs) is scoped to the http://127.0.0.1:PORT
// origin, so a random port every launch would silently discard all persisted
// UI state. The first launch binds a random port and saves it; later launches
// reuse it, falling back to a fresh random port (and re-saving) only if the
// saved one is taken.
//
// webFS is the embedded SPA (web.FS()). workDir is the project root the
// server resolves relative paths from. workspace is optional: when non-nil,
// /api/* requests are proxied to that remote SSH workspace's server
// instead of being handled locally. In remote mode the listener binds
// 127.0.0.1:0 (not 0.0.0.0) to avoid LAN exposure, and no browse
// panel is started.
func StartServer(webFS fs.FS, workDir string, workspace *remote.RemoteWorkspace) (*Handle, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("desktop: generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	if workspace != nil {
		return startRemoteServer(webFS, workspace, token)
	}

	bindAddr := "0.0.0.0:0"
	if p := loadSavedPort(); p > 0 {
		bindAddr = fmt.Sprintf("0.0.0.0:%d", p)
	}

	srv := server.New(bindAddr, "ocode", token, webFS)
	srv.SetWorkDir(workDir)

	ln, err := srv.Listen()
	if err != nil && bindAddr != "0.0.0.0:0" {
		// Saved port unavailable (another process grabbed it, or a second
		// desktop instance) — persisted UI state won't be visible this run.
		log.Printf("desktop: saved port %s unavailable, falling back to a random port: %v", bindAddr, err)
		srv = server.New("0.0.0.0:0", "ocode", token, webFS)
		srv.SetWorkDir(workDir)
		ln, err = srv.Listen()
	}
	if err != nil {
		return nil, fmt.Errorf("desktop: listen: %w", err)
	}

	// The webview always opens the loopback origin (stable localStorage
	// scope); the listener itself is on 0.0.0.0 so LAN/tailscale share URLs
	// connect. Read the actual bound port from the listener.
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("desktop: parse bound address %s: %w", ln.Addr().String(), err)
	}
	addr := ln.Addr().String()
	url := fmt.Sprintf("http://127.0.0.1:%s", portStr)
	saveBoundPort(addr)
	saveDebugHandle(url, token)

	// Browse origin: a second loopback listener, isolated from the SPA
	// origin, backing the embedded browser panel. Failing to bind it means
	// the panel cannot work at all, so boot fails loudly rather than
	// silently serving a half-functional UI. Chrome-mode options come from
	// the ocode config (chrome_path, idle_timeout_minutes); a load failure
	// keeps defaults rather than blocking boot.
	browseOpts := server.LoadBrowseOptions(srv.ProcessSupervisor())

	if err := server.StartBrowse(srv, token, url, browseOpts); err != nil {
		return nil, fmt.Errorf("desktop: %w", err)
	}

	go func() {
		log.Printf("desktop: serving on %s", url)
		if err := srv.Serve(ln); err != nil {
			log.Printf("desktop: serve error: %v", err)
		}
	}()

	return &Handle{
		URL:   url,
		Token: token,
		Srv:   srv,
	}, nil
}

// portFilePath is the file the desktop shell remembers its listen port in.
// It lives next to the global ocode config (~/.config/opencode on unix,
// %APPDATA%\opencode on Windows).
func portFilePath() (string, error) {
	if customDir := os.Getenv("OPENCODE_CONFIG_DIR"); customDir != "" {
		return filepath.Join(customDir, "desktop-port"), nil
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("desktop: APPDATA is not set")
		}
		return filepath.Join(appData, "opencode", "desktop-port"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "desktop-port"), nil
}

// loadSavedPort returns the port used by a previous desktop launch, or 0 when
// none is saved (first launch) or the file is unreadable/invalid.
func loadSavedPort() int {
	path, err := portFilePath()
	if err != nil {
		log.Printf("desktop: resolve port file path: %v", err)
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("desktop: read saved port file %s: %v", path, err)
		}
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 || port > 65535 {
		log.Printf("desktop: ignoring invalid saved port %q in %s", strings.TrimSpace(string(data)), path)
		return 0
	}
	return port
}

// saveBoundPort persists the port of the bound listen address for the next
// launch. Failure is non-fatal (the app still runs, resume just won't
// survive the next relaunch) but always logged.
func saveBoundPort(addr string) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("desktop: parse bound address %q to save port: %v", addr, err)
		return
	}
	path, err := portFilePath()
	if err != nil {
		log.Printf("desktop: resolve port file path: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("desktop: create config dir for port file: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(portStr+"\n"), 0o600); err != nil {
		log.Printf("desktop: save port file %s: %v", path, err)
	}
}

// saveDebugHandle writes the current launch's URL and auth token to a local,
// owner-only file so a stuck/high-memory desktop session can be diagnosed
// (curl /debug/pprof/heap, /debug/pprof/goroutine, /api/debug/runtime)
// without needing the process's stdout, which a Finder-launched .app loses.
// Overwritten on every boot — stale content just means the app isn't
// running. Not read back by the app itself (unlike desktop-port); failure is
// non-fatal and logged. Debug-only; do not treat as a stable API.
func saveDebugHandle(url, token string) {
	path, err := portFilePath()
	if err != nil {
		return
	}
	path = filepath.Join(filepath.Dir(path), "desktop-debug-handle")
	content := fmt.Sprintf("url=%s\ntoken=%s\n", url, token)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		log.Printf("desktop: save debug handle file %s: %v", path, err)
	}
}

// ResolveWindowTitle returns a user-friendly window title for the desktop app.
// It prefers the saved project name (if the workDir matches a known project),
// otherwise falls back to the directory basename. Returns "ocode" as the last
// resort for empty, root, home, or temp paths.
func ResolveWindowTitle(workDir string) string {
	// Reject broad/sensitive roots first — even if they appear in the project
	// store, they make poor window titles.
	clean := filepath.Clean(workDir)
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return "ocode"
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "ocode"
	}
	if clean == os.TempDir() {
		return "ocode"
	}
	// Try to find the project by path in the saved projects store.
	// Compare normalized paths so trailing separators or equivalent forms match.
	if store, _, err := projects.NewStore(); err == nil && store != nil {
		for _, p := range store.List() {
			if filepath.Clean(p.Path) == clean && p.Name != "" {
				return p.Name
			}
		}
	}
	// Fall back to directory basename.
	return filepath.Base(clean)
}

// IsUnsafeDesktopRoot reports whether dir is too broad/sensitive to use as the
// desktop server's default workDir. The Finder/Dock-launched .app starts with
// cwd "/" and the old fallback used the home directory — both are huge trees
// that trigger macOS TCC prompts (Documents, Desktop, Downloads, etc.) when the
// file tree or LSP walker scans them, and the bash change-recorder already
// skips them via unsafeWalkRoot.
func IsUnsafeDesktopRoot(dir string) bool {
	clean := filepath.Clean(dir)
	if clean == string(filepath.Separator) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return true
	}
	return false
}

// ResolveFallbackWorkDir picks a safe workDir when the desktop is launched
// without a meaningful cwd (Finder/Dock launch with cwd "/" or a bare home
// directory that is not a project). It prefers the most-recently-used saved
// project that still exists on disk, so a returning user lands on their last
// project instead of an empty view. On a fresh install (no saved projects) it
// falls back to the ocode global data dir (~/.local/share/ocode), a small safe
// directory that never triggers TCC and is cheap to walk. TempDir is the last
// resort.
func ResolveFallbackWorkDir() string {
	// Try the most-recently-used saved project that still exists.
	if store, _, err := projects.NewStore(); err == nil && store != nil {
		list := store.List()
		var best *projects.Project
		for i := range list {
			if list[i].Path == "" {
				continue
			}
			if _, statErr := os.Stat(list[i].Path); statErr != nil {
				continue
			}
			if best == nil || list[i].LastUsedAt.After(best.LastUsedAt) {
				best = &list[i]
			}
		}
		if best != nil {
			log.Printf("desktop: Finder launch — using last project %q as workDir (cwd was unsafe)", best.Path)
			return best.Path
		}
	} else if err != nil {
		log.Printf("desktop: load project store for fallback workDir: %v", err)
	}
	if dir, err := paths.OcodeGlobalDataDir(); err == nil && dir != "" {
		log.Printf("desktop: Finder launch — no saved project, using global data dir %q as safe workDir", dir)
		return dir
	}
	tmp := os.TempDir()
	log.Printf("desktop: Finder launch — using temp dir %q as safe workDir", tmp)
	return tmp
}

// startRemoteServer boots a minimal server for remote SSH workspaces:
// serves the embedded SPA at /, proxies /api/* to the remote server
// through the SSH tunnel, binds 127.0.0.1:0 (never LAN), and skips
// the browse panel. No agent/LSP/git infrastructure runs locally —
// all execution is on the remote server.
func startRemoteServer(webFS fs.FS, workspace *remote.RemoteWorkspace, localToken string) (*Handle, error) {
	proxy, err := NewRemoteProxy(workspace, localToken)
	if err != nil {
		return nil, fmt.Errorf("create proxy: %w", err)
	}
	bindAddr := "127.0.0.1:0"
	if p := loadSavedPort(); p > 0 {
		bindAddr = fmt.Sprintf("127.0.0.1:%d", p)
	}

	mux := http.NewServeMux()
	// Proxy all API traffic to the remote server
	mux.Handle("/api/", proxy)
	// Serve SPA (fallback to index.html for client-side routing)
	mux.Handle("/", remoteSPAHandler(webFS))

	srv := &http.Server{Addr: bindAddr, Handler: mux}

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("desktop: remote listen: %w", err)
	}

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("desktop: parse bound address %s: %w", ln.Addr().String(), err)
	}
	addr := ln.Addr().String()
	url := fmt.Sprintf("http://127.0.0.1:%s", portStr)
	saveBoundPort(addr)
	saveDebugHandle(url, localToken)

	go func() {
		log.Printf("desktop: serving remote workspace on %s (proxy → remote)", url)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("desktop: serve error: %v", err)
		}
	}()

	// Srv is nil in remote mode (no local server; the remote
	// server is the authority). main.go guards handle.Srv == nil.
	return &Handle{
		URL:   url,
		Token: localToken,
		Srv:   nil,
	}, nil
}

// remoteSPAHandler serves the embedded React SPA for remote
// workspaces. Mirrors internal/server.spaHandler but lives here
// to avoid a server→desktop dependency.
func remoteSPAHandler(webFS fs.FS) http.Handler {
	if webFS == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := webFS.(fs.ReadFileFS).Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
