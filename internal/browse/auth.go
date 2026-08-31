package browse

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const browseCookie = "ocode_browse"

type grantEntry struct {
	stateKey string
	expires  time.Time
}

// browseSession is the server-authoritative per-session state. stateKey pins
// the panel; localDoc records whether the session's LAST DOCUMENT was served
// from local mode. It is the ONLY signal used to authorize local-mode
// requests — never the Referer header (a suppressed, missing, or malicious
// Referer must not widen the surface). localDoc is set true only when a fresh
// SPA grant redeems against a local target, and is flipped back to false the
// moment an external document is served. While it is false, every local-mode
// request (documents AND subresources) is refused, so an external page cannot
// probe the local network through the panel's session.
type browseSession struct {
	stateKey string
	localDoc bool
}

// authStore holds one-time grants and live browse sessions. Grants are
// minted by the main server (after it has authenticated the API token) and
// redeemed exactly once for a scoped HttpOnly cookie confined to /b/.
type authStore struct {
	mu       sync.Mutex
	grants   map[string]grantEntry
	sessions map[string]browseSession // cookie value -> session state
	// origins records, per stateKey, the SPA origin that minted the grant.
	// The capture script posts telemetry with this exact targetOrigin, so it
	// must be the origin the SPA page was loaded from (Origin header of the
	// grant request) — not the server's bound listener address.
	origins map[string]string
}

func newAuthStore() *authStore {
	return &authStore{grants: map[string]grantEntry{}, sessions: map[string]browseSession{}, origins: map[string]string{}}
}

func randToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is unrecoverable and must not be swallowed.
		panic("browse: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (a *authStore) mint(stateKey, spaOrigin string) string {
	tok := randToken()
	a.mu.Lock()
	a.grants[tok] = grantEntry{stateKey: stateKey, expires: time.Now().Add(60 * time.Second)}
	if spaOrigin != "" {
		a.origins[stateKey] = spaOrigin
	}
	a.mu.Unlock()
	return tok
}

func (a *authStore) originFor(stateKey string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	o, ok := a.origins[stateKey]
	return o, ok
}

// redeem consumes a grant (one-time) and returns a new session cookie value.
// localDoc is whether the grant's target was a local/private upstream (the
// SPA initiated this navigation, so local mode is authorized for the session).
func (a *authStore) redeem(grant string, localDoc bool) (cookie, stateKey string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, present := a.grants[grant]
	if !present || time.Now().After(e.expires) {
		delete(a.grants, grant)
		return "", "", false
	}
	delete(a.grants, grant)
	cookie = randToken()
	a.sessions[cookie] = browseSession{stateKey: e.stateKey, localDoc: localDoc}
	return cookie, e.stateKey, true
}

func (a *authStore) sessionStateKey(cookie string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[cookie]
	return s.stateKey, ok
}

// isValidSession reports whether the cookie belongs to a live session for
// stateKey. Used by non-/b/ endpoints (the TLS __bypass POST) that must
// authorize without minting a redirect.
func (a *authStore) isValidSession(cookie, stateKey string) bool {
	sk, ok := a.sessionStateKey(cookie)
	return ok && sk == stateKey
}

// sessionLocalDoc reports whether the session is currently in local mode.
// The session is only ever marked local by a fresh local-target grant.
func (a *authStore) sessionLocalDoc(cookie string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[cookie]
	return ok && s.localDoc
}

// setLocalDoc transitions the session's mode. Called on every document load:
// an external document sets local=false (the session left local mode); a
// local document keeps it true. Unknown/expired cookies are a no-op.
func (a *authStore) setLocalDoc(cookie string, v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[cookie]
	if !ok {
		return
	}
	s.localDoc = v
	a.sessions[cookie] = s
}

func (a *authStore) revoke(stateKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c, sk := range a.sessions {
		if sk.stateKey == stateKey {
			delete(a.sessions, c)
		}
	}
	delete(a.origins, stateKey)
}

// authenticate resolves the request to a stateKey, redeeming a ?__grant= on
// first navigation. Returns (stateKey, redirectURL, ok). When redirectURL is
// non-empty the caller must 302 there after setting the returned cookie.
// A grant whose target is a local upstream marks the new session local
// (localDoc=true) — the sole way local mode is entered.
func (a *authStore) authenticate(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if g := r.URL.Query().Get("__grant"); g != "" {
		local := false
		if t, err := parseTarget(r.URL.Path, r.URL.RawQuery); err == nil {
			local = t.Local
		}
		cookieVal, stateKey, ok := a.redeem(g, local)
		if !ok {
			return "", "", false
		}
		http.SetCookie(w, &http.Cookie{
			Name: browseCookie, Value: cookieVal, Path: "/b/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		clean := *r.URL
		q := clean.Query()
		q.Del("__grant")
		clean.RawQuery = q.Encode()
		return stateKey, clean.String(), true
	}
	c, err := r.Cookie(browseCookie)
	if err != nil {
		return "", "", false // no cookie, no grant: reject.
	}
	sk, ok := a.sessionStateKey(c.Value)
	return sk, "", ok
}
