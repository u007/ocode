package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// sampleSearchRequest is a three-file grep result set with deterministic paths
// and summaries that carry line numbers.
func sampleSearchRequest(intent string) tool.SearchJudgeRequest {
	return tool.SearchJudgeRequest{
		Tool:   "grep",
		Intent: intent,
		Query: map[string]string{
			"pattern": "foo",
			"path":    "internal",
			"include": "*.go",
		},
		Results: []tool.SearchResult{
			{Path: "internal/a.go", Summary: "L1:foo\nL9:bar", Count: 2},
			{Path: "internal/b.go", Summary: "L3:foo", Count: 1},
			{Path: "docs/c.md", Summary: "L5:foo", Count: 1},
		},
	}
}

func searchResultPaths(rs []tool.SearchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}

func TestSearchJudgeStateShape(t *testing.T) {
	state := buildSearchJudgeState(sampleSearchRequest("find the foo helper"))

	if state["request"] != "find the foo helper" {
		t.Fatalf("request = %v", state["request"])
	}
	if state["tool"] != "grep" {
		t.Fatalf("tool = %v", state["tool"])
	}
	wantQuery := map[string]string{"pattern": "foo", "path": "internal", "include": "*.go"}
	if got, ok := state["query"].(map[string]string); !ok || !reflect.DeepEqual(got, wantQuery) {
		t.Fatalf("query = %#v, want %#v", state["query"], wantQuery)
	}
	cands, ok := state["candidates"].([]map[string]any)
	if !ok || len(cands) != 3 {
		t.Fatalf("candidates = %v", state["candidates"])
	}
	first := cands[0]
	if first["id"] != "internal/a.go" || first["path"] != "internal/a.go" {
		t.Fatalf("candidate id/path = %v/%v", first["id"], first["path"])
	}
	if first["count"] != 2 {
		t.Fatalf("candidate count = %v, want 2", first["count"])
	}
	if first["summary"] != "L1:foo\nL9:bar" {
		t.Fatalf("candidate summary = %q", first["summary"])
	}
}

func TestSearchJudgeStateQueryWhitelist(t *testing.T) {
	req := sampleSearchRequest("x")
	req.Query["token"] = "SUPER-SECRET"
	req.Query["api_key"] = "ALSO-SECRET"

	state := buildSearchJudgeState(req)
	got, _ := state["query"].(map[string]string)
	if len(got) != 3 {
		t.Fatalf("query = %#v, want only pattern/path/include", got)
	}
	for _, secret := range []string{"token", "api_key"} {
		if _, ok := got[secret]; ok {
			t.Fatalf("secret key %q leaked into the judge query: %#v", secret, got)
		}
	}
}

func TestSearchJudgeSummaryCapKeepsLineNumbers(t *testing.T) {
	if len("L10:alpha") == 0 {
		t.Fatal("sanity")
	}
	long := "L10:alpha\nL20:beta\n" + strings.Repeat("x", 800)
	req := sampleSearchRequest("x")
	req.Results = []tool.SearchResult{{Path: "a.go", Summary: long, Count: 2}}

	state := buildSearchJudgeState(req)
	cands := state["candidates"].([]map[string]any)
	summary, _ := cands[0]["summary"].(string)
	if !strings.HasPrefix(summary, "L10:alpha\nL20:beta") {
		t.Fatalf("line numbers must survive into the summary, got %q", summary)
	}
	if !strings.HasSuffix(summary, "…(truncated)") {
		t.Fatalf("long summary must be truncated, got %q", summary)
	}
	if len(summary) > searchJudgeSummaryCap+len("…(truncated)") {
		t.Fatalf("summary over cap: %d", len(summary))
	}
}

func TestSearchJudgeInstructionsReferenceCandidateAndGuardAgainstFalseVetos(t *testing.T) {
	instr := searchJudgeInstructions(tool.SearchResult{Path: "internal/db/migrate_test.go"}, 2)
	if !strings.Contains(instr, "`candidates[2]`") {
		t.Fatalf("instructions must reference the candidate state path, got %q", instr)
	}
	if !strings.Contains(instr, "`internal/db/migrate_test.go`") {
		t.Fatalf("instructions must name the candidate path, got %q", instr)
	}
	low := strings.ToLower(instr)
	if !strings.Contains(low, "test") || !strings.Contains(low, "fixture") {
		t.Fatalf("instructions must guard against vetoing test/fixture files, got %q", instr)
	}
	if !strings.Contains(low, "substring") {
		t.Fatalf("instructions must guard against the substring collision, got %q", instr)
	}
}

