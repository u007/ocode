package agent

import (
	"strings"
	"testing"
)

// recapPromptStub records the prompt runRecap builds and returns a canned
// recap, so these tests never touch a provider.
type recapPromptStub struct {
	gotUser string
	calls   int
}

func (c *recapPromptStub) GetProvider() string { return "testprov" }
func (c *recapPromptStub) GetModel() string    { return "test-model" }
func (c *recapPromptStub) Chat(messages []Message, _ []map[string]interface{}) (*Message, error) {
	c.calls++
	for _, m := range messages {
		if m.Role == "user" {
			c.gotUser = m.Content
		}
	}
	return &Message{Role: "assistant", Content: "canned recap"}, nil
}

// recapPromptFor runs runRecap against a stub and returns the prompt it built.
//
// config is deliberately left nil: recapClient short-circuits straight to
// a.client when there is no config, so the test never constructs a real
// provider client (which is what a configured RecapModel or small model would
// do) and therefore cannot reach the network.
func recapPromptFor(t *testing.T, short bool) string {
	t.Helper()
	stub := &recapPromptStub{}
	a := &Agent{client: stub}
	msgs := []Message{
		{Role: "user", Content: "the retry helper drops the backoff between attempts"},
		{Role: "assistant", Content: "it now carries the previous delay forward"},
	}
	if got := a.runRecap(msgs, "", short); got != "canned recap" {
		t.Fatalf("runRecap(short=%v) = %q, want the stub's reply", short, got)
	}
	if stub.calls != 1 {
		t.Fatalf("expected exactly one model call, got %d", stub.calls)
	}
	return stub.gotUser
}

// TestRecapFullAsksForTheBiggerPicture pins both halves of the full /recap
// contract: the five-section outline stays (it is what the reader scans), and
// the prompt still asks for what the conversation MEANS rather than only what
// happened in it. A recap that lists the process without the point is the
// failure this instruction exists to prevent, and nothing else in the package
// would notice it — the recap prompt had no coverage at all before this.
func TestRecapFullAsksForTheBiggerPicture(t *testing.T) {
	prompt := recapPromptFor(t, false)

	for _, want := range []string{
		"WHAT USER WANT",
		"WHAT FIND",
		"DECISION",
		"DO",
		"TASKS",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("full recap must keep the %q section; got:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "what it MEANS") {
		t.Errorf("full recap must ask what it means, not just what happened; got:\n%s", prompt)
	}
}

// TestRecapShortLeadsWithTheOutcome pins the auto-recap line: one sentence,
// outcome first, room to name the actions. The old instruction capped it at 100
// characters, which is roughly one clause — not enough to state a result AND
// what was done, so the line could only ever echo activity.
func TestRecapShortLeadsWithTheOutcome(t *testing.T) {
	prompt := recapPromptFor(t, true)

	for _, want := range []string{
		"ONE SENTENCE",
		"at most 200 characters",
		"Lead with the outcome",
		"name the actions",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("short recap prompt must contain %q; got:\n%s", want, prompt)
		}
	}
	// The five-section outline belongs to the full recap only. The short line
	// is rendered into the TUI transcript as a single `recap: …` row, so
	// inheriting the section list would break its whole contract.
	if strings.Contains(prompt, "WHAT USER WANT") {
		t.Errorf("the short recap must stay a one-liner, not inherit the section list; got:\n%s", prompt)
	}
}
