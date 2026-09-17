package server

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// mediaTokenTTL bounds how long a media capability URL stays valid. The SPA
// asks for a fresh one whenever the element fails, so a restart or an expiry
// self-heals without the user doing anything.
const mediaTokenTTL = 6 * time.Hour

// mediaTokenMaxLen rejects absurdly long query values before any lookup, so a
// hostile request can't make the store hash a megabyte-long key.
const mediaTokenMaxLen = 128

// mediaGrant is the exact request triple a token authorizes. The token is a
// capability for ONE media file: it is never the master credential and cannot
// be replayed against another path/root/host.
type mediaGrant struct {
	path        string
	projectRoot string
	host        string
	expiresAt   time.Time
}

// mediaTokenStore holds issued media capabilities in memory.
//
// Why a bespoke capability instead of the existing auth paths: a browser's
// <video>/<audio> element cannot attach an Authorization header, and the
// master `?token=` query form is deliberately forbidden in --remote mode
// because it leaks the *whole* credential to logs and proxies. A media token
// is a much smaller blast radius — single file, media-only, short TTL — which
// is what makes a query-string capability acceptable here.
//
// Tokens are in-process only and die with the server (restart → the SPA
// refreshes on the element's error event). Losing them on restart is intended:
// no on-disk store to leak.
type mediaTokenStore struct {
	mu     sync.Mutex
	grants map[string]mediaGrant
}

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{grants: make(map[string]mediaGrant)}
}

// newMediaTokenValue returns 32 bytes of base64url randomness (256 bits) —
// infeasible to guess, so an invalid token is a bad URL, not a brute-force
// attempt (media-token rejections therefore don't feed the credential rate
// limiter; see mediaAuthMiddleware).
func newMediaTokenValue() (string, bool) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), true
}

// issue stores a grant for the exact triple and returns its token, or "" when
// the system RNG failed (caller answers 500 — never a predictable token).
func (st *mediaTokenStore) issue(path, projectRoot, host string) string {
	tok, ok := newMediaTokenValue()
	if !ok {
		return ""
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(time.Now())
	st.grants[tok] = mediaGrant{
		path:        path,
		projectRoot: projectRoot,
		host:        host,
		expiresAt:   time.Now().Add(mediaTokenTTL),
	}
	return tok
}

// validate reports whether token authorizes exactly this request triple and
// has not expired. An expired entry is dropped (lazy pruning).
func (st *mediaTokenStore) validate(token, path, projectRoot, host string) bool {
	if token == "" || len(token) > mediaTokenMaxLen {
		return false
	}
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	g, ok := st.grants[token]
	if !ok {
		return false
	}
	if now.After(g.expiresAt) {
		delete(st.grants, token)
		return false
	}
	return g.path == path && g.projectRoot == projectRoot && g.host == host
}

// pruneLocked drops expired grants. Called on issue (the only growth point)
// so the map can't accumulate dead entries over a long-lived server.
func (st *mediaTokenStore) pruneLocked(now time.Time) {
	for tok, g := range st.grants {
		if now.After(g.expiresAt) {
			delete(st.grants, tok)
		}
	}
}
