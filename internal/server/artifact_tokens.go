package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
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

type artifactTokenStore struct {
	store *artifacts.Store
	mu    sync.Mutex
}

func newArtifactTokenStore(dir string) *artifactTokenStore {
	s, err := artifacts.NewStore(dir)
	if err != nil {
		return &artifactTokenStore{}
	}
	if err := s.Migrate(); err != nil {
		// keep store; methods may still work
	}
	// ensure prune of expired at startup is deferred; not blocking
	return &artifactTokenStore{store: s}
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
	if _, err := st.store.GetArtifact(artifactID); err != nil {
		_, _ = st.store.CreateArtifact(artifacts.Artifact{ID: artifactID, Status: "active"})
	}
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
		ip := realIP(r)
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

		if at.PasswordHash != "" {
			pw := r.URL.Query().Get("password")
			if pw == "" {
				pw = r.Header.Get("X-Artifact-Password")
			}
			if pw == "" {
				if c, err := r.Cookie("artifact_password_" + artifactID); err == nil {
					pw = c.Value
				}
			}
			if !store.verifyTokenPassword(at, pw) {
				if rl != nil {
					rl.recordFailure(ip)
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="artifact"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		if email := artifactVisitorEmail(r); email != "" && store.store != nil {
			if _, err := store.store.GetAllowEntry(artifactID, email); err != nil {
				if rl != nil {
					rl.recordFailure(ip)
				}
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
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

func artifactVisitorEmail(r *http.Request) string {
	if r == nil {
		return ""
	}
	if c, err := r.Cookie("artifact_email"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("email")
}
