package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

type fileRulesEnv struct {
	globalPath string
	project    string
	now        time.Time
}

func newFileRulesEnv(t *testing.T) *fileRulesEnv {
	t.Helper()
	setHomeTree(t, t.TempDir())
	gp, err := config.GlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("global config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(gp), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &fileRulesEnv{globalPath: gp, project: t.TempDir(), now: time.Unix(1_700_000_000, 0)}
	prev := fileRulesNow
	fileRulesNow = func() time.Time { return env.now }
	t.Cleanup(func() { fileRulesNow = prev })
	return env
}

func (e *fileRulesEnv) writeGlobal(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(e.globalPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *fileRulesEnv) writeProject(t *testing.T, body string) {
	t.Helper()
	p := config.ProjectSettingsPathForRoot(e.project)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *fileRulesEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

func (e *fileRulesEnv) manager() *PermissionManager {
	pm := NewPermissionManager()
	pm.workDir = e.project
	return pm
}

func bashRule(pm *PermissionManager, prefix string) (PermissionLevel, bool) {
	l, ok := pm.bashPrefixSnapshot()[prefix]
	return l, ok
}

func TestFileRules_LoadsGlobalAndThrottlesChecks(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"bun run":"allow"}}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	if l, ok := bashRule(pm, "bun run"); !ok || l != PermissionAllow {
		t.Fatalf("bun run = %q,%v; want allow", l, ok)
	}

	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"bun run":"allow","rm -rf":"deny"}}}}`)
	env.advance(fileRulesCheckInterval - time.Second)
	pm.refreshFileRules()
	if _, ok := bashRule(pm, "rm -rf"); ok {
		t.Fatal("change was picked up before the check interval elapsed")
	}
	env.advance(2 * time.Second)
	pm.refreshFileRules()
	if l, ok := bashRule(pm, "rm -rf"); !ok || l != PermissionDeny {
		t.Fatalf("rm -rf = %q,%v; want deny after interval", l, ok)
	}
}

func TestFileRules_UnchangedContentDoesNotBumpGeneration(t *testing.T) {
	env := newFileRulesEnv(t)
	body := `{"permissions":{"bash":{"prefixes":{"bun run":"allow"}}}}`
	env.writeGlobal(t, body)
	pm := env.manager()
	pm.refreshFileRules()
	rf := sharedRuleFile(env.globalPath)
	gen := rf.gen.Load()

	// Rewriting identical bytes (new mtime) must not count as a change.
	env.writeGlobal(t, body)
	env.advance(fileRulesCheckInterval + time.Second)
	pm.refreshFileRules()
	if got := rf.gen.Load(); got != gen {
		t.Fatalf("generation %d -> %d for identical content", gen, got)
	}
}

func TestFileRules_SharedAcrossSessions(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"bun run":"allow"}}}}`)
	a, b := env.manager(), env.manager()
	a.refreshFileRules()
	b.refreshFileRules()
	if sharedRuleFile(env.globalPath) != sharedRuleFile(env.globalPath) {
		t.Fatal("global rule file is not a single shared instance")
	}

	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"make":"deny"}}}}`)
	env.advance(fileRulesCheckInterval + time.Second)
	// Only session a triggers the disk check; b must still see the new rules.
	a.refreshFileRules()
	b.refreshFileRules()
	for name, pm := range map[string]*PermissionManager{"a": a, "b": b} {
		if l, ok := bashRule(pm, "make"); !ok || l != PermissionDeny {
			t.Fatalf("session %s: make = %q,%v; want deny", name, l, ok)
		}
		if _, ok := bashRule(pm, "bun run"); ok {
			t.Fatalf("session %s: removed rule 'bun run' still present", name)
		}
	}
}

func TestFileRules_ProjectOverridesGlobalButDenyWins(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"bun run":"allow","git push":"deny"}}}}`)
	env.writeProject(t, `{"permissions":{"bash":{"prefixes":{"bun run":"ask","git push":"allow"}}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	if l, _ := bashRule(pm, "bun run"); l != PermissionAsk {
		t.Fatalf("bun run = %q; want project ask over global allow", l)
	}
	if l, _ := bashRule(pm, "git push"); l != PermissionDeny {
		t.Fatalf("git push = %q; want global deny to win", l)
	}
}

func TestFileRules_ProjectCanGrantOverGlobalAsk(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"bun run":"ask"}}}}`)
	env.writeProject(t, `{"permissions":{"bash":{"prefixes":{"bun run":"allow"},"auto_allow_prefixes":["mytool"]}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	if l, _ := bashRule(pm, "bun run"); l != PermissionAllow {
		t.Fatalf("bun run = %q; project allow must override global ask", l)
	}
	if !pm.bashAutoAllow.load()["mytool"] {
		t.Fatal("project auto-allow prefix not applied")
	}
}

func TestFileRules_GlobalAutoAllowRespectsDeny(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"mytool":"deny"},"auto_allow_prefixes":["mytool","git"]}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	if pm.bashAutoAllow.load()["mytool"] {
		t.Fatal("auto-allow admitted a prefix the same file denies")
	}
	if pm.bashAutoAllow.load()["git"] {
		t.Fatal("bare git must not be auto-allowed (ValidateBashPrefixRule)")
	}
}

func TestFileRules_ProjectRulesAreProjectScoped(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeProject(t, `{"permissions":{"bash":{"prefixes":{"bun run":"deny"}}}}`)
	in := env.manager()
	out := NewPermissionManager()
	out.workDir = t.TempDir()
	in.refreshFileRules()
	out.refreshFileRules()
	if _, ok := bashRule(in, "bun run"); !ok {
		t.Fatal("project session missing its project rule")
	}
	if _, ok := bashRule(out, "bun run"); ok {
		t.Fatal("another project's session received the rule")
	}
}

func TestFileRules_RemovalKeepsSessionOverrides(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"a":"allow","b":"allow"}}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	pm.SetBashPrefixRule("b", PermissionDeny) // in-session change to a file-given rule
	pm.SetBashPrefixRule("session-only", PermissionAllow)

	env.writeGlobal(t, `{}`)
	env.advance(fileRulesCheckInterval + time.Second)
	pm.refreshFileRules()
	if _, ok := bashRule(pm, "a"); ok {
		t.Fatal("rule dropped from the file was not removed")
	}
	if l, ok := bashRule(pm, "b"); !ok || l != PermissionDeny {
		t.Fatalf("b = %q,%v; session override must survive", l, ok)
	}
	if _, ok := bashRule(pm, "session-only"); !ok {
		t.Fatal("session-only rule was wiped by a reload")
	}
}

func TestFileRules_BrokenFileKeepsLastGoodRules(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"bash":{"prefixes":{"rm -rf":"deny"}}}}`)
	pm := env.manager()
	pm.refreshFileRules()

	env.writeGlobal(t, `{"permissions": `)
	env.advance(fileRulesCheckInterval + time.Second)
	pm.refreshFileRules()
	if l, ok := bashRule(pm, "rm -rf"); !ok || l != PermissionDeny {
		t.Fatalf("rm -rf = %q,%v; a broken file must not lift the ban", l, ok)
	}
}

func TestFileRules_ToolWildcardNotDuplicated(t *testing.T) {
	env := newFileRulesEnv(t)
	env.writeGlobal(t, `{"permissions":{"tools":{"mcp_foo_*":"deny"}}}`)
	pm := env.manager()
	pm.refreshFileRules()
	n := len(pm.patterns.load())
	env.writeProject(t, `{}`)
	env.advance(fileRulesCheckInterval + time.Second)
	pm.refreshFileRules()
	if got := len(pm.patterns.load()); got != n {
		t.Fatalf("pattern count %d -> %d after unrelated reload", n, got)
	}
}
