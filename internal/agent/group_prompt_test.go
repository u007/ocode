package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestGroupChildPrompt_Present: a child wired to a notes bus
// has a system-prompt fragment that names its id, describes
// the <oc-note> emit format, and tells it the rule
// (leads-not-facts, cross-agent-value only). The fragment is
// the [ocode:notes] marker and is positioned BEFORE the spec
// system prompt so the agent sees the protocol before its
// role instructions.
func TestGroupChildPrompt_Present(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")

	msgs := child.BasePromptMessages("")
	joined := joinContents(msgs)

	if !strings.Contains(joined, "[ocode:notes]") {
		t.Errorf("grouped child missing [ocode:notes] fragment; got:\n%s", joined)
	}
	if !strings.Contains(joined, "a2") {
		t.Errorf("grouped child missing its own id (a2); got:\n%s", joined)
	}
	if !strings.Contains(joined, "<oc-note") {
		t.Errorf("grouped child missing the <oc-note> format example; got:\n%s", joined)
	}
	if !strings.Contains(joined, "verify") {
		t.Errorf("grouped child missing the leads-not-facts rule (verify, not facts); got:\n%s", joined)
	}
}

// TestGroupChildPrompt_AbsentForSoloRun: a child with no bus
// gets NO [ocode:notes] fragment. The fragment is gated on
// bus presence; solo runs are unchanged. This is the
// zero-overhead non-group path.
func TestGroupChildPrompt_AbsentForSoloRun(t *testing.T) {
	child := NewAgent(&MockClient{}, nil, nil, nil)
	// No SetNoteBus.
	msgs := child.BasePromptMessages("")
	joined := joinContents(msgs)
	if strings.Contains(joined, "[ocode:notes]") {
		t.Errorf("solo child has [ocode:notes] fragment; got:\n%s", joined)
	}
}

// TestGroupChildPrompt_SeqBySystemFiled documents that the
// fragment tells the child NOT to author seq= or by=
// attributes — those are filled by the system. This is the
// non-forgeable-id guarantee. A child writing <oc-note
// by="a9" seq="42"> must not be able to spoof another agent.
func TestGroupChildPrompt_SeqBySystemFiled(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	msgs := child.BasePromptMessages("")
	joined := joinContents(msgs)
	// The fragment must say that seq and by are filled by
	// the system (so a child that authors them anyway is
	// breaking the protocol — and the parser will reject
	// because it ignores By on the wire anyway).
	if !strings.Contains(joined, "seq=") && !strings.Contains(joined, "seq ") {
		t.Errorf("prompt missing explanation that seq= is system-filled:\n%s", joined)
	}
	if !strings.Contains(joined, "by=") && !strings.Contains(joined, "by ") {
		t.Errorf("prompt missing explanation that by= is system-filled:\n%s", joined)
	}
}

// TestGroupChildPrompt_PolicyFragment: the emit policy says
// "share only cross-agent-value findings." A child that
// floods the bus with self-only findings breaks the model.
// The prompt must state the policy so the child knows.
func TestGroupChildPrompt_PolicyFragment(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	msgs := child.BasePromptMessages("")
	joined := joinContents(msgs)
	// The wording doesn't have to match verbatim; we check
	// the SEMANTIC anchor: "cross-agent" or "shared" or
	// "other agents" must appear in the policy section.
	if !strings.Contains(joined, "cross-agent") &&
		!strings.Contains(joined, "other agents") {
		t.Errorf("prompt missing cross-agent-value policy:\n%s", joined)
	}
}

// TestGroupChildPrompt_StableForReuse confirms the
// [ocode:notes] fragment is identical across calls. The
// existing-prompt-markers machinery dedupes on the marker;
// even without that dedupe, a no-input call must produce
// the same fragment. This protects the cache-stability
// invariant in append_stable.go: the prompt is part of the
// stable prefix, so it must not change per loop.
func TestGroupChildPrompt_StableForReuse(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	first := child.BasePromptMessages("")
	second := child.BasePromptMessages("")
	if !messagesEqual(first, second) {
		t.Errorf("[ocode:notes] fragment not stable across calls:\n--- first ---\n%s\n--- second ---\n%s",
			dumpMessages(first), dumpMessages(second))
	}
}

// TestGroupChildPrompt_IdFromAgent confirms the fragment uses
// the agent's stored id, not a placeholder. A child with id
// "a7" sees "a7" in its prompt; another with id "a3" sees
// "a3". This pins the parameterization.
func TestGroupChildPrompt_IdFromAgent(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	a7 := NewAgent(&MockClient{}, nil, nil, nil)
	a7.SetNoteBus(bus, "a7")
	got := joinContents(a7.BasePromptMessages(""))
	if !strings.Contains(got, "a7") {
		t.Errorf("agent a7's prompt missing 'a7':\n%s", got)
	}
	// And a3 in the same bus must NOT appear in a7's prompt
	// as the agent's own id (it might appear in examples of
	// "other agents" — but not as the protagonist).
	if strings.Contains(got, "You are agent a3") {
		t.Errorf("a7's prompt says it is a3:\n%s", got)
	}
}

// joinContents is a debug helper for substring-searching
// across the message list.
func joinContents(ms []Message) string {
	var b strings.Builder
	for _, m := range ms {
		b.WriteString(m.Content)
		b.WriteString("\n---\n")
	}
	return b.String()
}
