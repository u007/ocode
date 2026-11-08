package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/u007/ocode/internal/artifacts"
	ocodesync "github.com/u007/ocode/internal/sync"
)

// hubStub is a minimal implementation of the hub artifact contract used to
// exercise publishArtifactToHub without a real kakiit server or S3.
type hubStub struct {
	creates     []map[string]string
	blobHits    int
	uploads     int
	published   int
	failWith    int
	accessCalls []map[string]any
	invites     []map[string]any
	inviteFail  string
	order       []string
	unpubs      int
	deletes     int
	unpubFail   int
	deleteFail  int
	// publishFail fails the version publish with this status (0 = succeed).
	publishFail int
	// routeMissing makes unpublish and delete answer Hono's plain-text 404, as
	// an older hub without those routes does.
	routeMissing bool
}

// hubNotFoundBody is the JSON the hub sends with a 404 for a missing artifact.
const hubNotFoundBody = `{"error":"artifact not found","code":"not_found"}`

// writeHubFailure answers status the way the hub does: a 404 for a missing
// artifact carries the not_found JSON body.
func writeHubFailure(w http.ResponseWriter, status int) {
	if status == http.StatusNotFound {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(hubNotFoundBody))
		return
	}
	w.WriteHeader(status)
}

func (s *hubStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ocode/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if s.failWith != 0 {
			w.WriteHeader(s.failWith)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.creates = append(s.creates, body)
		s.order = append(s.order, "create")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"artifact":{"id":"art_test","name":"demo"}}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/blobs", func(w http.ResponseWriter, r *http.Request) {
		s.blobHits++
		var body struct {
			Hash string `json:"hash"`
			Size int    `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"exists":false,"uploadUrl":"http://` + r.Host + `/s3/` + body.Hash + `"}`))
	})
	mux.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
		s.uploads++
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/versions", func(w http.ResponseWriter, r *http.Request) {
		if s.publishFail != 0 {
			writeHubFailure(w, s.publishFail)
			return
		}
		s.published++
		s.order = append(s.order, "version")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"version":{"id":"v1"},"token":""}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/access", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.accessCalls = append(s.accessCalls, body)
		s.order = append(s.order, "access")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"mode":"ok"}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/invites", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		email, _ := body["email"].(string)
		if email == s.inviteFail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad invite"}`))
			return
		}
		s.invites = append(s.invites, body)
		s.order = append(s.order, "invite")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email":     email,
			"inviteUrl": "https://hub.test/p/art_test/pub/invite/tok",
			"emailSent": true,
		})
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/unpublish", func(w http.ResponseWriter, r *http.Request) {
		s.unpubs++
		if s.routeMissing {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 Not Found"))
			return
		}
		if s.unpubFail != 0 {
			writeHubFailure(w, s.unpubFail)
			return
		}
		s.order = append(s.order, "unpublish")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.deletes++
		if s.routeMissing {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 Not Found"))
			return
		}
		if s.deleteFail != 0 {
			writeHubFailure(w, s.deleteFail)
			return
		}
		s.order = append(s.order, "delete")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return mux
}

func newHubHandlerTestStore(t *testing.T) (*Handler, *artifacts.Store) {
	t.Helper()
	st, err := newArtifactTokenStore(t.TempDir())
	require.NoError(t, err)
	h := &Handler{artifactTokens: st, artifactRL: newRateLimiter()}
	return h, st.store
}

func TestPublishArtifactToHubReusesTokenOnRepublish(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	out1, err := h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.NoError(t, err)
	url1 := out1.PublicURL
	token1, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)
	require.NotEmpty(t, token1)
	require.Contains(t, url1, "/p/art_test/"+token1+"/")
	require.Len(t, stub.creates, 1)
	require.Equal(t, "art_test", stub.creates[0]["id"])
	require.Equal(t, token1, stub.creates[0]["token"])
	require.Equal(t, 1, stub.uploads)
	require.Equal(t, 1, stub.published)

	// Re-publish must reuse the same public token and therefore the same link.
	out2, err := h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.NoError(t, err)
	url2 := out2.PublicURL
	token2, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)
	require.Equal(t, token1, token2)
	require.Equal(t, url1, url2)
	require.Len(t, stub.creates, 2)
	require.Equal(t, token1, stub.creates[1]["token"])
	require.Equal(t, 2, stub.published)
}

func TestPublishArtifactToHubSurfacesErrorWithoutFallback(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{failWith: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "502"), "error should carry the hub status: %v", err)
	// No version was accepted by the hub.
	require.Equal(t, 0, stub.published)
}

