package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// A prompt override edit must invalidate the summary caches the same way a
// prompt-version bump does; otherwise the old prompt's output keeps replaying.
func TestSpeechSummaryPromptSalt_TracksOverride(t *testing.T) {
	t.Cleanup(func() { config.SetPromptOverrides(nil) })

	config.SetPromptOverrides(nil)
	if got := speechSummaryPromptSalt(); got != speechSummaryPromptVersion {
		t.Fatalf("no override: salt = %q, want %q", got, speechSummaryPromptVersion)
	}
	config.SetPromptOverrides(map[string]config.PromptOverride{
		"speech_summary": {Enabled: false, Text: "Shorter."},
	})
	if got := speechSummaryPromptSalt(); got != speechSummaryPromptVersion {
		t.Fatalf("disabled override: salt = %q, want %q", got, speechSummaryPromptVersion)
	}
	config.SetPromptOverrides(map[string]config.PromptOverride{
		"speech_summary": {Enabled: true, Text: "Shorter."},
	})
	salted := speechSummaryPromptSalt()
	if salted == speechSummaryPromptVersion {
		t.Fatal("enabled override did not change the salt")
	}
	if speechSummaryCacheKey("hello", "m") == speechSummaryCacheKeyFor("hello", "m", speechSummaryPromptVersion) {
		t.Fatal("cache key ignored the override salt")
	}
}

// A subagent_<name> override edited after a spec is installed must reach the
// next prompt build. The spec keeps the raw built-in text, so the override is
// not frozen at install time.
func TestSubAgentSpecPromptFollowsOverrideEdit(t *testing.T) {
	resetPromptOverrides(t, nil)

	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetSpec(&AgentSpec{Name: "explore", SystemPrompt: exploreSubAgentPrompt})
	if got := joinContents(a.BasePromptMessages()); strings.Contains(got, "Prefer rg.") {
		t.Fatal("no override set, but the assembled prompt carries one")
	}

	config.SetPromptOverrides(map[string]config.PromptOverride{
		"subagent_explore": {Enabled: true, Text: "Prefer rg."},
	})
	if got := joinContents(a.BasePromptMessages()); !strings.Contains(got, exploreSubAgentPrompt+"\n\nPrefer rg.") {
		t.Fatal("override edited after SetSpec did not reach the next prompt build")
	}
	if got := a.Spec().SystemPrompt; got != exploreSubAgentPrompt {
		t.Fatalf("spec prompt = %q, want the raw built-in text", got)
	}
}

func TestMDSummaryPromptHash_TracksOverride(t *testing.T) {
	t.Cleanup(func() { config.SetPromptOverrides(nil) })

	config.SetPromptOverrides(nil)
	if got := mdSummaryPromptHash(); got != "" {
		t.Fatalf("no override: hash = %q, want empty", got)
	}
	config.SetPromptOverrides(map[string]config.PromptOverride{
		"md_summary": {Enabled: true, Text: "Mention the owning team."},
	})
	if got := mdSummaryPromptHash(); got == "" {
		t.Fatal("enabled override: hash is empty")
	}
}
