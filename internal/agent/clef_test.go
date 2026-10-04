package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clefTestClient builds a ClefClient pointed at a test server. BaseURLOverride is
// a test seam; production always derives the URL from the stored Workers AI
// credential (see clefRunURL).
func clefTestClient(t *testing.T, srvURL string) *ClefClient {
	t.Helper()
	c := newClefClient("test-key", "acct123", "@cf/cloudflare/clef-flash")
	c.BaseURLOverride = srvURL
	return c
}

func TestClefRunURL(t *testing.T) {
	const want = "https://api.cloudflare.com/client/v4/accounts/abc123/ai/run/@cf/cloudflare/clef-flash"
	tests := []struct {
		name, base, account, model string
		want                       string
	}{
		{
			name: "strips the chat ai/v1 suffix and uses the native run path",
			base: "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1", account: "abc123",
			model: "@cf/cloudflare/clef-flash", want: want,
		},
		{
			name: "tolerates a trailing slash",
			base: "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1/", account: "abc123",
			model: "@cf/cloudflare/clef-flash", want: want,
		},
		{
			name: "derives from the account id when no base is stored",
			base: "", account: "abc123", model: "@cf/cloudflare/clef-flash", want: want,
		},
		{
			name: "handles the larger model",
			base: "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1", account: "abc123",
			model: "@cf/cloudflare/clef",
			want:  "https://api.cloudflare.com/client/v4/accounts/abc123/ai/run/@cf/cloudflare/clef",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clefRunURL(tc.base, tc.account, tc.model); got != tc.want {
				t.Errorf("clefRunURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClefRunURL_KeepsPortFromSchemeLessBase is the trap from the spec's guard
// table. When trimming a path off a scheme-less "host:port/..." authority, the
// port must be PRESERVED. An earlier draft reset the port to "", which made the
// authority look portless and hid the very variable that decides where the
// request goes.
func TestClefRunURL_KeepsPortFromSchemeLessBase(t *testing.T) {
	got := clefRunURL("127.0.0.1:8787/ai/v1", "acct", "@cf/cloudflare/clef-flash")
	if !strings.Contains(got, "127.0.0.1:8787") {
		t.Errorf("clefRunURL dropped the port: %q", got)
	}
}

// TestClefDecide_SendsBareSelectorAndParsesAnswers is the load-bearing test for
// the model-id trap. The body must carry "clef-flash", NOT the URL id
// "@cf/cloudflare/clef-flash" and NOT a first-slash split
// "cloudflare/clef-flash" — Cloudflare's schema pattern accepts only
// "clef" or "clef-flash". A body the API rejects is indistinguishable from a
// backend that does not work, which is why this asserts the exact field.
func TestClefDecide_SendsBareSelectorAndParsesAnswers(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		_, _ = w.Write([]byte(`{
		  "model": "clef-flash",
		  "answers": {
		    "verdict":  {"type":"choice","choice":"allow","probabilities":{"allow":0.91,"deny":0.09},"confidence":0.91},
		    "relevant": {"type":"noul","noul":0.77}
		  },
		  "usage": {"input_tokens": 1200, "output_tokens": 8}
		}`))
	}))
	defer srv.Close()

	c := clefTestClient(t, srv.URL)
	resp, err := c.DecideCtx(context.Background(),
		map[string]any{"tool": "bash"},
		map[string]TypesafeQuestion{
			"verdict":  {Type: "choice", Criteria: map[string]string{"allow": "ok", "deny": "no"}},
			"relevant": {Type: "noul"},
		})
	if err != nil {
		t.Fatalf("DecideCtx: %v", err)
	}

	if sel, _ := body["model"].(string); sel != "clef-flash" {
		t.Errorf("body model = %q, want the bare selector %q", sel, "clef-flash")
	}
	if _, ok := body["state"]; !ok {
		t.Error("body has no state key")
	}
	if q, ok := body["questions"].(map[string]any); !ok || len(q) != 2 {
		t.Errorf("body questions = %#v, want 2 entries", body["questions"])
	}

	if got := resp.Answers["verdict"].Choice; got != "allow" {
		t.Errorf("choice = %q, want allow", got)
	}
	if got := resp.Answers["verdict"].Confidence; got != 0.91 {
		t.Errorf("confidence = %v, want 0.91", got)
	}
	if got := resp.Answers["relevant"].Noul; got != 0.77 {
		t.Errorf("noul = %v, want 0.77", got)
	}
	if resp.Usage.InputTokens != 1200 {
		t.Errorf("input_tokens = %d, want 1200", resp.Usage.InputTokens)
	}
	if got := resp.Model; got != "clef-flash" {
		t.Errorf("response model = %q", got)
	}
}

// TestClefChatAlwaysFails pins the decision-only contract. Clef answers typed
// questions and never generates text, so Chat must fail loudly rather than
// return empty content that a caller would treat as a real reply.
func TestClefChatAlwaysFails(t *testing.T) {
	c := newClefClient("k", "a", "@cf/cloudflare/clef-flash")
	if _, err := c.Chat(nil, nil); err != ErrClefDecisionOnly {
		t.Errorf("Chat err = %v, want ErrClefDecisionOnly", err)
	}
}

func TestClefSatisfiesDecider(t *testing.T) {
	var _ Decider = (*ClefClient)(nil)
}

func TestClefDecideCtx_CancelledContextAbortsBeforeSending(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer srv.Close()

	c := clefTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.DecideCtx(ctx, "s", map[string]TypesafeQuestion{"q": {Type: "noul"}}); err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
	if called {
		t.Error("request was sent despite a cancelled context")
	}
}

// TestClefDecideCtx_EmptyQuestionsRejected pins the fail-fast contract: a request
// with nothing to decide is a programming error, not a round trip.
func TestClefDecideCtx_EmptyQuestionsRejected(t *testing.T) {
	c := newClefClient("k", "a", "@cf/cloudflare/clef-flash")
	if _, err := c.DecideCtx(context.Background(), "s", nil); err == nil {
		t.Error("expected an error for zero questions")
	}
}

// TestClefDecideCtx_Non2xxIsProviderStatusError keeps a Cloudflare failure
// classifiable and provider-attributed rather than collapsing into a generic
// transport error — the retry and debug paths key on that.
func TestClefDecideCtx_Non2xxIsProviderStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":7000,"message":"rate limited"}]}`))
	}))
	defer srv.Close()

	c := clefTestClient(t, srv.URL)
	_, err := c.DecideCtx(context.Background(), "s", map[string]TypesafeQuestion{"q": {Type: "noul"}})
	if err == nil {
		t.Fatal("expected an error for a 429")
	}
	if !strings.Contains(err.Error(), "cloudflare-workers") {
		t.Errorf("error should name the provider, got %v", err)
	}
}

