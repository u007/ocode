package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// searchSeedMessages builds a transcript whose matches are spread across the
// fields the matcher must cover, plus a decoy that shares no term.
func searchSeedMessages() []agent.Message {
	withCall := agent.Message{Role: "assistant", Content: "calling the tool"}
	withCall.ToolCalls = []agent.ToolCall{{ID: "c1", Type: "function"}}
	withCall.ToolCalls[0].Function.Name = "NEEDLE_tool"
	withCall.ToolCalls[0].Function.Arguments = `{"path":"a.go"}`

	return []agent.Message{
		{Role: "user", Content: "plain opening line"},                              // 0
		{Role: "assistant", Content: "this mentions needle in content"},            // 1
		{Role: "assistant", Content: "", ReasoningContent: "reasoning has NEEDLE"}, // 2
		{Role: "assistant", Content: "", Notice: "a NEEDLE notice"},                // 3
		{Role: "tool", Content: "tool output without the term", ToolID: "c1"},      // 4
		withCall, // 5 tool-call name matches
		{Role: "user", Content: "totally unrelated tail"}, // 6
	}
}

// searchHandler seeds one session under a temp project and returns the handler.
// The project must be registered with the handler (newTestProjectStore) because
// Resolve only finds sessions under a KNOWN project root.
func searchHandler(t *testing.T, msgs []agent.Message) (*Handler, string) {
	t.Helper()
	workDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir)

	id := session.NewSessionID()
	if err := session.SaveForDir(workDir, id, "search fixture", msgs, nil); err != nil {
		t.Fatalf("save session: %v", err)
	}
	h.sessions.Register(id, workDir)
	return h, id
}

func doSearch(t *testing.T, h *Handler, id, query string) sessionSearchResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sessions/"+id+"/search?q="+query, nil)
	h.HandleSearchSession(rec, req, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp sessionSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	return resp
}

// TestSearchSessionCoversAllMatchedFields pins that the server-side matcher
// looks at exactly the same fields the web find bar does (content, reasoning,
// notice, tool-call name/args). A field dropped here silently makes the web
// find bar miss hits the TUI finds.
func TestSearchSessionCoversAllMatchedFields(t *testing.T) {
	h, id := searchHandler(t, searchSeedMessages())

	resp := doSearch(t, h, id, "needle")
	want := []int{1, 2, 3, 5} // content, reasoning, notice, tool-call NAME
	if fmt.Sprint(resp.Indices) != fmt.Sprint(want) {
		t.Errorf("indices = %v, want %v (all matched fields)", resp.Indices, want)
	}
	if resp.Total != len(want) {
		t.Errorf("total = %d, want %d", resp.Total, len(want))
	}
	if resp.Truncated {
		t.Errorf("truncated = true, want false")
	}
	if resp.Scanned != 7 {
		t.Errorf("scanned = %d, want 7", resp.Scanned)
	}
}

// TestSearchSessionMatchesToolCallArguments covers the other half of the
// tool-call surface (arguments), which the web matcher also searches.
func TestSearchSessionMatchesToolCallArguments(t *testing.T) {
	h, id := searchHandler(t, searchSeedMessages())

	resp := doSearch(t, h, id, "a.go")
	if fmt.Sprint(resp.Indices) != fmt.Sprint([]int{5}) {
		t.Errorf("indices = %v, want [5] (tool-call arguments)", resp.Indices)
	}
}

// TestSearchSessionIsCaseInsensitive pins the lowercase normalization.
func TestSearchSessionIsCaseInsensitive(t *testing.T) {
	h, id := searchHandler(t, searchSeedMessages())

	upper := doSearch(t, h, id, "NEEDLE")
	lower := doSearch(t, h, id, "needle")
	if fmt.Sprint(upper.Indices) != fmt.Sprint(lower.Indices) {
		t.Errorf("case sensitivity differs: %v vs %v", upper.Indices, lower.Indices)
	}
	if len(lower.Indices) == 0 {
		t.Fatal("expected matches")
	}
}

// TestSearchSessionIndicesAlignWithPagination is the linchpin: the client maps a
// search index onto its loaded window by paging older history with
// `offset = loaded`. That math is only sound if the endpoint's indices are the
// same positions PaginatedLoad slices. This asserts the correspondence directly
// by fetching pages and locating the reported index.
func TestSearchSessionIndicesAlignWithPagination(t *testing.T) {
	h, id := searchHandler(t, searchSeedMessages())
	resp := doSearch(t, h, id, "needle")

	entry, err := h.sessions.Resolve(id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, idx := range resp.Indices {
		// Load the page that CONTAINS idx: skip everything after it.
		offset := resp.Scanned - 1 - idx
		page, total, err := session.PaginatedLoad(entry.ProjectRoot, id, 1, offset)
		if err != nil {
			t.Fatalf("paginated load: %v", err)
		}
		if total != resp.Scanned {
			t.Fatalf("page total = %d, want %d", total, resp.Scanned)
		}
		if len(page) != 1 {
			t.Fatalf("page len = %d, want 1", len(page))
		}
		if !agent.MessageMatchesQuery(page[0], "needle") {
			t.Errorf("page at index %d does not match — search index != pagination index", idx)
		}
	}
}

// TestSearchSessionEmptyQueryIs400 pins the required-param contract.
func TestSearchSessionEmptyQueryIs400(t *testing.T) {
	h, id := searchHandler(t, searchSeedMessages())

	rec := httptest.NewRecorder()
	h.HandleSearchSession(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/search", nil), id)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestSearchSessionUnknownSession404s pins that a missing session is a clean
// 404 rather than an empty successful result.
func TestSearchSessionUnknownSession404s(t *testing.T) {
	workDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(workDir)

	rec := httptest.NewRecorder()
	id := session.NewSessionID()
	h.HandleSearchSession(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/search?q=x", nil), id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestSearchSessionRespectsLimitAndKeepsTotalExact pins that the index cap does
// not distort the reported Total — the counter shows the real hit count even
// when only the first N indices are returned.
func TestSearchSessionRespectsLimitAndKeepsTotalExact(t *testing.T) {
	msgs := make([]agent.Message, 0, 10)
	for i := 0; i < 10; i++ {
		msgs = append(msgs, agent.Message{Role: "user", Content: "needle here"})
	}
	h, id := searchHandler(t, msgs)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sessions/"+id+"/search?q=needle&limit=3", nil)
	h.HandleSearchSession(rec, req, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp sessionSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Indices) != 3 {
		t.Errorf("indices len = %d, want the cap of 3", len(resp.Indices))
	}
	if resp.Total != 10 {
		t.Errorf("total = %d, want the exact count 10 even when capped", resp.Total)
	}
	if !resp.Truncated {
		t.Errorf("truncated = false, want true when the cap was hit")
	}
}

// TestAgentMessageMatchesQueryEmptyNeedle pins that an empty needle never
// matches (guards a caller that forgets to reject "").
func TestAgentMessageMatchesQueryEmptyNeedle(t *testing.T) {
	msg := agent.Message{Role: "user", Content: "anything"}
	if agent.MessageMatchesQuery(msg, "") {
		t.Error("empty needle should not match")
	}
}
