package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// discoveryJudgeHarness is a fake /systemone endpoint for the discovery judge:
// it captures every request body and replies with a canned answer set.
type discoveryJudgeHarness struct {
	mu       sync.Mutex
	body     map[string]any
	reply    string
	status   int
	requests int
}

// newDiscoveryJudgeAgent returns an agent whose newClientFn builds a TypeSafe
// client pointed at a fake /systemone server. The server replies with reply, or
// status when status != 0.
func newDiscoveryJudgeAgent(t *testing.T, reply string, status int) (*Agent, *discoveryJudgeHarness) {
	t.Helper()
	h := &discoveryJudgeHarness{reply: reply, status: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		h.body = body
		if h.status != 0 {
			http.Error(w, "boom", h.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(h.reply))
	}))
	t.Cleanup(srv.Close)

	a := NewAgent(nil, nil, &config.Config{}, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("test-key", "jev-latest", srv.URL)
	}
	return a, h
}

// typesafeNoulReply builds a /systemone reply with one noul per entry.
func typesafeNoulReply(nouls map[string]float64) string {
	answers := make(map[string]any, len(nouls))
	for id, n := range nouls {
		answers[id] = map[string]any{"type": "noul", "noul": n}
	}
	b, _ := json.Marshal(map[string]any{
		"model":   "jev-latest",
		"answers": answers,
		"usage":   map[string]any{"input_tokens": 11, "output_tokens": 3},
	})
	return string(b)
}

func discoveryJudgeCandidates() []discovery.Doc {
	return []discovery.Doc{
		{ID: "skill:a", Kind: "skill", Name: "a", Text: "do a"},
		{ID: "skill:b", Kind: "skill", Name: "b", Text: "do b"},
		{ID: "md:c", Name: "c", Kind: "md", Text: "doc c"},
	}
}

func docIDList(docs []discovery.Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

func TestDiscoveryJudgeStateShape(t *testing.T) {
	long := strings.Repeat("x", 1500)
	candidates := []discovery.Doc{
		{ID: "skill:a", Kind: "skill", Name: "a", Text: long},
		{ID: "md:b", Kind: "md", Name: "b", Text: "short"},
	}
	tail := make([]Message, 0, 8)
	for i := 0; i < 8; i++ {
		tail = append(tail, Message{Role: "assistant", Content: "m" + string(rune('0'+i))})
	}

	state := buildDiscoveryJudgeState(tail, "the request", candidates)
	if state["request"] != "the request" {
		t.Fatalf("request = %v", state["request"])
	}
	entries, ok := state["transcript_tail"].([]map[string]any)
	if !ok {
		t.Fatalf("transcript_tail type = %T", state["transcript_tail"])
	}
	if len(entries) != 6 {
		t.Fatalf("transcript_tail should be bounded to 6, got %d", len(entries))
	}
	if entries[0]["content"] != "m2" || entries[5]["content"] != "m7" {
		t.Fatalf("transcript_tail should be the last 6 messages, got %v", entries)
	}

	cands, ok := state["candidates"].([]map[string]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("candidates = %v", state["candidates"])
	}
	if cands[0]["id"] != "skill:a" || cands[0]["kind"] != "skill" || cands[0]["name"] != "a" {
		t.Fatalf("candidate entry missing id/kind/name: %v", cands[0])
	}
	if got := len(cands[0]["summary"].(string)); got != discoveryJudgeSummaryCap {
		t.Fatalf("summary should be capped at %d, got %d", discoveryJudgeSummaryCap, got)
	}
	if cands[1]["summary"] != "short" {
		t.Fatalf("short summary must pass through, got %v", cands[1]["summary"])
	}
}

func TestDiscoveryJudgeKeepsAboveThreshold(t *testing.T) {
	a, h := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"skill:a": 0.95,
		"skill:b": 0.10,
		"md:c":    0.85,
	}), 0)
	var in, out int64
	a.OnSideUsage = func(p, c, _, _ int64, _ *float64) { in, out = p, c }

	client := a.discoveryJudgeClient()
	if client == nil {
		t.Fatal("judge client should resolve with a keyed typesafe factory")
	}
	keep, err := a.judgeDiscoveryCandidates(client, []Message{{Role: "user", Content: "do a"}}, "do a", discoveryJudgeCandidates())
	if err != nil {
		t.Fatalf("judge error: %v", err)
	}
	got := docIDList(keep)
	want := []string{"skill:a", "md:c"} // >= 0.85 floor; 0.10 vetoed
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keep = %v want %v", got, want)
	}
	if in != 11 || out != 3 {
		t.Fatalf("side usage not recorded: in=%d out=%d", in, out)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	qs, ok := h.body["questions"].(map[string]any)
	if !ok || len(qs) != 3 {
		t.Fatalf("expected exactly 3 questions, got %v", h.body["questions"])
	}
	for _, d := range discoveryJudgeCandidates() {
		q, ok := qs[d.ID].(map[string]any)
		if !ok {
			t.Fatalf("missing question for %s", d.ID)
		}
		if q["type"] != "noul" {
			t.Fatalf("question %s type = %v, want noul", d.ID, q["type"])
		}
		instr, _ := q["instructions"].(string)
		if !strings.Contains(instr, "candidates[") {
			t.Fatalf("question %s should reference its state path, got %q", d.ID, instr)
		}
	}
}

func TestDiscoveryJudgeMissingAnswerKept(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"skill:a": 0.95,
		"skill:b": 0.10,
		// md:c omitted
	}), 0)
	client := a.discoveryJudgeClient()
	keep, err := a.judgeDiscoveryCandidates(client, nil, "q", discoveryJudgeCandidates())
	if err != nil {
		t.Fatalf("judge error: %v", err)
	}
	got := docIDList(keep)
	want := []string{"skill:a", "md:c"} // missing answer kept (fail-open)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keep = %v want %v", got, want)
	}
}

func TestDiscoveryJudgeTransportError(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, "", http.StatusInternalServerError)
	client := a.discoveryJudgeClient()
	keep, err := a.judgeDiscoveryCandidates(client, nil, "q", discoveryJudgeCandidates())
	if err == nil {
		t.Fatal("expected an error from a 500 response")
	}
	if keep != nil {
		t.Fatalf("keep must be nil on transport error, got %v", docIDList(keep))
	}
}

func TestDiscoveryJudgeRecordsSideUsage(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{"skill:a": 0.99}), 0)
	var in, out int64
	a.OnSideUsage = func(p, c, _, _ int64, _ *float64) { in, out = p, c }
	client := a.discoveryJudgeClient()
	if _, err := a.judgeDiscoveryCandidates(client, nil, "q", discoveryJudgeCandidates()[:1]); err != nil {
		t.Fatal(err)
	}
	if in != 11 || out != 3 {
		t.Fatalf("side usage = in=%d out=%d, want 11/3", in, out)
	}
}

func TestDiscoveryJudgeClientNilWhenNotConnected(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })

	a := NewAgent(nil, nil, &config.Config{}, nil)

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("", "jev-latest", "http://127.0.0.1:1")
	}
	if got := a.discoveryJudgeClient(); got != nil {
		t.Fatalf("keyless typesafe client must not be the judge: %+v", got)
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &GenericClient{Provider: "openai", Model: "gpt"}
	}
	if got := a.discoveryJudgeClient(); got != nil {
		t.Fatalf("non-typesafe client must not be the judge: %+v", got)
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }
	if got := a.discoveryJudgeClient(); got != nil {
		t.Fatalf("nil client must not be the judge: %+v", got)
	}
}
