package browse

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"golang.org/x/net/publicsuffix"
)

// cookieJar isolates upstream site cookies per stateKey in a server-side
// jar. Site cookies never reach the browser: every proxied host shares the
// single browse origin, so browser-stored cookies would leak across hosts
// and tabs. Each stateKey owns a standard net/http/cookiejar, which applies
// Domain/Path/Secure/Expires against the REAL upstream URL, so sites see
// exactly the cookies they set (host-scoped, never port-scoped — RFC 6265,
// as in a browser). Cookies set from page JavaScript (document.cookie) stay
// in the browser and are not forwarded.
type cookieJar struct {
	mu   sync.Mutex
	jars map[string]*cookiejar.Jar // stateKey -> jar
}

func newCookieJar() *cookieJar {
	return &cookieJar{jars: make(map[string]*cookiejar.Jar)}
}

func (c *cookieJar) jarFor(stateKey string, create bool) *cookiejar.Jar {
	c.mu.Lock()
	defer c.mu.Unlock()
	j, ok := c.jars[stateKey]
	if !ok && create {
		// cookiejar.New only errors on a bad PublicSuffixList. publicsuffix.List
		// rejects domain-only cookies at a public suffix (e.g. ".com"), matching
		// browser cookie isolation instead of letting one upstream host set a
		// cookie shared across every other host visited under this stateKey.
		j, _ = cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		c.jars[stateKey] = j
	}
	return j
}

// Store records resp's Set-Cookie headers for stateKey against u.
func (c *cookieJar) Store(stateKey string, u *url.URL, resp *http.Response) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}
	c.jarFor(stateKey, true).SetCookies(u, cookies)
}

// Apply sets req's Cookie header from stateKey's jar for req.URL, replacing
// whatever the browser sent (the browse session cookie, which must never go
// upstream).
func (c *cookieJar) Apply(stateKey string, req *http.Request) {
	req.Header.Del("Cookie")
	j := c.jarFor(stateKey, false)
	if j == nil {
		return
	}
	for _, ck := range j.Cookies(req.URL) {
		req.AddCookie(ck)
	}
}

// Clear drops every cookie held for stateKey (panel close / revoke).
func (c *cookieJar) Clear(stateKey string) {
	c.mu.Lock()
	delete(c.jars, stateKey)
	c.mu.Unlock()
}
