package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/cloudconnect"
	"github.com/u007/ocode/internal/cloudhub"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
	"github.com/u007/ocode/internal/projects"
)

// kakiitTestKey is the deterministic raw key injected through resolveHubKey in
// these tests. Tests never touch the real native keychain or a real key file.
var kakiitTestKey = []byte("01234567890123456789012345678901")

type fakeHubClient struct {
	authURL       string
	startErr      error
	bundle        cloudhub.TokenBundle
	redeemErr     error
	conns         []cloudhub.ConnectionMeta
	listErr       error
	disconnectErr error
	refreshToken  string
	refreshExp    time.Time
	refreshErr    error

	startInstallID string
	startState     string
	startChallenge string
	redeemVerifier string
	redeemState    string
	disconnectedID string
	refreshCalls   int
}

func (f *fakeHubClient) RegisterCallback(_ context.Context, _, _ string) error { return nil }

func (f *fakeHubClient) StartOAuth(_ context.Context, provider, connectorID, installID, clientState, challenge string) (string, error) {
	f.startInstallID, f.startState, f.startChallenge = installID, clientState, challenge
	if f.startErr != nil {
		return "", f.startErr
	}
	if f.authURL == "" {
		return "https://hub.example.test/oauth/start", nil
	}
	return f.authURL, nil
}

func (f *fakeHubClient) Redeem(_ context.Context, code, verifier, clientState string) (cloudhub.TokenBundle, error) {
	f.redeemVerifier, f.redeemState = verifier, clientState
	if f.redeemErr != nil {
		return cloudhub.TokenBundle{}, f.redeemErr
	}
	return f.bundle, nil
}

func (f *fakeHubClient) ListConnections(_ context.Context) ([]cloudhub.ConnectionMeta, error) {
	return f.conns, f.listErr
}

func (f *fakeHubClient) Disconnect(_ context.Context, connectionID string) error {
	f.disconnectedID = connectionID
	return f.disconnectErr
}

func (f *fakeHubClient) RefreshToken(_ context.Context, connectionID string) (string, time.Time, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return "", time.Time{}, f.refreshErr
	}
	return f.refreshToken, f.refreshExp, nil
}

