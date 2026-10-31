package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// styleFragment returns the communication-style system message, or "".
func styleFragment(msgs []Message) string {
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, promptCommunicationStyleMarker) {
			return m.Content
		}
	}
	return ""
}

// TestCommunicationStylePromptGatedOnFlag pins that the fragment follows the
// setting and is a SYSTEM message (it sits in the cached prefix).
func TestCommunicationStylePromptGatedOnFlag(t *testing.T) {
	resetPromptOverrides(t, nil)
	ag := &Agent{}
	ag.SetCommunicationStyleEnabled(true)
	if !ag.CommunicationStyleEnabled() {
		t.Fatal("CommunicationStyleEnabled() = false after enabling")
	}
	msgs := ag.BasePromptMessages()
	if !containsPromptMarker(msgs, promptCommunicationStyleMarker) {
		t.Fatalf("expected the %s fragment in the base prompt", promptCommunicationStyleMarker)
	}
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, promptCommunicationStyleMarker) && m.Role != "system" {
			t.Fatalf("communication-style fragment role = %q, want system", m.Role)
		}
	}

	ag.SetCommunicationStyleEnabled(false)
	if containsPromptMarker(ag.BasePromptMessages(), promptCommunicationStyleMarker) {
		t.Fatalf("expected no %s fragment once disabled", promptCommunicationStyleMarker)
	}
}

// TestCommunicationStylePromptCarriesTheStandard pins the load-bearing rules so
// a careless edit of the const cannot silently drop one.
func TestCommunicationStylePromptCarriesTheStandard(t *testing.T) {
	resetPromptOverrides(t, nil)
	ag := &Agent{}
	ag.SetCommunicationStyleEnabled(true)
	got := styleFragment(ag.BasePromptMessages())
	for _, want := range []string{
		"# Agent Communication & Style Standard",
		"ASD-STE100 Structural Constraints",
		"Maximum 20 words per action",
		"Maximum 25 words per sentence",
		"Strictly active voice",
		"before* the action",
		"One discrete action per numbered line",
		"Software Domain Overrides",
		"Never rotate synonyms",
		"`authService`",
		"Give code edits and terminal commands first",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("communication-style fragment missing %q", want)
		}
	}
}

// TestCommunicationStylePromptAppliesOverride pins that a user override from
// the Prompts settings page is appended to the fragment.
func TestCommunicationStylePromptAppliesOverride(t *testing.T) {
	resetPromptOverrides(t, map[string]config.PromptOverride{
		"communication_style": {Enabled: true, Text: "Prefer British spelling."},
	})
	ag := &Agent{}
	ag.SetCommunicationStyleEnabled(true)
	got := styleFragment(ag.BasePromptMessages())
	if !strings.HasSuffix(got, "\n\nPrefer British spelling.") {
		t.Fatalf("override not appended to the fragment: %q", got[len(got)-80:])
	}
}

// TestNewAgentDefaultsCommunicationStyleOn pins default-ON: a nil config, and a
// default config, both enable the fragment; an explicit false disables it.
func TestNewAgentDefaultsCommunicationStyleOn(t *testing.T) {
	if !NewAgent(nil, nil, nil, nil).CommunicationStyleEnabled() {
		t.Fatal("a nil config must default communication style to ENABLED")
	}
	on := &config.Config{Ocode: config.OcodeConfig{CommunicationStyleEnabled: true}}
	if !NewAgent(nil, nil, on, nil).CommunicationStyleEnabled() {
		t.Fatal("CommunicationStyleEnabled=true in config must enable the fragment")
	}
	off := &config.Config{Ocode: config.OcodeConfig{CommunicationStyleEnabled: false}}
	if NewAgent(nil, nil, off, nil).CommunicationStyleEnabled() {
		t.Fatal("CommunicationStyleEnabled=false in config must disable the fragment")
	}
}

// TestCommunicationStyleCatalogEntry pins that the fragment is overridable.
func TestCommunicationStyleCatalogEntry(t *testing.T) {
	e, ok := PromptCatalogEntryFor("communication_style")
	if !ok {
		t.Fatal(`PromptCatalogEntryFor("communication_style") not found`)
	}
	if e.Builtin != communicationStylePromptContent {
		t.Error("catalog Builtin does not match communicationStylePromptContent")
	}
	if e.Kind != PromptKindMainAgent {
		t.Errorf("Kind = %q, want %q", e.Kind, PromptKindMainAgent)
	}
}

// TestMarkHelperAgentTurnsOffUserFacingFragments pins the helper policy point:
// a primary agent built by NewAgent carries all four user-facing fragments, and
// MarkHelperAgent turns every one of them off in a single call.
func TestMarkHelperAgentTurnsOffUserFacingFragments(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	if !a.RecapPromptEnabled() || !a.PlanPromptEnabled() ||
		!a.SuggestedResponsesEnabled() || !a.CommunicationStyleEnabled() {
		t.Fatalf("primary agent defaults: recap=%v plan=%v suggested=%v style=%v, want all true",
			a.RecapPromptEnabled(), a.PlanPromptEnabled(),
			a.SuggestedResponsesEnabled(), a.CommunicationStyleEnabled())
	}

	a.MarkHelperAgent()

	if a.RecapPromptEnabled() || a.PlanPromptEnabled() ||
		a.SuggestedResponsesEnabled() || a.CommunicationStyleEnabled() {
		t.Fatalf("after MarkHelperAgent: recap=%v plan=%v suggested=%v style=%v, want all false",
			a.RecapPromptEnabled(), a.PlanPromptEnabled(),
			a.SuggestedResponsesEnabled(), a.CommunicationStyleEnabled())
	}
}
