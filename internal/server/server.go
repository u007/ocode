package server

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

type Server struct {
	addr     string
	username string
	password string
	mux      *http.ServeMux
	handler  *Handler
}

func New(addr, username, password string) *Server {
	mux := http.NewServeMux()
	h := NewHandler()
	s := &Server{
		addr:     addr,
		username: username,
		password: password,
		mux:      mux,
		handler:  h,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("POST /api/chat", s.authMiddleware(s.handleChat))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.authMiddleware(s.handleSendMessage))
	s.mux.HandleFunc("GET /api/models", s.authMiddleware(s.handleListModels))
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	if s.username == "" && s.password == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.username || pass != s.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="ocode"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChat(w, r)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListSessions(w, r)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleGetSession(w, r, id)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleSendMessage(w, r, id)
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListModels(w, r)
}

func (s *Server) Start() error {
	fmt.Fprintf(os.Stderr, "serving on %s\n", s.addr)
	return http.ListenAndServe(s.addr, s.mux)
}

func Run(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 4096, "Port to listen on")
	host := fs.String("host", "0.0.0.0", "Host to bind to")
	openBrowser := fs.Bool("open", false, "Open browser after starting")
	fs.Parse(args)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	username := os.Getenv("OPENCODE_SERVER_USERNAME")
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")

	srv := New(addr, username, password)

	if *openBrowser {
		go func() {
			openURL(fmt.Sprintf("http://localhost:%d", *port))
		}()
	}

	return srv.Start()
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readBodyJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) WithCORS() *Server {
	wrapped := http.NewServeMux()
	wrapped.HandleFunc("POST /api/chat", corsMiddleware(s.handleChat))
	wrapped.HandleFunc("GET /api/sessions", corsMiddleware(s.handleListSessions))
	wrapped.HandleFunc("GET /api/sessions/{id}", corsMiddleware(s.handleGetSession))
	wrapped.HandleFunc("POST /api/sessions/{id}/message", corsMiddleware(s.handleSendMessage))
	wrapped.HandleFunc("GET /api/models", corsMiddleware(s.handleListModels))
	s.mux = wrapped
	return s
}

type ChatRequest struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId,omitempty"`
	Model     string `json:"model,omitempty"`
}

type ChatResponse struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type SessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ModelInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}
