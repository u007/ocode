package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/u007/ocode/internal/artifacts"
)

// newArtifactVisitorFixture builds a Handler whose artifact store lives in a
// temp dir (never the global data dir) and returns the visitor-path handlers
// wrapped with the real artifactAuthMiddlewareImpl.
func newArtifactVisitorFixture(t *testing.T) (*Handler, *artifactTokenStore) {
	t.Helper()
	st, err := newArtifactTokenStore(t.TempDir())
	require.NoError(t, err)
	h := &Handler{artifactTokens: st, artifactRL: newRateLimiter()}
	return h, st
}

func createTestArtifact(t *testing.T, st *artifactTokenStore, id string) {
	t.Helper()
	_, err := st.store.CreateArtifact(artifacts.Artifact{ID: id, Name: id, Status: "active"})
	require.NoError(t, err)
}

// publishTestVersion stores files as one version of artifactID.
func publishTestVersion(t *testing.T, st *artifactTokenStore, artifactID, label string, createdAt time.Time, files map[string]string) {
	t.Helper()
	var fs []artifacts.File
	for p, body := range files {
		hash, err := st.store.PutBlob([]byte(body))
		require.NoError(t, err)
		fs = append(fs, artifacts.File{Path: p, BlobHash: hash, Size: int64(len(body)), Mode: "0644", Mime: "text/plain; charset=utf-8"})
	}
	_, err := st.store.PublishVersion(artifacts.Version{
		ArtifactID: artifactID,
		Version:    label,
		Entrypoint: "index.html",
		CreatedAt:  createdAt,
	}, fs)
	require.NoError(t, err)
}

func visitorRequest(method, target, remote string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	return req
}

// Finding 1: the artifact tailscale exposure routes are owner-only.
func TestArtifactTailscaleRoutesRequireAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	srv := New("127.0.0.1:1234", "user", "pass", nil)
	stub := &tsStub{}
	for _, sh := range []*tailscaleShare{srv.artifactShare, srv.artifactTailscaleShare, srv.tsArtifactShare, srv.artifactsShare} {
		sh.exposeFn = stub.record
		sh.removeFn = func(string) {}
	}
	h := srv.serveHandler()

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/artifact-tailscale-share"},
		{http.MethodPost, "/api/artifact-tailscale-share/start"},
		{http.MethodPost, "/api/artifact-tailscale-share/stop"},
		{http.MethodGet, "/api/artifacts/tailscale-share"},
		{http.MethodPost, "/api/artifacts/tailscale-share/start"},
		{http.MethodPost, "/api/artifacts/tailscale-share/stop"},
		{http.MethodGet, "/api/artifacts/expose"},
		{http.MethodPost, "/api/artifacts/expose/start"},
		{http.MethodPost, "/api/artifacts/expose/stop"},
	}
	for i, rt := range routes {
		rec := httptest.NewRecorder()
		// A distinct client per request: the owner limiter blocks after five
		// failed attempts from one address.
		req := httptest.NewRequest(rt.method, rt.path, nil)
		req.RemoteAddr = "198.51.100." + strconv.Itoa(i+1) + ":1"
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s must require auth", rt.method, rt.path)
	}
	require.Zero(t, stub.count("serve")+stub.count("full"), "unauthenticated request started an exposure")
}

// Finding 2: an allow-listed artifact denies a valid token with no verified
// email; self-asserted email (cookie or query) never passes.
func TestArtifactEmailGateFailsClosed(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "hi"})
	require.NoError(t, st.store.AddAllowEntry(artifacts.AllowEntry{ArtifactID: "art1", Email: "a@x.com", InviteTokenHash: sha256Sum("invtok")}))
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)
	accept := artifactAuthMiddlewareImpl(st, h.artifactRL, h.HandleInviteAccept)

	// Valid token, no verified email: denied.
	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.1:1"))
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Self-asserted email in a cookie or query, never verified: denied.
	req := visitorRequest(http.MethodGet, "/p/art1/"+tok+"/?email=a@x.com", "203.0.113.1:1")
	req.AddCookie(&http.Cookie{Name: "artifact_email", Value: "a@x.com"})
	rec = httptest.NewRecorder()
	serve(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Accepting the invite as the wrong artifact id fails.
	rec = httptest.NewRecorder()
	accept(rec, visitorRequest(http.MethodGet, "/p/art2/invite/invtok", "203.0.113.1:1"))
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Accept on the right artifact, then the access cookie grants the serve.
	rec = httptest.NewRecorder()
	accept(rec, visitorRequest(http.MethodGet, "/p/art1/invite/invtok", "203.0.113.1:1"))
	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	var access *http.Cookie
	for _, c := range cookies {
		if c.Name == "artifact_access_art1" {
			access = c
		}
	}
	require.NotNil(t, access)
	require.Equal(t, "invtok", access.Value)

	req = visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.1:1")
	req.AddCookie(access)
	rec = httptest.NewRecorder()
	serve(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "hi", rec.Body.String())

	// A wrong access cookie value is denied.
	bad := visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.1:1")
	bad.AddCookie(&http.Cookie{Name: "artifact_access_art1", Value: "nope"})
	rec = httptest.NewRecorder()
	serve(rec, bad)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestArtifactNoAllowlistNeedsNoEmail(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "open"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)

	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.2:1"))
	require.Equal(t, http.StatusOK, rec.Code)
}

