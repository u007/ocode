package server

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// pulsePromptCaptureClient records the messages handed to the provider, so a
// test can assert on the EXACT system prompt the model receives. The Pulse
// prompt reaches the model as a single system-role message (the override
// replaces the whole base set), so this is the real payload.
type pulsePromptCaptureClient struct {
	got []agent.Message
}

func (c *pulsePromptCaptureClient) Chat(messages []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.got = append(c.got, messages...)
	return &agent.Message{Role: "assistant", Content: "ok"}, nil
}
func (c *pulsePromptCaptureClient) GetProvider() string { return "fake" }
func (c *pulsePromptCaptureClient) GetModel() string    { return "fake-model" }

// pulseSystemText returns the system-role text the provider was given.
func pulseSystemText(msgs []agent.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// The build path: a configured pulse_system_prompt must reach the model
// APPENDED to the built-in prompt. This is the regression that matters — the
// old code REPLACED the whole prompt, so a short style instruction silently
// deleted the tool list, the stop-report format and the activation-gate
// protocol.
func TestPulseAgentSendsBuiltInPlusCustomPrompt(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)

	cl := &pulsePromptCaptureClient{}
	cfg := &config.Config{}
	cfg.Ocode.PulseSystemPrompt = "Answer in at most two sentences."
	ag := agent.NewAgent(cl, nil, cfg, nil)
	h.configurePulseAgent(ag, cfg)
	t.Cleanup(ag.Shutdown)

	if _, err := ag.Step([]agent.Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	sys := pulseSystemText(cl.got)

	if !strings.Contains(sys, "Answer in at most two sentences.") {
		t.Fatalf("custom instructions missing from the provider payload:\n%s", sys)
	}
	for _, want := range []string{
		"You are the Pulse assistant, the operator's overview chat",
		"CURRENT message explicitly asks",
		"stops this round, nothing needs me.",
		"Activation gate:",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("provider payload lost the built-in contract %q", want)
		}
	}
	// Every registered pulse tool must still be advertised to the model.
	for _, pt := range h.pulseTools() {
		if !strings.Contains(sys, "- "+pt.Name()+": "+pt.Description()) {
			t.Errorf("provider payload missing tool %q", pt.Name())
		}
	}
}

// The live-override path: HandleSetPulseSystemPrompt re-applies the prompt to
// an already-built agent, so a save must APPEND there too — otherwise a saved
// style instruction would wipe the contracts mid-session.
func TestPulseLiveOverrideAppendsCustomPrompt(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)

	cl := &pulsePromptCaptureClient{}
	cfg := &config.Config{}
	ag := agent.NewAgent(cl, nil, cfg, nil)
	h.configurePulseAgent(ag, cfg)
	t.Cleanup(ag.Shutdown)

	// Exactly what HandleSetPulseSystemPrompt does on a live agent.
	ag.SetSystemPromptOverride(pulseSystemPrompt("Answer in at most two sentences.", h.pulseTools()))

	if _, err := ag.Step([]agent.Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	sys := pulseSystemText(cl.got)

	if !strings.Contains(sys, "Answer in at most two sentences.") {
		t.Fatalf("live custom instructions missing:\n%s", sys)
	}
	if !strings.Contains(sys, "You are the Pulse assistant, the operator's overview chat") {
		t.Fatalf("live override lost the built-in prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "stops this round, nothing needs me.") {
		t.Fatalf("live override lost the stop-report rule:\n%s", sys)
	}
}

// With no custom text the provider payload must be exactly the built-in
// prompt: a session that never customised it keeps a byte-identical cached
// system prefix (CLAUDE.md "Prompt Cache Stability").
func TestPulseAgentWithoutCustomPromptSendsBuiltInOnly(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)

	cl := &pulsePromptCaptureClient{}
	cfg := &config.Config{}
	ag := agent.NewAgent(cl, nil, cfg, nil)
	h.configurePulseAgent(ag, cfg)
	t.Cleanup(ag.Shutdown)

	if _, err := ag.Step([]agent.Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	sys := pulseSystemText(cl.got)

	if strings.Contains(sys, pulseCustomPromptHeader) {
		t.Fatalf("no custom prompt must not append an operator section:\n%s", sys)
	}
	if !strings.Contains(sys, "You are the Pulse assistant, the operator's overview chat") {
		t.Fatalf("built-in prompt missing:\n%s", sys)
	}
}
