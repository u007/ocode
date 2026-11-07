package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/artifacts"
	"github.com/u007/ocode/internal/paths"
	"golang.org/x/crypto/argon2"
)

// artifactTokenTTLDefault is only used when callers pass a zero expiry; the
// store itself does not impose a default (issue honours the supplied value).
const artifactTokenMaxLen = 128

// artifactIDKey/accessLevelKey/tokenIDKey are unexported context keys.
type artifactIDKey struct{}
type accessLevelKey struct{}
type tokenIDKey struct{}

func WithArtifactID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, artifactIDKey{}, id)
	ctx = context.WithValue(ctx, "artifactID", id)
	return ctx
}

func WithAccessLevel(ctx context.Context, level int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, accessLevelKey{}, level)
	ctx = context.WithValue(ctx, "accessLevel", level)
	return ctx
}

func WithTokenID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, tokenIDKey{}, id)
	ctx = context.WithValue(ctx, "tokenID", id)
	return ctx
}

func ArtifactIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(artifactIDKey{}).(string); ok {
		return v
	}
	if v, ok := ctx.Value("artifactID").(string); ok {
		return v
	}
	return ""
}

func AccessLevelFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(accessLevelKey{}).(int); ok {
		return v
	}
	if v, ok := ctx.Value("accessLevel").(int); ok {
		return v
	}
	// handle float64 from some test harnesses
	if v, ok := ctx.Value("accessLevel").(float64); ok {
		return int(v)
	}
	return 0
}

func TokenIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(tokenIDKey{}).(string); ok {
		return v
	}
	if v, ok := ctx.Value("tokenID").(string); ok {
		return v
	}
	return ""
}

// artifactArgon2Concurrency bounds how many Argon2id verifications the visitor
// path runs at once. Each one allocates 64 MiB, so an unbounded burst of
// password attempts is a memory and CPU amplifier.
const artifactArgon2Concurrency = 4

type artifactTokenStore struct {
	store *artifacts.Store
	mu    sync.Mutex
	// pwSem is a non-blocking semaphore over Argon2 verifications. A nil
	// channel (zero-value store) never acquires, so it fails closed.
	pwSem chan struct{}
}

func newArtifactTokenStore(dir string) (*artifactTokenStore, error) {
	s, err := artifacts.NewStore(dir)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(); err != nil {
		s.Close()
		return nil, err
	}
	// ensure prune of expired at startup is deferred; not blocking
	return &artifactTokenStore{store: s, pwSem: make(chan struct{}, artifactArgon2Concurrency)}, nil
}

// checkPassword verifies a password for token at under the Argon2 semaphore.
// The attempt is counted against key BEFORE the hash runs, and a success
// clears that key. busy reports that every verification slot was taken: the
// caller answers 429 and the attempt is not counted, since the server, not the
// visitor, was the limit.
func (st *artifactTokenStore) checkPassword(rl *rateLimiter, key string, at artifacts.AccessToken, password string) (ok bool, busy bool) {
	select {
	case st.pwSem <- struct{}{}:
	default:
		return false, true
	}
	defer func() { <-st.pwSem }()
	if rl != nil {
		rl.recordFailure(key)
	}
	ok = verifyPassword(password, at.PasswordHash)
	if ok && rl != nil {
		rl.reset(key)
	}
	return ok, false
}

// passwordCookieName is the per-artifact cookie that remembers a passed
// password gate, so subresources (JS/CSS) load without ?password=.
func passwordCookieName(artifactID string) string {
	return "artifact_password_" + artifactID
}

// passwordCookieValue derives the cookie value from the token's stored
// Argon2 hash: HMAC-SHA256(key=PasswordHash, msg=artifactID|tokenID), prefixed
// by the token id. The raw password is never stored client-side, and the value
// stops verifying as soon as the password (and so the hash) changes.
func passwordCookieValue(artifactID string, at artifacts.AccessToken) string {
	mac := hmac.New(sha256.New, []byte(at.PasswordHash))
	mac.Write([]byte(artifactID + "|" + at.ID))
	return at.ID + "." + hex.EncodeToString(mac.Sum(nil))
}

