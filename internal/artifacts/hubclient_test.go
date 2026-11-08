package artifacts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHubClientPublishFlow(t *testing.T) {
	var gotAuth string
	var uploaded []byte
	var versionPayload map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ocode/artifacts", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["id"] != "art_1" || body["token"] != "public-token-value" {
			t.Errorf("create body = %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"artifact":{"id":"art_1","name":"demo"}}`))
	})
	mux.HandleFunc("/api/ocode/artifacts/art_1/blobs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hash string `json:"hash"`
			Size int    `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Hash == strings.Repeat("a", 64) {
			_, _ = w.Write([]byte(`{"exists":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"exists":false,"uploadUrl":"` + serverURL(r) + `/s3/` + body.Hash + `"}`))
	})
	mux.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("s3 method = %s", r.Method)
		}
		uploaded, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/ocode/artifacts/art_1/versions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&versionPayload)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"version":{"id":"v1"},"token":""}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewHubClient(srv.URL, "sync-bearer")
	c.HTTP = srv.Client()

	ctx := context.Background()
	if _, err := c.EnsureArtifact(ctx, "art_1", "demo", "public-token-value"); err != nil {
		t.Fatalf("EnsureArtifact: %v", err)
	}
	if gotAuth != "Bearer sync-bearer" {
		t.Fatalf("Authorization = %q", gotAuth)
	}

	// Already-present blob: no PUT.
	if err := c.UploadBlob(ctx, "art_1", strings.Repeat("a", 64), []byte("skip")); err != nil {
		t.Fatalf("UploadBlob existing: %v", err)
	}
	// Missing blob: presigned PUT carries the bytes.
	missing := strings.Repeat("b", 64)
	if err := c.UploadBlob(ctx, "art_1", missing, []byte("hello")); err != nil {
		t.Fatalf("UploadBlob missing: %v", err)
	}
	if string(uploaded) != "hello" {
		t.Fatalf("uploaded = %q", uploaded)
	}

	files := []File{{Path: "index.html", BlobHash: missing, Size: 5, Mode: "0644", Mime: "text/html"}}
	if _, err := c.PublishVersion(ctx, "art_1", "index.html", "", files); err != nil {
		t.Fatalf("PublishVersion: %v", err)
	}
	fs, _ := versionPayload["files"].([]any)
	if len(fs) != 1 {
		t.Fatalf("files payload = %#v", versionPayload["files"])
	}

	wantURL := srv.URL + "/p/art_1/public-token-value/"
	if got := c.PublicURL("art_1", "public-token-value"); got != wantURL {
		t.Fatalf("PublicURL = %q, want %q", got, wantURL)
	}
}

func TestHubClientSurfacesHubError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"hub down"}`))
	}))
	defer srv.Close()

	c := NewHubClient(srv.URL, "tok")
	c.HTTP = srv.Client()
	if _, err := c.EnsureArtifact(context.Background(), "a", "n", "t"); err == nil {
		t.Fatal("expected error on 502")
	} else if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v, want status 502", err)
	}
}

func TestNewHubPublicToken(t *testing.T) {
	a, err := NewHubPublicToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewHubPublicToken()
	if a == b || len(a) < 40 {
		t.Fatalf("token = %q / %q", a, b)
	}
}

func TestHubClientSetArtifactAccess(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"mode":"password"}`))
	}))
	defer srv.Close()

	c := NewHubClient(srv.URL, "tok")
	if err := c.SetArtifactAccess(context.Background(), "art_1", "password", "hunter2hunter2"); err != nil {
		t.Fatalf("SetArtifactAccess: %v", err)
	}
	if method != http.MethodPut || path != "/api/ocode/artifacts/art_1/access" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["mode"] != "password" || body["password"] != "hunter2hunter2" {
		t.Fatalf("body = %#v", body)
	}

	// Public mode carries no password field at all.
	body = nil
	if err := c.SetArtifactAccess(context.Background(), "art_1", "public", ""); err != nil {
		t.Fatalf("SetArtifactAccess public: %v", err)
	}
	if _, ok := body["password"]; ok {
		t.Fatalf("public body must omit the password: %#v", body)
	}
}

func TestHubClientSetArtifactAccessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid access mode"}`))
	}))
	defer srv.Close()

	c := NewHubClient(srv.URL, "tok")
	err := c.SetArtifactAccess(context.Background(), "art_1", "friends", "")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v, want status 400", err)
	}
}

func TestHubClientAddArtifactInvite(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"email":"a@b.com","inviteUrl":"https://hub.test/i","emailSent":true}`))
	}))
	defer srv.Close()

	c := NewHubClient(srv.URL, "tok")
	invite, err := c.AddArtifactInvite(context.Background(), "art_1", "a@b.com", "public-token")
	if err != nil {
		t.Fatalf("AddArtifactInvite: %v", err)
	}
	if path != "/api/ocode/artifacts/art_1/invites" {
		t.Fatalf("path = %s", path)
	}
	if body["email"] != "a@b.com" || body["publicToken"] != "public-token" {
		t.Fatalf("body = %#v", body)
	}
	if invite.Email != "a@b.com" || invite.InviteURL != "https://hub.test/i" || !invite.EmailSent {
		t.Fatalf("invite = %#v", invite)
	}
}

func TestHubClientAddArtifactInviteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"public token does not match"}`))
	}))
	defer srv.Close()

	c := NewHubClient(srv.URL, "tok")
	if _, err := c.AddArtifactInvite(context.Background(), "art_1", "a@b.com", "wrong"); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v, want status 400", err)
	}
}

// serverURL reconstructs the test server base URL from a request, since the
// handler does not otherwise know it.
func serverURL(r *http.Request) string {
	scheme := "http"
	return scheme + "://" + r.Host
}
