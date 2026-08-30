package browse

import (
	"net/http"
	"slices"
	"sync"
)

// cookieJar holds upstream cookies entirely server-side. Site cookies are
// NEVER handed to the browser: the browser only ever holds the scoped
// ocode_browse session cookie (auth.go). Keying by (stateKey, origin) means
// two sites browsed in the same panel keep separate jars, and two panels keep
// separate jars for the same site.
type cookieJar struct {
	mu   sync.Mutex
	data map[string]map[string]*http.Cookie // key -> cookieName -> cookie
}

func newCookieJar() *cookieJar {
	return &cookieJar{data: map[string]map[string]*http.Cookie{}}
}

func jarKey(stateKey, origin string) string { return stateKey + "\x00" + origin }

// Store absorbs Set-Cookie headers from an upstream response into the jar.
func (j *cookieJar) Store(stateKey, origin string, resp *http.Response) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	key := jarKey(stateKey, origin)
	j.mu.Lock()
	defer j.mu.Unlock()
	m := j.data[key]
	if m == nil {
		m = map[string]*http.Cookie{}
		j.data[key] = m
	}
	for _, c := range cookies {
		// Expiry with empty value / MaxAge<0 deletes.
		if c.MaxAge < 0 || (c.Value == "" && c.MaxAge == 0 && !c.Expires.IsZero()) {
			delete(m, c.Name)
			continue
		}
		m[c.Name] = c
	}
}

// Apply attaches this (stateKey, origin)'s jar cookies to an outbound upstream
// request. Path/expiry nuance is intentionally coarse (name=value only): the
// jar is per-origin already, and finer scoping is not needed for embedding.
func (j *cookieJar) Apply(stateKey, origin string, req *http.Request) {
	key := jarKey(stateKey, origin)
	j.mu.Lock()
	m := j.data[key]
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	j.mu.Unlock()
	if len(names) == 0 {
		return
	}
	// Deterministic order for stable Cookie headers (and testability).
	slices.Sort(names)
	for _, name := range names {
		j.mu.Lock()
		c := m[name]
		j.mu.Unlock()
		if c != nil {
			req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
}
