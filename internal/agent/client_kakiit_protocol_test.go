package agent

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/pricing"
)

// kakiitProtocolFixture is a canned /v1/models body used to seed the live
// cache for routing tests (no network).
const kakiitProtocolFixture = `{
  "data": [
    {"id":"claude-sonnet-4-6","context_length":200000,
     "pricing":{"prompt":"0.000003","completion":"0.000015"},
     "x_kakiit_protocol":"anthropic-messages","x_kakiit_free":false},
    {"id":"gpt-5.6-luna","context_length":400000,
     "pricing":{"prompt":"0.00000125","completion":"0.000010"},
     "x_kakiit_protocol":"openai-responses","x_kakiit_free":true},
    {"id":"deepseek-v4-flash","context_length":128000,
     "pricing":{"prompt":"0.00000027","completion":"0.0000011"},
     "x_kakiit_protocol":"openai-chat","x_kakiit_free":false},
    {"id":"mystery-model","context_length":8192,
     "x_kakiit_free":false}
  ]
}`

// seedKakiitProtocols installs the routing fixture into the live cache.
func seedKakiitProtocols(t *testing.T) {
	t.Helper()
	resetKakiitCache(t)
	seedKakiitCacheForTest(t, parseKakiitFixtureEntries(t, kakiitProtocolFixture))
}

func parseKakiitFixtureEntries(t *testing.T, fixture string) []kakiitModelInfo {
	t.Helper()
	return unmarshalKakiitModelsForTest(t, []byte(fixture))
}

// anthropicOKStream is a minimal successful Anthropic Messages SSE stream.
const anthropicOKStream = "data: {\"type\":\"message_start\",\"message\":{\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
	"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n\n" +
	"data: {\"type\":\"message_stop\"}\n\n"

// responsesOKStream is a minimal successful Responses SSE stream.
const responsesOKStream = "event: response.created\n" +
	"data: {\"type\":\"response.created\",\"model\":\"m\"}\n\n" +
	"event: response.output_text.delta\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
	"event: response.completed\n" +
	"data: {\"type\":\"response.completed\",\"model\":\"m\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
	"data: [DONE]\n"

// flatTool is the flat built-in tool shape chatAnthropic requires.
func flatTool() map[string]interface{} {
	return map[string]interface{}{
		"name":        "preview_open",
		"description": "Open a preview",
		"parameters": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

// TestKakiitAnthropicModelRoutesToMessagesPathWithAnthropicToolShape pins the
// core routing contract: an anthropic-messages kakiit model lands on
// /v1/messages (NOT /chat/completions, which 500s) and its tools carry the
// Anthropic shape {name, description, input_schema} verbatim — the flat
// Definition() shape, never a normalized/nested form.
func TestKakiitAnthropicModelRoutesToMessagesPathWithAnthropicToolShape(t *testing.T) {
	seedKakiitProtocols(t)

	var gotPath string
	var gotBody string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		if req.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
			gotBody = string(b)
		}
		return statusResponse(http.StatusOK, anthropicOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "claude-sonnet-4-6"
	msg, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, []map[string]interface{}{flatTool()})
	if err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("unexpected message %+v", msg)
	}
	if !strings.HasSuffix(gotPath, "/v1/messages") {
		t.Fatalf("anthropic-messages model posted to %q, want /v1/messages (chat/completions 500s deterministically)", gotPath)
	}
	// Anthropic tool shape: name/description/input_schema, no nested function{}.
	if !strings.Contains(gotBody, "\"input_schema\"") {
		t.Fatalf("tools must carry Anthropic input_schema shape, body: %s", gotBody)
	}
	if strings.Contains(gotBody, "\"function\":") && strings.Contains(gotBody, "\"parameters\"") {
		t.Fatalf("nested OpenAI tool shape leaked onto the Anthropic path: %s", gotBody)
	}
	if !strings.Contains(gotBody, "\"name\":\"preview_open\"") {
		t.Fatalf("flat tool name missing from Anthropic payload: %s", gotBody)
	}
}

// TestKakiitResponsesModelRoutesToResponsesPath pins that an openai-responses
// kakiit model is served by the Responses API, not chat/completions.
func TestKakiitResponsesModelRoutesToResponsesPath(t *testing.T) {
	seedKakiitProtocols(t)

	var gotPath string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		if strings.HasSuffix(gotPath, "/responses") {
			return statusResponse(http.StatusOK, responsesOKStream), nil
		}
		return statusResponse(http.StatusInternalServerError, `{"error":"wrong endpoint"}`), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "gpt-5.6-luna"
	msg, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("unexpected message %+v", msg)
	}
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("openai-responses model posted to %q, want .../responses", gotPath)
	}
}