// passwordCookieOK reports whether the request carries a valid password-gate
// cookie for this token. It is cheap (no Argon2), so it is checked first.
func passwordCookieOK(r *http.Request, artifactID string, at artifacts.AccessToken) bool {
	if at.PasswordHash == "" || at.ID == "" {
		return false
	}
	c, err := r.Cookie(passwordCookieName(artifactID))
	if err != nil || len(c.Value) > 2*artifactTokenMaxLen {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(passwordCookieValue(artifactID, at)))
}

// inviteAccessOK reports whether the request carries the access cookie that
// an accepted invite sets, and that cookie names an accepted allow-list entry
// for this artifact. The cookie holds the raw invite token; only its hash is
// stored, so the check is a hash lookup and survives restarts.
func (st *artifactTokenStore) inviteAccessOK(r *http.Request, artifactID string) bool {
	if st == nil || st.store == nil {
		return false
	}
	c, err := r.Cookie("artifact_access_" + artifactID)
	if err != nil || c.Value == "" || len(c.Value) > artifactTokenMaxLen {
		return false
	}
	_, err = st.store.GetAcceptedAllowEntryByInviteHash(artifactID, sha256Sum(c.Value))
	return err == nil
}

// artifactClientKey is the rate-limit and access-log key for a visitor. Behind
// `tailscale serve`/`funnel` every request arrives from loopback, so the real
// client is taken from X-Forwarded-For, right-most entry, which is the one the
// local proxy appended or overwrote. A non-loopback peer is its own address and
// its forwarding headers are ignored, so a remote caller cannot pick its key.
func artifactClientKey(r *http.Request) string {
	host := realIP(r)
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	hops := strings.Split(xff, ",")
	last := strings.TrimSpace(hops[len(hops)-1])
	if net.ParseIP(last) == nil {
		return host
	}
	return last
}

func artifactStoreDir() string {
	if d, err := paths.GlobalDataDir(); err == nil && d != "" {
		return filepath.Join(d, "artifacts")
	}
	return "artifacts"
}

func newArtifactTokenValue() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("artifacts: rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// hashPassword returns an Argon2id encoded hash (salt + hash).
func hashPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h)), nil
}

func verifyPassword(password, encoded string) bool {
	if encoded == "" {
		// no password gate
		return true
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (st *artifactTokenStore) issue(artifactID, kind string, expiresAt time.Time, password string) (string, string, error) {
	if st.store == nil {
		return "", "", errors.New("artifact token store not initialized")
	}
	// ensure artifact row exists for FK
	tok, err := newArtifactTokenValue()
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(tok))
	tokenHash := hex.EncodeToString(sum[:])
	pwHash := ""
	if password != "" {
		pwHash, err = hashPassword(password)
		if err != nil {
			return "", "", err
		}
	}
	at, err := st.store.MintToken(artifacts.AccessToken{
		ArtifactID:   artifactID,
		TokenHash:    tokenHash,
		Kind:         kind,
		ExpiresAt:    expiresAt,
		PasswordHash: pwHash,
	})
	if err != nil {
		return "", "", err
	}
	return tok, at.ID, nil
}

func (st *artifactTokenStore) validate(token, artifactID string) (artifacts.AccessToken, int, error) {
	if token == "" || len(token) > artifactTokenMaxLen {
		return artifacts.AccessToken{}, 0, errors.New("invalid token")
	}
	if st.store == nil {
		return artifacts.AccessToken{}, 0, errors.New("artifact token store not initialized")
	}
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])
	at, err := st.store.GetTokenByHash(tokenHash)
	if err != nil {
		return artifacts.AccessToken{}, 0, fmt.Errorf("invalid token: %w", err)
	}
	// constant-time compare
	if subtle.ConstantTimeCompare([]byte(at.TokenHash), []byte(tokenHash)) != 1 {
		return artifacts.AccessToken{}, 0, errors.New("invalid token")
	}
	if at.ArtifactID != artifactID {
		return artifacts.AccessToken{}, 0, errors.New("artifact mismatch")
	}
	if !at.RevokedAt.IsZero() {
		return artifacts.AccessToken{}, 0, errors.New("token revoked")
	}
	if !at.ExpiresAt.IsZero() && time.Now().After(at.ExpiresAt) {
		return artifacts.AccessToken{}, 0, errors.New("token expired")
	}
	level := 1
	if strings.HasPrefix(at.Kind, "public") {
		level = 2
	}
	return at, level, nil
}

