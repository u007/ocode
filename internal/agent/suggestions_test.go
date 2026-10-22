package agent

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestExtractSuggestions covers the parser that splits a model reply into its
// human-readable content and the suggested follow-up options.
func TestExtractSuggestions(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantContent string
		wantSug     []string
	}{
		{
			name:        "no block leaves content untouched",
			content:     "Here is what I did.\n\nAll good.",
			wantContent: "Here is what I did.\n\nAll good.",
			wantSug:     nil,
		},
		{
			name:        "well formed block is stripped and parsed",
			content:     "Here is what I did.\n\n" + SuggestionsBlockOpen + "\n[\"Run the tests\", \"Open a PR\"]\n" + SuggestionsBlockClose,
			wantContent: "Here is what I did.",
			wantSug:     []string{"Run the tests", "Open a PR"},
		},
		{
			name:        "block with trailing text keeps the tail",
			content:     "Reply.\n" + SuggestionsBlockOpen + "\n[\"A\"]\n" + SuggestionsBlockClose + "\nTrailing prose.",
			wantContent: "Reply.\n\nTrailing prose.",
			wantSug:     []string{"A"},
		},
		{
			name:        "more than five entries are truncated",
			content:     "Reply." + SuggestionsBlockOpen + `["a","b","c","d","e","f","g"]` + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     []string{"a", "b", "c", "d", "e"},
		},
		{
			name:        "empty entries are dropped",
			content:     "Reply." + SuggestionsBlockOpen + `["", "  ", "real"]` + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     []string{"real"},
		},
		{
			name:        "malformed json yields no suggestions but still strips the block",
			content:     "Reply." + SuggestionsBlockOpen + "\n[not json\n" + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     nil,
		},
		{
			name:        "unterminated block leaves the content untouched",
			content:     "Reply." + SuggestionsBlockOpen + `["a"]`,
			wantContent: "Reply." + SuggestionsBlockOpen + `["a"]`,
			wantSug:     nil,
		},
		{
			name:        "empty array yields no suggestions",
			content:     "Reply." + SuggestionsBlockOpen + "[]" + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     nil,
		},
		{
			name:        "multiline entries are collapsed to one line",
			content:     "Reply." + SuggestionsBlockOpen + `["first line\nsecond line"]` + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     []string{"first line second line"},
		},
		{
			name:        "an overlong entry is truncated with an ellipsis",
			content:     "Reply." + SuggestionsBlockOpen + `["` + strings.Repeat("x", 300) + `"]` + SuggestionsBlockClose,
			wantContent: "Reply.",
			wantSug:     []string{strings.Repeat("x", maxSuggestionLength) + "…"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotContent, gotSug := ExtractSuggestions(tc.content)
			if gotContent != tc.wantContent {
				t.Errorf("content = %q, want %q", gotContent, tc.wantContent)
			}
			if len(gotSug) != len(tc.wantSug) {
				t.Fatalf("suggestions = %v, want %v", gotSug, tc.wantSug)
			}
			for i := range gotSug {
				if gotSug[i] != tc.wantSug[i] {
					t.Errorf("suggestions[%d] = %q, want %q", i, gotSug[i], tc.wantSug[i])
				}
			}
		})
	}
}

// TestExtractSuggestionsTruncatesOnRuneBoundary pins that an overlong option
// is cut between runes. A byte cut that lands mid-rune leaves invalid UTF-8,
// which json.Marshal turns into U+FFFD on the wire and in the persisted message.
func TestExtractSuggestionsTruncatesOnRuneBoundary(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// One ASCII byte plus 129 three-byte em dashes: a byte cut at 120 lands mid-rune.
		{name: "em dash", input: "a" + strings.Repeat("—", 129)},
		// Same shape with CJK ideographs, also three bytes each.
		{name: "cjk", input: "a" + strings.Repeat("漢", 129)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, sug := ExtractSuggestions("Reply." + SuggestionsBlockOpen + `["` + tc.input + `"]` + SuggestionsBlockClose)
			if len(sug) != 1 {
				t.Fatalf("suggestions = %v, want exactly one", sug)
			}
			got := sug[0]
			if !utf8.ValidString(got) {
				t.Fatalf("truncated suggestion is not valid UTF-8: %q", got)
			}
			body := strings.TrimSuffix(got, "…")
			if n := utf8.RuneCountInString(body); n != maxSuggestionLength {
				t.Fatalf("truncated body has %d runes, want %d: %q", n, maxSuggestionLength, got)
			}
		})
	}
}

