package desktop

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/server"
)

// Routes the SPA uses to read and rotate the durable share token. Both are
// mounted with server.HandleAuthedDesktopRoute, so they require a valid
// credential — the desktop's own launch token in practice, since the SPA
// always authenticates with it.
const (
	ShareTokenPath      = "/api/desktop/share-token"
	ShareTokenResetPath = "/api/desktop/share-token/reset"
)

// ShareTokenStore owns the ON-DISK half of the desktop's durable share token.
//
// Why it exists: the Share dialog hands other devices a URL whose credential
// is the server auth token. The launch token is minted per launch, so every
// restart invalidated every link ever shared. The port the link points at is
// already sticky (desktop-port) for exactly this reason; this makes the
// credential sticky too, so a link survives a restart and can be revoked on
// demand instead.
//
// The token is a SECOND credential (server.SetShareToken), never the webview's
// per-launch token, so rotating it invalidates outstanding links without
// logging the local window out or forcing a reload.
//
// srv supplies generation and the live in-memory value; this type owns only
// the file. Keeping the two apart means there is exactly one token generator
// in the codebase and exactly one writer to disk.
type ShareTokenStore struct {
	path   string
	rotate func() (string, error)
	live   func() string
	set    func(string)
}

// NewShareTokenStore returns a store whose file sits next to desktop-port in
// the ocode config dir (~/.config/opencode on unix, %APPDATA%\opencode on
// Windows), reusing portFilePath so the location has a single source of truth.
func NewShareTokenStore(srv *server.Server) (*ShareTokenStore, error) {
	p, err := portFilePath()
	if err != nil {
		return nil, err
	}
	return &ShareTokenStore{
		path:   filepath.Join(filepath.Dir(p), "desktop-share-token"),
		rotate: srv.RotateShareToken,
		live:   srv.ShareToken,
		set:    srv.SetShareToken,
	}, nil
}

// Path is the file backing the store. Exported for diagnostics and tests.
func (s *ShareTokenStore) Path() string { return s.path }

// LoadOrCreate returns the persisted share token and guarantees the running
// server accepts it. A missing, unreadable, or malformed file yields a freshly
// generated token written before it is used: every failure mode is
// recoverable, whereas a token that could not be saved would break again on
// the next launch anyway.
func (s *ShareTokenStore) LoadOrCreate() (string, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		token := strings.TrimSpace(string(data))
		if validShareToken(token) {
			s.set(token)
			return token, nil
		}
		log.Printf("desktop: ignoring malformed share token file %s (%q); generating a new one", s.path, token)
	case !os.IsNotExist(err):
		log.Printf("desktop: read share token file %s: %v", s.path, err)
	}

	token, err := s.rotate()
	if err != nil {
		return "", err
	}
	if err := s.write(token); err != nil {
		return "", err
	}
	return token, nil
}

// Reset generates a fresh token, persists it, and installs it on the server.
// Returns the new token. Every URL carrying the old one stops working the
// moment this returns.
func (s *ShareTokenStore) Reset() (string, error) {
	token, err := s.rotate()
	if err != nil {
		return "", err
	}
	if err := s.write(token); err != nil {
		return "", err
	}
	return token, nil
}

// write persists the token owner-only. The write is the durable half of a
// rotation, so it happens BEFORE the caller returns the token to anyone.
func (s *ShareTokenStore) write(token string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("desktop: create config dir for share token: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("desktop: save share token file %s: %w", s.path, err)
	}
	return nil
}

// validShareToken reports whether tok is a well-formed token: hex, and exactly
// the expected length. Validating on read means a truncated write or a stray
// hand-edit regenerates instead of producing a token nobody can authenticate
// with (and, worse, one that silently replaces a working link).
func validShareToken(tok string) bool {
	if len(tok) != server.ShareTokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(tok)
	return err == nil
}

// TokenHandler serves GET ShareTokenPath. Auth is applied at mount time by
// server.HandleAuthedDesktopRoute; this handler only marshals JSON.
func (s *ShareTokenStore) TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The LIVE value, not the file: the SPA must never be handed a token
		// the running server would reject.
		writeShareTokenJSON(w, s.live())
	})
}

// ResetHandler serves POST ShareTokenResetPath, rotating the token on disk and
// on the server. Auth is applied at mount time; this handler owns the side
// effect.
func (s *ShareTokenStore) ResetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := s.Reset()
		if err != nil {
			// Logged and surfaced as a 500. A failed reset leaves the previous
			// token live and persisted, so the SPA keeps showing the working
			// link rather than a URL that no longer authenticates.
			log.Printf("desktop: reset share token: %v", err)
			http.Error(w, "reset share token", http.StatusInternalServerError)
			return
		}
		writeShareTokenJSON(w, token)
	})
}

func writeShareTokenJSON(w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"token": token}); err != nil {
		// The status line is already committed at this point; all that is left
		// is to record the truncated response instead of pretending it worked.
		log.Printf("desktop: write share token response: %v", err)
	}
}
