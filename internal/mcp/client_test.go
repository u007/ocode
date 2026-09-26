package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

var mcpTestDataHome string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ocode-mcp-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create MCP test data directory")
		os.Exit(1)
	}
	mcpTestDataHome = dir
	if err := os.Setenv("XDG_DATA_HOME", dir); err != nil {
		fmt.Fprintln(os.Stderr, "set MCP test data directory")
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming")); err != nil {
		fmt.Fprintln(os.Stderr, "set MCP test APPDATA directory")
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func mcpTestServerName(t *testing.T) string {
	t.Helper()
	return "test-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
}

func storeMCPServerToken(t *testing.T, name, serverURL string, token auth.MCPAuthToken) {
	t.Helper()
	token.ServerURL = serverURL
	if err := auth.SetMCPAuthForServer(name, serverURL, token); err != nil {
		t.Fatal(err)
	}
}

func writeMCPResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  result,
	})
}

func TestRemoteClientAttachesStoredTokenWithoutStaticOAuth(t *testing.T) {
	var authorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		writeMCPResult(w, map[string]any{"tools": []any{}})
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	storeMCPServerToken(t, name, server.URL, auth.MCPAuthToken{
		AccessToken: "stored-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
		ClientID:    "stored-client",
	})

	client, err := NewRemoteClient(name, config.MCPConfig{Type: "remote", URL: server.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	if got, _ := authorization.Load().(string); got != "Bearer stored-access" {
		t.Fatalf("Authorization = %q, want stored bearer token", got)
	}
}

func TestRemoteClientReportsPlainTextHTTPStatusBeforeJSONDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("authorization rejected for https://secret.example/private?token=leaked-access"))
	}))
	t.Cleanup(server.Close)

	client, err := NewRemoteClient("plain-text-401", config.MCPConfig{Type: "remote", URL: server.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListTools()
	if err == nil {
		t.Fatal("ListTools succeeded, want HTTP 401 error")
	}
	httpErr, ok := errors.AsType[*RemoteHTTPError](err)
	if !ok {
		t.Fatalf("error type = %T, want *RemoteHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized || !httpErr.AuthenticationRequired {
		t.Fatalf("RemoteHTTPError = %+v, want HTTP 401 authentication failure", httpErr)
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error = %q, want HTTP 401 status", err)
	}
	if strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %q, want status handling before JSON decoding", err)
	}
	if strings.Contains(err.Error(), "secret.example") || strings.Contains(err.Error(), "leaked-access") {
		t.Fatalf("error leaked response credentials or URL: %q", err)
	}
}

func TestRemoteClientDiscoversMetadataRefreshesAndRetriesOnce(t *testing.T) {
	var mcpRequests atomic.Int32
	var refreshRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			mcpRequests.Add(1)
			switch r.Header.Get("Authorization") {
			case "Bearer expired-access":
				w.Header().Add("WWW-Authenticate", `Basic realm="other"`)
				w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/.well-known/oauth-protected-resource"))
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("expired"))
			case "Bearer rotated-access":
				writeMCPResult(w, map[string]any{"tools": []any{}})
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("missing bearer token"))
			}
		case "/.well-known/oauth-protected-resource":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL + "/authorization"},
			})
		case "/authorization":
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/oauth-authorization-server/authorization":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL + "/authorization",
				"token_endpoint": server.URL + "/token",
			})
		case "/token":
			refreshRequests.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse refresh form: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if got := r.Form.Get("grant_type"); got != "refresh_token" {
				t.Errorf("grant_type = %q, want refresh_token", got)
			}
			if got := r.Form.Get("client_id"); got != "stored-client" {
				t.Errorf("client_id = %q, want stored client", got)
			}
			if got := r.Form.Get("refresh_token"); got != "stored-refresh" {
				t.Errorf("refresh_token = %q, want stored refresh token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "rotated-access",
				"refresh_token": "rotated-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         "ZohoBooks.modules.ALL",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	storeMCPServerToken(t, name, server.URL+"/mcp", auth.MCPAuthToken{
		AccessToken:  "expired-access",
		RefreshToken: "stored-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour).Unix(),
		ClientID:     "stored-client",
		Scopes:       []string{"ZohoBooks.settings.READ"},
	})

	client, err := NewRemoteClient(name, config.MCPConfig{Type: "remote", URL: server.URL + "/mcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	if got := mcpRequests.Load(); got != 2 {
		t.Fatalf("MCP requests = %d, want initial request plus one retry", got)
	}
	if got := refreshRequests.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want exactly one", got)
	}

	stored, ok := auth.GetMCPAuthForServer(name, server.URL+"/mcp")
	if !ok {
		t.Fatal("rotated URL-bound credential was not persisted")
	}
	if stored.AccessToken != "rotated-access" || stored.RefreshToken != "rotated-refresh" {
		t.Fatal("rotated tokens were not persisted")
	}
	if stored.ClientID != "stored-client" || stored.Expiry <= time.Now().Unix() {
		t.Fatal("rotated credential lost client metadata or received an expired lifetime")
	}
}

