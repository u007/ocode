package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The local judge must satisfy the narrow Decider interface (and only that).
var _ Decider = (*DebertaClient)(nil)

// TestDeberta_IsDecisionModel pins the routing: the local judge is a decision
// backend under its own provider prefix, and the Jev and clef routes are unchanged.
func TestDeberta_IsDecisionModel(t *testing.T) {
	cases := map[string]bool{
		"deberta/deberta-v3-base-nli": true,
		"typesafe/jev-latest":         true,
		"deberta":                     false,
		"openai/gpt-4o":               false,
	}
	for model, want := range cases {
		if got := isDecisionModel(model); got != want {
			t.Errorf("isDecisionModel(%q) = %v, want %v", model, got, want)
		}
	}
	if got := DecisionBackendName("deberta/deberta-v3-base-nli"); got != "deberta" {
		t.Errorf("DecisionBackendName = %q, want deberta", got)
	}
}

// TestDeberta_NewClientNeedsNoKey: the sidecar is unauthenticated and has no
// registry base URL, so NewClient must still build the client with no credential.
func TestDeberta_NewClientNeedsNoKey(t *testing.T) {
	t.Setenv("OCODE_DEBERTA_URL", "http://127.0.0.1:9999/")
	c := NewClient(&config.Config{}, "deberta/deberta-v3-base-nli")
	d, ok := c.(*DebertaClient)
	if !ok {
		t.Fatalf("NewClient returned %T, want *DebertaClient", c)
	}
	if d.BaseURL != "http://127.0.0.1:9999" || d.Model != "deberta-v3-base-nli" {
		t.Errorf("client = %+v, want trimmed URL and bare model", d)
	}
	if got := deciderLabel(d); got != "deberta/deberta-v3-base-nli" {
		t.Errorf("deciderLabel = %q", got)
	}
}

// TestDeberta_ResolveDeciderForSlot: a judge slot configured as a deberta model
// resolves to a usable Decider, which is what "use it in place of Jev" means.
func TestDeberta_ResolveDeciderForSlot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Discovery.JudgeModel = "deberta/deberta-v3-base-nli"
	a := &Agent{config: cfg}
	dec := a.resolveDecider(slotDiscovery)
	if dec == nil {
		t.Fatal("resolveDecider returned nil for a deberta slot")
	}
	if _, ok := dec.(*DebertaClient); !ok {
		t.Errorf("resolveDecider = %T, want *DebertaClient", dec)
	}
}

// TestDeberta_DecideWireContract checks what the sidecar receives: the request
// goes to /systemone, carries the bare model id and the questions, and sends no
// Authorization header. The canned answer then decodes into TypesafeResponse.
func TestDeberta_DecideWireContract(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/systemone" {
			t.Errorf("request = %s %s, want POST /systemone", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"model":"deberta-v3-base-nli","answers":{"q1":{"type":"noul","noul":0.91}},"usage":{}}`))
	}))
	defer srv.Close()

	c := newDebertaClient("deberta-v3-base-nli", srv.URL)
	q := map[string]TypesafeQuestion{"q1": {Type: "noul", Instructions: "The query matches the doc."}}
	resp, err := c.DecideCtx(context.Background(), "some state", q)
	if err != nil {
		t.Fatalf("DecideCtx: %v", err)
	}
	if got := resp.Answers["q1"].Noul; got != 0.91 {
		t.Errorf("noul = %v, want 0.91", got)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none", gotAuth)
	}
	if gotBody["model"] != "deberta-v3-base-nli" || gotBody["state"] != "some state" {
		t.Errorf("body = %v", gotBody)
	}
	if _, ok := gotBody["questions"].(map[string]any); !ok {
		t.Errorf("questions missing from body: %v", gotBody)
	}
}

// TestDeberta_RefusalIsAnError: an over-window state gets a 422 from the sidecar.
// It must surface as an error, never as a zero answer the judge would read as a verdict.
func TestDeberta_RefusalIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"too long; refusing rather than truncating"}`))
	}))
	defer srv.Close()

	c := newDebertaClient("deberta-v3-base-nli", srv.URL)
	q := map[string]TypesafeQuestion{"q1": {Type: "score", Instructions: "x"}}
	resp, err := c.DecideCtx(context.Background(), "big", q)
	if err == nil {
		t.Fatalf("expected an error for a 422, got response %+v", resp)
	}
	if resp != nil {
		t.Errorf("response = %+v, want nil on refusal", resp)
	}
}

// TestDeberta_SidecarDownNamesThePlugin: a connection failure should say how to
// start the judge, since that is the only fix a user can act on.
func TestDeberta_SidecarDownNamesThePlugin(t *testing.T) {
	c := newDebertaClient("deberta-v3-base-nli", "http://127.0.0.1:1")
	_, err := c.DecideCtx(context.Background(), "s", map[string]TypesafeQuestion{"q": {Type: "score", Instructions: "x"}})
	if err == nil || !strings.Contains(err.Error(), "plugins/deberta-judge") {
		t.Fatalf("err = %v, want a message naming plugins/deberta-judge", err)
	}
}

// TestDeberta_ChatIsDecisionOnly: the judge must never reach a chat path.
func TestDeberta_ChatIsDecisionOnly(t *testing.T) {
	_, err := newDebertaClient("deberta-v3-base-nli", "http://x").Chat(nil, nil)
	if !errors.Is(err, ErrDebertaDecisionOnly) {
		t.Fatalf("Chat err = %v, want ErrDebertaDecisionOnly", err)
	}
}
