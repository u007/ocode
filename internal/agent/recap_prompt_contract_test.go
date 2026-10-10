package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// The recap contract is a STATIC system-role fragment (prompt.go), so it must
// stay a function of construction-time state only. These tests pin the
// content the model depends on, the gating, the position in the cached
// prefix, and — most importantly — that helper agents do NOT receive it.

// recapContractPhrases are the substrings the fragment must keep. Each one is
// a rule the model is expected to follow, so dropping any of them silently
// degrades every response:
//
//   - the five section names, in order (ASKED → WORKED → FOUND → DECIDED →
//     NEXT);
//   - the adaptive WORKED labels, which is what makes a new-feature recap say
//     BUILT and a bug recap say FIXED instead of one fixed wording;
//   - BOTTOM LINE, the final-conclusion line;
//   - the per-item reason requirement on NEXT, and the "None — <reason>"
//     form so an empty list is still an answer;
//   - caveman style, and the skip rule that stops the block firing about
//     nothing.
var recapContractPhrases = []string{
	"## Recap",
	"Caveman style",
	"ASKED",
	"WORKED",
	"BUILT",
	"FIXED",
	"CHANGED",
	"FOUND",
	"DECIDED",
	"BOTTOM LINE",
	"NEXT",
	"Every item states its reason",
	"None — <reason>",
	"Skip the block entirely",
}

func TestRecapContract_EnabledByDefaultForPrimaryAgent(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	if !a.RecapPromptEnabled() {
		t.Fatal("primary agent must carry the recap contract by default")
	}

	msgs := a.BasePromptMessages()
	var got string
	for _, m := range msgs {
		if promptMarker(m.Content) == promptRecapMarker {
			got = m.Content
		}
	}
	if got == "" {
		t.Fatalf("[%s] fragment missing from base prompt", promptRecapMarker)
	}
	for _, want := range recapContractPhrases {
		if !strings.Contains(got, want) {
			t.Errorf("recap fragment dropped %q; got:\n%s", want, got)
		}
	}

	// Sections must read in order, or the model emits them shuffled.
	order := []string{"ASKED", "WORKED", "FOUND", "DECIDED", "NEXT"}
	at := -1
	for _, sec := range order {
		i := strings.Index(got, ". "+sec+" ")
		if i < 0 {
			t.Fatalf("section %q not present as a numbered section; got:\n%s", sec, got)
		}
		if i < at {
			t.Errorf("section %q appears out of order; got:\n%s", sec, got)
		}
		at = i
	}

	// The contract must be system-role: every system-role transcript message is
	// hoisted into the cached system block, so a user-role contract would ride
	// the transcript instead of the prefix.
	for _, m := range msgs {
		if promptMarker(m.Content) == promptRecapMarker && m.Role != "system" {
			t.Errorf("recap fragment role = %q, want system", m.Role)
		}
	}
}

func TestRecapContract_DisabledOmitsFragment(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetRecapPromptEnabled(false)

	if a.RecapPromptEnabled() {
		t.Fatal("RecapPromptEnabled() = true after SetRecapPromptEnabled(false)")
	}
	for _, m := range a.BasePromptMessages() {
		if promptMarker(m.Content) == promptRecapMarker {
			t.Fatalf("recap fragment present while disabled:\n%s", m.Content)
		}
	}
	// Flip it back on and require the fragment to RETURN. Without this the
	// assertions above pass just as happily against an agent that never
	// injects the fragment at all — a vacuous green for a dead feature.
	a.SetRecapPromptEnabled(true)
	found := false
	for _, m := range a.BasePromptMessages() {
		if promptMarker(m.Content) == promptRecapMarker {
			found = true
		}
	}
	if !found {
		t.Error("recap fragment did not return after re-enabling; the gate is not what controls it")
	}
}

// The fragment sits directly after the mode fragment: the mode says what the
// agent may do, the recap says how it closes a turn. Position is pinned because
// it is part of the cached-prefix byte layout.
func TestRecapContract_FollowsModeFragment(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.mode = ModeBuild

	markers := collectMarkers(a.BasePromptMessages())
	mi, ri := -1, -1
	for i, m := range markers {
		switch m {
		case promptModeMarker:
			mi = i
		case promptRecapMarker:
			ri = i
		}
	}
	if mi < 0 || ri < 0 {
		t.Fatalf("missing fragment: mode=%d recap=%d in %v", mi, ri, markers)
	}
	if ri != mi+1 {
		t.Errorf("recap fragment must directly follow the mode fragment; order = %v", markers)
	}
}

// Cache-stability: two base prompts built back to back must be byte-identical.
// The fragment is a const, so any per-turn variance here would bust the cached
// prefix every request (see append_stable.go).
func TestRecapContract_ByteStableAcrossCalls(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	first := a.BasePromptMessages()
	second := a.BasePromptMessages()
	if len(first) != len(second) {
		t.Fatalf("fragment count changed between calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("base prompt not byte-stable at %d:\n%q\nvs\n%q", i, first[i].Content, second[i].Content)
		}
	}
}

// A dispatched task sub-agent must NOT get the contract. Its result is handed
// back to the parent as a tool result, not rendered to the user, and the child
// never sees the whole conversation — so a recap it wrote would be noise in the
// parent's transcript and factually wrong.
func TestRecapContract_TaskSubagentOptsOut(t *testing.T) {
	client := &captureClient{}
	parent := NewAgent(client, nil, nil, nil)
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionAllow}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "prober",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"read"},
		Source:      "test",
	})

	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(json.RawMessage(`{"prompt":"look","agent":"prober"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}
	if len(client.Messages) == 0 {
		t.Fatal("sub-agent never called the client; the assertion below would be vacuous")
	}
	joined := joinContents(client.Messages)
	if strings.Contains(joined, promptRecapMarker) {
		t.Errorf("sub-agent received the recap contract; it must opt out:\n%s", joined)
	}
	if !strings.Contains(joined, "## Recap") {
		t.Log("note: sub-agent prompt did not contain the recap heading either, consistent with the opt-out")
	}
}

// Same rule for a /btw side query: its answer is a tool result, not a
// user-facing turn, so it opts out too.
func TestRecapContract_SideQueryAgentOptsOut(t *testing.T) {
	parent := NewAgent(&MockClient{}, nil, nil, nil)
	if !parent.RecapPromptEnabled() {
		t.Fatal("precondition: parent should carry the contract")
	}

	child, err := parent.newSideQueryAgent(AskLoopOptions{Client: &MockClient{}})
	if err != nil {
		t.Fatalf("newSideQueryAgent: %v", err)
	}
	if child.RecapPromptEnabled() {
		t.Error("side-query agent must opt out of the recap contract")
	}
	for _, m := range child.BasePromptMessages() {
		if promptMarker(m.Content) == promptRecapMarker {
			t.Errorf("side-query agent received the recap fragment:\n%s", m.Content)
		}
	}
}
