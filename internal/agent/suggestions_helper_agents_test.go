package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// suggestionsEchoClient answers every turn with a reply that ENDS in a
// suggestions block, so any agent that has the fragment enabled will emit one,
// and records every system prompt it was given.
type suggestionsEchoClient struct {
	Prompts []string
}

func (c *suggestionsEchoClient) Chat(messages []Message, _ []map[string]interface{}) (*Message, error) {
	for _, m := range messages {
		if m.Role == "system" {
			c.Prompts = append(c.Prompts, m.Content)
		}
	}
	return &Message{Role: "assistant", Content: "Child done.\n" + SuggestionsBlockOpen + `["Run the tests"]` + SuggestionsBlockClose}, nil
}

func (c *suggestionsEchoClient) GetProvider() string { return "mock" }
func (c *suggestionsEchoClient) GetModel() string    { return "mock-model" }

// TestHelperAgentsDoNotGetSuggestionsFragment pins that the suggested-responses
// fragment is a MAIN-agent affordance only.
//
// The prompt fragment's own doc comment says:
//
//	"It applies to PRIMARY agents only. Sub-agents, ask helpers and the
//	 advisor turn it off (see SetSuggestedResponsesEnabled)."
//
// but no helper agent actually called SetSuggestedResponsesEnabled(false), so
// all three kept the fragment. The consequences are not cosmetic:
//
//   - a SUB-AGENT's reply is returned to the parent as a TOOL RESULT, so the
//     raw <ocode:suggestions> block landed in the parent's transcript;
//   - the ADVISOR's advice is injected into the parent as a USER-ROLE message,
//     so the raw block landed in the parent's PROMPT and could prompt the
//     model to emit another one.
//
// This test asserts the fragment is absent from each helper agent's prompt.
func TestHelperAgentsDoNotGetSuggestionsFragment(t *testing.T) {
	t.Run("sub-agent", func(t *testing.T) {
		reg := NewAgentRegistry()
		cap := &suggestionsEchoClient{}
		parent := &Agent{
			client:      cap,
			tools:       make(map[string]tool.Tool),
			config:      &config.Config{Ocode: config.OcodeConfig{SuggestedResponsesEnabled: true}},
			permissions: NewPermissionManager(),
			mode:        ModeBuild,
		}
		parent.permissions.SetMode(PermissionModeYOLO)
		parent.SetSuggestedResponsesEnabled(true)
		taskTool := TaskTool{mainAgent: parent, registry: reg}
		parent.tools["task"] = taskTool

		if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"do a thing","agent":"general"}`)); err != nil {
			t.Fatalf("task execute: %v", err)
		}
		if len(cap.Prompts) == 0 {
			t.Fatal("sub-agent saw no system prompt")
		}
		for _, p := range cap.Prompts {
			if strings.Contains(p, promptSuggestionsMarker) {
				t.Fatalf("the sub-agent's system prompt carries the suggested-responses fragment")
			}
		}
	})

	t.Run("ask side-query", func(t *testing.T) {
		cap := &suggestionsEchoClient{}
		main := newTestAgent(cap, nil, nil, nil)
		main.SetSuggestedResponsesEnabled(true)

		done := make(chan struct{})
		cancel := main.AskLoopAsync(
			[]Message{{Role: "user", Content: "what is up"}},
			AskLoopOptions{
				Client:   cap,
				MaxSteps: 4,
			},
			func(content string, err error) { close(done) },
		)
		defer cancel()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("AskLoopAsync did not deliver a result")
		}
		if len(cap.Prompts) == 0 {
			t.Fatal("the ask helper saw no system prompt")
		}
		for _, p := range cap.Prompts {
			if strings.Contains(p, promptSuggestionsMarker) {
				t.Fatalf("the ask helper's system prompt carries the suggested-responses fragment")
			}
		}
	})
}
