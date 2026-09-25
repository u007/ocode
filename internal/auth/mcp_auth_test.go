package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func resetMCPAuthForTest(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	dataHome := filepath.Join(home, "data")
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("APPDATA", appData)

	mcpAuthMu.Lock()
	mcpCache = nil
	mcpAuthMu.Unlock()

	t.Cleanup(func() {
		mcpAuthMu.Lock()
		mcpCache = nil
		mcpAuthMu.Unlock()
	})

	if runtime.GOOS == "windows" {
		return filepath.Join(appData, "opencode", "mcp-auth.json")
	}
	return filepath.Join(dataHome, "opencode", "mcp-auth.json")
}

func writeMCPAuthFixture(t *testing.T, path string, value any) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMCPAuthJSON(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGetMCPAuthReadsUpstreamOpenCodeSchema(t *testing.T) {
	path := resetMCPAuthForTest(t)
	expiry := float64(time.Now().Add(time.Hour).Unix()) + 0.75
	writeMCPAuthFixture(t, path, map[string]any{
		"zoho-books": map[string]any{
			"tokens": map[string]any{
				"accessToken":  "upstream-access",
				"refreshToken": "upstream-refresh",
				"expiresAt":    expiry,
				"scope":        "ZohoBooks.modules.ALL ZohoBooks.settings.READ",
			},
			"clientInfo": map[string]any{
				"clientId": "upstream-client",
			},
			"serverUrl": "https://mcp.example.test/mcp",
		},
	})

	token, ok := GetMCPAuth("zoho-books")
	if !ok {
		t.Fatal("GetMCPAuth did not find upstream OpenCode credential")
	}
	if token.AccessToken != "upstream-access" || token.RefreshToken != "upstream-refresh" {
		t.Fatal("GetMCPAuth returned the wrong upstream tokens")
	}
	if token.Expiry != int64(expiry) {
		t.Fatalf("Expiry = %d, want %d", token.Expiry, int64(expiry))
	}
	if len(token.Scopes) != 2 {
		t.Fatalf("len(Scopes) = %d, want 2", len(token.Scopes))
	}
}

func TestMCPAuthRejectsUpstreamServerNamedTokens(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"tokens": map[string]any{
			"keep": map[string]any{"access_token": "keep-access"},
		},
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	err = SetMCPAuthForServer("tokens", "https://mcp.example.test/mcp", MCPAuthToken{
		AccessToken: "upstream-access",
		Expiry:      time.Now().Add(time.Hour).Unix(),
		ClientID:    "upstream-client",
	})
	if err == nil {
		t.Fatal("upstream server named tokens was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected reserved server write changed the auth file")
	}
}

func TestDeleteMCPAuthTokensPreservesNativeCredentialContainer(t *testing.T) {
	resetMCPAuthForTest(t)
	if err := SetMCPAuth("tokens", MCPAuthToken{AccessToken: "delete-me", Expiry: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPAuth("keep", MCPAuthToken{AccessToken: "keep-me", Expiry: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMCPAuth("tokens"); err != nil {
		t.Fatal(err)
	}
	if _, ok := GetNativeMCPAuth("tokens"); ok {
		t.Fatal("native server named tokens was not deleted")
	}
	kept, ok := GetNativeMCPAuth("keep")
	if !ok || kept.AccessToken != "keep-me" {
		t.Fatal("deleting native server tokens removed other credentials")
	}
}

func TestMCPAuthNativeWritesPreserveForeignEntriesAndMetadata(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"future-top-level": map[string]any{"keep": true},
		"foreign": map[string]any{
			"tokens": map[string]any{
				"accessToken": "foreign-access",
			},
			"clientInfo": map[string]any{
				"clientId": "foreign-client",
			},
			"serverUrl": "https://foreign.example.test/mcp",
		},
		"tokens": map[string]any{
			"native": map[string]any{
				"access_token": "old-native-access",
				"expiry":       time.Now().Add(-time.Hour).Unix(),
			},
		},
	})

	if err := SetMCPAuth("native", MCPAuthToken{
		AccessToken: "new-native-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	document := readMCPAuthJSON(t, path)
	if _, ok := document["future-top-level"]; !ok {
		t.Fatal("native write removed unknown top-level metadata")
	}
	if _, ok := document["foreign"]; !ok {
		t.Fatal("native write removed an upstream server entry")
	}
}

func TestDeleteMCPAuthPreservesForeignEntriesAndMetadata(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"future-top-level": map[string]any{"keep": true},
		"foreign": map[string]any{
			"tokens": map[string]any{
				"accessToken": "foreign-access",
			},
		},
		"tokens": map[string]any{
			"remove-me": map[string]any{
				"access_token": "native-access",
				"expiry":       time.Now().Add(time.Hour).Unix(),
			},
		},
	})

	if err := DeleteMCPAuth("remove-me"); err != nil {
		t.Fatal(err)
	}

	document := readMCPAuthJSON(t, path)
	if _, ok := document["future-top-level"]; !ok {
		t.Fatal("delete removed unknown top-level metadata")
	}
	if _, ok := document["foreign"]; !ok {
		t.Fatal("delete removed an upstream server entry")
	}
}

func TestGetMCPAuthReadsNativeSchema(t *testing.T) {
	path := resetMCPAuthForTest(t)
	expiry := time.Now().Add(time.Hour).Unix()
	writeMCPAuthFixture(t, path, map[string]any{
		"tokens": map[string]any{
			"native": map[string]any{
				"access_token":  "native-access",
				"refresh_token": "native-refresh",
				"token_type":    "Bearer",
				"expiry":        expiry,
				"scopes":        []string{"native.read"},
			},
		},
	})

	token, ok := GetMCPAuth("native")
	if !ok {
		t.Fatal("GetMCPAuth did not find native credential")
	}
	if token.AccessToken != "native-access" || token.RefreshToken != "native-refresh" {
		t.Fatal("GetMCPAuth returned the wrong native tokens")
	}
	if token.Expiry != expiry || len(token.Scopes) != 1 || token.Scopes[0] != "native.read" {
		t.Fatal("GetMCPAuth returned the wrong native token metadata")
	}
}

func TestGetMCPAuthForServerRequiresExactServerURL(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"zoho-books": map[string]any{
			"tokens": map[string]any{
				"accessToken": "bound-access",
				"expiresAt":   time.Now().Add(time.Hour).Unix(),
			},
			"clientInfo": map[string]any{
				"clientId": "bound-client",
			},
			"serverUrl": "https://mcp.example.test/mcp",
		},
	})

	token, ok := GetMCPAuthForServer("zoho-books", "https://mcp.example.test/mcp")
	if !ok {
		t.Fatal("GetMCPAuthForServer did not find exact URL-bound credential")
	}
	if token.ClientID != "bound-client" || token.ServerURL != "https://mcp.example.test/mcp" {
		t.Fatal("GetMCPAuthForServer returned the wrong client or server binding")
	}
	if _, ok := GetMCPAuthForServer("zoho-books", "https://mcp.example.test/mcp?other=server"); ok {
		t.Fatal("GetMCPAuthForServer accepted a credential for a different URL")
	}
}

func TestSetMCPAuthForServerWritesUpstreamSchemaAndPreservesMetadata(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"future-top-level": map[string]any{"keep": true},
		"foreign": map[string]any{
			"tokens": map[string]any{
				"accessToken": "foreign-access",
			},
			"serverUrl": "https://foreign.example.test/mcp",
		},
		"zoho-books": map[string]any{
			"tokens": map[string]any{
				"accessToken":  "old-access",
				"refreshToken": "old-refresh",
				"futureToken":  "keep-token",
			},
			"clientInfo": map[string]any{
				"clientId":     "old-client",
				"futureClient": "keep-client",
			},
			"futureEntry": "keep-entry",
			"serverUrl":   "https://mcp.example.test/mcp",
		},
	})
	expiry := time.Now().Add(time.Hour).Unix()

	if err := SetMCPAuthForServer("zoho-books", "https://mcp.example.test/mcp", MCPAuthToken{
		AccessToken:  "rotated-access",
		RefreshToken: "rotated-refresh",
		TokenType:    "DPoP",
		Expiry:       expiry,
		Scopes:       []string{"ZohoBooks.modules.ALL"},
		ClientID:     "stored-client",
	}); err != nil {
		t.Fatal(err)
	}

	document := readMCPAuthJSON(t, path)
	if _, ok := document["future-top-level"]; !ok {
		t.Fatal("upstream write removed unknown top-level metadata")
	}
	if _, ok := document["foreign"]; !ok {
		t.Fatal("upstream write removed a foreign server entry")
	}
	entry, ok := document["zoho-books"].(map[string]any)
	if !ok {
		t.Fatal("upstream server entry is missing after write")
	}
	if entry["futureEntry"] != "keep-entry" || entry["serverUrl"] != "https://mcp.example.test/mcp" {
		t.Fatal("upstream write did not preserve entry metadata or bind the exact URL")
	}
	tokens, ok := entry["tokens"].(map[string]any)
	if !ok {
		t.Fatal("upstream token object is missing after write")
	}
	if tokens["accessToken"] != "rotated-access" || tokens["refreshToken"] != "rotated-refresh" {
		t.Fatal("upstream write did not store rotated camelCase tokens")
	}
	if tokens["tokenType"] != "DPoP" {
		t.Fatal("upstream write did not preserve the OAuth token type")
	}
	if number, ok := tokens["expiresAt"].(float64); !ok || int64(number) != expiry {
		t.Fatal("upstream write did not store expiresAt")
	}
	if tokens["scope"] != "ZohoBooks.modules.ALL" || tokens["futureToken"] != "keep-token" {
		t.Fatal("upstream write did not preserve token metadata")
	}
	clientInfo, ok := entry["clientInfo"].(map[string]any)
	if !ok {
		t.Fatal("upstream clientInfo is missing after write")
	}
	if clientInfo["clientId"] != "stored-client" || clientInfo["futureClient"] != "keep-client" {
		t.Fatal("upstream write did not preserve client metadata")
	}
}

