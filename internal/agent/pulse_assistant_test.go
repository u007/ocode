package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

type pulseStubTool struct{ name string }

func (p pulseStubTool) Name() string        { return p.name }
func (p pulseStubTool) Description() string { return "stub" }
func (p pulseStubTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": p.name}
}
func (p pulseStubTool) Execute(_ json.RawMessage) (string, error) { return "{}", nil }
func (p pulseStubTool) Parallel() bool                            { return true }

func TestSetSystemPromptOverrideReplacesBasePrompt(t *testing.T) {
	a := newTestAgent(&MockClient{}, nil, nil, nil)
	if base := a.BasePromptMessages(); len(base) < 2 {
		t.Fatalf("precondition: normal base prompt should have several fragments, got %d", len(base))
	}
	a.SetSystemPromptOverride("  you are the pulse assistant  ")
	base := a.BasePromptMessages()
	if len(base) != 1 {
		t.Fatalf("override must yield exactly one system message, got %d", len(base))
	}
	if base[0].Role != "system" {
		t.Fatalf("override role = %q, want system", base[0].Role)
	}
	if !strings.Contains(base[0].Content, "you are the pulse assistant") {
		t.Fatalf("override text missing: %q", base[0].Content)
	}
	for _, m := range base {
		if strings.Contains(m.Content, "<env>") || strings.Contains(m.Content, promptRecapMarker) {
			t.Fatalf("override leaked a normal fragment: %q", m.Content)
		}
	}
	// Stable across calls (cacheable) and through PrepareMessages.
	again := a.BasePromptMessages()
	if again[0].Content != base[0].Content {
		t.Fatal("override prompt is not byte-stable")
	}
	prepared := a.PrepareMessages([]Message{{Role: "user", Content: "hi"}}, "")
	if len(prepared) != 2 || prepared[0].Content != base[0].Content {
		t.Fatalf("PrepareMessages = %+v", prepared)
	}
}

func TestSetPulseSnapshotInjectsUserRoleTail(t *testing.T) {
	a := newTestAgent(&MockClient{}, nil, nil, nil)
	in := []Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q"}}

	if out := injectPulseTail(in, a); len(out) != len(in) {
		t.Fatalf("nil hook must inject nothing, got %d messages", len(out))
	}

	a.SetPulseSnapshot(func() string { return "running | ocode | ses_1 | title | task" })
	out := injectPulseTail(in, a)
	if len(out) != len(in)+1 {
		t.Fatalf("want one injected message, got %d -> %d", len(in), len(out))
	}
	last := out[len(out)-1]
	if last.Role != "user" {
		t.Fatalf("board role = %q, must be user", last.Role)
	}
	if !strings.HasPrefix(last.Content, "[ocode:pulse]\n") || !strings.HasSuffix(last.Content, "\n[/ocode:pulse]") {
		t.Fatalf("board not wrapped in markers: %q", last.Content)
	}
	if !strings.Contains(last.Content, "ses_1") {
		t.Fatalf("board content missing: %q", last.Content)
	}
	for i, m := range out[:len(in)] {
		if m.Role != in[i].Role || m.Content != in[i].Content {
			t.Fatalf("existing message %d mutated", i)
		}
	}
	for _, m := range out {
		if m.Role == "system" && strings.Contains(m.Content, "ocode:pulse") {
			t.Fatal("board must never be system-role")
		}
	}

	a.SetPulseSnapshot(func() string { return "   " })
	if out := injectPulseTail(in, a); len(out) != len(in) {
		t.Fatal("blank snapshot must inject nothing")
	}
}

func TestRestrictToToolsReplacesBuiltins(t *testing.T) {
	a := newTestAgent(&MockClient{}, nil, nil, nil)
	if _, ok := a.tools["bash"]; !ok {
		t.Fatal("precondition: NewAgent registers bash")
	}
	a.RestrictToTools([]tool.Tool{pulseStubTool{"pulse_board"}})
	if len(a.tools) != 1 {
		names := []string{}
		for n := range a.tools {
			names = append(names, n)
		}
		t.Fatalf("tools = %v, want only pulse_board", names)
	}
	if !a.skipDiscovery {
		t.Fatal("discovery must be off")
	}
}

type blockingRecapClient struct{ release chan struct{} }

func (c *blockingRecapClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	<-c.release
	return &Message{Role: "assistant", Content: "late"}, nil
}
func (c *blockingRecapClient) GetProvider() string { return "fake" }
func (c *blockingRecapClient) GetModel() string    { return "fake" }

func TestRecapCtxHonoursDeadline(t *testing.T) {
	cl := &blockingRecapClient{release: make(chan struct{})}
	defer close(cl.release)
	a := newTestAgent(cl, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	text, err := a.RecapCtx(ctx, []Message{{Role: "user", Content: "hi"}}, "")
	if !errors.Is(err, context.DeadlineExceeded) || text != "" {
		t.Fatalf("RecapCtx = %q, %v; want deadline error", text, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("RecapCtx ignored its deadline")
	}
}

func TestRecapStillReturnsTextOnFailure(t *testing.T) {
	a := newTestAgent(&MockClient{Err: errors.New("boom")}, nil, nil, nil)
	if got := a.Recap([]Message{{Role: "user", Content: "hi"}}, ""); got != "Recap failed: boom" {
		t.Fatalf("Recap = %q", got)
	}
	a = newTestAgent(&MockClient{Response: &Message{Role: "assistant", Content: "  "}}, nil, nil, nil)
	if got := a.Recap([]Message{{Role: "user", Content: "hi"}}, ""); got != "Recap returned empty." {
		t.Fatalf("Recap = %q", got)
	}
}