func TestRemoteClientRefreshRejectionRequiresReauthorizationWithoutRetryLoop(t *testing.T) {
	var mcpRequests atomic.Int32
	var refreshRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			mcpRequests.Add(1)
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/protected"))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("expired"))
		case "/protected":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL + "/authorization"},
			})
		case "/authorization":
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/oauth-authorization-server/authorization":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL + "/authorization",
				"token_endpoint": server.URL + "/token",
			})
		case "/token":
			refreshRequests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("invalid_grant rejected-client rejected-refresh"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	storeMCPServerToken(t, name, server.URL+"/mcp", auth.MCPAuthToken{
		AccessToken:  "expired-access",
		RefreshToken: "rejected-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour).Unix(),
		ClientID:     "rejected-client",
	})

	client, err := NewRemoteClient(name, config.MCPConfig{Type: "remote", URL: server.URL + "/mcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListTools()
	if err == nil {
		t.Fatal("ListTools succeeded after rejected refresh")
	}
	reauthErr, ok := errors.AsType[*ReauthorizationRequiredError](err)
	if !ok {
		t.Fatalf("error type = %T, want *ReauthorizationRequiredError", err)
	}
	if reauthErr.ServerName != name {
		t.Fatalf("ReauthorizationRequiredError.ServerName = %q, want %q", reauthErr.ServerName, name)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "reauthorization") {
		t.Fatalf("error = %q, want reauthorization-required guidance", err)
	}
	if got := mcpRequests.Load(); got != 1 {
		t.Fatalf("MCP requests = %d, want no retry after refresh rejection", got)
	}
	if got := refreshRequests.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want exactly one", got)
	}
	for _, secret := range []string{"expired-access", "rejected-refresh", "rejected-client", server.URL + "/mcp"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked credential or server URL: %q", err)
		}
	}
}