func TestPublishArtifactToHubFirstPublishUndoneOnLaterFailure(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{publishFail: http.StatusInternalServerError}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	// The hub artifact created by this first publish is removed, and the
	// local token it was given is cleared.
	require.Equal(t, 1, stub.deletes)
	require.Equal(t, []string{"create", "delete"}, stub.order)
	token, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestPublishArtifactToHubRepublishFailureKeepsHubAndToken(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.NoError(t, err)
	token1, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)

	// The previous version keeps serving on the hub, so a failed re-publish
	// must not delete the artifact or drop the token.
	stub.publishFail = http.StatusInternalServerError
	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.Error(t, err)
	require.Equal(t, 0, stub.deletes)
	require.Equal(t, 1, stub.published)
	token2, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)
	require.Equal(t, token1, token2)
}

func TestPublishArtifactToHubFailedUndoKeepsToken(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{publishFail: http.StatusInternalServerError, deleteFail: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{})
	require.Error(t, err)
	// The hub copy could not be removed, so the token stays: local unpublish
	// and delete can still reach it.
	token, err := store.GetArtifactHubToken(a.ID)
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestPublishArtifactToHubRequiresSyncToken(t *testing.T) {
	setHomeTree(t, t.TempDir())
	// Explicitly ensure no token exists.
	require.NoError(t, ocodesync.ClearToken())

	srv := httptest.NewServer((&hubStub{}).handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)

	_, err = h.publishArtifactToHub(context.Background(), a, "index.html", "", nil, store, artifactHubAccess{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not connected")
}

func newHubPublishRequest(t *testing.T) (*http.Request, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644))
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", nil)
	return req, dir
}

func TestHandlePublishHubSuccessReturnsURL(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	url, _ := body["hub_url"].(string)
	require.Contains(t, url, "/p/art_test/")
	require.Equal(t, 1, stub.published)
}

func TestHandlePublishHubFailureReturns502(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{failWith: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "hub publish")
	require.Equal(t, 0, stub.published)
}

func TestHandlePublishHubNotConnectedReturns409(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.ClearToken())

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"code":"hub_not_connected"`)
	require.Contains(t, w.Body.String(), "/publish local")
}

func TestHandlePublishSecretScanRefusalAndOverride(t *testing.T) {
	setHomeTree(t, t.TempDir())

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o644))

	h, store := newHubHandlerTestStore(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, Status: "active",
	})
	require.NoError(t, err)

	// Refused without a reason, with the code the dialog keys its override on.
	w := httptest.NewRecorder()
	h.HandlePublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", nil), "art_test")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"code":"secret_scan"`)
	require.Contains(t, w.Body.String(), "secret scan hit")

	// A recorded reason lets the same publish through (local artifact: no hub).
	w = httptest.NewRecorder()
	body := strings.NewReader(`{"override_reason":"rotated, safe to publish"}`)
	h.HandlePublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", body), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"hub_url":""`)
}

