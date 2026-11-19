package agent

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// kakiitDefaultBaseURL is the production kakiit LLM gateway root. It is also
// the entry in the providers map; pinned here so a URL change fails loudly.
const kakiitDefaultBaseURL = "https://hub.mercstudio.com/api/llm"

// newKakiitTestClient builds a GenericClient for kakiit the way NewClient does,
// for tests that exercise the transport rather than the resolution path.
func newKakiitTestClient(baseURL string) *GenericClient {
	return &GenericClient{Provider: "kakiit", Model: "test-model", BaseURL: baseURL, APIKey: "kk-test"}
}

// TestKakiitProviderBuildsOpenAICompatibleClient pins registration: the
// providers map carries kakiit with the production base URL, and NewClient
// builds a GenericClient (the OpenAI-compatible client class) honoring the
// env override, the auth.json credential's BaseURL, and the default.
func TestKakiitProviderBuildsOpenAICompatibleClientWithConfiguredBaseURL(t *testing.T) {
	setHomeTree(t, t.TempDir())
	t.Setenv("KAKIIT_API_KEY", "kk-env-key")
	t.Setenv("KAKIIT_BASE_URL", "")

	// Default base URL when nothing overrides it.
	cfg := &config.Config{}
	client := NewClient(cfg, "kakiit/test-model")
	gc, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected *GenericClient for kakiit, got %T", client)
	}
	if gc.BaseURL != kakiitDefaultBaseURL {
		t.Fatalf("BaseURL = %q, want default %q", gc.BaseURL, kakiitDefaultBaseURL)
	}
	if gc.APIKey != "kk-env-key" {
		t.Fatalf("APIKey = %q, want env key", gc.APIKey)
	}
	if gc.Provider != "kakiit" {
		t.Fatalf("Provider = %q, want kakiit", gc.Provider)
	}
	if gc.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", gc.Model)
	}
}

// TestKakiitBaseURLOverrides covers KAKIIT_BASE_URL env and the
// kakiit_base_url ocode config value, each winning over the default.
func TestKakiitBaseURLOverrides(t *testing.T) {
	setHomeTree(t, t.TempDir())
	t.Setenv("KAKIIT_API_KEY", "kk-env-key")

	t.Run("env override", func(t *testing.T) {
		t.Setenv("KAKIIT_BASE_URL", "http://127.0.0.1:9999/api/llm")
		cfg := &config.Config{}
		gc, ok := NewClient(cfg, "kakiit/test-model").(*GenericClient)
		if !ok {
			t.Fatalf("expected *GenericClient, got %T", NewClient(cfg, "kakiit/test-model"))
		}
		if gc.BaseURL != "http://127.0.0.1:9999/api/llm" {
			t.Fatalf("BaseURL = %q, want env override", gc.BaseURL)
		}
	})

	t.Run("auth.json credential base URL", func(t *testing.T) {
		t.Setenv("KAKIIT_API_KEY", "")
		t.Setenv("KAKIIT_BASE_URL", "")
		if err := auth.Set("kakiit", auth.Credential{Kind: auth.KindAPIKey, Key: "kk-stored", BaseURL: "http://127.0.0.1:8888/api/llm"}); err != nil {
			t.Fatalf("auth.Set: %v", err)
		}
		defer auth.Remove("kakiit")
		cfg := &config.Config{}
		gc, ok := NewClient(cfg, "kakiit/test-model").(*GenericClient)
		if !ok {
			t.Fatalf("expected *GenericClient, got nil")
		}
		if gc.BaseURL != "http://127.0.0.1:8888/api/llm" {
			t.Fatalf("BaseURL = %q, want auth.json credential base URL", gc.BaseURL)
		}
		if gc.APIKey != "kk-stored" {
			t.Fatalf("APIKey = %q, want stored key", gc.APIKey)
		}
	})
}

// TestKakiitSendsOpencodeSessionHeader pins the sticky header: a kakiit chat
// request carries X-Opencode-Session with a stable value, exactly like the
// opencode zen providers (same helper, same per-conversation identity).
func TestKakiitSendsOpencodeSessionHeader(t *testing.T) {
	seedKakiitProtocols(t)

	var got string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("X-Opencode-Session")
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/v1")
	c.Model = "deepseek-v4-flash"
	c.setOpencodeSessionID("ses-kakiit-test")
	if _, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}
	if got != "ses-kakiit-test" {
		t.Fatalf("X-Opencode-Session = %q, want ses-kakiit-test (sticky session header must ride kakiit requests)", got)
	}
}

