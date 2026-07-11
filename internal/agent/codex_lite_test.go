package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func codexLiteStubTransport(t *testing.T, gotHeader *http.Header, gotPayload *map[string]interface{}) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			*gotHeader = req.Header.Clone()
			if err := json.NewDecoder(req.Body).Decode(gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.completed\ndata: {\"type\":\"response.completed\",\"model\":\"gpt-5.6-luna\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}
}

func TestOpenAIResponsesLiteModeForGPT56(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() { llmHTTPClient = originalClient }()

	var gotHeader http.Header
	var gotPayload map[string]interface{}
	llmHTTPClient = codexLiteStubTransport(t, &gotHeader, &gotPayload)

	client := &GenericClient{Provider: "openai", Model: "gpt-5.6-luna", APIKey: "token", UseOAuth: true}
	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{
			"name":        "read_file",
			"description": "read a file",
			"parameters":  map[string]interface{}{"type": "object"},
		}},
	}
	messages := []Message{{Role: "system", Content: "be terse"}, {Role: "user", Content: "hi"}}
	if _, err := client.chatOpenAIResponses(context.Background(), messages, tools); err != nil {
		t.Fatal(err)
	}

	if got := gotHeader.Get("x-openai-internal-codex-responses-lite"); got != "true" {
		t.Fatalf("lite header = %q, want %q", got, "true")
	}
	if gotPayload["instructions"] != "" {
		t.Fatalf("instructions = %#v, want empty (moved into input)", gotPayload["instructions"])
	}
	if _, ok := gotPayload["tools"]; ok {
		t.Fatalf("top-level tools present in lite mode: %#v", gotPayload["tools"])
	}
	if gotPayload["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false", gotPayload["parallel_tool_calls"])
	}
	if key, _ := gotPayload["prompt_cache_key"].(string); key == "" {
		t.Fatalf("prompt_cache_key missing or empty: %#v", gotPayload["prompt_cache_key"])
	}
	reasoning, _ := gotPayload["reasoning"].(map[string]interface{})
	if reasoning["context"] != "all_turns" {
		t.Fatalf("reasoning = %#v, want context all_turns (mandatory with lite header)", gotPayload["reasoning"])
	}

	input, ok := gotPayload["input"].([]interface{})
	if !ok || len(input) != 3 {
		t.Fatalf("input = %#v, want 3 items (additional_tools, developer msg, user msg)", gotPayload["input"])
	}
	first, _ := input[0].(map[string]interface{})
	if first["type"] != "additional_tools" || first["role"] != "developer" {
		t.Fatalf("input[0] = %#v, want additional_tools developer item", input[0])
	}
	itemTools, _ := first["tools"].([]interface{})
	if len(itemTools) != 1 {
		t.Fatalf("input[0].tools = %#v, want 1 tool", first["tools"])
	}
	second, _ := input[1].(map[string]interface{})
	if second["type"] != "message" || second["role"] != "developer" {
		t.Fatalf("input[1] = %#v, want developer instructions message", input[1])
	}
	content, _ := second["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("input[1].content = %#v", second["content"])
	}
	text, _ := content[0].(map[string]interface{})
	if text["type"] != "input_text" || text["text"] != "be terse" {
		t.Fatalf("input[1].content[0] = %#v", content[0])
	}
	third, _ := input[2].(map[string]interface{})
	if third["role"] != "user" {
		t.Fatalf("input[2] = %#v, want user message", input[2])
	}
}

func TestOpenAIResponsesNonLiteModelsKeepLegacyShape(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() { llmHTTPClient = originalClient }()

	var gotHeader http.Header
	var gotPayload map[string]interface{}
	llmHTTPClient = codexLiteStubTransport(t, &gotHeader, &gotPayload)

	client := &GenericClient{Provider: "openai", Model: "gpt-5.5", APIKey: "token", UseOAuth: true}
	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{
			"name":        "read_file",
			"description": "read a file",
			"parameters":  map[string]interface{}{"type": "object"},
		}},
	}
	messages := []Message{{Role: "system", Content: "be terse"}, {Role: "user", Content: "hi"}}
	if _, err := client.chatOpenAIResponses(context.Background(), messages, tools); err != nil {
		t.Fatal(err)
	}

	if got := gotHeader.Get("x-openai-internal-codex-responses-lite"); got != "" {
		t.Fatalf("lite header = %q, want unset for gpt-5.5", got)
	}
	if gotPayload["instructions"] != "be terse" {
		t.Fatalf("instructions = %#v, want top-level", gotPayload["instructions"])
	}
	if _, ok := gotPayload["tools"]; !ok {
		t.Fatal("top-level tools missing for non-lite model")
	}
	if _, ok := gotPayload["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls = %#v, want omitted for non-lite model", gotPayload["parallel_tool_calls"])
	}
	if _, ok := gotPayload["reasoning"]; ok {
		t.Fatalf("reasoning = %#v, want omitted for non-lite model without thinking budget", gotPayload["reasoning"])
	}
	if key, _ := gotPayload["prompt_cache_key"].(string); key == "" {
		t.Fatalf("prompt_cache_key missing or empty: %#v", gotPayload["prompt_cache_key"])
	}
}

func TestOpenAICodexResponsesLite(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.6-sol", true},
		{"gpt-5.6-terra", true},
		{"gpt-5.6-luna", true},
		{"gpt-5.6", true},
		{"gpt-5.5", false},
		{"gpt-5.4-mini", false},
		{"gpt-5.3-codex-spark", false},
	}
	for _, c := range cases {
		if got := openAICodexResponsesLite(c.model); got != c.want {
			t.Errorf("openAICodexResponsesLite(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}