// TestExtractSuggestionsNeverLeaksRawBlock pins the core promise: whatever
// the model emitted, the returned content never contains the raw block.
func TestExtractSuggestionsNeverLeaksRawBlock(t *testing.T) {
	content := "Done.\n" + SuggestionsBlockOpen + "\n[\"A\"]\n" + SuggestionsBlockClose
	clean, _ := ExtractSuggestions(content)
	if strings.Contains(clean, SuggestionsBlockOpen) || strings.Contains(clean, SuggestionsBlockClose) {
		t.Fatalf("cleaned content still carries the raw block: %q", clean)
	}
	if strings.Contains(clean, `["A"]`) {
		t.Fatalf("cleaned content still carries the raw JSON: %q", clean)
	}
}

// TestSuggestedResponsesPromptFragmentGatedOnFlag pins that the prompt
// fragment follows the setting.
func TestSuggestedResponsesPromptFragmentGatedOnFlag(t *testing.T) {
	ag := &Agent{}
	ag.SetSuggestedResponsesEnabled(true)
	if !ag.SuggestedResponsesEnabled() {
		t.Fatal("SuggestedResponsesEnabled() = false after enabling")
	}
	msgs := ag.BasePromptMessages()
	if !containsPromptMarker(msgs, promptSuggestionsMarker) {
		t.Fatalf("expected the %s fragment in the base prompt, got %d messages", promptSuggestionsMarker, len(msgs))
	}

	ag.SetSuggestedResponsesEnabled(false)
	msgs = ag.BasePromptMessages()
	if containsPromptMarker(msgs, promptSuggestionsMarker) {
		t.Fatalf("expected no %s fragment once disabled", promptSuggestionsMarker)
	}
}

// TestSuggestedResponsesPromptFragmentIsSystemRole pins that the fragment is
// a SYSTEM message: it sits in the cached prefix, so a per-turn-varying
// system message would re-cache the whole prompt every turn.
func TestSuggestedResponsesPromptFragmentIsSystemRole(t *testing.T) {
	ag := &Agent{}
	ag.SetSuggestedResponsesEnabled(true)
	for _, m := range ag.BasePromptMessages() {
		if strings.Contains(m.Content, promptSuggestionsMarker) && m.Role != "system" {
			t.Fatalf("suggestions fragment role = %q, want system", m.Role)
		}
	}
}

// TestNewAgentDefaultsSuggestedResponsesOn pins the default-ON contract: a
// config that never mentions the setting still enables it.
func TestNewAgentDefaultsSuggestedResponsesOn(t *testing.T) {
	ag := NewAgent(nil, nil, nil, nil)
	if !ag.SuggestedResponsesEnabled() {
		t.Fatal("a nil config must default suggested responses to ENABLED")
	}
}

// suggestionsStepClient is a stub LLM client that replays canned responses.
type suggestionsStepClient struct {
	responses []*Message
	calls     int
}

func (c *suggestionsStepClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	if c.calls >= len(c.responses) {
		return &Message{Role: "assistant", Content: "done"}, nil
	}
	response := c.responses[c.calls]
	c.calls++
	return response, nil
}

func (c *suggestionsStepClient) GetProvider() string { return "mock" }
func (c *suggestionsStepClient) GetModel() string    { return "mock-model" }

// TestStepPersistsCleanContentWithSuggestions pins the choke-point contract
// at the level the CALLER sees it.
//
// Step returns newMsgs, and the server appends exactly those messages to the
// transcript it persists. newMsgs holds a VALUE copy of the assistant message,
// so the extraction must run BEFORE the copy — cleaning resp afterwards would
// leave the copy carrying the raw <ocode:suggestions> block, which is what
// lands on disk and what the model sees on the next turn.
//
// This test failed live against a real server: the persisted transcript held
// TWO assistant messages — the cleaned one and a second one still carrying the
// raw block.
func TestStepPersistsCleanContentWithSuggestions(t *testing.T) {
	client := &suggestionsStepClient{responses: []*Message{
		{Role: "assistant", Content: "Here is what I did.\n\n" + SuggestionsBlockOpen + `["Run the tests", "Open a PR"]` + SuggestionsBlockClose},
	}}
	a := newTestAgent(client, nil, nil, nil)
	a.SetSuggestedResponsesEnabled(true)

	messages, err := a.Step([]Message{{Role: "user", Content: "fix the build"}})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	assistantCount := 0
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		assistantCount++
		if strings.Contains(m.Content, SuggestionsBlockOpen) || strings.Contains(m.Content, SuggestionsBlockClose) {
			t.Fatalf("the raw suggestions block reached the returned messages: %q", m.Content)
		}
	}
	if assistantCount != 1 {
		t.Fatalf("assistant messages = %d, want 1 (a duplicate row means the transcript is persisted twice): %+v", assistantCount, messages)
	}

	// The parsed options must survive on the returned message.
	var found bool
	for _, m := range messages {
		if m.Role == "assistant" && len(m.Suggestions) == 2 && m.Suggestions[0] == "Run the tests" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the parsed suggestions did not survive on the returned message: %+v", messages)
	}
}