// TestClefRequestTimeoutMatchesTypeSafe keeps a slot's behaviour from depending
// on which backend it points at: a judge that times out differently per provider
// is a bug that only shows up after someone switches backends.
func TestClefRequestTimeoutMatchesTypeSafe(t *testing.T) {
	if clefRequestTimeout != typesafeRequestTimeout {
		t.Errorf("clefRequestTimeout = %v, want it to equal typesafeRequestTimeout (%v)",
			clefRequestTimeout, typesafeRequestTimeout)
	}
	if clefRequestTimeout != 30*time.Second {
		t.Errorf("clefRequestTimeout = %v, want 30s", clefRequestTimeout)
	}
}

// TestIsDecisionModel_RoutesClef pins the routing predicate, which is what makes
// the backend exist at all. cloudflare-workers serves BOTH chat models and clef,
// so the provider prefix alone is not enough — the model must be a known clef id.
func TestIsDecisionModel_RoutesClef(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"cloudflare-workers/@cf/cloudflare/clef-flash", true},
		{"cloudflare-workers/@cf/cloudflare/clef", true},
		// Bare id carries no provider, so it must not route (hijack guard).
		{"@cf/cloudflare/clef-flash", false},
		{"typesafe/jev-latest", true},
		// Same provider, not a decision model — must stay on the chat path.
		{"cloudflare-workers/@cf/zai-org/glm-4.7-flash", false},
		{"cloudflare-workers/@cf/meta/llama-3.3-70b-instruct-fp8-fast", false},
		// A clef-named model from a DIFFERENT provider must not be hijacked.
		{"someotherprovider/@cf/cloudflare/clef-flash", false},
		{"openai/gpt-5", false},
	}
	for _, tc := range tests {
		if got := isDecisionModel(tc.id); got != tc.want {
			t.Errorf("isDecisionModel(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestClefLabelIsProviderQualified pins attribution. A clef-backed judge must not
// label itself "typesafe/...", or the usage ledger books Cloudflare tokens to
// TypeSafe and per-provider spend is silently wrong.
func TestClefLabelIsProviderQualified(t *testing.T) {
	c := newClefClient("k", "a", "@cf/cloudflare/clef-flash")
	got := deciderLabel(c)
	if !strings.HasPrefix(got, "cloudflare-workers/") {
		t.Errorf("deciderLabel = %q, want a cloudflare-workers/ prefix", got)
	}
	if strings.Contains(got, "typesafe") {
		t.Errorf("deciderLabel = %q must not name typesafe", got)
	}
}

func TestDecodeClefResponse(t *testing.T) {
	bare, err := decodeClefResponse([]byte(`{"answers":{"a":{"choice":"yes"}}}`))
	if err != nil || len(bare.Answers) != 1 {
		t.Fatalf("bare: %v %+v", err, bare)
	}
	wrapped, err := decodeClefResponse([]byte(`{"result":{"answers":{"a":{"choice":"yes"}}},"success":true,"errors":[]}`))
	if err != nil || len(wrapped.Answers) != 1 {
		t.Fatalf("wrapped: %v %+v", err, wrapped)
	}
	if _, err := decodeClefResponse([]byte(`{"success":false,"errors":[{"message":"bad"}]}`)); err == nil {
		t.Fatal("success=false must error")
	}
	if _, err := decodeClefResponse([]byte(`{}`)); err == nil {
		t.Fatal("empty body must error")
	}
}