func (st *artifactTokenStore) revoke(id string) error {
	if st.store == nil {
		return errors.New("artifact token store not initialized")
	}
	return st.store.RevokeToken(id)
}

func (st *artifactTokenStore) pruneExpired() {
	if st.store == nil {
		return
	}
	_ = st.store.PruneExpiredTokens()
}

// password gate helper exposed for middleware
func (st *artifactTokenStore) verifyTokenPassword(at artifacts.AccessToken, password string) bool {
	return verifyPassword(password, at.PasswordHash)
}

func artifactAuthMiddlewareImpl(store *artifactTokenStore, rl *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Blocked-state is checked before any token or Argon2 work.
		ip := artifactClientKey(r)
		if rl != nil && rl.isBlocked(ip) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) < 1 || parts[0] != "p" {
			http.NotFound(w, r)
			return
		}
		if len(parts) < 2 || parts[1] == "" {
			http.NotFound(w, r)
			return
		}
		artifactID := parts[1]

		if len(parts) >= 3 && (parts[2] == "invite" || parts[2] == "gate") {
			ctx := WithArtifactID(r.Context(), artifactID)
			ctx = WithAccessLevel(ctx, 0)
			next(w, r.WithContext(ctx))
			return
		}

		if len(parts) < 3 || parts[2] == "" {
			http.NotFound(w, r)
			return
		}
		token := parts[2]

		if len(token) > artifactTokenMaxLen {
			http.NotFound(w, r)
			return
		}

		at, level, err := store.validate(token, artifactID)
		if err != nil {
			if rl != nil {
				rl.recordFailure(ip)
			}
			http.NotFound(w, r)
			return
		}

		// Password gate. The derived cookie is checked first (no Argon2). A
		// presented ?password= / header is verified under the semaphore, and
		// the attempt is counted inside checkPassword before the hash runs.
		if at.PasswordHash != "" && !passwordCookieOK(r, artifactID, at) {
			pw := r.URL.Query().Get("password")
			if pw == "" {
				pw = r.Header.Get("X-Artifact-Password")
			}
			if pw == "" {
				if rl != nil {
					rl.recordFailure(ip)
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="artifact"`)
				http.NotFound(w, r)
				return
			}
			ok, busy := store.checkPassword(rl, ip, at, pw)
			if busy {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "too many password checks in progress", http.StatusTooManyRequests)
				return
			}
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="artifact"`)
				http.NotFound(w, r)
				return
			}
		}

		// Email allow-list gate, fail closed. An artifact with any allow-list
		// entry needs a server-verified invite cookie (an accepted invite for
		// THIS artifact). A self-asserted email, with no verification, never
		// passes, and a lookup error denies.
		hasList, err := store.store.HasAllowlist(artifactID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if hasList && !store.inviteAccessOK(r, artifactID) {
			if rl != nil {
				rl.recordFailure(ip)
			}
			http.NotFound(w, r)
			return
		}

		if rl != nil {
			rl.reset(ip)
		}
		ctx := WithArtifactID(r.Context(), artifactID)
		ctx = WithAccessLevel(ctx, level)
		ctx = WithTokenID(ctx, at.ID)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) artifactAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	store := (*artifactTokenStore)(nil)
	rl := (*rateLimiter)(nil)
	if s != nil {
		if s.handler != nil {
			store = s.handler.artifactTokens
		}
		rl = s.artifactRL
	}
	return artifactAuthMiddlewareImpl(store, rl, next)
}

func (h *Handler) artifactAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	store := (*artifactTokenStore)(nil)
	rl := (*rateLimiter)(nil)
	if h != nil {
		store = h.artifactTokens
		rl = h.artifactRL
	}
	return artifactAuthMiddlewareImpl(store, rl, next)
}
