package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/memory"
)

// TestBasePromptShape_PerPrimaryAgent asserts the marker order and presence
// for each built-in primary agent. It guards against drift in the prompt
// assembler — adding a new fragment requires updating this test deliberately.
//
// Scope: only marker order + per-mode prompt presence. Does NOT snapshot full
// prompt text (which would churn on every wording tweak). Does NOT cover every
// entrypoint (TUI/CLI/server/ACP/subagent) because they all funnel through
// BasePromptMessages — that's the single chokepoint.
func TestBasePromptShape_PerPrimaryAgent(t *testing.T) {
	primaryModes := []Mode{ModeBuild, ModePlan, ModeReview, ModeDebug, ModeDocs}

	for _, mode := range primaryModes {
		t.Run(string(mode), func(t *testing.T) {
			a := &Agent{
				client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
				mode:   mode,
			}
			msgs := a.BasePromptMessages("")

			wantOrder := []string{
				promptEnvMarker,
				promptProviderMarker,
				promptModeMarker,
			}
			gotOrder := collectMarkers(msgs)
			if !startsWith(gotOrder, wantOrder) {
				t.Fatalf("marker order mismatch:\n  got:  %v\n  want prefix: %v", gotOrder, wantOrder)
			}

			modeMsg := findMarker(msgs, promptModeMarker)
			if modeMsg == "" {
				t.Fatal("mode fragment missing")
			}
			expected := mode.SystemPrompt()
			if expected != "" && !strings.Contains(modeMsg, strings.SplitN(expected, "\n", 2)[0]) {
				t.Errorf("mode fragment does not contain expected mode prompt opening: got %q", modeMsg[:min(120, len(modeMsg))])
			}
		})
	}
}

func TestBasePromptShape_SelectionAppendedLast(t *testing.T) {
	a := &Agent{
		client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
		mode:   ModeBuild,
	}
	msgs := a.BasePromptMessages("user-selected text here")
	markers := collectMarkers(msgs)
	if len(markers) == 0 {
		t.Fatal("no markers")
	}
	if markers[len(markers)-1] != promptSelectionMarker {
		t.Errorf("selection marker should be last; got order %v", markers)
	}
}

func TestPrepareMessages_DoesNotDuplicateFragments(t *testing.T) {
	a := &Agent{
		client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
		mode:   ModeBuild,
	}
	first := a.PrepareMessages(nil, "")
	twice := a.PrepareMessages(first, "")
	if len(first) != len(twice) {
		t.Errorf("PrepareMessages duplicated fragments: first=%d twice=%d", len(first), len(twice))
	}
}

func TestBuildModePrompt_IncludesAdvisorContextPacketGuidance(t *testing.T) {
	p := ModeBuild.SystemPrompt()
	if !strings.Contains(p, "When calling the advisor tool, provide a compact context packet") {
		t.Fatalf("build mode prompt missing advisor context-packet guidance")
	}
	for _, want := range []string{
		"files/lines already inspected",
		"key evidence or command outputs",
		"exact decision/questions",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("build mode prompt missing advisor context detail %q", want)
		}
	}
}

func TestBasePromptMessages_IncludesMemoryContextWhenEnabled(t *testing.T) {
	wd := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})

	if err := os.MkdirAll(filepath.Join(wd, "skills", "ocode-mem"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "skills", "ocode-mem", "SKILL.md"), []byte("---\nname: ocode-mem\n---\n# Memory guidance\nUse the memory files.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	snap, err := memory.Status(wd)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		snap.User.Path:    "remember user prefs\n",
		snap.Project.Path: "remember project decisions\n",
		snap.Global.Path:  "remember global lessons\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := &Agent{client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"}, mode: ModeBuild}
	a.SetMemoryEnabled(true)

	base := a.BasePromptMessages("")
	ctx := findMarker(base, promptContextMarker)
	if ctx == "" {
		t.Fatal("expected base prompt to include the context fragment")
	}
	for _, want := range []string{"Memory context is enabled.", "Memory guidance", "User memory", "Project memory", "Global history"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("memory context missing %q: %s", want, ctx)
		}
	}
}

func TestBasePromptMessages_IncludesDocPromptWhenEnabled(t *testing.T) {
	a := &Agent{client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"}, mode: ModeBuild}
	a.SetDocPromptEnabled(true)

	base := a.BasePromptMessages("")
	doc := findMarker(base, promptDocPromptMarker)
	if doc == "" {
		t.Fatal("expected base prompt to include the doc prompt fragment when DocPromptEnabled is true")
	}
	if !strings.Contains(doc, "Documentation-First Development") {
		t.Fatalf("doc prompt missing heading: %s", doc)
	}
	for _, want := range []string{
		"Read existing documentation",
		"Check documentation alignment",
		"Update inline documentation",
		"State explicitly if no doc updates are needed",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc prompt missing %q: %s", want, doc)
		}
	}
}

func TestBasePromptMessages_DoesNotIncludeDocPromptWhenDisabled(t *testing.T) {
	a := &Agent{client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"}, mode: ModeBuild}
	a.SetDocPromptEnabled(false)

	base := a.BasePromptMessages("")
	doc := findMarker(base, promptDocPromptMarker)
	if doc != "" {
		t.Fatal("expected base prompt to NOT include the doc prompt fragment when DocPromptEnabled is false")
	}
}

func TestBuildReferenceCatalog_IncludesLoadingGuidance(t *testing.T) {
	cat := BuildReferenceCatalog(nil)
	for _, want := range []string{
		"--- Reference Guidance ---",
		"When a slash command, skill, or agent is mentioned by name",
		"Agent Catalog",
	} {
		if !strings.Contains(cat, want) {
			t.Fatalf("reference catalog missing %q: %s", want, cat)
		}
	}
}

func collectMarkers(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		if mk := promptMarker(m.Content); mk != "" {
			out = append(out, mk)
		}
	}
	return out
}

func findMarker(msgs []Message, marker string) string {
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, marker) {
			return m.Content
		}
	}
	return ""
}

func startsWith(got, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestEnvironmentPrompt_UsesWorkDirOverride verifies that when SetWorkDir is called,
// the environment prompt uses the overridden directory instead of os.Getwd().
func TestEnvironmentPrompt_UsesWorkDirOverride(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	a := &Agent{
		client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
		mode:   ModeBuild,
	}

	// Without workDir override, should use os.Getwd()
	msgs := a.BasePromptMessages("")
	envMsg := findMarker(msgs, promptEnvMarker)
	if envMsg == "" {
		t.Fatal("environment prompt missing")
	}
	if !strings.Contains(envMsg, origWD) {
		t.Errorf("expected environment prompt to contain original wd %q, got:\n%s", origWD, envMsg)
	}

	// With workDir override, should use the overridden directory
	overrideDir := "/tmp/test-override-dir"
	a.SetWorkDir(overrideDir)
	msgs = a.BasePromptMessages("")
	envMsg = findMarker(msgs, promptEnvMarker)
	if envMsg == "" {
		t.Fatal("environment prompt missing after SetWorkDir")
	}
	if !strings.Contains(envMsg, overrideDir) {
		t.Errorf("expected environment prompt to contain override dir %q, got:\n%s", overrideDir, envMsg)
	}
	if strings.Contains(envMsg, origWD) {
		t.Errorf("environment prompt should not contain original wd %q after override", origWD)
	}
}
