package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTypesafeClientDecide(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"allow","probabilities":{"allow":0.97,"deny":0.03},"confidence":0.94}},"usage":{"input_tokens":312,"output_tokens":48}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTypesafeClient("sk-test", "jev-latest", srv.URL)
	resp, err := c.Decide(map[string]any{"tool": "bash"}, map[string]TypesafeQuestion{
		"verdict": {Type: "choice", Instructions: "Allow?", Criteria: map[string]string{"allow": "safe", "deny": "unsafe"}},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotPath != "/systemone" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["model"] != "jev-latest" {
		t.Fatalf("model = %v", gotBody["model"])
	}
	if _, ok := gotBody["state"].(map[string]any); !ok {
		t.Fatalf("state not forwarded as object: %v", gotBody["state"])
	}
	ans, ok := resp.Answers["verdict"]
	if !ok {
		t.Fatalf("missing verdict answer: %+v", resp)
	}
	if ans.Choice != "allow" || ans.Confidence != 0.94 {
		t.Fatalf("answer = %+v", ans)
	}
	if resp.Usage.InputTokens != 312 || resp.Usage.OutputTokens != 48 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestTypesafeClientDecideStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := newTypesafeClient("bad", "jev-latest", srv.URL)
	_, err := c.Decide("x", map[string]TypesafeQuestion{"q": {Type: "noul", Instructions: "?"}})
	if err == nil {
		t.Fatal("expected error")
	}
	pse, ok := errors.AsType[*providerStatusError](err)
	if !ok || pse.Code != http.StatusUnauthorized {
		t.Fatalf("expected providerStatusError 401, got %v", err)
	}
}

func TestTypesafeClientChatIsDecisionOnly(t *testing.T) {
	c := newTypesafeClient("k", "jev-latest", "http://127.0.0.1:1")
	if _, err := c.Chat([]Message{{Role: "user", Content: "hi"}}, nil); !errors.Is(err, ErrTypesafeDecisionOnly) {
		t.Fatalf("expected ErrTypesafeDecisionOnly, got %v", err)
	}
	if c.GetProvider() != "typesafe" || c.GetModel() != "jev-latest" {
		t.Fatalf("identity = %s/%s", c.GetProvider(), c.GetModel())
	}
}

func TestNewClientBuildsTypesafeClient(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "sk-env")
	t.Setenv("OPENCODE_AUTH_TOKEN", "")
	client := NewClientWithProfile(nil, "typesafe/jev-latest", "")
	tc, ok := client.(*TypesafeClient)
	if !ok {
		t.Fatalf("expected *TypesafeClient, got %T", client)
	}
	if tc.APIKey != "sk-env" || tc.Model != "jev-latest" || tc.BaseURL != "https://api.typesafe.ai/v1" {
		t.Fatalf("client = %+v", tc)
	}
}