func TestRemoteClientStaticOAuthTokenAttachmentRemainsCompatible(t *testing.T) {
	var authorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		writeMCPResult(w, map[string]any{"tools": []any{}})
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	if err := auth.SetMCPAuth(name, auth.MCPAuthToken{
		AccessToken: "native-static-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	client, err := NewRemoteClient(name, config.MCPConfig{
		Type:    "remote",
		URL:     server.URL,
		Enabled: true,
		OAuth: &config.MCPOAuthConfig{
			Enabled:          &enabled,
			AuthorizationURL: server.URL + "/authorize",
			TokenURL:         server.URL + "/token",
			ClientID:         "static-client",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	if got, _ := authorization.Load().(string); got != "Bearer native-static-access" {
		t.Fatalf("Authorization = %q, want native static OAuth token", got)
	}
}

func TestRemoteClientSerializesConcurrentRefreshAndPreservesUnrotatedFields(t *testing.T) {
	var mcpRequests atomic.Int32
	var refreshRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			mcpRequests.Add(1)
			if r.Header.Get("Authorization") == "Bearer refreshed-access" {
				writeMCPResult(w, map[string]any{"tools": []any{}})
				return
			}
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/protected"))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("expired"))
		case "/protected":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL + "/authorization"},
			})
		case "/authorization":
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/oauth-authorization-server/authorization":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL + "/authorization",
				"token_endpoint": server.URL + "/token",
			})
		case "/token":
			refreshRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "refreshed-access",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	storeMCPServerToken(t, name, server.URL+"/mcp", auth.MCPAuthToken{
		AccessToken:  "expired-access",
		RefreshToken: "preserved-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour).Unix(),
		Scopes:       []string{"ZohoBooks.settings.READ"},
		ClientID:     "stored-client",
	})
	clientA, err := NewRemoteClient(name, config.MCPConfig{Type: "remote", URL: server.URL + "/mcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := NewRemoteClient(name, config.MCPConfig{Type: "remote", URL: server.URL + "/mcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, client := range []*MCPClient{clientA, clientB} {
		wg.Go(func() {
			_, err := client.ListTools()
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := refreshRequests.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want one shared refresh", got)
	}
	if got := mcpRequests.Load(); got != 4 {
		t.Fatalf("MCP requests = %d, want two initial requests and one retry per client", got)
	}
	stored, ok := auth.GetMCPAuthForServer(name, server.URL+"/mcp")
	if !ok {
		t.Fatal("refreshed URL-bound credential is missing")
	}
	if stored.RefreshToken != "preserved-refresh" || len(stored.Scopes) != 1 || stored.Scopes[0] != "ZohoBooks.settings.READ" {
		t.Fatal("refresh discarded an omitted refresh token or scope")
	}
}

func TestRemoteClientURLBoundCredentialDoesNotUseStaticOAuthRefresh(t *testing.T) {
	enabled := true
	tests := []struct {
		name  string
		oauth *config.MCPOAuthConfig
	}{
		{name: "incomplete static config", oauth: &config.MCPOAuthConfig{Enabled: &enabled}},
		{
			name: "unrelated complete static config",
			oauth: &config.MCPOAuthConfig{
				Enabled:          &enabled,
				AuthorizationURL: "https://unrelated.example/authorize",
				TokenURL:         "https://unrelated.example/token",
				ClientID:         "unrelated-client",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var staticRefreshes atomic.Int32
			staticServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				staticRefreshes.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(staticServer.Close)
			if tt.oauth != nil && tt.oauth.TokenURL != "" {
				tt.oauth.TokenURL = staticServer.URL
			}

			name := mcpTestServerName(t)
			const serverURL = "https://bound.example.test/mcp"
			storeMCPServerToken(t, name, serverURL, auth.MCPAuthToken{
				AccessToken:  "expired-bound-access",
				RefreshToken: "bound-refresh",
				TokenType:    "Bearer",
				Expiry:       time.Now().Add(-time.Hour).Unix(),
				ClientID:     "bound-client",
			})

			client, err := NewRemoteClient(name, config.MCPConfig{
				Type:    "remote",
				URL:     serverURL,
				Enabled: true,
				OAuth:   tt.oauth,
			})
			if err != nil {
				t.Fatalf("URL-bound credential was forced through static OAuth: %v", err)
			}
			if got := staticRefreshes.Load(); got != 0 {
				t.Fatalf("static refresh requests = %d, want URL-bound discovery", got)
			}
			if got := client.AuthHeader(); got != "Bearer expired-bound-access" {
				t.Fatalf("AuthHeader = %q, want stored URL-bound token", got)
			}
		})
	}
}

func TestRemoteClientStaticOAuthDoesNotUseMismatchedUpstreamCredential(t *testing.T) {
	var authorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		writeMCPResult(w, map[string]any{"tools": []any{}})
	}))
	t.Cleanup(server.Close)

	name := mcpTestServerName(t)
	storeMCPServerToken(t, name, "https://old.example.test/mcp", auth.MCPAuthToken{
		AccessToken: "old-server-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
		ClientID:    "old-client",
	})
	enabled := true
	client, err := NewRemoteClient(name, config.MCPConfig{
		URL:     server.URL,
		Enabled: true,
		OAuth: &config.MCPOAuthConfig{
			Enabled:          &enabled,
			AuthorizationURL: "https://auth.example.test/authorize",
			TokenURL:         "https://auth.example.test/token",
			ClientID:         "static-client",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(); err == nil {
		t.Fatal("ListTools succeeded without a native static-OAuth credential")
	} else if _, ok := errors.AsType[*ReauthorizationRequiredError](err); !ok {
		t.Fatalf("ListTools error = %T, want *ReauthorizationRequiredError", err)
	}
	if got, _ := authorization.Load().(string); got != "" {
		t.Fatalf("Authorization = %q, want no token for a different server URL", got)
	}
}

func TestRemoteClientFallsBackToRootMetadataWithoutChallenge(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL,
				"token_endpoint": server.URL + "/token",
			})
		case "/.well-known/oauth-protected-resource/mcp":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewRemoteClient("metadata-root-fallback", config.MCPConfig{URL: server.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.discoverTokenEndpoint(""); err != nil {
		t.Fatalf("root metadata fallback failed: %v", err)
	}
}

func TestRemoteClientDoesNotFollowMetadataRedirect(t *testing.T) {
	var receiverHits atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, receiver.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	client, err := NewRemoteClient("metadata-redirect", config.MCPConfig{URL: redirect.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, redirect.URL+"/.well-known/oauth-protected-resource")
	if _, err := client.discoverTokenEndpoint(challenge); err == nil {
		t.Fatal("metadata discovery unexpectedly followed redirect")
	}
	if got := receiverHits.Load(); got != 0 {
		t.Fatalf("metadata redirect target received %d requests, want none", got)
	}
}

func TestRemoteClientRejectsMismatchedProtectedResourceMetadata(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/different-resource",
				"authorization_servers": []string{server.URL},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewRemoteClient("metadata-resource-mismatch", config.MCPConfig{URL: server.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/.well-known/oauth-protected-resource")
	if _, err := client.discoverTokenEndpoint(challenge); err == nil || !strings.Contains(err.Error(), "resource identity") {
		t.Fatalf("discoverTokenEndpoint error = %v, want resource identity mismatch", err)
	}
}

func TestRemoteClientRejectsMismatchedAuthorizationIssuer(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL + "/different-issuer",
				"token_endpoint": server.URL + "/token",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewRemoteClient("metadata-issuer-mismatch", config.MCPConfig{URL: server.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/.well-known/oauth-protected-resource")
	if _, err := client.discoverTokenEndpoint(challenge); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("discoverTokenEndpoint error = %v, want issuer mismatch", err)
	}
}

func TestRemoteClientRejectsEmptyDiscoveredTokenEndpoint(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              server.URL + "/mcp",
				"authorization_servers": []string{server.URL},
			})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         server.URL,
				"token_endpoint": "",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewRemoteClient("metadata-empty-token-endpoint", config.MCPConfig{URL: server.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, server.URL+"/.well-known/oauth-protected-resource")
	if _, err := client.discoverTokenEndpoint(challenge); err == nil || !strings.Contains(err.Error(), "token endpoint") {
		t.Fatalf("discoverTokenEndpoint error = %v, want empty token endpoint error", err)
	}
}

func TestResourceMetadataURLIncludesResourcePathWithoutChallenge(t *testing.T) {
	client, err := NewRemoteClient("resource-path", config.MCPConfig{URL: "https://mcp.example.test/mcp/secret?ignored=true"})
	if err != nil {
		t.Fatal(err)
	}
	urls, err := client.resourceMetadataURLs("")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://mcp.example.test/.well-known/oauth-protected-resource/mcp/secret"
	if len(urls) == 0 || urls[0] != want {
		t.Fatalf("resourceMetadataURLs = %q, want first %q", urls, want)
	}
}

func TestRemoteClientRejectsCleartextNonLoopbackServer(t *testing.T) {
	if _, err := NewRemoteClient("cleartext-server", config.MCPConfig{URL: "http://mcp.example.test/mcp"}); err == nil {
		t.Fatal("NewRemoteClient accepted cleartext non-loopback MCP URL")
	}
}

func TestRemoteClientRejectsNonHTTPSMetadataOutsideLoopback(t *testing.T) {
	metadataURL, err := url.Parse("http://metadata.example.test/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOAuthMetadataURL(metadataURL); err == nil {
		t.Fatal("validateOAuthMetadataURL accepted insecure non-loopback metadata URL")
	}
}