// TestStepKeepsRawBlockWhenDisabled pins that the block is only stripped when
// the feature is on: with the feature disabled the model may legitimately use
// the marker as prose, and the content must pass through untouched.
func TestStepKeepsRawBlockWhenDisabled(t *testing.T) {
	raw := "Here is what I did.\n\n" + SuggestionsBlockOpen + `["Run the tests"]` + SuggestionsBlockClose
	client := &suggestionsStepClient{responses: []*Message{
		{Role: "assistant", Content: raw},
	}}
	a := newTestAgent(client, nil, nil, nil)
	a.SetSuggestedResponsesEnabled(false)

	messages, err := a.Step([]Message{{Role: "user", Content: "fix the build"}})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	for _, m := range messages {
		if m.Role == "assistant" && len(m.Suggestions) != 0 {
			t.Fatalf("suggestions were extracted while the feature is disabled: %+v", m.Suggestions)
		}
	}
}

// containsPromptMarker reports whether any message carries the marker.
func containsPromptMarker(msgs []Message, marker string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
}

// TestSuggestionsNeverReachTheProviderPayload pins that the UI-only
// Suggestions field is invisible to a provider request.
//
// The provider builders construct their payloads from Role/Content/ToolCalls
// explicitly, so a new Message field cannot leak — but that is a property of
// the builders, not of the struct. If a builder ever spread the struct, the
// model would see its own suggestions echoed back (and a strict server could
// reject the unknown key).
func TestSuggestionsNeverReachTheProviderPayload(t *testing.T) {
	c := &GenericClient{Provider: "openai", Model: "gpt-4o"}
	msgs := []Message{
		{Role: "user", Content: "fix the build"},
		{Role: "assistant", Content: "Fixed it.", Suggestions: []string{"Run the tests"}},
	}

	out, err := c.convertToOpenAIMessages(msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	for _, m := range out {
		for k, v := range m {
			if k == "suggestions" {
				t.Fatalf("the suggestions field leaked into the provider payload: %+v", out)
			}
			if s, ok := v.(string); ok && strings.Contains(s, "Run the tests") {
				t.Fatalf("suggestion text %q leaked into the provider payload key %q: %+v", s, k, out)
			}
		}
	}
	// The real assistant text must survive the conversion.
	sawDone := false
	for _, m := range out {
		if m["role"] == "assistant" && m["content"] == "Fixed it." {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("real assistant turn dropped: %+v", out)
	}
}

// suggestionsToolClient is a stub LLM client that replays canned responses and
// can fail on demand, so the error/cancel paths of Step are reachable.
type suggestionsToolClient struct {
	responses []*Message
	err       error
	calls     int
}

func (c *suggestionsToolClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.calls >= len(c.responses) {
		return &Message{Role: "assistant", Content: "done"}, nil
	}
	response := c.responses[c.calls]
	c.calls++
	return response, nil
}

func (c *suggestionsToolClient) GetProvider() string { return "mock" }
func (c *suggestionsToolClient) GetModel() string    { return "mock-model" }

// TestStepStripsSuggestionsBlockOnToolCallRound pins the fix for the case the
// original test missed: a round that ends with TOOL CALLS.
//
// The model routinely emits the suggestions block in a round that also calls a
// tool (it writes the reply, then calls a tool). The extraction used to be
// gated on `len(resp.ToolCalls) == 0`, so the raw block survived into:
//   - newMsgs, which the server appends to the persisted transcript, and
//   - messages, the live LLM context, so the model sees its own block echoed
//     back on the next turn.
//
// Both are documented promises of the feature, so both are pinned here.
func TestStepStripsSuggestionsBlockOnToolCallRound(t *testing.T) {
	raw := "Working on it.\n\n" + SuggestionsBlockOpen + `["Run the tests"]` + SuggestionsBlockClose
	client := &suggestionsToolClient{responses: []*Message{
		{
			Role:      "assistant",
			Content:   raw,
			ToolCalls: []ToolCall{namedToolCall("tc1", "read", `{"path":"a.go"}`)},
		},
		{Role: "assistant", Content: "All done."},
	}}
	a := newTestAgent(client, nil, nil, nil)
	a.SetSuggestedResponsesEnabled(true)

	msgs, err := a.Step([]Message{{Role: "user", Content: "fix the build"}})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		if strings.Contains(m.Content, SuggestionsBlockOpen) || strings.Contains(m.Content, SuggestionsBlockClose) {
			t.Fatalf("the raw suggestions block survived a tool-call round: %q", m.Content)
		}
	}
}

// TestStepStripsSuggestionsBlockOnErrorRound pins the same contract for a
// round that FAILS after the model already streamed the block: the raw block
// must not survive in the returned messages either.
func TestStepStripsSuggestionsBlockOnErrorRound(t *testing.T) {
	raw := "Working on it.\n\n" + SuggestionsBlockOpen + `["Run the tests"]` + SuggestionsBlockClose
	client := &suggestionsToolClient{
		responses: []*Message{{Role: "assistant", Content: raw}},
		err:       errors.New("provider exploded"),
	}
	a := newTestAgent(client, nil, nil, nil)
	a.SetSuggestedResponsesEnabled(true)

	msgs, _ := a.Step([]Message{{Role: "user", Content: "fix the build"}})
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, SuggestionsBlockOpen) {
			t.Fatalf("the raw suggestions block survived a failed round: %q", m.Content)
		}
	}
}

