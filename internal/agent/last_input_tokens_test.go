package agent

import "testing"

// sequenceUsageClient reports a prompt-token count on the first Chat call and
// zero on every call after, letting a test assert that a zero/omitted provider
// reading does not erase the last real context occupancy.
type sequenceUsageClient struct {
	calls int
	first int64
}

func (c *sequenceUsageClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	c.calls++
	pt := int64(0)
	if c.calls == 1 {
		pt = c.first
	}
	return &Message{Role: "assistant", Content: "ok", Usage: &TokenUsage{PromptTokens: &pt}}, nil
}

func (c *sequenceUsageClient) GetProvider() string { return "mock" }
func (c *sequenceUsageClient) GetModel() string    { return "mock-model" }

// LastInputTokens is the backend-authoritative context occupancy the web/
// desktop server reports. It starts at 0 (no fabricated estimate), records the
// provider-reported input tokens from Step's response, and is not clobbered by
// a later response that reports zero.
func TestLastInputTokensRecordsProviderUsage(t *testing.T) {
	c := &sequenceUsageClient{first: 1234}
	ag := NewAgent(c, nil, nil, nil)

	if got := ag.LastInputTokens(); got != 0 {
		t.Fatalf("before any call LastInputTokens = %d, want 0", got)
	}
	if _, err := ag.Step([]Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("first Step: %v", err)
	}
	if got := ag.LastInputTokens(); got != 1234 {
		t.Fatalf("after first Step LastInputTokens = %d, want 1234", got)
	}

	if _, err := ag.Step([]Message{{Role: "user", Content: "again"}}); err != nil {
		t.Fatalf("second Step: %v", err)
	}
	if got := ag.LastInputTokens(); got != 1234 {
		t.Fatalf("after zero-usage Step LastInputTokens = %d, want 1234 (not erased)", got)
	}
}

func TestLastInputTokensNilAgentSafe(t *testing.T) {
	var ag *Agent
	if got := ag.LastInputTokens(); got != 0 {
		t.Fatalf("nil agent LastInputTokens = %d, want 0", got)
	}
}

// Compaction replaces the transcript, so the pre-compaction reading describes a
// shape that no longer exists. runCompact must clear it: the web/desktop gauge
// then reports unknown until the next provider call records the fresh value,
// instead of showing a stale large number.
func TestRunCompactClearsLastInputTokens(t *testing.T) {
	client := &scriptedCaptureClient{Responses: []string{validSummaryText("summary")}}
	a := &Agent{client: client}
	a.lastInputTokens.Store(9999)

	rt := compactRuntime{
		Enabled:               true,
		KeepRecentTurns:       1,
		SummaryTimeoutSeconds: 1,
		SummaryMaxRetries:     0,
		MaxSummaryInputTokens: 50000,
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
		{Role: "assistant", Content: "did work"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
		{Role: "tool", ToolID: "call1", Content: "result1"},
		{Role: "assistant", Content: "done1"},
		{Role: "user", Content: "recent tail"},
		{Role: "assistant", Content: "tail response"},
	}

	res := a.runCompact(msgs, rt, "", false)
	if !res.OK {
		t.Fatalf("runCompact failed: %#v", res)
	}
	if got := a.LastInputTokens(); got != 0 {
		t.Fatalf("after compaction LastInputTokens = %d, want 0 (stale occupancy cleared)", got)
	}
}
