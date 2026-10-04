package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
)

const inlineTestMainModel = "mimo-v2.5"

// inlineTestSummary satisfies validateSummary.
var inlineTestSummary = strings.Join(requiredSummarySections, "\n- x\n\n") + "\n- x\n"

type inlineTestRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func (r inlineTestRequest) isInline() bool {
	if len(r.Messages) == 0 {
		return false
	}
	last := r.Messages[len(r.Messages)-1]
	return last.Role == "user" && strings.Contains(string(last.Content), "The conversation segment is the ENTIRE conversation above this message.")
}

// inlineTestHarness records every provider request and answers each with
// respond's status/body.
func inlineTestHarness(t *testing.T, respond func(inlineTestRequest) (int, string)) (*Agent, compactRuntime, *[]inlineTestRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []inlineTestRequest
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var parsed inlineTestRequest
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Errorf("parse request body: %v", err)
		}
		mu.Lock()
		seen = append(seen, parsed)
		mu.Unlock()
		code, body := respond(parsed)
		return statusResponse(code, body), nil
	}))
	client := &GenericClient{Provider: "opencode-go", Model: inlineTestMainModel, APIKey: "k", BaseURL: "https://example.test/v1"}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:                         true,
		KeepRecentTurns:                 1,
		SummaryTimeoutSeconds:           5,
		SummaryFirstTokenTimeoutSeconds: 5,
		MaxSummaryInputTokens:           50000,
		WindowTokens:                    int(ModelWindow("opencode-go/" + inlineTestMainModel)),
		Provider:                        "opencode-go",
		Model:                           inlineTestMainModel,
	}
	if rt.WindowTokens <= 0 {
		t.Fatalf("registry has no window for opencode-go/%s", inlineTestMainModel)
	}
	return a, rt, &seen
}

func inlineTestStream(t *testing.T, content string) string {
	t.Helper()
	chunk, err := json.Marshal(map[string]any{
		"model":   inlineTestMainModel,
		"choices": []map[string]any{{"delta": map[string]string{"content": content}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(chunk) + "\n\ndata: [DONE]\n"
}

func inlineTestMessages() []Message {
	return []Message{
		{Role: "user", Content: "original ask marker-oldest"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second ask"},
		{Role: "assistant", Content: "second answer"},
		{Role: "user", Content: "recent tail"},
		{Role: "assistant", Content: "tail response"},
	}
}

func TestRunCompactUsesOneInlineRequestOnTheMainModel(t *testing.T) {
	a, rt, seen := inlineTestHarness(t, func(inlineTestRequest) (int, string) {
		return http.StatusOK, inlineTestStream(t, inlineTestSummary)
	})
	// An explicit summary model must not divert the inline request: it could
	// not reuse the main model's cached prefix.
	a.config = &config.Config{}
	a.config.Ocode.Compact.SummaryProvider = "opencode-go"
	a.config.Ocode.Compact.SummaryModel = "deepseek-v4-flash"

	res := a.runCompact(inlineTestMessages(), rt, "", true)
	if !res.OK {
		t.Fatalf("compaction failed: %v", res.Err)
	}
	if len(*seen) != 1 {
		t.Fatalf("inline compaction sent %d requests, want 1", len(*seen))
	}
	req := (*seen)[0]
	if req.Model != inlineTestMainModel {
		t.Fatalf("inline summary ran on %q, want the main model %q", req.Model, inlineTestMainModel)
	}
	if !req.isInline() {
		t.Fatal("request does not end with the inline summary instruction")
	}
	var roles []string
	transcript := false
	for _, m := range req.Messages {
		roles = append(roles, m.Role)
		if strings.Contains(string(m.Content), "marker-oldest") && m.Role == "user" && !strings.Contains(string(m.Content), "Conversation segment:") {
			transcript = true
		}
	}
	if !transcript {
		t.Fatalf("inline request did not resend the conversation as its own messages (roles %v)", roles)
	}
	if !strings.Contains(res.Summary.Content, compactionSummaryMarker) || !strings.Contains(res.Summary.Content, "## Critical Context") {
		t.Fatalf("summary message not built from the inline response: %q", res.Summary.Content)
	}
}

func TestRunCompactUsesBatchedLoopWhenConversationLeavesNoHeadroom(t *testing.T) {
	a, rt, seen := inlineTestHarness(t, func(inlineTestRequest) (int, string) {
		return http.StatusOK, inlineTestStream(t, inlineTestSummary)
	})
	a.lastInputTokens.Store(int64(rt.WindowTokens - inlineSummaryReserveTokens + 1))

	res := a.runCompact(inlineTestMessages(), rt, "", true)
	if !res.OK {
		t.Fatalf("compaction failed: %v", res.Err)
	}
	if len(*seen) == 0 {
		t.Fatal("no summary request was sent")
	}
	for i, req := range *seen {
		if req.isInline() {
			t.Fatalf("request %d is an inline summary although the conversation leaves no headroom", i)
		}
	}
}

func TestRunCompactUsesBatchedLoopAfterInlineProviderError(t *testing.T) {
	a, rt, seen := inlineTestHarness(t, func(req inlineTestRequest) (int, string) {
		if req.isInline() {
			return http.StatusBadRequest, `{"error":{"message":"prompt is too long","type":"invalid_request_error"}}`
		}
		return http.StatusOK, inlineTestStream(t, inlineTestSummary)
	})
	rt.SummaryMaxRetries = 0

	res := a.runCompact(inlineTestMessages(), rt, "", true)
	if !res.OK {
		t.Fatalf("compaction failed: %v", res.Err)
	}
	inline, batched := 0, 0
	for _, req := range *seen {
		if req.isInline() {
			inline++
		} else {
			batched++
		}
	}
	if inline == 0 || batched == 0 {
		t.Fatalf("want an inline attempt then the batched loop, got inline=%d batched=%d", inline, batched)
	}
}