func TestSearchJudgeKeepsSlightRelevanceSkipsDifferentScope(t *testing.T) {
	a, h := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"internal/a.go": 0.90, // clearly relevant
		"internal/b.go": 0.20, // different scope -> vetoed
		"docs/c.md":     0.52, // slightly relevant -> kept at the 0.5 floor
	}), 0)

	judge := a.searchResultJudge()
	if judge == nil {
		t.Fatal("searchResultJudge should resolve with a keyed typesafe factory")
	}
	req := sampleSearchRequest("find the foo helper")
	keep, vetoed, err := judge(req)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if vetoed != 1 {
		t.Fatalf("vetoed = %d, want 1", vetoed)
	}
	if got, want := strings.Join(searchResultPaths(keep), ","), "internal/a.go,docs/c.md"; got != want {
		t.Fatalf("kept = %v, want %v", got, want)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.requests != 1 {
		t.Fatalf("requests = %d, want 1", h.requests)
	}
	qs, ok := h.body["questions"].(map[string]any)
	if !ok || len(qs) != 3 {
		t.Fatalf("expected 3 questions, got %v", h.body["questions"])
	}
	state, _ := h.body["state"].(map[string]any)
	if state["tool"] != "grep" || state["request"] != "find the foo helper" {
		t.Fatalf("state tool/request = %v/%v", state["tool"], state["request"])
	}
}

func TestSearchJudgeMissingAnswerKept(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"internal/a.go": 0.90,
		// internal/b.go omitted
		"docs/c.md": 0.10,
	}), 0)

	judge := a.searchResultJudge()
	keep, _, err := judge(sampleSearchRequest("q"))
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	// b.go has no answer -> kept (fail-open); c.md is below floor -> vetoed.
	if got, want := strings.Join(searchResultPaths(keep), ","), "internal/a.go,internal/b.go"; got != want {
		t.Fatalf("kept = %v, want %v", got, want)
	}
}

func TestSearchJudgeTransportErrorFailsOpen(t *testing.T) {
	a, _ := newDiscoveryJudgeAgent(t, "", http.StatusInternalServerError)
	judge := a.searchResultJudge()
	req := sampleSearchRequest("q")
	keep, vetoed, err := judge(req)
	if err == nil {
		t.Fatal("expected the judge error to be surfaced so the tool can disclose it")
	}
	if vetoed != 0 {
		t.Fatalf("vetoed = %d, want 0 on failure", vetoed)
	}
	if len(keep) != len(req.Results) {
		t.Fatalf("fail-open must return every result, got %d of %d", len(keep), len(req.Results))
	}
}

func TestSearchJudgeEmptyIntentSkipsDecide(t *testing.T) {
	a, h := newDiscoveryJudgeAgent(t, typesafeNoulReply(map[string]float64{
		"internal/a.go": 0.0, // would veto if the judge ran
	}), 0)

	var mu sync.Mutex
	var lines []string
	prev := DebugAppend
	t.Cleanup(func() { DebugAppend = prev })
	DebugAppend = func(kind, msg string) {
		mu.Lock()
		lines = append(lines, kind+": "+msg)
		mu.Unlock()
	}

	judge := a.searchResultJudge()
	req := sampleSearchRequest("")
	keep, vetoed, err := judge(req)
	if err != nil {
		t.Fatalf("empty intent must not error: %v", err)
	}
	if vetoed != 0 {
		t.Fatalf("vetoed = %d, want 0", vetoed)
	}
	if len(keep) != len(req.Results) {
		t.Fatalf("empty intent must be unfiltered, got %d of %d", len(keep), len(req.Results))
	}
	h.mu.Lock()
	requests := h.requests
	h.mu.Unlock()
	if requests != 0 {
		t.Fatalf("empty intent issued %d judge request(s), want 0", requests)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "intent-missing") {
		t.Fatalf("expected an intent-missing debug line, got:\n%s", joined)
	}
}

