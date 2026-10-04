package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func dedupeUser(content string) Message { return Message{Role: "user", Content: content} }

func TestDedupeTrailingUserMessages(t *testing.T) {
	imgA := Image{MIMEType: "image/png", Data: "AAA"}
	imgB := Image{MIMEType: "image/png", Data: "BBB"}

	tests := []struct {
		name string
		in   []Message
		want []Message
	}{
		{
			name: "nil stays nil",
			in:   nil,
			want: nil,
		},
		{
			name: "empty stays empty",
			in:   []Message{},
			want: []Message{},
		},
		{
			name: "single trailing user message is untouched",
			in:   []Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "x"}, dedupeUser("b")},
			want: []Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "x"}, dedupeUser("b")},
		},
		{
			name: "two identical trailing user messages collapse to the last",
			in: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "a", UserSeq: 1},
				{Role: "assistant", Content: "x"},
				dedupeUser("go"),
				{Role: "user", Content: "go", UserSeq: 7},
			},
			want: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "a", UserSeq: 1},
				{Role: "assistant", Content: "x"},
				{Role: "user", Content: "go", UserSeq: 7},
			},
		},
		{
			name: "three identical trailing user messages collapse to one",
			in:   []Message{dedupeUser("go"), dedupeUser("go"), dedupeUser("go")},
			want: []Message{dedupeUser("go")},
		},
		{
			name: "surrounding whitespace does not defeat the collapse",
			in:   []Message{dedupeUser("go"), dedupeUser("  go\n")},
			want: []Message{dedupeUser("  go\n")},
		},
		{
			name: "collapse is adjacent-only so the user's order survives",
			in:   []Message{dedupeUser("a"), dedupeUser("b"), dedupeUser("a")},
			want: []Message{dedupeUser("a"), dedupeUser("b"), dedupeUser("a")},
		},
		{
			name: "distinct trailing user messages are untouched",
			in:   []Message{dedupeUser("a"), dedupeUser("b")},
			want: []Message{dedupeUser("a"), dedupeUser("b")},
		},
		{
			name: "a run of duplicates followed by a distinct message keeps order",
			in:   []Message{dedupeUser("a"), dedupeUser("a"), dedupeUser("b")},
			want: []Message{dedupeUser("a"), dedupeUser("b")},
		},
		{
			name: "duplicate pair before the tail is untouched",
			in:   []Message{dedupeUser("a"), dedupeUser("a"), {Role: "assistant", Content: "x"}},
			want: []Message{dedupeUser("a"), dedupeUser("a"), {Role: "assistant", Content: "x"}},
		},
		{
			name: "an intervening system message ends the trailing run",
			in:   []Message{dedupeUser("a"), {Role: "system", Content: "note"}, dedupeUser("a")},
			want: []Message{dedupeUser("a"), {Role: "system", Content: "note"}, dedupeUser("a")},
		},
		{
			name: "a trailing tool message ends the trailing run",
			in:   []Message{dedupeUser("a"), dedupeUser("a"), {Role: "tool", Content: "result", ToolID: "t1"}},
			want: []Message{dedupeUser("a"), dedupeUser("a"), {Role: "tool", Content: "result", ToolID: "t1"}},
		},
		{
			name: "same text with different images is not a duplicate",
			in: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
				{Role: "user", Content: "look", Images: []Image{imgB}},
			},
			want: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
				{Role: "user", Content: "look", Images: []Image{imgB}},
			},
		},
		{
			name: "same text and same images is a duplicate",
			in: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
				{Role: "user", Content: "look", Images: []Image{imgA}},
			},
			want: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
			},
		},
		{
			name: "image is compared alongside content, not after it",
			in: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
				{Role: "user", Content: "look"},
			},
			want: []Message{
				{Role: "user", Content: "look", Images: []Image{imgA}},
				{Role: "user", Content: "look"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]Message(nil), tc.in...)
			got := dedupeTrailingUserMessages(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("message count = %d, want %d\n got: %+v\nwant: %+v", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i].Role != tc.want[i].Role || got[i].Content != tc.want[i].Content || got[i].UserSeq != tc.want[i].UserSeq || len(got[i].Images) != len(tc.want[i].Images) {
					t.Fatalf("message %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
			// The transcript is shared with the UI and the persistence layer, so
			// dedup must never write through to the caller's slice.
			for i := range tc.in {
				if tc.in[i].Role != original[i].Role || tc.in[i].Content != original[i].Content || tc.in[i].UserSeq != original[i].UserSeq {
					t.Fatalf("input slice was mutated at %d: %+v, want %+v", i, tc.in[i], original[i])
				}
			}
		})
	}
}