// TestSuggestionsStreamFilterFlushReleasesHeldText pins the flush contract the
// error/cancel paths depend on.
//
// The filter holds back text it cannot yet classify: up to
// len(SuggestionsBlockOpen)-1 bytes of a possible marker prefix, and — once
// it has seen the marker — EVERYTHING after it. A round that ends without an
// explicit flush therefore never emits that held text, so the visible reply
// is truncated (and the tail of the reply is silently lost).
//
// Step calls flushFilter() on the error and cancellation returns for exactly
// this reason.
func TestSuggestionsStreamFilterFlushReleasesHeldText(t *testing.T) {
	t.Run("flushAll emits the held tail", func(t *testing.T) {
		var got []string
		f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
			if kind == "text" {
				got = append(got, text)
			}
		}}
		// A partial marker prefix: the filter must hold it back.
		f.delta("text", "Hello "+SuggestionsBlockOpen[:5])
		if len(got) != 1 || got[0] != "Hello " {
			t.Fatalf("held prefix was not held back: %+v", got)
		}
		// The flush must release it.
		f.flushAll()
		joined := strings.Join(got, "")
		if !strings.Contains(joined, SuggestionsBlockOpen[:5]) {
			t.Fatalf("flushAll did not release the held text: %q", joined)
		}
	})

	t.Run("flushAll releases the whole tail after the marker", func(t *testing.T) {
		var got []string
		f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
			if kind == "text" {
				got = append(got, text)
			}
		}}
		f.delta("text", "Reply."+SuggestionsBlockOpen+`["a"]`)
		f.delta("text", SuggestionsBlockClose+"tail text")
		joined := strings.Join(got, "")
		if strings.Contains(joined, "tail text") {
			t.Fatalf("the tail leaked before the flush: %q", joined)
		}
		f.flushAll()
		joined = strings.Join(got, "")
		if !strings.Contains(joined, "tail text") {
			t.Fatalf("flushAll did not release the text held after the marker: %q", joined)
		}
	})

	t.Run("flush is idempotent", func(t *testing.T) {
		var got []string
		f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
			if kind == "text" {
				got = append(got, text)
			}
		}}
		f.delta("text", "held")
		f.flushAll()
		first := len(got)
		f.flushAll()
		if len(got) != first {
			t.Fatalf("a second flush re-emitted text: %+v", got)
		}
	})
}