// TestKakiitRequestIDStableAcrossRetriesUniqueAcrossTurns pins the per-turn
// X-Request-Id: minted once per turn (ChatWithContext call), reused for every
// retry within that turn, and different on the next turn.
func TestKakiitRequestIDStableAcrossRetriesUniqueAcrossTurns(t *testing.T) {
	seedKakiitProtocols(t)

	var ids []string
	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		ids = append(ids, req.Header.Get("X-Request-Id"))
		if n == 1 {
			// Transient 500 inside the first turn → retry with the SAME id.
			return statusResponse(http.StatusInternalServerError, `{"error":"gateway blip"}`), nil
		}
		if n == 2 {
			return statusResponse(http.StatusOK, openAIChatOKStream), nil
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/v1")
	c.Model = "deepseek-v4-flash"
	if _, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if _, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "again"}}, nil); err != nil {
		t.Fatalf("second turn: %v", err)
	}

	if len(ids) != 3 {
		t.Fatalf("expected 3 requests (1 failed + 1 retry + 1 next turn), got %d: %v", len(ids), ids)
	}
	if ids[0] == "" || ids[1] == "" || ids[2] == "" {
		t.Fatalf("every kakiit request must carry X-Request-Id, got %v", ids)
	}
	if ids[0] != ids[1] {
		t.Fatalf("retries within one turn must reuse the request id: %q vs %q", ids[0], ids[1])
	}
	if ids[0] == ids[2] {
		t.Fatalf("request ids must differ across turns: %q reused", ids[0])
	}
	if !strings.HasPrefix(ids[0], "req_") {
		t.Fatalf("request id %q should carry the req_ prefix", ids[0])
	}
}

// TestKakiitAPIKeyResolutionPrecedence pins env > auth.json for the key
// (the standard resolution order every keyed provider follows).
func TestKakiitAPIKeyResolutionPrecedence(t *testing.T) {
	setHomeTree(t, t.TempDir())

	// auth.json wins when env is unset.
	t.Setenv("KAKIIT_API_KEY", "")
	if err := auth.Set("kakiit", auth.Credential{Kind: auth.KindAPIKey, Key: "kk-stored-key"}); err != nil {
		t.Fatalf("auth.Set: %v", err)
	}
	defer auth.Remove("kakiit")
	gc, ok := NewClient(&config.Config{}, "kakiit/test-model").(*GenericClient)
	if !ok || gc == nil {
		t.Fatal("expected client from stored credential")
	}
	if gc.APIKey != "kk-stored-key" {
		t.Fatalf("APIKey = %q, want stored key when env unset", gc.APIKey)
	}

	// Env wins when both exist.
	t.Setenv("KAKIIT_API_KEY", "kk-env-wins")
	gc2, ok := NewClient(&config.Config{}, "kakiit/test-model").(*GenericClient)
	if !ok || gc2 == nil {
		t.Fatal("expected client with env key")
	}
	if gc2.APIKey != "kk-env-wins" {
		t.Fatalf("APIKey = %q, want env key to outrank auth.json", gc2.APIKey)
	}
}

// TestKakiitNoCredentialYieldsClearTypedError pins that a kakiit model with no
// credential does not produce a client that 401s later or panics: NewClient
// returns nil (the keyed-provider refusal), and kakiit is NOT key-optional.
func TestKakiitNoCredentialYieldsClearTypedError(t *testing.T) {
	setHomeTree(t, t.TempDir())
	t.Setenv("KAKIIT_API_KEY", "")
	t.Setenv("OPENCODE_AUTH_TOKEN", "")

	if client := NewClient(&config.Config{}, "kakiit/test-model"); client != nil {
		t.Fatalf("expected nil client for kakiit without credential, got %T", client)
	}
	if KeyOptionalProvider("kakiit") {
		t.Fatal("kakiit must not be key-optional — a keyless client would 401 on first use")
	}
}
