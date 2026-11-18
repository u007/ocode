package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/cloudconnect"
	"github.com/u007/ocode/internal/cloudhub"
	"github.com/u007/ocode/internal/cloudkeystore"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
)

// hubClient is the narrow surface the kakiit handlers need. *cloudhub.Client
// satisfies it; tests inject a fake through newHubCloudClient.
type hubClient interface {
	RegisterCallback(ctx context.Context, installID, callbackURL string) error
	StartOAuth(ctx context.Context, provider, connectorID, installID, clientState, challenge string) (string, error)
	Redeem(ctx context.Context, code, verifier, clientState string) (cloudhub.TokenBundle, error)
	ListConnections(ctx context.Context) ([]cloudhub.ConnectionMeta, error)
	Disconnect(ctx context.Context, connectionID string) error
	RefreshToken(ctx context.Context, connectionID string) (string, time.Time, error)
}

var (
	newHubCloudClient = func() (hubClient, error) { return cloudhub.NewFromSync() }
	resolveHubKey     = cloudkeystore.Resolve
)

// hubOAuthFlow binds the opaque client_state to the project/host that opened
// the flow and to the PKCE verifier for the redeem hop. The verifier is created
// here (not sent at start) so the challenge forwarded to kakiit always matches
// the verifier used at /oauth/redeem.
type hubOAuthFlow struct {
	provider  string
	project   string
	host      string
	verifier  string
	createdAt time.Time
}

const hubFlowTTL = 5 * time.Minute

var (
	hubFlowsMu sync.Mutex
	hubFlows   = map[string]hubOAuthFlow{}
)

func storeHubFlow(state string, flow hubOAuthFlow) {
	hubFlowsMu.Lock()
	defer hubFlowsMu.Unlock()
	now := time.Now()
	for k, f := range hubFlows {
		if now.Sub(f.createdAt) > hubFlowTTL {
			delete(hubFlows, k)
		}
	}
	hubFlows[state] = flow
}

func lookupHubFlow(state string) (hubOAuthFlow, bool) {
	hubFlowsMu.Lock()
	defer hubFlowsMu.Unlock()
	f, ok := hubFlows[state]
	if !ok {
		return hubOAuthFlow{}, false
	}
	if time.Since(f.createdAt) > hubFlowTTL {
		delete(hubFlows, state)
		return hubOAuthFlow{}, false
	}
	return f, true
}

func consumeHubFlow(state string) (hubOAuthFlow, bool) {
	hubFlowsMu.Lock()
	defer hubFlowsMu.Unlock()
	f, ok := hubFlows[state]
	delete(hubFlows, state)
	if !ok || time.Since(f.createdAt) > hubFlowTTL {
		return hubOAuthFlow{}, false
	}
	return f, true
}

// kakiitProject validates the project param against this server's allowed
// roots. host is only a binding value for remote projects — the owning host's
// server receives the request directly, so this server never dials it here.
func (h *Handler) kakiitProject(w http.ResponseWriter, r *http.Request) (root, host string, ok bool) {
	project := r.URL.Query().Get("project")
	host = r.URL.Query().Get("host")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return "", "", false
	}
	for _, allowed := range h.allowedProjectRoots() {
		if allowed == project {
			return allowed, host, true
		}
	}
	http.Error(w, "project not allowed", http.StatusBadRequest)
	return "", "", false
}

// clientStateBinding checks the web UI's client_state shape:
// encodeURIComponent(project) + "." + encodeURIComponent(host) + "." + nonce.
// It is a binding check, not a trust decision: the in-memory flow lookup is
// authoritative. Rejecting a state that names a different project or host
// stops a callback captured on one project being replayed on another.
func clientStateBinding(state, project, host string) bool {
	parts := strings.Split(state, ".")
	if len(parts) < 3 {
		return false
	}
	stateProject, err := url.QueryUnescape(strings.Join(parts[:len(parts)-2], "."))
	if err != nil || stateProject != project {
		return false
	}
	stateHost, err := url.QueryUnescape(parts[len(parts)-2])
	return err == nil && stateHost == host
}

