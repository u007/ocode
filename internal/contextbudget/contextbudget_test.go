package contextbudget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func int64p(v int64) *int64 { return &v }

func TestGroupMCPToolDefs(t *testing.T) {
	serverNames := []string{"claude_ai_Gmail", "context7"}
	toolNames := map[string]struct{}{
		"claude_ai_Gmail_search": {},
		"claude_ai_Gmail_send":   {},
		"context7_query":         {},
		"bash":                   {},
	}
	defs := []map[string]interface{}{
		{"name": "claude_ai_Gmail_search", "description": "search"},
		{"name": "claude_ai_Gmail_send", "description": "send"},
		{"name": "context7_query", "description": "query"},
		{"name": "bash", "description": "run bash"},
	}

	grouped, builtin := GroupMCPToolDefs(defs, toolNames, serverNames)

	if len(grouped["claude_ai_Gmail"]) != 2 {
		t.Errorf("expected 2 tools for claude_ai_Gmail, got %d", len(grouped["claude_ai_Gmail"]))
	}
	if len(grouped["context7"]) != 1 {
		t.Errorf("expected 1 tool for context7, got %d", len(grouped["context7"]))
	}
	if len(builtin) != 1 || builtin[0]["name"] != "bash" {
		t.Errorf("expected bash in builtin, got %v", builtin)
	}
}

func TestAggregateTelemetry(t *testing.T) {
	spend := 0.25
	msgs := []agent.Message{
		{Usage: &agent.TokenUsage{
			PromptTokens:            int64p(100),
			CompletionTokens:        int64p(50),
			TotalTokens:             int64p(150),
			CacheReadTokens:         int64p(30),
			PromptIncludesCacheRead: true,
		}},
		{Usage: &agent.TokenUsage{PromptTokens: int64p(10), CompletionTokens: int64p(5)}, Spend: &spend},
	}

	got := AggregateTelemetry(msgs)

	if got.InputTokens != 80 {
		t.Errorf("InputTokens = %d, want 80 (100-30+10)", got.InputTokens)
	}
	if got.OutputTokens != 55 {
		t.Errorf("OutputTokens = %d, want 55", got.OutputTokens)
	}
	if got.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", got.CachedTokens)
	}
	if got.TotalTokens != 165 {
		t.Errorf("TotalTokens = %d, want 165 (150 + 10+5)", got.TotalTokens)
	}
	if got.Spend == nil || *got.Spend != 0.25 {
		t.Errorf("Spend = %v, want 0.25", got.Spend)
	}
	if !got.HasData() {
		t.Error("HasData() = false, want true")
	}
}

func TestLatestRequestUsage(t *testing.T) {
	msgs := []agent.Message{
		{Usage: &agent.TokenUsage{PromptTokens: int64p(1)}},
		{
			Usage: &agent.TokenUsage{
				PromptTokens:     int64p(120),
				CompletionTokens: int64p(30),
				TotalTokens:      int64p(150),
			},
		},
		{},
	}
	in, out, total := LatestRequestUsage(msgs)
	if in != 120 || out != 30 || total != 150 {
		t.Fatalf("LatestRequestUsage = (%d,%d,%d), want (120,30,150)", in, out, total)
	}

	// Total omitted → falls back to in+out.
	in, out, total = LatestRequestUsage([]agent.Message{
		{Usage: &agent.TokenUsage{PromptTokens: int64p(7), CompletionTokens: int64p(3)}},
	})
	if in != 7 || out != 3 || total != 10 {
		t.Fatalf("fallback = (%d,%d,%d), want (7,3,10)", in, out, total)
	}

	if in, out, total = LatestRequestUsage(nil); in != 0 || out != 0 || total != 0 {
		t.Fatalf("empty = (%d,%d,%d), want zeros", in, out, total)
	}
}

func TestContextWindow(t *testing.T) {
	if w, ok := ContextWindow("gpt-4o"); !ok || w != 128000 {
		t.Fatalf("gpt-4o = (%d,%v), want (128000,true)", w, ok)
	}
	if _, ok := ContextWindow("definitely-not-a-real-model-xyz"); ok {
		t.Fatal("unknown model should not resolve a window")
	}
}

