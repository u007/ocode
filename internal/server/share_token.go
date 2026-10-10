package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
)

// ShareTokenBytes is the size of the desktop share token. It matches the
// launch token's 128 bits of entropy (hex-encoded to 32 characters).
const ShareTokenBytes = 16

// SetShareToken installs an optional SECOND credential that checkAuth accepts
// alongside the launch token (password). It exists so the desktop shell can
// hand out a link whose credential survives a restart without reusing — and
// therefore exposing the privilege of — the webview's per-launch token.
//
// Setting an empty token disables the second credential (the state every
// non-desktop server is in). Safe to call after Serve: checkAuth reads it per
// request under shareTokenMu.
func (s *Server) SetShareToken(token string) {
	s.shareTokenMu.Lock()
	s.shareToken = token
	s.shareTokenMu.Unlock()
}

// ShareToken returns the currently installed share token, or "" when none is
// configured.
func (s *Server) ShareToken() string {
	s.shareTokenMu.RLock()
	defer s.shareTokenMu.RUnlock()
	return s.shareToken
}

// RotateShareToken generates a fresh 128-bit share token, installs it, and
// returns it. The previous value stops authenticating immediately.
//
// Persistence is deliberately NOT handled here: the server has no notion of
// a config directory. The desktop shell owns that file (see
// internal/desktop.ShareTokenStore) and must write the returned token before
// responding, so a crash cannot leave memory and disk disagreeing about which
// link is live.
func (s *Server) RotateShareToken() (string, error) {
	raw := make([]byte, ShareTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("server: generate share token: %w", err)
	}
	token := hex.EncodeToString(raw)
	s.SetShareToken(token)
	return token, nil
}

// tokenMatches reports whether tok is one of the credentials this server
// accepts: the launch token (password), or the optional share token when one
// is installed.
//
// The launch-token comparison is deliberately the plain equality it has
// always been, so the primary credential path keeps byte-for-byte its
// previous behaviour. The share-token branch is skipped when unset: with
// both sides empty, an absent token would otherwise authenticate.
func (s *Server) tokenMatches(tok string) bool {
	if tok == s.password {
		return true
	}
	if share := s.ShareToken(); share != "" && tok == share {
		return true
	}
	return false
}

// HandleAuthedDesktopRoute mounts a desktop-shell-owned route on the API mux
// BEHIND the same authMiddleware every /api route uses.
//
// Distinct from HandleDesktopRoute, which is deliberately unauthenticated for
// the one-time storage-migration call. Use this for any desktop-owned route
// that reads or changes a credential: an unauthenticated mount would let an
// off-host caller (the listener binds 0.0.0.0 for share URLs) read the share
// token or revoke the links that carry it.
func (s *Server) HandleAuthedDesktopRoute(pattern string, h http.Handler) {
	s.mux.Handle(pattern, s.authMiddleware(h.ServeHTTP))
}
