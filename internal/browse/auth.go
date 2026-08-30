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

// authStore holds one-time grants and live browse sessions. Grants are
// minted by the main server (after it has authenticated the API token) and
// redeemed exactly once for a scoped HttpOnly cookie confined to /b/.
type authStore struct {
	mu       sync.Mutex
	grants   map[string]grantEntry
	sessions map[string]string // cookie value -> stateKey
}

func newAuthStore() *authStore {
	return &authStore{grants: map[string]grantEntry{}, sessions: map[string]string{}}
}

func randToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is unrecoverable and must not be swallowed.
		panic("browse: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (a *authStore) mint(stateKey string) string {
	tok := randToken()
	a.mu.Lock()
	a.grants[tok] = grantEntry{stateKey: stateKey, expires: time.Now().Add(60 * time.Second)}
	a.mu.Unlock()
	return tok
}

// redeem consumes a grant (one-time) and returns a new session cookie value.
func (a *authStore) redeem(grant string) (cookie, stateKey string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, present := a.grants[grant]
	if !present || time.Now().After(e.expires) {
		delete(a.grants, grant)
		return "", "", false
	}
	delete(a.grants, grant)
	cookie = randToken()
	a.sessions[cookie] = e.stateKey
	return cookie, e.stateKey, true
}

func (a *authStore) sessionStateKey(cookie string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sk, ok := a.sessions[cookie]
	return sk, ok
}

func (a *authStore) revoke(stateKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c, sk := range a.sessions {
		if sk == stateKey {
			delete(a.sessions, c)
		}
	}
}

// authenticate resolves the request to a stateKey, redeeming a ?__grant= on
// first navigation. Returns (stateKey, redirectURL, ok). When redirectURL is
// non-empty the caller must 302 there after setting the returned cookie.
func (a *authStore) authenticate(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if g := r.URL.Query().Get("__grant"); g != "" {
		cookieVal, stateKey, ok := a.redeem(g)
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