func TestRenderText(t *testing.T) {
	rep := Report{
		Model: "openai/gpt-4o",
		Sections: []Section{
			{Title: "Base Prompt", Rows: []Row{
				{Label: "Environment", Value: "~1.2k tok"},
				{Label: "Provider prompt (openai/gpt-4o)", Value: "~80 tok", Raw: true, Lines: []string{"line one", "line two"}},
				{Label: "(no ambient files found)"},
			}},
			{Title: "Discovery — [ocode:discovery] injected block", Rows: []Row{
				{Label: "Corpus (names-index, stable — injected every turn)", Subhead: true},
				{Label: "Skills in index", Value: "3"},
			}},
		},
		Notes: []string{"no live agent"},
	}

	out := rep.RenderText()
	for _, want := range []string{
		"Context Budget",
		"Base Prompt",
		"Environment",
		"~1.2k tok",
		"│ line one",
		"│ line two",
		"(no ambient files found)",
		"Corpus (names-index, stable — injected every turn)",
		"Skills in index",
		"no live agent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderText missing %q\n---\n%s", want, out)
		}
	}
}

func TestBuildNoAgentUsesFilesystemAndOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(strings.Repeat("x", 400)), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := Build(Input{
		WorkDir:       dir,
		Messages:      []agent.Message{{Usage: &agent.TokenUsage{PromptTokens: int64p(500)}}},
		Model:         "gpt-4o",
		ContextTokens: 12345,
		ContextSource: "actual",
	})

	if rep.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", rep.Model)
	}
	if len(rep.Notes) == 0 {
		t.Error("expected a note for the missing agent")
	}

	base := findSection(t, rep, "Base Prompt")
	ambient := findRow(t, base, "AGENTS.md")
	if !strings.Contains(ambient.Value, "tok") {
		t.Errorf("AGENTS.md row value = %q, want a token estimate", ambient.Value)
	}

	sess := findSection(t, rep, "Session Messages")
	ctx := findRow(t, sess, "Context")
	if !strings.Contains(ctx.Value, "12345 / 128000") {
		t.Errorf("Context row = %q, want the override tokens over the window", ctx.Value)
	}
	if !strings.Contains(ctx.Value, "actual") {
		t.Errorf("Context row = %q, want the override source", ctx.Value)
	}
}

func TestBuildEstimatesContextFromMessages(t *testing.T) {
	rep := Build(Input{
		Messages: []agent.Message{{Usage: &agent.TokenUsage{TotalTokens: int64p(9000)}}},
		Model:    "gpt-4o",
	})
	sess := findSection(t, rep, "Session Messages")
	ctx := findRow(t, sess, "Context")
	if !strings.Contains(ctx.Value, "9000") {
		t.Errorf("Context row = %q, want the message-derived estimate", ctx.Value)
	}
}

func TestBuildSessionUsageFromMessages(t *testing.T) {
	rep := Build(Input{
		Messages: []agent.Message{{Usage: &agent.TokenUsage{
			PromptTokens:     int64p(1000),
			CompletionTokens: int64p(200),
			TotalTokens:      int64p(1200),
			CacheReadTokens:  int64p(400),
		}}},
		Model: "gpt-4o",
	})
	sess := findSection(t, rep, "Session Messages")
	usage := findRow(t, sess, "Usage")
	if !strings.Contains(usage.Value, "Cache 400") {
		t.Errorf("Usage row = %q, want cache tokens surfaced", usage.Value)
	}
}

func findSection(t *testing.T, rep Report, title string) Section {
	t.Helper()
	for _, s := range rep.Sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("section %q not found in %v", title, sectionTitles(rep))
	return Section{}
}

func findRow(t *testing.T, sec Section, label string) Row {
	t.Helper()
	for _, r := range sec.Rows {
		if r.Label == label {
			return r
		}
	}
	t.Fatalf("row %q not found in section %q", label, sec.Title)
	return Row{}
}

func sectionTitles(rep Report) []string {
	var out []string
	for _, s := range rep.Sections {
		out = append(out, s.Title)
	}
	return out
}