// TestKakiitChatModelRoutesToChatCompletions pins that an openai-chat kakiit
// model stays on chat/completions (the default OpenAI-compatible path).
func TestKakiitChatModelRoutesToChatCompletions(t *testing.T) {
	seedKakiitProtocols(t)

	var gotPath string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "deepseek-v4-flash"
	if _, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Fatalf("openai-chat model posted to %q, want /chat/completions", gotPath)
	}
}

// TestKakiitMissingProtocolIsHardError pins the refusal: a kakiit model with
// no x_kakiit_protocol in the catalog must fail with the typed
// refusing-to-guess error and NEVER silently default to a protocol.
func TestKakiitMissingProtocolIsHardErrorNoFallback(t *testing.T) {
	seedKakiitProtocols(t)

	var sawRequest bool
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawRequest = true
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "mystery-model"
	_, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected a hard error for a kakiit model with no protocol")
	}
	if !strings.Contains(err.Error(), "reports no protocol") || !strings.Contains(err.Error(), "mystery-model") {
		t.Fatalf("error must name the model and the refusal: %v", err)
	}
	if sawRequest {
		t.Fatal("no HTTP request may be sent for a protocol-less kakiit model")
	}
}

// TestKakiitUnknownModelWithoutCacheIsHardError pins the same refusal for a
// model the cache has never seen (fetch failed / typo): no silent default.
func TestKakiitUnknownModelWithoutCacheIsHardError(t *testing.T) {
	resetKakiitCache(t)

	var sawRequest bool
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawRequest = true
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "never-heard-of"
	_, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected a hard error for an unknown kakiit model")
	}
	if !strings.Contains(err.Error(), "reports no protocol") {
		t.Fatalf("error must be the refusing-to-guess error: %v", err)
	}
	if sawRequest {
		t.Fatal("no HTTP request may be sent for an unknown kakiit model")
	}
}

// TestKakiitCacheBreakpointsSurviveAnthropicPathUnreordered pins prompt-cache
// stability: on the Anthropic path for a kakiit model the conversation
// breakpoints land on the last two user turns exactly as on the native
// anthropic path — cache_control is neither dropped nor reordered onto the
// first turn (which would invalidate the whole cached prefix).
func TestKakiitCacheBreakpointsSurviveAnthropicPathUnreordered(t *testing.T) {
	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "claude-sonnet-4-6"

	readCall := ToolCall{ID: "t1", Type: "function"}
	readCall.Function.Name = "read"
	readCall.Function.Arguments = "{}"
	msgs, err := c.buildAnthropicMessages([]Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ok", ToolCalls: []ToolCall{readCall}},
		{Role: "tool", ToolID: "t1", Content: "file body"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyAnthropicConversationBreakpoints(msgs)

	var userIdx []int
	for i, m := range msgs {
		if m["role"] == "user" {
			userIdx = append(userIdx, i)
		}
	}
	if len(userIdx) != 3 {
		t.Fatalf("want 3 user-role messages, got %d", len(userIdx))
	}
	if cacheMarked(msgs[userIdx[0]]) {
		t.Fatal("first user message must not carry a breakpoint")
	}
	if !cacheMarked(msgs[userIdx[1]]) || !cacheMarked(msgs[userIdx[2]]) {
		t.Fatal("last two user turns must carry breakpoints")
	}
}

// TestKakiitPricingHelperFeedsUsageLedger is a small guard that the gateway
// pricing flows through the pricing registry shape used by usage accounting.
func TestKakiitPricingHelperFeedsUsageLedger(t *testing.T) {
	seedKakiitProtocols(t)
	p, ok := KakiitModelCost("kakiit/claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected pricing for kakiit/claude-sonnet-4-6")
	}
	if p.InputPerMillion <= 0 || p.OutputPerMillion <= 0 {
		t.Fatalf("pricing lost: %+v", p)
	}
	var zero pricing.ModelPricing
	if p == zero {
		t.Fatal("non-free model pricing must not be zero")
	}
	if w := KakiitModelWindow("kakiit/claude-sonnet-4-6"); w != 200000 {
		t.Fatalf("context window = %d, want 200000", w)
	}
}
