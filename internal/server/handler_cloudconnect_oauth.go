package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/cloudconnect"
	"github.com/u007/ocode/internal/crashguard"
)

type oauthFlow struct {
	provider     string
	connectorID  string
	clientID     string
	clientSecret string
	verifier     string
	redirectURI  string
	createdAt    time.Time
}

var (
	oauthFlowsMu sync.Mutex
	oauthFlows   = map[string]oauthFlow{}
)

func init() {
	crashguard.Go(func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			oauthFlowsMu.Lock()
			now := time.Now()
			for k, v := range oauthFlows {
				if now.Sub(v.createdAt) > 10*time.Minute {
					delete(oauthFlows, k)
				}
			}
			oauthFlowsMu.Unlock()
		}
	})
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func challengeFor(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func oauthEndpoints(provider string) (string, string, error) {
	switch provider {
	case "gdrive":
		return "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", nil
	case "onedrive":
		return "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", "https://login.microsoftonline.com/common/oauth2/v2.0/token", nil
	case "dropbox":
		return "https://www.dropbox.com/oauth2/authorize", "https://api.dropboxapi.com/oauth2/token", nil
	default:
		return "", "", fmt.Errorf("oauth not supported for provider %q", provider)
	}
}

func oauthScopes(provider string) string {
	switch provider {
	case "gdrive":
		return "https://www.googleapis.com/auth/drive.readonly https://www.googleapis.com/auth/drive.file"
	case "onedrive":
		return "Files.ReadWrite Files.ReadWrite.All offline_access"
	default:
		return ""
	}
}

func (h *Handler) handleCloudConnectOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Provider     string `json:"provider"`
		ConnectorID  string `json:"connector_id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Provider == "" || in.ConnectorID == "" || in.ClientID == "" {
		http.Error(w, "provider, connector_id, client_id required", http.StatusBadRequest)
		return
	}
	authURLBase, _, err := oauthEndpoints(in.Provider)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirectURI := in.RedirectURI
	if redirectURI == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		redirectURI = scheme + "://" + r.Host + "/api/cloud/oauth/callback"
	}
	state := randomState()
	verifier := randomVerifier()
	challenge := challengeFor(verifier)

	oauthFlowsMu.Lock()
	oauthFlows[state] = oauthFlow{
		provider:     in.Provider,
		connectorID:  in.ConnectorID,
		clientID:     in.ClientID,
		clientSecret: in.ClientSecret,
		verifier:     verifier,
		redirectURI:  redirectURI,
		createdAt:    time.Now(),
	}
	oauthFlowsMu.Unlock()

	u, _ := url.Parse(authURLBase)
	q := u.Query()
	q.Set("client_id", in.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if sc := oauthScopes(in.Provider); sc != "" {
		q.Set("scope", sc)
	}
	if in.Provider == "dropbox" {
		q.Set("token_access_type", "offline")
	}
	u.RawQuery = q.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": u.String(), "state": state})
}

func (h *Handler) handleCloudConnectOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "code and state required", http.StatusBadRequest)
		return
	}
	oauthFlowsMu.Lock()
	flow, ok := oauthFlows[state]
	if ok {
		delete(oauthFlows, state)
	}
	oauthFlowsMu.Unlock()
	if !ok {
		http.Error(w, "unknown or expired state", http.StatusBadRequest)
		return
	}

	_, tokenURL, err := oauthEndpoints(flow.provider)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data := url.Values{}
	data.Set("code", code)
	data.Set("client_id", flow.clientID)
	if flow.clientSecret != "" {
		data.Set("client_secret", flow.clientSecret)
	}
	data.Set("redirect_uri", flow.redirectURI)
	data.Set("grant_type", "authorization_code")
	data.Set("code_verifier", flow.verifier)

	req, err := http.NewRequestWithContext(r.Context(), "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		http.Error(w, string(body), http.StatusBadGateway)
		return
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	mgr := h.cloudManager()
	if mgr == nil {
		http.Error(w, "cloud connectors not initialized", http.StatusServiceUnavailable)
		return
	}
	sec := cloudconnect.SecretBundle{
		ClientID:     flow.clientID,
		ClientSecret: flow.clientSecret,
		AccessKey:    tr.AccessToken,
		RefreshToken: tr.RefreshToken,
	}
	cfg := cloudconnect.Config{ID: flow.connectorID, Provider: flow.provider}
	if err := mgr.Register(cfg, sec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected", "connector_id": flow.connectorID})
}