// Finding 3: invite cookies verify, and the password gate sets a derived cookie
// that serves subresources without ?password=.
func TestArtifactPasswordGateAndCookie(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "page", "app.js": "js"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "secret")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)
	gate := artifactAuthMiddlewareImpl(st, h.artifactRL, h.HandlePasswordGate)

	// No password anywhere: refused.
	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/app.js", "203.0.113.3:1"))
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Wrong password at the gate: 401, no cookie.
	gateReq := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/p/art1/gate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.3:1"
		return req
	}
	rec = httptest.NewRecorder()
	gate(rec, gateReq(`{"token":"`+tok+`","password":"wrong"}`))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, rec.Result().Cookies())

	// Right password: 200 and the password cookie, which is not the raw password.
	rec = httptest.NewRecorder()
	gate(rec, gateReq(`{"token":"`+tok+`","password":"secret"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	var pwCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "artifact_password_art1" {
			pwCookie = c
		}
	}
	require.NotNil(t, pwCookie)
	require.NotContains(t, pwCookie.Value, "secret")
	require.True(t, pwCookie.HttpOnly)

	// The cookie loads a subresource with no ?password=.
	req := visitorRequest(http.MethodGet, "/p/art1/"+tok+"/app.js", "203.0.113.3:1")
	req.AddCookie(pwCookie)
	rec = httptest.NewRecorder()
	serve(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "js", rec.Body.String())

	// A forged cookie is refused.
	forged := visitorRequest(http.MethodGet, "/p/art1/"+tok+"/app.js", "203.0.113.3:1")
	forged.AddCookie(&http.Cookie{Name: "artifact_password_art1", Value: "secret"})
	rec = httptest.NewRecorder()
	serve(rec, forged)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// The cookie is bound to the stored hash: once the password changes, the old
// cookie stops verifying.
func TestPasswordCookieDiesWithPasswordChange(t *testing.T) {
	at := artifacts.AccessToken{ID: "tid1", PasswordHash: "argon2id$aaa$bbb"}
	val := passwordCookieValue("art1", at)
	r := httptest.NewRequest(http.MethodGet, "/p/art1/t/", nil)
	r.AddCookie(&http.Cookie{Name: passwordCookieName("art1"), Value: val})
	require.True(t, passwordCookieOK(r, "art1", at))

	at.PasswordHash = "argon2id$ccc$ddd"
	require.False(t, passwordCookieOK(r, "art1", at))
}

// Finding 4: the visitor key honours forwarded headers only from loopback.
func TestArtifactClientKey(t *testing.T) {
	mk := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/p/x/y/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	require.Equal(t, "10.0.0.1", artifactClientKey(mk("127.0.0.1:5555", "10.0.0.1")))
	require.Equal(t, "198.51.100.7", artifactClientKey(mk("127.0.0.1:5555", "evil, 198.51.100.7")))
	require.Equal(t, "127.0.0.1", artifactClientKey(mk("127.0.0.1:5555", "garbage")))
	require.Equal(t, "127.0.0.1", artifactClientKey(mk("127.0.0.1:5555", "")))
	// A remote peer cannot choose its key with a header.
	require.Equal(t, "198.51.100.9", artifactClientKey(mk("198.51.100.9:1", "10.0.0.1")))
}

// Finding 4: a success for one client leaves another client's failures counted.
func TestArtifactRateLimitIsPerKey(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < 5; i++ {
		rl.recordFailure("A")
	}
	require.True(t, rl.isBlocked("A"))
	rl.reset("B")
	require.True(t, rl.isBlocked("A"), "a success for B must not unblock A")
}

// Finding 5: a blocked client gets 429 before any password is checked, even
// with the correct password.
func TestArtifactBlockedClientRefusedBeforePasswordCheck(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "page"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "secret")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		h.artifactRL.recordFailure("192.0.2.1")
	}
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)
	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/?password=secret", "192.0.2.1:1"))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// Finding 5: when every Argon2 slot is taken the request gets 429 and the
// visitor is not counted as failing.
func TestArtifactPasswordSemaphoreBounded(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "page"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "secret")
	require.NoError(t, err)
	for i := 0; i < artifactArgon2Concurrency; i++ {
		st.pwSem <- struct{}{}
	}
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)
	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/?password=wrong", "192.0.2.2:1"))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
	require.False(t, h.artifactRL.isBlocked("192.0.2.2"))

	for i := 0; i < artifactArgon2Concurrency; i++ {
		<-st.pwSem
	}
	// With a free slot the right password still verifies.
	rec = httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/?password=secret", "192.0.2.2:1"))
	require.Equal(t, http.StatusOK, rec.Code)
}

// Finding 6: unpublished artifacts are not served, and unpublish really sets
// the non-live status.
func TestArtifactUnpublishedRefused(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "page"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)

	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.4:1"))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.HandleUnpublish(rec, httptest.NewRequest(http.MethodPost, "/api/artifacts/art1/unpublish", nil), "art1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	a, err := st.store.GetArtifact("art1")
	require.NoError(t, err)
	require.Equal(t, "unpublished", a.Status)

	// Tokens are revoked too, so the old link is refused on its own.
	rec = httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.4:1"))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// Finding 6: the newest version wins, regardless of the order ListVersions returns.
func TestArtifactServesNewestVersion(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	base := time.Now().UTC().Add(-time.Hour)
	publishTestVersion(t, st, "art1", "v2", base.Add(time.Minute), map[string]string{"index.html": "newer"})
	publishTestVersion(t, st, "art1", "v1", base, map[string]string{"index.html": "older"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)
	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/", "203.0.113.5:1"))
	require.Equal(t, "newer", rec.Body.String())
}

func TestLatestArtifactVersionPicksNewestExplicitly(t *testing.T) {
	t0 := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	vs := []artifacts.Version{
		{ID: "a", Version: "1", CreatedAt: t0},
		{ID: "c", Version: "3", CreatedAt: t0.Add(2 * time.Second)},
		{ID: "b", Version: "2", CreatedAt: t0.Add(time.Second)},
	}
	got, ok := latestArtifactVersion(vs)
	require.True(t, ok)
	require.Equal(t, "c", got.ID)
	_, ok = latestArtifactVersion(nil)
	require.False(t, ok)
}

// Finding 6: segment-based traversal check. "a..b.js" is a legal name; a ".."
// segment is refused before cleaning.
func TestArtifactDotDotSegments(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	publishTestVersion(t, st, "art1", "v1", time.Now().UTC(), map[string]string{"index.html": "page", "a..b.js": "dots"})
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	serve := artifactAuthMiddlewareImpl(st, h.artifactRL, h.handleServeArtifact)

	rec := httptest.NewRecorder()
	serve(rec, visitorRequest(http.MethodGet, "/p/art1/"+tok+"/a..b.js", "203.0.113.6:1"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "dots", rec.Body.String())

	for _, p := range []string{"/p/art1/" + tok + "/x/../index.html", "/p/art1/" + tok + "/../index.html"} {
		rec = httptest.NewRecorder()
		serve(rec, visitorRequest(http.MethodGet, p, "203.0.113.6:1"))
		require.Equal(t, http.StatusNotFound, rec.Code, p)
	}
}

// The JSON shape of the gate response stays stable for the web client.
func TestPasswordGateResponseIsJSON(t *testing.T) {
	h, st := newArtifactVisitorFixture(t)
	createTestArtifact(t, st, "art1")
	tok, _, err := st.issue("art1", "link", time.Time{}, "")
	require.NoError(t, err)
	gate := artifactAuthMiddlewareImpl(st, h.artifactRL, h.HandlePasswordGate)
	req := httptest.NewRequest(http.MethodPost, "/p/art1/gate", strings.NewReader(`{"token":"`+tok+`"}`))
	req.RemoteAddr = "203.0.113.7:1"
	rec := httptest.NewRecorder()
	gate(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body["status"])
}