func newKakiitTestHandler(t *testing.T, roots ...string) (*Handler, *fakeHubClient) {
	t.Helper()
	home := t.TempDir()
	setHomeTree(t, home)

	fake := &fakeHubClient{}
	oldNew, oldKey := newHubCloudClient, resolveHubKey
	newHubCloudClient = func() (hubClient, error) { return fake, nil }
	resolveHubKey = func() ([]byte, bool, error) { return append([]byte(nil), kakiitTestKey...), true, nil }
	t.Cleanup(func() {
		newHubCloudClient = oldNew
		resolveHubKey = oldKey
	})

	h := &Handler{}
	if len(roots) > 0 {
		h.workDir = roots[0]
	}
	if len(roots) > 1 {
		store, err := projects.NewStoreAt(filepath.Join(home, "projects.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, root := range roots[1:] {
			if err := store.Add(root); err != nil {
				t.Fatal(err)
			}
		}
		h.projects = store
	}
	return h, fake
}

// kakiitClientState mirrors the web UI's encodeURIComponent(project) + "." +
// encodeURIComponent(host) + "." + nonce shape.
func kakiitClientState(project, host string) string {
	return url.QueryEscape(project) + "." + url.QueryEscape(host) + ".nonce123"
}

func kakiitSeedV2(t *testing.T, root, provider, connectionID, access string) {
	t.Helper()
	f := &config.StorageConnectorsFileV2{
		Central: config.CentralConnector{
			ConnectorID: "kakiit",
			Mode:        "hub",
			Connections: map[string]config.ConnectionMeta{
				provider: {
					Provider:     provider,
					ConnectionID: connectionID,
					RootPath:     "",
					ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
					Status:       "active",
				},
			},
		},
	}
	plain, err := json.Marshal(map[string]cloudconnect.Credentials{
		provider: {AccessToken: access, ExpiresAt: time.Now().Add(time.Hour).UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := encryption.SealWithKey(plain, kakiitTestKey)
	if err != nil {
		t.Fatal(err)
	}
	f.CredentialsEnvelope = env
	if err := config.SaveV2(root, f); err != nil {
		t.Fatal(err)
	}
}

func kakiitReadAccess(t *testing.T, root, provider string) string {
	t.Helper()
	f, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.CredentialsEnvelope == "" {
		return ""
	}
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, kakiitTestKey)
	if err != nil {
		t.Fatal(err)
	}
	var creds map[string]cloudconnect.Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		t.Fatal(err)
	}
	return creds[provider].AccessToken
}

func TestCloudManagerCachesPerProjectRoot(t *testing.T) {
	h, _ := newKakiitTestHandler(t, t.TempDir())
	rootA, rootB := t.TempDir(), t.TempDir()

	a1 := h.cloudManager(rootA)
	a2 := h.cloudManager(rootA)
	b := h.cloudManager(rootB)
	if a1 == nil || a1 != a2 {
		t.Fatal("same project root did not return the same cached manager")
	}
	if a1 == b {
		t.Fatal("different project roots shared one manager")
	}
	if h.cloudManager("") != h.cloudconnect {
		t.Fatal("no-project legacy fallback was replaced")
	}
}

func TestKakiitStartRejectsProjectOutsideAllowedRoots(t *testing.T) {
	h, _ := newKakiitTestHandler(t, t.TempDir())
	body := `{"provider":"dropbox","connector_id":"kakiit","install_id":"inst-1","client_state":"cs","code_challenge":"ch"}`
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/start?project="+url.QueryEscape("/definitely/not/allowed"), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitStart(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestKakiitStartForwardsInstallIDAndStoresVerifier(t *testing.T) {
	root := t.TempDir()
	h, fake := newKakiitTestHandler(t, root)
	state := kakiitClientState(root, "")
	fake.authURL = "https://hub.example.test/auth?x=1"
	body, _ := json.Marshal(map[string]string{
		"provider": "dropbox", "connector_id": "kakiit", "install_id": "inst-9",
		"client_state": state, "code_challenge": "web-challenge",
	})
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/start?project="+url.QueryEscape(root), bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitStart(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.startInstallID != "inst-9" {
		t.Fatalf("install_id not forwarded: got %q", fake.startInstallID)
	}
	if fake.startState != state {
		t.Fatalf("client_state not forwarded: got %q", fake.startState)
	}
	// The verifier stored for this state must be the preimage of the challenge
	// sent to kakiit, otherwise the redeem hop cannot be verified.
	var out struct {
		AuthURL string `json:"auth_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.AuthURL != fake.authURL {
		t.Fatalf("auth_url = %q, want %q", out.AuthURL, fake.authURL)
	}
	stored, ok := lookupHubFlow(state)
	if !ok {
		t.Fatal("no in-memory flow stored for client_state")
	}
	if stored.project != root || stored.host != "" {
		t.Fatalf("flow binding = %q/%q, want %q/\"\"", stored.project, stored.host, root)
	}
	if stored.verifier == "" || challengeFor(stored.verifier) != fake.startChallenge {
		t.Fatalf("stored verifier %q does not match forwarded challenge %q", stored.verifier, fake.startChallenge)
	}
}

func TestKakiitReturnRejectsMismatchedClientState(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	h, _ := newKakiitTestHandler(t, rootA, rootB)
	stateA := kakiitClientState(rootA, "")

	// Register the flow against project A.
	body, _ := json.Marshal(map[string]string{
		"provider": "dropbox", "connector_id": "kakiit", "install_id": "inst-1",
		"client_state": stateA, "code_challenge": "ch",
	})
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/start?project="+url.QueryEscape(rootA), bytes.NewReader(body))
	h.handleKakiitStart(httptest.NewRecorder(), r)

	// Replay the callback against project B.
	body, _ = json.Marshal(map[string]string{"code": "one-time", "code_verifier": "", "client_state": stateA})
	r = httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/return?project="+url.QueryEscape(rootB), bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitReturn(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(rootB, ".ocode", "storage-connectors.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a v2 file was written for the mismatched project: %v", err)
	}
}

func TestKakiitReturnRejectsStateNamingAnotherProjectEvenWithMatchingFlow(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	h, fake := newKakiitTestHandler(t, rootA, rootB)
	stateA := kakiitClientState(rootA, "")
	// A flow recorded against rootB whose client_state still names rootA: the
	// in-memory binding check alone must reject this, before any file write.
	storeHubFlow(stateA, hubOAuthFlow{
		provider: "dropbox", project: rootB, host: "", verifier: "verifier", createdAt: time.Now(),
	})
	fake.bundle = cloudhub.TokenBundle{ConnectionID: "cid-1", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}

	body, _ := json.Marshal(map[string]string{"code": "one-time", "client_state": stateA})
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/return?project="+url.QueryEscape(rootB), bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitReturn(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if fake.redeemState != "" {
		t.Fatal("redeem was called for a state bound to another project")
	}
	if _, err := os.Stat(filepath.Join(rootB, ".ocode", "storage-connectors.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a v2 file was written for the wrong project: %v", err)
	}
}

func TestKakiitReturnWritesV2WithAccessTokenOnly(t *testing.T) {
	root := t.TempDir()
	h, fake := newKakiitTestHandler(t, root)
	state := kakiitClientState(root, "remote-host")
	body, _ := json.Marshal(map[string]string{
		"provider": "dropbox", "connector_id": "kakiit", "install_id": "inst-1",
		"client_state": state, "code_challenge": "ch",
	})
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/start?project="+url.QueryEscape(root)+"&host=remote-host", bytes.NewReader(body))
	h.handleKakiitStart(httptest.NewRecorder(), r)

	exp := time.Now().Add(time.Hour).UTC()
	fake.bundle = cloudhub.TokenBundle{ConnectionID: "cid-1", AccessToken: "short-lived-access", ExpiresAt: exp}
	body, _ = json.Marshal(map[string]string{"code": "one-time", "code_verifier": "", "client_state": state})
	r = httptest.NewRequest(http.MethodPost, "/api/cloud/oauth/kakiit/return?project="+url.QueryEscape(root)+"&host=remote-host", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitReturn(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/html") {
		t.Fatalf("content-type = %q, want HTML", w.Header().Get("Content-Type"))
	}
	if strings.Contains(w.Body.String(), "short-lived-access") {
		t.Fatal("access token leaked into the HTML response")
	}
	f, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	meta := f.Central.Connections["dropbox"]
	if meta.ConnectionID != "cid-1" || f.Central.Mode != "hub" {
		t.Fatalf("v2 metadata = %+v / mode %q", meta, f.Central.Mode)
	}
	if got := kakiitReadAccess(t, root, "dropbox"); got != "short-lived-access" {
		t.Fatalf("stored access token = %q", got)
	}
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, kakiitTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "refresh_token") {
		t.Fatalf("hub-mode envelope contains a refresh token: %s", plain)
	}
	if fake.redeemState != state || fake.redeemVerifier == "" {
		t.Fatalf("redeem used state=%q verifier=%q", fake.redeemState, fake.redeemVerifier)
	}
}

func TestKakiitRefreshErrorIsExplicitWhenHubUnreachable(t *testing.T) {
	root := t.TempDir()
	h, fake := newKakiitTestHandler(t, root)
	kakiitSeedV2(t, root, "dropbox", "cid-1", "stale-access")
	fake.refreshErr = errors.New("dial tcp: hub unreachable")

	body := `{"connection_id":"cid-1"}`
	r := httptest.NewRequest(http.MethodPost, "/api/cloud/token/refresh?project="+url.QueryEscape(root), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.handleKakiitTokenRefresh(w, r)
	if w.Code != http.StatusBadGateway && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 502/503: %s", w.Code, w.Body.String())
	}
	bodyText := w.Body.String()
	if !strings.Contains(strings.ToLower(bodyText), "hub") {
		t.Fatalf("error does not name the hub failure: %s", bodyText)
	}
	if strings.Contains(bodyText, "stale-access") {
		t.Fatalf("stale token was used in the response: %s", bodyText)
	}
}

func TestKakiitFirstUseFetchesFromHubInNewProject(t *testing.T) {
	root := t.TempDir()
	h, fake := newKakiitTestHandler(t, root)
	exp := time.Now().Add(time.Hour).UTC()
	fake.conns = []cloudhub.ConnectionMeta{{Provider: "dropbox", ConnectionID: "cid-1", RootPath: "", ExpiresAt: exp}}
	fake.refreshToken = "fresh-access"
	fake.refreshExp = exp

	r := httptest.NewRequest(http.MethodGet, "/api/cloud/connections?project="+url.QueryEscape(root), nil)
	w := httptest.NewRecorder()
	h.handleKakiitConnections(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	f, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := f.Central.Connections["dropbox"]
	if !ok || meta.ConnectionID != "cid-1" {
		t.Fatalf("v2 file not written for the new project: %+v", f.Central.Connections)
	}
	if got := kakiitReadAccess(t, root, "dropbox"); got != "fresh-access" {
		t.Fatalf("stored access token = %q, want fresh-access", got)
	}
	if fake.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fake.refreshCalls)
	}
}

func TestKakiitDisconnectClearsOtherProjectCopy(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	h, fake := newKakiitTestHandler(t, rootA, rootB)
	kakiitSeedV2(t, rootA, "dropbox", "cid-1", "access-a")
	kakiitSeedV2(t, rootB, "dropbox", "cid-1", "access-b")

	r := httptest.NewRequest(http.MethodDelete, "/api/cloud/oauth/kakiit/disconnect?project="+url.QueryEscape(rootA)+"&connection_id=cid-1", nil)
	w := httptest.NewRecorder()
	h.handleKakiitDisconnect(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d: %s", w.Code, w.Body.String())
	}
	if fake.disconnectedID != "cid-1" {
		t.Fatalf("disconnected id = %q", fake.disconnectedID)
	}

	// The second project's cached copy is still on disk until its next refresh.
	if got := kakiitReadAccess(t, rootB, "dropbox"); got != "access-b" {
		t.Fatalf("rootB token before refresh = %q", got)
	}
	fake.refreshErr = &cloudhub.HubError{Op: "refresh", StatusCode: 404, Body: "Connection not found"}
	body := `{"connection_id":"cid-1"}`
	r = httptest.NewRequest(http.MethodPost, "/api/cloud/token/refresh?project="+url.QueryEscape(rootB), strings.NewReader(body))
	w = httptest.NewRecorder()
	h.handleKakiitTokenRefresh(w, r)
	if w.Code == http.StatusOK {
		t.Fatalf("refresh unexpectedly succeeded: %s", w.Body.String())
	}
	if got := kakiitReadAccess(t, rootB, "dropbox"); got != "" {
		t.Fatalf("rootB credentials were not cleared after revocation: %q", got)
	}
	f, err := config.LoadV2(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Central.Connections["dropbox"]; ok {
		t.Fatal("revoked connection still listed in rootB's v2 file")
	}
}
