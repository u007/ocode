package agent

import (
	"net/http"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/knowledge"
)

// docSearchJudgeDocs returns three docs whose paths are unique and whose rank
// order is deterministic (path ascending on a tie).
func docSearchJudgeDocs() []*knowledge.Doc {
	return []*knowledge.Doc{
		{Path: "concepts/alpha.md", Title: "Alpha", Type: "concept", Description: "about alpha"},
		{Path: "concepts/beta.md", Title: "Beta", Type: "concept", Description: "about beta"},
		{Path: "concepts/gamma.md", Title: "Gamma", Type: "concept", Description: "about gamma"},
	}
}

func docSearchPaths(docs []*knowledge.Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Path
	}
	return out
}

func TestDocSearchJudgeStateShape(t *testing.T) {
	long := strings.Repeat("x", 1500)
	docs := []*knowledge.Doc{
		{Path: "a.md", Title: "A", Type: "concept", Description: "desc a", Tags: []string{"x", "y"}, Body: long},
		{Path: "b.md", Title: "B"},
	}

	state := buildDocSearchJudgeState("the query", docs)
	if state["request"] != "the query" {
		t.Fatalf("request = %v", state["request"])
	}
	cands, ok := state["candidates"].([]map[string]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("candidates = %v", state["candidates"])
	}
	if cands[0]["id"] != "a.md" || cands[0]["name"] != "A" || cands[0]["type"] != "concept" {
		t.Fatalf("candidate entry missing id/name/type: %v", cands[0])
	}
	tags, ok := cands[0]["tags"].([]string)
	if !ok || len(tags) != 2 {
		t.Fatalf("candidate tags = %v", cands[0]["tags"])
	}
	summary, _ := cands[0]["summary"].(string)
	if !strings.HasSuffix(summary, "…(truncated)") {
		t.Fatalf("long summary must be truncated, got %d chars", len(summary))
	}
	if len(summary) > docSearchJudgeSummaryCap+len("…(truncated)") {
		t.Fatalf("summary over cap: %d", len(summary))
	}
	if _, has := cands[1]["summary"]; has {
		t.Fatalf("doc with no description/body must omit summary, got %v", cands[1]["summary"])
	}
}

func TestDocSearchJudgeKeepsSlightRelevanceSkipsDifferentScope(t *testing.T) {
	a, h := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"concepts/alpha.md": 0.90, // clearly relevant
		"concepts/beta.md":  0.52, // slightly relevant -> kept at the 0.5 floor
		"concepts/gamma.md": 0.20, // different scope -> skipped
	}), 0)
	var in, out int64
	a.OnSideUsage = func(p, c, _, _ int64, _ *float64) { in, out = p, c }

	client := a.discoveryJudgeClient()
	if client == nil {
		t.Fatal("judge client should resolve with a keyed typesafe factory")
	}
	keep, err := a.judgeDocSearchResults(client, "alpha beta", docSearchJudgeDocs())
	if err != nil {
		t.Fatalf("judge error: %v", err)
	}
	got := docSearchPaths(keep)
	want := []string{"concepts/alpha.md", "concepts/beta.md"}
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
	for _, d := range docSearchJudgeDocs() {
		q, ok := qs[d.Path].(map[string]any)
		if !ok {
			t.Fatalf("missing question for %s", d.Path)
		}
		if q["type"] != "noul" {
			t.Fatalf("question %s type = %v, want noul", d.Path, q["type"])
		}
		instr, _ := q["instructions"].(string)
		if !strings.Contains(instr, "candidates[") {
			t.Fatalf("question %s should reference its state path, got %q", d.Path, instr)
		}
	}
}

func TestDocSearchJudgeMissingAnswerKept(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"concepts/alpha.md": 0.90,
		"concepts/beta.md":  0.10,
		// gamma omitted
	}), 0)
	client := a.discoveryJudgeClient()
	keep, err := a.judgeDocSearchResults(client, "q", docSearchJudgeDocs())
	if err != nil {
		t.Fatalf("judge error: %v", err)
	}
	got := docSearchPaths(keep)
	want := []string{"concepts/alpha.md", "concepts/gamma.md"} // missing answer kept (fail-open)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keep = %v want %v", got, want)
	}
}

func TestDocSearchJudgeTransportError(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, "", http.StatusInternalServerError)
	client := a.discoveryJudgeClient()
	keep, err := a.judgeDocSearchResults(client, "q", docSearchJudgeDocs())
	if err == nil {
		t.Fatal("expected an error from a 500 response")
	}
	if keep != nil {
		t.Fatalf("keep must be nil on transport error, got %v", docSearchPaths(keep))
	}
}

func TestDocSearchJudgeCallbackFailsOpen(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, "", http.StatusInternalServerError)
	judge := a.docSearchJudge()
	if judge == nil {
		t.Fatal("docSearchJudge should resolve with a keyed typesafe factory")
	}
	docs := docSearchJudgeDocs()
	got, err := judge("q", docs)
	if err != nil {
		t.Fatalf("callback must never surface the judge error: %v", err)
	}
	if len(got) != len(docs) {
		t.Fatalf("callback must fail open (all %d docs), got %d", len(docs), len(got))
	}
}

func TestDocSearchJudgeNilWhenNotConnected(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })

	a := NewAgent(nil, nil, &config.Config{}, nil)

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("", "jev-latest", "http://127.0.0.1:1")
	}
	if got := a.docSearchJudge(); got != nil {
		t.Fatal("keyless typesafe client must not enable the doc_search judge")
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &GenericClient{Provider: "openai", Model: "gpt"}
	}
	if got := a.docSearchJudge(); got != nil {
		t.Fatal("non-typesafe client must not enable the doc_search judge")
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }
	if got := a.docSearchJudge(); got != nil {
		t.Fatal("nil client must not enable the doc_search judge")
	}
}

func TestRelevanceJudgeFloorIsLenient(t *testing.T) {
	if relevanceJudgeMinConfidenceDefault != 0.5 {
		t.Fatalf("relevance floor = %v, want 0.5", relevanceJudgeMinConfidenceDefault)
	}
	if relevanceJudgeMinConfidenceDefault >= autoJudgeMinConfidenceDefault {
		t.Fatalf("relevance floor %v must be below the high-stakes permission floor %v",
			relevanceJudgeMinConfidenceDefault, autoJudgeMinConfidenceDefault)
	}
}