func TestHandleListArtifactsIncludesShareURL(t *testing.T) {
	setHomeTree(t, t.TempDir())
	t.Setenv("OCODE_SYNC_URL", "https://hub.test")

	h, store := newHubHandlerTestStore(t)
	hubArtifact, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_hub", Name: "demo", ProjectPath: "/p", HostingMode: "hub",
	})
	require.NoError(t, err)
	require.NoError(t, store.SetArtifactHubToken(hubArtifact.ID, "pub-token"))
	_, err = store.CreateArtifact(artifacts.Artifact{
		ID: "art_local", Name: "local", ProjectPath: "/p", HostingMode: "self",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandleListArtifacts(w, httptest.NewRequest(http.MethodGet, "/api/artifacts", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Artifacts []artifacts.Artifact `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	urls := map[string]string{}
	for _, a := range body.Artifacts {
		urls[a.ID] = a.ShareURL
	}
	require.Equal(t, "https://hub.test/p/art_hub/pub-token/", urls["art_hub"])
	require.Empty(t, urls["art_local"], "local artifacts have no canonical share link")
}

func TestParseArtifactAccess(t *testing.T) {
	t.Run("absent mode leaves the hub setting alone", func(t *testing.T) {
		access, err := parseArtifactAccess(map[string]any{})
		require.NoError(t, err)
		require.Empty(t, access.Mode)
	})

	t.Run("public needs no password", func(t *testing.T) {
		access, err := parseArtifactAccess(map[string]any{"access_mode": "public"})
		require.NoError(t, err)
		require.Equal(t, "public", access.Mode)
	})

	t.Run("password must be at least 8 characters", func(t *testing.T) {
		_, err := parseArtifactAccess(map[string]any{"access_mode": "password", "password": "short"})
		require.ErrorContains(t, err, "8 characters")
	})

	t.Run("allowlist normalizes and dedupes emails", func(t *testing.T) {
		access, err := parseArtifactAccess(map[string]any{
			"access_mode":   "allowlist",
			"invite_emails": []any{" A@B.com ", "a@b.com", "c@d.com"},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"a@b.com", "c@d.com"}, access.Emails)
	})

	t.Run("allowlist rejects an invalid address", func(t *testing.T) {
		_, err := parseArtifactAccess(map[string]any{"access_mode": "allowlist", "invite_emails": []any{"nope"}})
		require.ErrorContains(t, err, "invalid invite email")
	})

	t.Run("unknown mode is rejected", func(t *testing.T) {
		_, err := parseArtifactAccess(map[string]any{"access_mode": "friends"})
		require.ErrorContains(t, err, "invalid access_mode")
	})
}

func TestPublishArtifactToHubAppliesAccessAndCollectsInviteErrors(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{inviteFail: "bad@example.com"}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	a, err := store.CreateArtifact(artifacts.Artifact{ID: "art_test", Name: "demo", HostingMode: "hub"})
	require.NoError(t, err)
	hash, err := store.PutBlob([]byte("hi"))
	require.NoError(t, err)
	files := []artifacts.File{{Path: "index.html", BlobHash: hash, Size: 2, Mode: "0644", Mime: "text/html"}}

	out, err := h.publishArtifactToHub(context.Background(), a, "index.html", "", files, store, artifactHubAccess{
		Mode:   "allowlist",
		Emails: []string{"good@example.com", "bad@example.com"},
	})
	require.NoError(t, err)
	require.Len(t, stub.accessCalls, 1)
	require.Equal(t, "allowlist", stub.accessCalls[0]["mode"])
	require.Len(t, stub.invites, 1)
	require.Equal(t, "good@example.com", stub.invites[0]["email"])
	require.NotEmpty(t, stub.invites[0]["publicToken"])
	require.Len(t, out.Invites, 1)
	require.Equal(t, "good@example.com", out.Invites[0].Email)
	require.True(t, out.Invites[0].EmailSent)
	require.Len(t, out.InviteErrors, 1)
	require.Contains(t, out.InviteErrors[0], "bad@example.com")
	// Access is applied before the version exists; invites only after it is live.
	require.Equal(t, []string{"create", "access", "version", "invite"}, stub.order)
}

func TestHandlePublishSendsAccessFieldsToHub(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644))
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", strings.NewReader(
		`{"access_mode":"allowlist","invite_emails":["a@b.com"]}`))
	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "allowlist", body["access_mode"])
	invites, ok := body["hub_invites"].([]any)
	require.True(t, ok)
	require.Len(t, invites, 1)
	require.Len(t, stub.accessCalls, 1)
	require.Len(t, stub.invites, 1)
}

func TestHandlePublishRejectsAccessFieldsForLocalArtifact(t *testing.T) {
	setHomeTree(t, t.TempDir())

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "local", Status: "active",
	})
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", strings.NewReader(
		`{"access_mode":"password","password":"longenough"}`))
	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "capability tokens")
}

func TestHandlePublishRejectsShortPasswordBeforePublishing(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644))
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/publish", strings.NewReader(
		`{"access_mode":"password","password":"short"}`))
	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, 0, stub.published)
}

// seedHubArtifact creates a hub-hosted artifact that already has its stored
// public token, as a first successful publish would leave it.
func seedHubArtifact(t *testing.T, store *artifacts.Store, status string) artifacts.Artifact {
	t.Helper()
	a, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: "/p", HostingMode: "hub", Status: status,
	})
	require.NoError(t, err)
	require.NoError(t, store.SetArtifactHubToken(a.ID, "pub-token"))
	return a
}

func TestHandlePublishHubFailureLeavesNoLocalVersion(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{failWith: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "unpublished",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())

	// The hub refused, so no version is committed and the status is untouched.
	versions, err := store.ListVersions("art_test")
	require.NoError(t, err)
	require.Empty(t, versions)
	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "unpublished", a.Status)
}

func TestHandlePublishHubNotConnectedLeavesNoLocalVersion(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.ClearToken())

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "unpublished",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	versions, err := store.ListVersions("art_test")
	require.NoError(t, err)
	require.Empty(t, versions)
	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "unpublished", a.Status)
}

func TestHandleUnpublishHubArtifactReachesHubFirst(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleUnpublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/unpublish", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, stub.unpubs)
	require.Equal(t, []string{"unpublish"}, stub.order)

	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "unpublished", a.Status)
}

func TestHandleUnpublishHubFailureKeepsLocalState(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{unpubFail: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")
	_, err := store.MintToken(artifacts.AccessToken{ArtifactID: "art_test", TokenHash: "hash-1", Kind: "public"})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandleUnpublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/unpublish", nil), "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "hub unpublish")

	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "active", a.Status)
	tokens, err := store.ListTokens("art_test")
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.True(t, tokens[0].RevokedAt.IsZero(), "a failed hub unpublish must not revoke local tokens")
}

func TestHandleDeleteHubArtifactReachesHubFirst(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, stub.deletes)
	require.Equal(t, []string{"delete"}, stub.order)
	_, err := store.GetArtifact("art_test")
	require.ErrorIs(t, err, artifacts.ErrNotFound)
}

func TestHandleDeleteHubArtifactHub404CountsAsGone(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{deleteFail: http.StatusNotFound}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err := store.GetArtifact("art_test")
	require.ErrorIs(t, err, artifacts.ErrNotFound)
}

func TestHandleDeleteHubFailureKeepsLocalRow(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{deleteFail: http.StatusBadGateway}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "hub delete")
	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "active", a.Status)
}

func TestHandleHubRemovalNotConnectedReturns409(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.ClearToken())

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleUnpublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/unpublish", nil), "art_test")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"code":"hub_not_connected"`)

	w = httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"code":"hub_not_connected"`)

	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "active", a.Status)
}

func TestHandleHubRemovalSkipsHubWithoutPublicToken(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.ClearToken())

	// A hub-hosted row with no stored token never reached the hub, so deleting
	// it needs no connection and no hub call.
	h, store := newHubHandlerTestStore(t)
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: "/p", HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = store.GetArtifact("art_test")
	require.ErrorIs(t, err, artifacts.ErrNotFound)
}

func TestHandleLocalArtifactNeverReachesHub(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: "/p", HostingMode: "self", Status: "active",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.HandleUnpublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/unpublish", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Equal(t, 0, stub.unpubs)
	require.Equal(t, 0, stub.deletes)
}

func TestHandleHubPlain404FromMissingRouteKeepsLocalState(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	// An older hub without unpublish/delete answers Hono's plain-text 404. That
	// is not "already gone": the hub still has the artifact.
	stub := &hubStub{routeMissing: true}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	seedHubArtifact(t, store, "active")

	w := httptest.NewRecorder()
	h.HandleUnpublish(w, httptest.NewRequest(http.MethodPost, "/api/artifacts/art_test/unpublish", nil), "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "does not support")

	w = httptest.NewRecorder()
	h.HandleDeleteArtifact(w, httptest.NewRequest(http.MethodDelete, "/api/artifacts/art_test", nil), "art_test")
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "does not support")

	a, err := store.GetArtifact("art_test")
	require.NoError(t, err)
	require.Equal(t, "active", a.Status)
	token, err := store.GetArtifactHubToken("art_test")
	require.NoError(t, err)
	require.Equal(t, "pub-token", token)
}

// failLocalVersionInsert makes the next local version insert fail, after the
// hub has already accepted the publish.
func failLocalVersionInsert(t *testing.T, store *artifacts.Store) {
	t.Helper()
	_, err := store.DB().Exec(`CREATE TRIGGER fail_version_insert BEFORE INSERT ON artifact_versions
BEGIN SELECT RAISE(ABORT, 'injected local commit failure'); END;`)
	require.NoError(t, err)
}

func TestHandlePublishLocalCommitFailureUndoesFirstHubPublish(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "unpublished",
	})
	require.NoError(t, err)
	failLocalVersionInsert(t, store)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "rolled back")
	// The hub accepted the version, then the local insert failed: the fresh
	// first publish is removed from the hub and its token cleared.
	require.Equal(t, []string{"create", "version", "delete"}, stub.order)
	token, err := store.GetArtifactHubToken("art_test")
	require.NoError(t, err)
	require.Empty(t, token)
	versions, err := store.ListVersions("art_test")
	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestHandlePublishLocalCommitFailureOnRepublishKeepsHubCopy(t *testing.T) {
	setHomeTree(t, t.TempDir())
	require.NoError(t, ocodesync.SaveToken("sync-bearer"))

	stub := &hubStub{}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()
	t.Setenv("OCODE_SYNC_URL", srv.URL)

	h, store := newHubHandlerTestStore(t)
	req, dir := newHubPublishRequest(t)
	h.workDir = dir
	_, err := store.CreateArtifact(artifacts.Artifact{
		ID: "art_test", Name: "demo", ProjectPath: dir, HostingMode: "hub", Status: "active",
	})
	require.NoError(t, err)
	require.NoError(t, store.SetArtifactHubToken("art_test", "pub-token"))
	failLocalVersionInsert(t, store)

	w := httptest.NewRecorder()
	h.HandlePublish(w, req, "art_test")
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "re-run publish")
	// A re-publish cannot be undone on the hub: the new version stays there,
	// and the artifact and its token are kept.
	require.Equal(t, 1, stub.published)
	require.Equal(t, 0, stub.deletes)
	token, err := store.GetArtifactHubToken("art_test")
	require.NoError(t, err)
	require.Equal(t, "pub-token", token)
}