// TestDedupeTrailingUserMessagesSeesThroughTailInjections pins that the
// volatile user-role tail blocks appended by Step (discovery, todo re-anchor,
// notes delta, LSP delta, selection) do not defeat the trim. They are appended
// AFTER the user's message, so they extend the trailing run rather than
// separating the duplicate pair, and their own distinct content keeps them.
func TestDedupeTrailingUserMessagesSeesThroughTailInjections(t *testing.T) {
	in := []Message{
		{Role: "assistant", Content: "earlier"},
		dedupeUser("do it"),
		dedupeUser("do it"),
		dedupeUser("[ocode:discovery]\nattached tools: none"),
		dedupeUser("[ocode:todo]\n- [•] still working"),
	}
	got := dedupeTrailingUserMessages(in)
	want := []string{"do it", "[ocode:discovery]\nattached tools: none", "[ocode:todo]\n- [•] still working"}
	if len(got) != 1+len(want) {
		t.Fatalf("message count = %d, want %d: %+v", len(got), 1+len(want), got)
	}
	for i, w := range want {
		if got[i+1].Role != "user" || got[i+1].Content != w {
			t.Fatalf("message %d = %+v, want user %q", i+1, got[i+1], w)
		}
	}
}

// TestDedupeTrailingUserMessagesKeepsInputSliceWhenNothingToTrim proves an
// ordinary turn allocates nothing, so the messages array stays byte-identical
// and prompt-cache breakpoints do not move.
func TestDedupeTrailingUserMessagesKeepsInputSliceWhenNothingToTrim(t *testing.T) {
	in := []Message{dedupeUser("a"), {Role: "assistant", Content: "x"}, dedupeUser("b")}
	got := dedupeTrailingUserMessages(in)
	if len(got) != len(in) || &got[0] != &in[0] {
		t.Fatalf("unchanged input should be returned as-is, got a fresh slice")
	}
}

// TestChatWithContextCollapsesDuplicateTrailingUserInput is the end-to-end
// proof: the wire payload the provider receives must carry a single copy.
func TestChatWithContextCollapsesDuplicateTrailingUserInput(t *testing.T) {
	var body []byte
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.ChatWithContext(t.Context(), []Message{
		{Role: "assistant", Content: "earlier"},
		dedupeUser("do it"),
		dedupeUser("do it"),
	}, nil)
	if err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}

	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode payload %s: %v", body, err)
	}
	var users []string
	for _, m := range payload.Messages {
		if m.Role == "user" {
			users = append(users, m.Content)
		}
	}
	if len(users) != 1 || users[0] != "do it" {
		t.Fatalf("serialized user messages = %q, want exactly one %q", users, "do it")
	}
}

// TestChatWithContextKeepsDistinctTrailingUserInput pins the boundary of the
// trim: a re-submitted message after a real assistant turn is a follow-up and
// must reach the model twice.
func TestChatWithContextKeepsDistinctTrailingUserInput(t *testing.T) {
	var body []byte
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.ChatWithContext(t.Context(), []Message{
		dedupeUser("do it"),
		{Role: "assistant", Content: "ok"},
		dedupeUser("do it"),
	}, nil)
	if err != nil {
		t.Fatalf("ChatWithContext: %v", err)
	}
	if got := strings.Count(string(body), `"content":"do it"`); got != 2 {
		t.Fatalf("payload carries %d copies of the user message, want 2: %s", got, body)
	}
}

// TestChatAnthropicCollapsesDuplicateTrailingUserInput proves the trim is at a
// shared choke point, not an OpenAI-only one: the Anthropic Messages transport
// builds a completely different payload (system hoisted to a top-level field,
// content-block arrays) and must still receive a single copy.
func TestChatAnthropicCollapsesDuplicateTrailingUserInput(t *testing.T) {
	var body []byte
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		stream := "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
		return statusResponse(http.StatusOK, stream), nil
	}))

	client := &GenericClient{Provider: "anthropic", Model: "claude-test", BaseURL: "https://api.anthropic.test/v1", APIKey: "test-key"}
	if _, err := client.ChatWithContext(t.Context(), []Message{
		{Role: "assistant", Content: "earlier"},
		dedupeUser("do it"),
		dedupeUser("do it"),
	}, nil); err != nil {
		t.Fatalf("ChatWithContext(anthropic): %v", err)
	}
	if got := strings.Count(string(body), `"text":"do it"`); got != 1 {
		t.Fatalf("Anthropic payload carries %d copies of the user message, want 1: %s", got, body)
	}
}
