package agent

import (
	"sort"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// resetPromptOverrides clears the process-wide override store now and again on
// cleanup, so an override set by one test cannot leak into another.
func resetPromptOverrides(t *testing.T, m map[string]config.PromptOverride) {
	t.Helper()
	config.SetPromptOverrides(m)
	t.Cleanup(func() { config.SetPromptOverrides(nil) })
}

func TestPromptCatalogSortedUniqueAndComplete(t *testing.T) {
	entries := PromptCatalog()
	if len(entries) == 0 {
		t.Fatal("PromptCatalog() is empty")
	}
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key }) {
		t.Error("PromptCatalog() is not sorted by Key")
	}
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if seen[e.Key] {
			t.Errorf("duplicate catalog key %q", e.Key)
		}
		seen[e.Key] = true
		if strings.TrimSpace(e.Label) == "" {
			t.Errorf("%s: empty Label", e.Key)
		}
		if strings.TrimSpace(e.Description) == "" {
			t.Errorf("%s: empty Description", e.Key)
		}
		if strings.TrimSpace(e.Builtin) == "" {
			t.Errorf("%s: empty Builtin", e.Key)
		}
		switch e.Kind {
		case PromptKindSubagent, PromptKindJudge, PromptKindHelper, PromptKindMainAgent:
		default:
			t.Errorf("%s: unknown Kind %q", e.Key, e.Kind)
		}
	}
}

func TestPromptCatalogEntryFor(t *testing.T) {
	e, ok := PromptCatalogEntryFor("title")
	if !ok {
		t.Fatal(`PromptCatalogEntryFor("title") not found`)
	}
	if e.Builtin != titleSystemPrompt {
		t.Errorf(`"title" Builtin does not match titleSystemPrompt`)
	}
	if _, ok := PromptCatalogEntryFor("no_such_prompt"); ok {
		t.Error(`PromptCatalogEntryFor("no_such_prompt") reported found`)
	}
}

// With no overrides stored, every catalog entry resolves to exactly its const.
func TestPromptCatalogEffectiveIsBuiltinWhenStoreEmpty(t *testing.T) {
	resetPromptOverrides(t, nil)
	for _, e := range PromptCatalog() {
		if got := config.ApplyPromptOverride(e.Key, e.Builtin); got != e.Builtin {
			t.Errorf("%s: effective prompt changed with empty store", e.Key)
		}
	}
}

// An enabled override appends its text after a blank line for every entry.
func TestPromptCatalogEffectiveAppendsEnabledOverride(t *testing.T) {
	const extra = "Extra user guidance."
	m := make(map[string]config.PromptOverride)
	for _, e := range PromptCatalog() {
		m[e.Key] = config.PromptOverride{Enabled: true, Text: extra}
	}
	resetPromptOverrides(t, m)
	for _, e := range PromptCatalog() {
		want := e.Builtin + "\n\n" + extra
		if got := config.ApplyPromptOverride(e.Key, e.Builtin); got != want {
			t.Errorf("%s: effective prompt = %q, want builtin plus override", e.Key, got)
		}
	}
}

// The title prompt that is actually sent as the system message carries the
// user's override, and the default prompt is unchanged when none is set.
func TestGenerateTitleSendsOverriddenSystemPrompt(t *testing.T) {
	var sent string
	client := titleCaptureClient{titleStubClient: titleStubClient{reply: "Colour Choices"}, system: &sent}

	resetPromptOverrides(t, nil)
	a := &Agent{client: client}
	if got := a.generateTitle("pick a colour", "sure"); got == "" {
		t.Fatal("generateTitle returned empty title")
	}
	if sent != titleSystemPrompt {
		t.Fatalf("with empty store, system prompt changed")
	}

	resetPromptOverrides(t, map[string]config.PromptOverride{
		"title": {Enabled: true, Text: "Prefer British spelling."},
	})
	sent = ""
	a = &Agent{client: client}
	a.generateTitle("pick a colour", "sure")
	want := titleSystemPrompt + "\n\nPrefer British spelling."
	if sent != want {
		t.Fatalf("system prompt sent = %q, want built-in plus override", sent)
	}
}

// A sub-agent prompt is overridden only while it is the unmodified built-in
// text. A prompt the user wrote in an agent file passes through untouched.
// SubAgentPromptWithOverride is the one place a sub-agent prompt gets its
// override, so dispatch and main-agent switching cannot disagree.
func TestSubAgentPromptWithOverrideFollowsStore(t *testing.T) {
	resetPromptOverrides(t, map[string]config.PromptOverride{
		"subagent_explore": {Enabled: true, Text: "Prefer rg."},
	})
	if got, want := SubAgentPromptWithOverride("explore", exploreSubAgentPrompt), exploreSubAgentPrompt+"\n\nPrefer rg."; got != want {
		t.Errorf("explore with override = %q, want built-in plus override", got)
	}
	if got := SubAgentPromptWithOverride("general", generalSubAgentPrompt); got != generalSubAgentPrompt {
		t.Errorf("general with no override changed: %q", got)
	}
	const custom = "my own explore prompt"
	if got := SubAgentPromptWithOverride("explore", custom); got != custom {
		t.Errorf("user-written prompt changed to %q", got)
	}
}

func TestOverrideBuiltinPromptOnlyTouchesBuiltinText(t *testing.T) {
	resetPromptOverrides(t, map[string]config.PromptOverride{
		"subagent_explore": {Enabled: true, Text: "Use rg first."},
	})
	if got := overrideBuiltinPrompt("subagent_explore", exploreSubAgentPrompt); got != exploreSubAgentPrompt+"\n\nUse rg first." {
		t.Errorf("built-in explore prompt not overridden")
	}
	const userPrompt = "You are my custom explorer."
	if got := overrideBuiltinPrompt("subagent_explore", userPrompt); got != userPrompt {
		t.Errorf("user-defined explore prompt was modified: %q", got)
	}
	if got := overrideBuiltinPrompt("no_such_key", exploreSubAgentPrompt); got != exploreSubAgentPrompt {
		t.Errorf("unknown key modified the prompt: %q", got)
	}
}

// The compaction prompt that is rendered into a summary request carries the
// override, placed right after the built-in instruction.
func TestRenderSummaryPromptHonoursCompactionOverride(t *testing.T) {
	resetPromptOverrides(t, map[string]config.PromptOverride{
		"compaction": {Enabled: true, Text: "Keep file paths verbatim."},
	})
	got := renderSummaryPrompt("", "user: hi", 0, "")
	want := compactionSystemPrompt + "\n\nKeep file paths verbatim.\n\n" + summaryTemplate
	if !strings.HasPrefix(got, want) {
		t.Errorf("summary prompt does not start with built-in plus override")
	}
}

// titleCaptureClient records the system message of each title request.
type titleCaptureClient struct {
	titleStubClient
	system *string
}

func (c titleCaptureClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	for _, m := range messages {
		if m.Role == "system" {
			*c.system = m.Content
		}
	}
	return &Message{Role: "assistant", Content: c.reply}, nil
}
