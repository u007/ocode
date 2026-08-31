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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/server"
)

// Handle is the result of a successful server boot.
type Handle struct {
	URL   string // e.g. "http://127.0.0.1:52341" (no trailing slash)
	Token string // hex-encoded 16-byte random token (32 hex chars)
	Srv   *server.Server
}

// StartServer boots an ocode HTTP/SSE API server on 127.0.0.1 with a fresh
// auth token, and returns the handle the desktop shell needs to open its
// webview window. The server runs in a background goroutine; on desktop quit
// the shell calls handle.Srv.Shutdown(ctx) (bounded by a TTL) to drain agent
// sessions and gracefully terminate any running terminal ptys before the
// process exits. See desktopShutdownTimeout in cmd/ocode-desktop/main.go.
//
// The port is sticky across launches: the webview's localStorage (terminal
// tabs, editor tabs, session tabs) is scoped to the http://127.0.0.1:PORT
// origin, so a random port every launch would silently discard all persisted
// UI state. The first launch binds a random port and saves it; later launches
// reuse it, falling back to a fresh random port (and re-saving) only if the
// saved one is taken.
//
// webFS is the embedded SPA (web.FS()). workDir is the project root the
// server resolves relative paths from.
func StartServer(webFS fs.FS, workDir string) (*Handle, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("desktop: generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	bindAddr := "127.0.0.1:0"
	if p := loadSavedPort(); p > 0 {
		bindAddr = fmt.Sprintf("127.0.0.1:%d", p)
	}

	srv := server.New(bindAddr, "ocode", token, webFS)
	srv.SetWorkDir(workDir)

	ln, err := srv.Listen()
	if err != nil && bindAddr != "127.0.0.1:0" {
		// Saved port unavailable (another process grabbed it, or a second
		// desktop instance) — persisted UI state won't be visible this run.
		log.Printf("desktop: saved port %s unavailable, falling back to a random port: %v", bindAddr, err)
		srv = server.New("127.0.0.1:0", "ocode", token, webFS)
		srv.SetWorkDir(workDir)
		ln, err = srv.Listen()
	}
	if err != nil {
		return nil, fmt.Errorf("desktop: listen: %w", err)
	}

	// Read the actual bound address (Listen writes the *requested* address
	// back to s.addr, so we must use ln.Addr()).
	addr := ln.Addr().String()
	url := fmt.Sprintf("http://%s", addr)
	saveBoundPort(addr)
	saveDebugHandle(url, token)

	// Browse origin: a second loopback listener, isolated from the SPA
	// origin, backing the embedded browser panel. Failing to bind it means
	// the panel cannot work at all, so boot fails loudly rather than
	// silently serving a half-functional UI. Chrome-mode options come from
	// the ocode config (chrome_path, idle_timeout_minutes); a load failure
	// keeps defaults rather than blocking boot.
	browseOpts := &server.BrowseOptions{Supervisor: srv.ProcessSupervisor()}
	if ocfg, err := config.LoadOcodeConfigCopy(); err == nil && ocfg != nil {
		browseOpts.ChromePath = ocfg.Browser.ChromePath
		browseOpts.IdleTimeoutMinutes = ocfg.Browser.IdleTimeoutMinutes
	} else if err != nil {
		log.Printf("desktop: load ocode config for chrome options: %v (using defaults)", err)
	}
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