// A prose mention of the open marker before the real block must not swallow
// the answer between the mention and the block.
func TestExtractSuggestionsAnchorsOnTheFinalBlock(t *testing.T) {
	t.Run("mention before the block", func(t *testing.T) {
		content := "Use " + SuggestionsBlockOpen + " to suggest things. Real reply.\n" + SuggestionsBlockOpen + `["A"]` + SuggestionsBlockClose
		got, sug := ExtractSuggestions(content)
		if got != "Use "+SuggestionsBlockOpen+" to suggest things. Real reply." {
			t.Fatalf("content = %q", got)
		}
		if len(sug) != 1 || sug[0] != "A" {
			t.Fatalf("suggestions = %v, want [A]", sug)
		}
	})
	t.Run("mention after the block", func(t *testing.T) {
		content := "Reply." + SuggestionsBlockOpen + `["A"]` + SuggestionsBlockClose + " I use " + SuggestionsBlockOpen + " here."
		got, sug := ExtractSuggestions(content)
		if got != "Reply.\n\nI use "+SuggestionsBlockOpen+" here." {
			t.Fatalf("content = %q", got)
		}
		if len(sug) != 1 || sug[0] != "A" {
			t.Fatalf("suggestions = %v, want [A]", sug)
		}
	})
	t.Run("mention only, never closed", func(t *testing.T) {
		content := "Reply. I use " + SuggestionsBlockOpen + " here."
		got, sug := ExtractSuggestions(content)
		if got != content || sug != nil {
			t.Fatalf("got (%q, %v), want content unchanged and no suggestions", got, sug)
		}
	})
}

// streamThroughFilter feeds content to a filter one byte at a time (the worst
// case for marker straddling) and returns everything it emitted after flush.
func streamThroughFilter(content string) string {
	var got strings.Builder
	f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
		if kind == "text" {
			got.WriteString(text)
		}
	}}
	for i := 0; i < len(content); i++ {
		f.delta("text", content[i:i+1])
	}
	f.flushAll()
	return got.String()
}

// The live stream and the persisted message must agree, whatever the round
// shape: a closed block, a tail after the block, an unterminated block, a
// malformed block, a mention with no close, and a mention before the real block.
func TestSuggestionsStreamMatchesPersistedMessage(t *testing.T) {
	open, close := SuggestionsBlockOpen, SuggestionsBlockClose
	contents := []string{
		"Here is what I did.\n\n" + open + "\n[\"Run the tests\"]\n" + close,
		"Reply." + open + `["a"]` + close + "\nTrailing prose.",
		"Reply." + open + `["a"]`,
		"Reply." + open + "\n[not json\n" + close,
		"Reply. I use " + open + " here.",
		"Use " + open + " to suggest. Real reply.\n" + open + `["A"]` + close,
		"no block at all",
	}
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, c := range contents {
		persisted, _ := ExtractSuggestions(c)
		streamed := streamThroughFilter(c)
		if norm(streamed) != norm(persisted) {
			t.Errorf("stream/persisted mismatch for %q:\n stream    %q\n persisted %q", c, streamed, persisted)
		}
	}
}

// A closed block in a round that continues with tool calls is removed from the
// live stream, as the persisted message removes it.
func TestSuggestionsStreamFilterDropsClosedBlockOnToolRound(t *testing.T) {
	var got []string
	f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
		if kind == "text" {
			got = append(got, text)
		}
	}}
	f.delta("text", "Reply."+SuggestionsBlockOpen+`["a"]`+SuggestionsBlockClose+"tail text")
	f.flushAll()
	joined := strings.Join(got, "")
	if strings.Contains(joined, SuggestionsBlockOpen) || strings.Contains(joined, `["a"]`) {
		t.Fatalf("tool-round flush re-emitted the raw block: %q", joined)
	}
	if !strings.Contains(joined, "tail text") {
		t.Fatalf("tool-round flush dropped the tail: %q", joined)
	}
}

// An unterminated block is kept verbatim, as ExtractSuggestions keeps it, so
// the flush must release it rather than discard it.
func TestSuggestionsStreamFilterReleasesUnterminatedBlock(t *testing.T) {
	var got []string
	f := &suggestionsStreamFilter{onDelta: func(kind, text string) {
		if kind == "text" {
			got = append(got, text)
		}
	}}
	f.delta("text", "Reply."+SuggestionsBlockOpen+`["a"]`)
	f.flushAll()
	joined := strings.Join(got, "")
	if joined != "Reply."+SuggestionsBlockOpen+`["a"]` {
		t.Fatalf("unterminated block not released verbatim: %q", joined)
	}
}
