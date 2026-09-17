package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// typesafeJudgeHarness stands up a fake /systemone endpoint that replies with a
// fixed choice answer and captures the request body so tests can assert on the
// state ocode sends to Jev.
type typesafeJudgeHarness struct {
	mu    sync.Mutex
	body  map[string]any
	reply string
}

func newTypesafeJudge(t *testing.T, reply string) (*Agent, *typesafeJudgeHarness) {
	t.Helper()
	h := &typesafeJudgeHarness{reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		h.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(h.reply))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "typesafe/jev-latest"}
	a := NewAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", srv.URL)
	}
	return a, h
}

func typesafeChoiceReply(choice string, confidence float64) string {
	return typesafeReplyWithConcern(choice, confidence, "none")
}

func typesafeReplyWithConcern(choice string, confidence float64, concern string) string {
	return `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"` + choice + `","probabilities":{"allow":0.5,"deny":0.5},"confidence":` + jsonFloat(confidence) + `},"concern":{"type":"choice","choice":"` + concern + `","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":10,"output_tokens":2}}`
}

func jsonFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestConsultPermissionModelTypesafeAllow(t *testing.T) {
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	var in, out int64
	a.OnSideUsage = func(p, c, _, _ int64, _ *float64) { in, out = p, c }

	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls -la"}`), nil)
	if !allowed || !consulted {
		t.Fatalf("expected allow+consulted, got allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}
	if in != 10 || out != 2 {
		t.Fatalf("side usage not recorded: in=%d out=%d", in, out)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.body["model"] != "jev-latest" {
		t.Fatalf("model = %v", h.body["model"])
	}
	state, ok := h.body["state"].(map[string]any)
	if !ok {
		t.Fatalf("state should be an object, got %T", h.body["state"])
	}
	if state["tool"] != "bash" {
		t.Fatalf("state.tool = %v", state["tool"])
	}
	if args, _ := state["arguments"].(map[string]any); args["command"] != "ls -la" {
		t.Fatalf("state.arguments = %v", state["arguments"])
	}
	qs := h.body["questions"].(map[string]any)
	verdict := qs["verdict"].(map[string]any)
	if verdict["type"] != "choice" {
		t.Fatalf("question type = %v", verdict["type"])
	}
	criteria := verdict["criteria"].(map[string]any)
	if _, ok := criteria["allow"]; !ok {
		t.Fatalf("criteria missing allow: %v", criteria)
	}
	if _, ok := criteria["deny"]; !ok {
		t.Fatalf("criteria missing deny: %v", criteria)
	}
	concern := qs["concern"].(map[string]any)
	concernCriteria := concern["criteria"].(map[string]any)
	if len(concernCriteria) != len(typesafeConcerns) {
		t.Fatalf("concern criteria = %d, want %d", len(concernCriteria), len(typesafeConcerns))
	}
	if _, ok := concernCriteria["none"]; !ok {
		t.Fatalf("concern criteria missing none: %v", concernCriteria)
	}
}

func TestConsultPermissionModelTypesafeDeny(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("deny", 0.9, "outside_allowed_roots"))
	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls"}`), nil)
	if allowed || !consulted {
		t.Fatalf("expected deny+consulted, got allowed=%v consulted=%v", allowed, consulted)
	}
	if !strings.Contains(reason, "deny") {
		t.Fatalf("reason should name the deny verdict: %q", reason)
	}
	if !strings.Contains(reason, "outside the allowed roots") {
		t.Fatalf("reason should carry the typed concern label: %q", reason)
	}
}

func TestConsultPermissionModelTypesafeDenyWithoutConcernStillReports(t *testing.T) {
	// A reply lacking the concern answer (older server, partial response) must
	// still be a consulted deny with a readable reason.
	a, _ := newTypesafeJudge(t, `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"deny","probabilities":{"allow":0.1,"deny":0.9},"confidence":0.9}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls"}`), nil)
	if allowed || !consulted {
		t.Fatalf("expected deny+consulted, got allowed=%v consulted=%v", allowed, consulted)
	}
	if !strings.Contains(reason, "no category") {
		t.Fatalf("reason should say the model gave no category: %q", reason)
	}
}

func TestConsultPermissionModelTypesafeUnknownConcernIsQuoted(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("deny", 0.9, "something_new"))
	_, reason, _, _ := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls"}`), nil)
	if !strings.Contains(reason, `"something_new"`) {
		t.Fatalf("unknown concern should be quoted verbatim: %q", reason)
	}
}

func TestConsultPermissionModelTypesafeLowConfidenceDoesNotAllow(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.4))
	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls"}`), nil)
	if allowed {
		t.Fatal("low-confidence allow must not auto-grant")
	}
	if !consulted {
		t.Fatal("a rendered verdict counts as consulted")
	}
	if !strings.Contains(reason, "confidence") {
		t.Fatalf("reason should explain the confidence floor: %q", reason)
	}
}

func TestConsultPermissionModelTypesafeTransportFailureIsUnconsulted(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "typesafe/jev-latest"}
	a := NewAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", "http://127.0.0.1:1")
	}
	allowed, _, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ls"}`), nil)
	if allowed || consulted {
		t.Fatalf("transport failure must be unconsulted deny, got allowed=%v consulted=%v", allowed, consulted)
	}
}

func TestConsultPermissionModelTypesafeInterpreterSourceInState(t *testing.T) {
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	a.consultPermissionModel("bash", json.RawMessage(`{"command":"python3 -c 'print(1)'"}`), nil)

	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.body["state"].(map[string]any)
	interp, ok := state["interpreter"].(map[string]any)
	if !ok {
		t.Fatalf("interpreter execution should carry source in state: %v", state)
	}
	if interp["language"] != "python" {
		t.Fatalf("interpreter.language = %v", interp["language"])
	}
	src := interp["source"].(map[string]any)
	if !strings.Contains(src["text"].(string), "print(1)") {
		t.Fatalf("interpreter.source.text = %v", src["text"])
	}
}