func TestSearchJudgeTimeoutBudgetIsShort(t *testing.T) {
	if searchJudgeTimeout > 5*time.Second {
		t.Fatalf("searchJudgeTimeout = %s, want the short 4s budget (not the 30s Decide default)", searchJudgeTimeout)
	}
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { once.Do(func() { close(release) }); srv.Close() })

	prevClient := newClientFn
	t.Cleanup(func() { newClientFn = prevClient })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("test-key", "jev-latest", srv.URL)
	}
	prevTimeout := searchJudgeTimeout
	t.Cleanup(func() { searchJudgeTimeout = prevTimeout })
	searchJudgeTimeout = 150 * time.Millisecond

	a := NewAgent(nil, nil, &config.Config{}, nil)
	judge := a.searchResultJudge()
	if judge == nil {
		t.Fatal("searchResultJudge should resolve with a keyed typesafe factory")
	}
	req := sampleSearchRequest("q")

	start := time.Now()
	keep, _, err := judge(req)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a stalled judge must surface an error (fail-open with a footer)")
	}
	if len(keep) != len(req.Results) {
		t.Fatalf("fail-open must return every result, got %d of %d", len(keep), len(req.Results))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("judge took %s, want it bounded by the short searchJudgeTimeout", elapsed)
	}
}

func TestSearchResultJudgeNilWhenNotConnected(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })

	a := NewAgent(nil, nil, &config.Config{}, nil)

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("", "jev-latest", "http://127.0.0.1:1")
	}
	if got := a.searchResultJudge(); got != nil {
		t.Fatal("keyless typesafe client must not enable the search judge")
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &GenericClient{Provider: "openai", Model: "gpt"}
	}
	if got := a.searchResultJudge(); got != nil {
		t.Fatal("non-typesafe client must not enable the search judge")
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }
	if got := a.searchResultJudge(); got != nil {
		t.Fatal("nil client must not enable the search judge")
	}
}

// judgeProbeTool captures the search judge attached to its execution context,
// proving the agent wiring (not just the context helpers).
type judgeProbeTool struct {
	name string
	seen tool.SearchResultJudge
}

func (p *judgeProbeTool) Name() string        { return p.name }
func (p *judgeProbeTool) Description() string { return "probe" }
func (p *judgeProbeTool) Parallel() bool      { return false }
func (p *judgeProbeTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": p.name}
}
func (p *judgeProbeTool) Execute(json.RawMessage) (string, error) { return "probe", nil }
func (p *judgeProbeTool) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	p.seen = tool.SearchJudgeFromContext(ctx)
	return "probe", nil
}

func TestSearchJudgeAttachedToToolContextOnlyWhenConnected(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })

	a := NewAgent(nil, nil, &config.Config{}, nil)
	a.toolBatchDelay = 0

	// Not connected: no judge attached, dispatch still works.
	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }
	probe := &judgeProbeTool{name: "grep"}
	a.tools[probe.Name()] = probe
	if _, err := a.executeToolCallWithContext(context.Background(), probe.Name(), json.RawMessage(`{}`), nil, ""); err != nil {
		t.Fatalf("dispatch with no judge: %v", err)
	}
	if probe.seen != nil {
		t.Fatal("nil judge must not be attached to the tool context")
	}

	// Connected: the keyed client wires a judge onto a search tool's context.
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("test-key", "jev-latest", "http://127.0.0.1:1")
	}
	a.disco = nil // bypass the cached negative resolution from the first call
	probe2 := &judgeProbeTool{name: "grep"}
	a.tools[probe2.Name()] = probe2
	if _, err := a.executeToolCallWithContext(context.Background(), probe2.Name(), json.RawMessage(`{}`), nil, ""); err != nil {
		t.Fatalf("dispatch with judge: %v", err)
	}
	if probe2.seen == nil {
		t.Fatal("a connected typesafe client must attach a search judge to a search tool's context")
	}

	// A non-search tool must not resolve the judge factory at all: even with a
	// connected provider, a bash dispatch must stay judge-free.
	var factoryCalls int
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		factoryCalls++
		return newTypesafeClient("test-key", "jev-latest", "http://127.0.0.1:1")
	}
	a.disco = nil
	bash := &judgeProbeTool{name: "bash"}
	a.tools[bash.Name()] = bash
	if _, err := a.executeToolCallWithContext(context.Background(), bash.Name(), json.RawMessage(`{}`), nil, ""); err != nil {
		t.Fatalf("dispatch non-search tool: %v", err)
	}
	if bash.seen != nil {
		t.Fatal("a non-search tool must not receive a search judge")
	}
	if factoryCalls != 0 {
		t.Fatalf("non-search dispatch resolved the judge factory %d time(s), want 0", factoryCalls)
	}
}
