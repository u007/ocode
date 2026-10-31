package main

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Config is the bridge's runtime configuration.
type Config struct {
	// OcodeURL is the ocode serve base URL, e.g. http://127.0.0.1:4096.
	OcodeURL string
	// OcodeToken is sent as a Bearer token to ocode serve. It never reaches the pane.
	OcodeToken string
	// PaneKey authenticates the task pane (header X-Bridge-Key) to /api, /bridge and /mcp.
	PaneKey string
	// StaticDir serves the built task pane; empty disables static serving.
	StaticDir string
	// AllowedOrigins are browser origins allowed to call /api and /bridge besides same-origin.
	AllowedOrigins []string
	// CallTimeout bounds one tool call.
	CallTimeout time.Duration
	// WorkspaceRoot holds one directory per workbook; ocode sessions for a workbook are rooted there.
	// The pane registers it with ocode as a project (POST /api/projects). Empty means
	// <globalDataDir>/excel/workspaces (datadir.go).
	WorkspaceRoot string
	// DataDir persists the pane's small per-workbook state (undo journal, session index). Empty keeps it in
	// memory, which only tests use: the CLI always passes a directory (default <globalDataDir>/excel/state).
	DataDir string
}

// NewHandler wires the bridge routes.
func NewHandler(cfg Config) (http.Handler, *Hub, error) {
	c, err := loadContract()
	if err != nil {
		return nil, nil, err
	}
	target, err := url.Parse(cfg.OcodeURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, nil, fmt.Errorf("invalid ocode URL %q", cfg.OcodeURL)
	}
	hub := NewHub()
	mcp := NewMCP(hub, c)
	if cfg.CallTimeout > 0 {
		mcp.CallTimeout = cfg.CallTimeout
	}
	st, err := newStateStore(cfg.DataDir)
	if err != nil {
		return nil, nil, err
	}
	s := &srv{cfg: cfg, hub: hub, contract: c, state: st}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			// The pane's credential and browser identity never go upstream; ocode gets its own token.
			pr.Out.Header.Del("X-Bridge-Key")
			pr.Out.Header.Del("Origin")
			pr.Out.Header.Del("Referer")
			pr.Out.Header.Del("Authorization")
			if cfg.OcodeToken != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+cfg.OcodeToken)
			}
		},
		FlushInterval: -1, // SSE: flush every write
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "ocode serve is not reachable: "+err.Error(), http.StatusBadGateway)
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /bridge/health", s.health)
	mux.Handle("GET /bridge/tools", s.auth(http.HandlerFunc(s.listTools)))
	mux.Handle("GET /bridge/stream", s.auth(http.HandlerFunc(s.stream)))
	mux.Handle("POST /bridge/result", s.auth(http.HandlerFunc(s.result)))
	mux.Handle("POST /bridge/workspace/{workbook}", s.auth(http.HandlerFunc(s.workspace)))
	mux.Handle("GET /bridge/state/{workbook}/{key}", s.auth(http.HandlerFunc(s.getState)))
	mux.Handle("PUT /bridge/state/{workbook}/{key}", s.auth(http.HandlerFunc(s.putState)))
	wsRoot := cfg.WorkspaceRoot
	if wsRoot == "" {
		wsRoot, _ = defaultWorkspaceRoot() // on error stays "": chat and project registration are then refused
	}
	mux.Handle("/api/", s.auth(allowAPI(wsRoot, proxy)))
	// MCP is for ocode on the same machine: loopback only, same key.
	mux.Handle("/mcp", s.auth(s.loopbackOnly(mcp)))
	if cfg.StaticDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(cfg.StaticDir)))
	}
	return s.security(mux), hub, nil
}

type srv struct {
	cfg      Config
	hub      *Hub
	contract contract
	state    *stateStore
}

// security rejects cross-site browser requests to the authenticated routes and adds basic headers.
func (s *srv) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if o := r.Header.Get("Origin"); o != "" && !s.originAllowed(r, o) && !strings.HasPrefix(r.URL.Path, "/bridge/health") {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *srv) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	for _, a := range s.cfg.AllowedOrigins {
		if a == origin {
			return true
		}
	}
	return false
}

func (s *srv) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.PaneKey != "" {
			got := r.Header.Get("X-Bridge-Key")
			if got == "" {
				// EventSource cannot set headers; the pane uses fetch streaming, but a query key is
				// accepted on the stream route only, so the key never appears in /api URLs.
				if r.URL.Path == "/bridge/stream" {
					got = r.URL.Query().Get("key")
				}
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.PaneKey)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *srv) loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = strings.Trim(host[:i], "[]")
		}
		if host != "127.0.0.1" && host != "::1" {
			http.Error(w, "MCP endpoint is loopback-only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *srv) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "contract": s.contract.Version, "workbooks": s.hub.Workbooks()})
}

func (s *srv) listTools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(toolsJSON)
}

// stream is the pane's server-sent event stream: "call" and "cancel" events, plus a keep-alive.
func (s *srv) stream(w http.ResponseWriter, r *http.Request) {
	wb := r.URL.Query().Get("workbook")
	if wb == "" {
		http.Error(w, "workbook is required", http.StatusBadRequest)
		return
	}
	// instance identifies one page load of the pane: the same instance reconnecting replaces its
	// own stream silently, a different one evicts it (see Hub.Attach).
	instance := r.URL.Query().Get("instance")
	if instance == "" {
		http.Error(w, "instance is required", http.StatusBadRequest)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	events, done, detach := s.hub.Attach(wb, instance)
	defer detach()
	bw := bufio.NewWriter(w)
	write := func(name string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(bw, "event: %s\ndata: %s\n\n", name, b); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	if write("hello", map[string]any{"workbook": wb, "contract": s.contract.Version}) != nil {
		return
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case reason := <-done: // replaced by a newer pane for the same workbook; empty reason = silent
			if reason != "" {
				_ = write("evicted", map[string]string{"reason": reason})
			}
			return
		case ev := <-events:
			if write(ev.Name, ev.Data) != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(bw, ": ping\n\n"); err != nil || bw.Flush() != nil {
				return
			}
			fl.Flush()
		}
	}
}

// result receives a tool result from the pane.
func (s *srv) result(w http.ResponseWriter, r *http.Request) {
	wb := r.URL.Query().Get("workbook")
	var res Result
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&res); err != nil || res.ID == "" {
		http.Error(w, "bad result", http.StatusBadRequest)
		return
	}
	if !s.hub.Deliver(wb, res) {
		// Late or foreign result: not an error for the pane, but say so.
		w.WriteHeader(http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