func TestMCPAuthWriteReloadsLatestFileBeforeMerge(t *testing.T) {
	path := resetMCPAuthForTest(t)
	writeMCPAuthFixture(t, path, map[string]any{
		"tokens": map[string]any{
			"existing": map[string]any{"access_token": "old"},
		},
	})

	if _, ok := GetMCPAuth("existing"); !ok {
		t.Fatal("failed to warm auth cache")
	}

	// Simulate another OpenCode process writing after ocode cached the file.
	writeMCPAuthFixture(t, path, map[string]any{
		"external-after-cache": map[string]any{"keep": true},
		"tokens": map[string]any{
			"existing": map[string]any{"access_token": "newer-external"},
		},
	})

	if err := SetMCPAuth("new-native", MCPAuthToken{
		AccessToken: "new-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	document := readMCPAuthJSON(t, path)
	if _, ok := document["external-after-cache"]; !ok {
		t.Fatal("write used a stale cache and erased a concurrent external entry")
	}
	tokens, ok := document["tokens"].(map[string]any)
	if !ok || tokens["existing"] == nil {
		t.Fatal("write erased a concurrently updated native credential")
	}
}

func TestRefreshMCPAuthTokenForServerReusesNewerPersistedCredential(t *testing.T) {
	resetMCPAuthForTest(t)
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "expired-access",
			"refresh_token": "rotated-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(server.Close)

	const serverURL = "https://mcp.example.test/mcp"
	stale := MCPAuthToken{
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour).Unix(),
		ClientID:     "stored-client",
		ServerURL:    serverURL,
	}
	if err := SetMCPAuthForServer("server", serverURL, stale); err != nil {
		t.Fatal(err)
	}

	first, err := RefreshMCPAuthTokenForServer("server", serverURL, server.URL, stale.ClientID, stale)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RefreshMCPAuthTokenForServer("server", serverURL, server.URL, stale.ClientID, stale)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken != first.AccessToken || second.RefreshToken != first.RefreshToken {
		t.Fatal("second caller did not reuse the newer persisted rotation")
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want one shared refresh", got)
	}
}

func TestRequestMCPRefreshRejectsCleartextNonLoopbackEndpoint(t *testing.T) {
	_, err := requestMCPRefresh("http://oauth.example.test/token", "client", MCPAuthToken{RefreshToken: "refresh-secret"})
	if err == nil {
		t.Fatal("cleartext non-loopback refresh endpoint was accepted")
	}
	if _, ok := errors.AsType[*MCPRefreshError](err); !ok {
		t.Fatalf("error = %T, want *MCPRefreshError", err)
	}
}

func TestExchangeMCPCodeForTokenDoesNotForwardBodyOnRedirect(t *testing.T) {
	var receiverHits atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverHits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(receiver.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", receiver.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	_, err := exchangeMCPCodeForToken("client", "code", "verifier", "http://localhost:8085/callback", redirect.URL)
	if err == nil {
		t.Fatal("authorization-code exchange unexpectedly followed redirect")
	}
	if got := receiverHits.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want none", got)
	}
}

func TestRequestMCPRefreshDoesNotForwardBodyOnRedirect(t *testing.T) {
	var receiverHits atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverHits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(receiver.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", receiver.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	_, err := requestMCPRefresh(redirect.URL, "client", MCPAuthToken{RefreshToken: "refresh-secret"})
	if err == nil {
		t.Fatal("refresh unexpectedly followed redirect")
	}
	if got := receiverHits.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want none", got)
	}
}

func TestRefreshMCPAuthTokenForServerRejectsChangedOrDeletedBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, serverName, serverURL string)
	}{
		{
			name: "rebound",
			mutate: func(t *testing.T, serverName, serverURL string) {
				t.Helper()
				if err := SetMCPAuthForServer(serverName, serverURL, MCPAuthToken{
					AccessToken: "replacement-access",
					TokenType:   "Bearer",
					Expiry:      time.Now().Add(time.Hour).Unix(),
					ClientID:    "replacement-client",
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "deleted",
			mutate: func(t *testing.T, serverName, _ string) {
				t.Helper()
				if err := DeleteMCPAuth(serverName); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetMCPAuthForTest(t)
			var refreshes atomic.Int32
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refreshes.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "unexpected-access",
					"expires_in":   3600,
				})
			}))
			t.Cleanup(tokenServer.Close)

			const (
				serverName = "server"
				oldURL     = "https://old.example.test/mcp"
				newURL     = "https://new.example.test/mcp"
			)
			stale := MCPAuthToken{
				AccessToken:  "stale-access",
				RefreshToken: "stale-refresh",
				TokenType:    "Bearer",
				Expiry:       time.Now().Add(-time.Hour).Unix(),
				ClientID:     "stale-client",
				ServerURL:    oldURL,
			}
			if err := SetMCPAuthForServer(serverName, oldURL, stale); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, serverName, newURL)

			_, err := RefreshMCPAuthTokenForServer(serverName, oldURL, tokenServer.URL, stale.ClientID, stale)
			if err == nil {
				t.Fatal("stale refresh succeeded after the binding changed")
			}
			if _, ok := errors.AsType[*MCPRefreshError](err); !ok {
				t.Fatalf("error = %T, want *MCPRefreshError", err)
			}
			if got := refreshes.Load(); got != 0 {
				t.Fatalf("refresh requests = %d, want none after binding change", got)
			}
			if replacement, ok := GetMCPAuthForServer(serverName, newURL); tc.name == "rebound" {
				if !ok || replacement.AccessToken != "replacement-access" {
					t.Fatal("stale refresh replaced the new URL-bound credential")
				}
			} else if _, ok := GetMCPAuthForServer(serverName, oldURL); ok {
				t.Fatal("stale refresh resurrected a deleted credential")
			}
		})
	}
}