// loadHubV2File migrates a v1 file exactly once before any hub write. Without
// this guard LoadV2 would decode v1 metadata as an empty v2 file and SaveV2
// would destroy root_path/settings/max_cache_bytes.
func loadHubV2File(root string) (*config.StorageConnectorsFileV2, error) {
	f, err := config.LoadV2(root)
	if errors.Is(err, config.ErrV2MigrationRequired) {
		if migrateErr := cloudconnect.MigrateV1ToV2(root); migrateErr != nil {
			return nil, fmt.Errorf("migrate v1 connectors: %w", migrateErr)
		}
		f, err = config.LoadV2(root)
	}
	return f, err
}

func hubCredentials(f *config.StorageConnectorsFileV2, key []byte) (map[string]cloudconnect.Credentials, error) {
	creds := map[string]cloudconnect.Credentials{}
	if f.CredentialsEnvelope == "" {
		return creds, nil
	}
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, key)
	if err != nil {
		return nil, fmt.Errorf("open credentials envelope: %w", err)
	}
	if err := json.Unmarshal(plain, &creds); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return creds, nil
}

// sealHubCredentials writes the credentials back with NO refresh token: hub
// mode keeps only the short-lived access token locally. An empty map clears
// the envelope entirely (used when a central connection is revoked).
func sealHubCredentials(f *config.StorageConnectorsFileV2, creds map[string]cloudconnect.Credentials, key []byte) error {
	if len(creds) == 0 {
		f.CredentialsEnvelope = ""
		return nil
	}
	plain, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	env, err := encryption.SealWithKey(plain, key)
	if err != nil {
		return fmt.Errorf("seal credentials: %w", err)
	}
	f.CredentialsEnvelope = env
	return nil
}

// handleKakiitCallbackRegister registers this install's loopback callback.
// PUT /api/cloud/oauth/kakiit/callback/register?project=...
func (h *Handler) handleKakiitCallbackRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, _, ok := h.kakiitProject(w, r); !ok {
		return
	}
	var in struct {
		InstallID   string `json:"install_id"`
		CallbackURL string `json:"callback_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.InstallID == "" || in.CallbackURL == "" {
		http.Error(w, "install_id and callback_url required", http.StatusBadRequest)
		return
	}
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := client.RegisterCallback(r.Context(), in.InstallID, in.CallbackURL); err != nil {
		http.Error(w, "hub callback registration failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleKakiitStart starts the hub->provider OAuth hop.
// POST /api/cloud/oauth/kakiit/start?project=...&host=...
func (h *Handler) handleKakiitStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, host, ok := h.kakiitProject(w, r)
	if !ok {
		return
	}
	var in struct {
		Provider      string `json:"provider"`
		ConnectorID   string `json:"connector_id"`
		InstallID     string `json:"install_id"`
		ClientState   string `json:"client_state"`
		CodeChallenge string `json:"code_challenge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Provider == "" || in.ConnectorID == "" || in.InstallID == "" || in.ClientState == "" || in.CodeChallenge == "" {
		http.Error(w, "provider, connector_id, install_id, client_state and code_challenge required", http.StatusBadRequest)
		return
	}
	if !clientStateBinding(in.ClientState, root, host) {
		http.Error(w, "client_state does not bind this project and host", http.StatusBadRequest)
		return
	}
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	// The web never sends its PKCE verifier, so generate the verifier/challenge
	// pair here; only the matching challenge goes to kakiit for the redeem hop.
	verifier := randomVerifier()
	authURL, err := client.StartOAuth(r.Context(), in.Provider, in.ConnectorID, in.InstallID, in.ClientState, challengeFor(verifier))
	if err != nil {
		http.Error(w, "hub oauth start failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	storeHubFlow(in.ClientState, hubOAuthFlow{
		provider: in.Provider, project: root, host: host, verifier: verifier, createdAt: time.Now(),
	})
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL, "client_state": in.ClientState})
}

const kakiitClosePopupHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Connected</title></head>` +
	`<body><p>Cloud connection established. You can close this window.</p>` +
	`<script>window.close();</script></body></html>`

// handleKakiitReturn is the fixed callback kakiit redirects the popup to. The
// one-time code is redeemed here, the v2 file is written, and the popup closes.
// GET carries code/client_state in the query (browser redirect); POST accepts
// the pinned JSON body for callers that redeem explicitly.
// /api/cloud/oauth/kakiit/return?project=...&host=...
func (h *Handler) handleKakiitReturn(w http.ResponseWriter, r *http.Request) {
	var code, state string
	switch r.Method {
	case http.MethodGet:
		code = r.URL.Query().Get("code")
		state = r.URL.Query().Get("client_state")
	case http.MethodPost:
		var in struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			ClientState  string `json:"client_state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		code, state = in.Code, in.ClientState
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, host, ok := h.kakiitProject(w, r)
	if !ok {
		return
	}
	if code == "" || state == "" {
		http.Error(w, "code and client_state required", http.StatusBadRequest)
		return
	}
	// Verify the binding BEFORE consuming the flow or writing any file, so a
	// mismatched callback leaves the real project's state untouched.
	if !clientStateBinding(state, root, host) {
		http.Error(w, "client_state does not bind this project and host", http.StatusBadRequest)
		return
	}
	flow, ok := consumeHubFlow(state)
	if !ok || flow.project != root || flow.host != host {
		http.Error(w, "unknown or expired client_state", http.StatusBadRequest)
		return
	}
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	bundle, err := client.Redeem(r.Context(), code, flow.verifier, state)
	if err != nil {
		http.Error(w, "hub code redeem failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	key, _, err := resolveHubKey()
	if err != nil {
		http.Error(w, "resolve connector key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := loadHubV2File(root)
	if err != nil {
		http.Error(w, "load connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	creds, err := hubCredentials(f, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Hub mode: access token + expiry ONLY — no refresh token, no client secret.
	creds[flow.provider] = cloudconnect.Credentials{AccessToken: bundle.AccessToken, ExpiresAt: bundle.ExpiresAt}
	if f.Central.Connections == nil {
		f.Central.Connections = map[string]config.ConnectionMeta{}
	}
	f.Central.ConnectorID = "kakiit"
	f.Central.Mode = "hub"
	f.Central.Connections[flow.provider] = config.ConnectionMeta{
		Provider:     flow.provider,
		ConnectionID: bundle.ConnectionID,
		ExpiresAt:    bundle.ExpiresAt.UTC().Format(time.RFC3339),
		Status:       "active",
	}
	if err := sealHubCredentials(f, creds, key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := config.SaveV2(root, f); err != nil {
		http.Error(w, "save connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Register the cached access token with this root's manager so browse/read
	// work immediately. It carries no refresh token; expiry forces /refresh.
	if mgr := h.cloudManager(root); mgr != nil {
		_ = mgr.RegisterTransient(
			cloudconnect.Config{ID: bundle.ConnectionID, Provider: flow.provider},
			cloudconnect.SecretBundle{
				AccessToken:          bundle.AccessToken,
				AccessTokenExpiresAt: bundle.ExpiresAt,
			},
		)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(kakiitClosePopupHTML))
}

type hubConnectionView struct {
	Provider     string `json:"provider"`
	ConnectionID string `json:"connection_id"`
	RootPath     string `json:"root_path"`
	ExpiresAt    string `json:"expires_at"`
}

// handleKakiitConnections lists central connections for a project.
// GET /api/cloud/connections?project=...
func (h *Handler) handleKakiitConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, _, ok := h.kakiitProject(w, r)
	if !ok {
		return
	}
	f, err := loadHubV2File(root)
	if err != nil {
		http.Error(w, "load connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(f.Central.Connections) > 0 {
		views := make([]hubConnectionView, 0, len(f.Central.Connections))
		for _, meta := range f.Central.Connections {
			views = append(views, hubConnectionView{meta.Provider, meta.ConnectionID, meta.RootPath, meta.ExpiresAt})
		}
		writeJSON(w, http.StatusOK, views)
		return
	}

	// First use in this project: discover the central connection and fetch a
	// valid token from the hub. A refresh failure is explicit — never fall back
	// to a stale local token when the hub is unreachable.
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	conns, err := client.ListConnections(r.Context())
	if err != nil {
		http.Error(w, "hub connections list failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	key, _, err := resolveHubKey()
	if err != nil {
		http.Error(w, "resolve connector key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	creds, err := hubCredentials(f, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]hubConnectionView, 0, len(conns))
	for _, c := range conns {
		access, expires, err := client.RefreshToken(r.Context(), c.ConnectionID)
		if err != nil {
			http.Error(w, fmt.Sprintf("hub token refresh for %s failed: %v", c.Provider, err), http.StatusBadGateway)
			return
		}
		creds[c.Provider] = cloudconnect.Credentials{AccessToken: access, ExpiresAt: expires}
		f.Central.Connections[c.Provider] = config.ConnectionMeta{
			Provider:     c.Provider,
			ConnectionID: c.ConnectionID,
			RootPath:     c.RootPath,
			ExpiresAt:    expires.UTC().Format(time.RFC3339),
			Status:       "active",
		}
		views = append(views, hubConnectionView{c.Provider, c.ConnectionID, c.RootPath, f.Central.Connections[c.Provider].ExpiresAt})
	}
	f.Central.ConnectorID = "kakiit"
	f.Central.Mode = "hub"
	if err := sealHubCredentials(f, creds, key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := config.SaveV2(root, f); err != nil {
		http.Error(w, "save connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// handleKakiitTokenRefresh refreshes a central connection for this project.
// POST /api/cloud/token/refresh?project=...
func (h *Handler) handleKakiitTokenRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, _, ok := h.kakiitProject(w, r)
	if !ok {
		return
	}
	var in struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.ConnectionID == "" {
		http.Error(w, "connection_id required", http.StatusBadRequest)
		return
	}
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	f, err := loadHubV2File(root)
	if err != nil {
		http.Error(w, "load connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	key, _, err := resolveHubKey()
	if err != nil {
		http.Error(w, "resolve connector key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	access, expires, err := client.RefreshToken(r.Context(), in.ConnectionID)
	if err != nil {
		// A revoked central connection (404/410) invalidates every project's
		// cached copy; clear this project's copy so the next list starts fresh.
		var hubErr *cloudhub.HubError
		if errors.As(err, &hubErr) && (hubErr.StatusCode == 404 || hubErr.StatusCode == 410) {
			h.clearHubConnection(root, in.ConnectionID, key)
		}
		http.Error(w, "hub token refresh failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	creds, err := hubCredentials(f, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	refreshedProvider := ""
	for provider, meta := range f.Central.Connections {
		if meta.ConnectionID == in.ConnectionID {
			creds[provider] = cloudconnect.Credentials{AccessToken: access, ExpiresAt: expires}
			meta.ExpiresAt = expires.UTC().Format(time.RFC3339)
			f.Central.Connections[provider] = meta
			refreshedProvider = provider
		}
	}
	if err := sealHubCredentials(f, creds, key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := config.SaveV2(root, f); err != nil {
		http.Error(w, "save connector settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if refreshedProvider != "" {
		if mgr := h.cloudManager(root); mgr != nil {
			_ = mgr.RegisterTransient(
				cloudconnect.Config{ID: in.ConnectionID, Provider: refreshedProvider},
				cloudconnect.SecretBundle{
					AccessToken:          access,
					AccessTokenExpiresAt: expires,
				},
			)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": access,
		"expires_at":   expires.UTC().Format(time.RFC3339),
	})
}

// clearHubConnection removes one revoked central connection from a project's
// v2 file. A failure is best-effort and intentionally silent: the hub already
// refused the token, so keeping a stale cached copy is never an acceptable
// fallback and the caller is already returning the hub's explicit error.
func (h *Handler) clearHubConnection(root, connectionID string, key []byte) {
	f, err := loadHubV2File(root)
	if err != nil {
		return
	}
	creds, err := hubCredentials(f, key)
	if err != nil {
		return
	}
	for provider, meta := range f.Central.Connections {
		if meta.ConnectionID == connectionID {
			delete(creds, provider)
			delete(f.Central.Connections, provider)
		}
	}
	if err := sealHubCredentials(f, creds, key); err != nil {
		return
	}
	_ = config.SaveV2(root, f)
}

// handleKakiitDisconnect revokes a central connection globally.
// DELETE /api/cloud/oauth/kakiit/disconnect?project=...&connection_id=...
func (h *Handler) handleKakiitDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, _, ok := h.kakiitProject(w, r)
	if !ok {
		return
	}
	connectionID := r.URL.Query().Get("connection_id")
	if connectionID == "" {
		http.Error(w, "connection_id required", http.StatusBadRequest)
		return
	}
	client, err := newHubCloudClient()
	if err != nil {
		http.Error(w, "hub unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := client.Disconnect(r.Context(), connectionID); err != nil {
		http.Error(w, "hub disconnect failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if mgr := h.cloudManager(root); mgr != nil {
		mgr.Remove(connectionID)
	}
	// Other projects' cached copies intentionally stay on disk: each goes stale
	// and is cleared on its next refresh (see handleKakiitTokenRefresh).
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
