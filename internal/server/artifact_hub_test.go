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
		s.published++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"version":{"id":"v1"},"token":""}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_test/access", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.accessCalls = append(s.accessCalls, body)
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
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email":     email,
			"inviteUrl": "https://hub.test/p/art_test/pub/invite/tok",
			"emailSent": true,
		})
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
