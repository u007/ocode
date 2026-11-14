package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/tool"
)

// setupClaudeAgentHome isolates HOME, Claude Code's config dir, the bundled
// plugin dir and the cwd, so only the fixtures a test writes are discovered.
func setupClaudeAgentHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	setHomeTree(t, home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir())
	prev := bundled.PluginsDir
	bundled.PluginsDir = ""
	t.Cleanup(func() { bundled.PluginsDir = prev })
	return home
}

func writeClaudeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installClaudeTestPlugin writes a Claude Code-installed plugin named name with
// one agent file. It then rewrites installed_plugins.json to list every plugin
// under the fake home's cache, so earlier installs in the same test survive.
func installClaudeTestPlugin(t *testing.T, home, name, agentFile, agentBody string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "plugins", "cache", name, "1.0.0")
	writeClaudeTestFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name":"`+name+`","description":"test plugin","version":"1.0.0"}`)
	writeClaudeTestFile(t, filepath.Join(dir, "agents", agentFile), agentBody)

	installs, err := filepath.Glob(filepath.Join(home, ".claude", "plugins", "cache", "*", "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, inst := range installs {
		pluginName := filepath.Base(filepath.Dir(inst))
		entries = append(entries, `"`+pluginName+`@market":[{"scope":"user","installPath":"`+inst+`","version":"1.0.0"}]`)
	}
	writeClaudeTestFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		`{"version":2,"plugins":{`+strings.Join(entries, ",")+`}}`)
	return dir
}

const claudeContextAgent = "---\nname: context\ndescription: OKF lookup (read-only)\nmodel: haiku\ntools: Read, Grep, Glob, Skill\n---\nBODY"

func loadClaudeTestRegistry(t *testing.T) (*AgentRegistry, []LoadDiagnostic) {
	t.Helper()
	r := NewAgentRegistry()
	diags := r.LoadMarkdownAgents()
	return r, diags
}

func diagsContain(diags []LoadDiagnostic, level, substr string) bool {
	for _, d := range diags {
		if d.Level == level && strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

// TestClaudeAgentContextLoadsReadOnly loads the example context agent from a
// Claude Code install and checks its name, mode, model, tool allowlist and the
// enforcement of that allowlist at tool-call time.
func TestClaudeAgentContextLoadsReadOnly(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "okf", "context.md", claudeContextAgent)

	r, diags := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "okf:context")
	if def == nil {
		t.Fatalf("okf:context not loaded; diags: %+v", diags)
	}
	if def.Mode != AgentModeSubagent {
		t.Errorf("Mode = %q, want subagent", def.Mode)
	}
	if def.Model != "" {
		t.Errorf("Model = %q, want empty (inherit) for alias haiku", def.Model)
	}
	if !diagsContain(diags, "warning", "haiku") {
		t.Errorf("want a warning about the haiku alias, got %+v", diags)
	}
	if want := "read,grep,glob,skill"; strings.Join(def.Tools, ",") != want {
		t.Errorf("Tools = %v, want %s", def.Tools, want)
	}
	if len(def.Permissions) != 0 {
		t.Errorf("Permissions = %v, want empty: rules would land on the parent's shared PermissionManager", def.Permissions)
	}

	// The allowlist is enforced by isToolAllowed, which gates both what is
	// advertised and every call.
	a := newTestAgent(nil, []tool.Tool{tool.ReadTool{}, tool.GrepTool{}, tool.GlobTool{}, tool.SkillTool{}, tool.WriteTool{}, tool.EditTool{}, tool.BashTool{}}, nil, nil)
	defer a.Shutdown()
	a.SetSpec(&AgentSpec{Name: def.Name, Tools: def.Tools})
	for _, name := range []string{"read", "grep", "glob", "skill"} {
		if !a.isToolAllowed(name) {
			t.Errorf("isToolAllowed(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"write", "edit", "bash"} {
		if a.isToolAllowed(name) {
			t.Errorf("isToolAllowed(%q) = true, want false for a read-only agent", name)
		}
	}
}

// TestClaudeAgentUnknownToolGrantsNothing checks that a name with no mapping is
// dropped with a warning and that the known names still apply.
func TestClaudeAgentUnknownToolGrantsNothing(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "mix", "mix.md",
		"---\ndescription: mixed\ntools: Read, Bogus\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "mix:mix")
	if def == nil {
		t.Fatalf("mix:mix not loaded; diags: %+v", diags)
	}
	if strings.Join(def.Tools, ",") != "read" {
		t.Errorf("Tools = %v, want [read]", def.Tools)
	}
	if !diagsContain(diags, "warning", "Bogus") {
		t.Errorf("want a warning naming Bogus, got %+v", diags)
	}
}

// TestClaudeAgentLSMapsToList checks that Claude Code's LS grants ocode's
// directory listing tool, which is named "list".
func TestClaudeAgentLSMapsToList(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "lsx", "lsx.md",
		"---\ndescription: lister\ntools: LS\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "lsx:lsx")
	if def == nil {
		t.Fatalf("lsx:lsx not loaded; diags: %+v", diags)
	}
	if strings.Join(def.Tools, ",") != "list" {
		t.Errorf("Tools = %v, want [list]", def.Tools)
	}
}

// TestClaudeAgentAllUnknownToolsIsNotLoaded checks that a tools field that
// maps to nothing does not load the agent. An empty Tools would mean every tool,
// so this must fail closed.
func TestClaudeAgentAllUnknownToolsIsNotLoaded(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "bad", "bad.md",
		"---\ndescription: all unknown\ntools: Bogus, AlsoBogus\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	if def := findDef(r.All(), "bad:bad"); def != nil {
		t.Fatalf("agent that maps to no tool was loaded with Tools=%v", def.Tools)
	}
	if !diagsContain(diags, "error", "grants no tool") {
		t.Errorf("want an error diagnostic for the skipped agent, got %+v", diags)
	}
}

// TestClaudeAgentEmptyToolsFieldIsNotLoaded covers the block-list and empty
// forms, where splitFrontmatter sees an empty value. Presence alone must not
// fall through to "all tools".
func TestClaudeAgentEmptyToolsFieldIsNotLoaded(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "blk", "blk.md",
		"---\ndescription: block list\ntools:\n  - Read\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	if def := findDef(r.All(), "blk:blk"); def != nil {
		t.Fatalf("agent with an empty tools field was loaded with Tools=%v", def.Tools)
	}
	if !diagsContain(diags, "error", "grants no tool") {
		t.Errorf("want an error diagnostic, got %+v", diags)
	}
}

// TestClaudeAgentFlowListAndNoToolsField checks the flow form and a file with
// no tools field. The flow form strips the brackets and quotes. No tools field
// keeps the default tool set (Tools nil).
func TestClaudeAgentFlowListAndNoToolsField(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "flow", "flow.md",
		"---\ndescription: flow form\ntools: [\"Read\", \"Grep\"]\n---\nBODY")
	installClaudeTestPlugin(t, home, "plain", "plain.md",
		"---\ndescription: no tools\n---\nBODY")

	r, _ := loadClaudeTestRegistry(t)
	flow := findDef(r.All(), "flow:flow")
	if flow == nil || strings.Join(flow.Tools, ",") != "read,grep" {
		t.Fatalf("flow form Tools = %+v, want [read grep]", flow)
	}
	plain := findDef(r.All(), "plain:plain")
	if plain == nil {
		t.Fatal("plain:plain not loaded")
	}
	if plain.Tools != nil {
		t.Errorf("no tools field gave Tools=%v, want nil (default tools)", plain.Tools)
	}
	if plain.Mode != AgentModeSubagent {
		t.Errorf("Mode = %q, want subagent when mode is unset", plain.Mode)
	}
}

// TestClaudeAgentModelInheritAndRealIDs checks that inherit and an ocode
// provider/model id are kept without a warning, and that a model alias is cleared
// with one.
func TestClaudeAgentModelInheritAndRealIDs(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "mdl", "inh.md",
		"---\ndescription: inherit\nmodel: inherit\n---\nBODY")
	installClaudeTestPlugin(t, home, "mdl2", "real.md",
		"---\ndescription: real id\nmodel: anthropic/claude-haiku-4-5\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	if d := findDef(r.All(), "mdl:inh"); d == nil || d.Model != "" {
		t.Fatalf("mdl:inh Model = %+v, want empty", d)
	}
	if d := findDef(r.All(), "mdl2:real"); d == nil || d.Model != "anthropic/claude-haiku-4-5" {
		t.Fatalf("mdl2:real Model = %+v, want anthropic/claude-haiku-4-5", d)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "alias") {
			t.Errorf("unexpected model-alias warning: %+v", d)
		}
	}
}

// TestClaudeAgentPermissionBlockIgnored checks that a permission block on a
// Claude Code plugin agent is dropped with a warning rather than applied to the
// parent session's shared PermissionManager.
func TestClaudeAgentPermissionBlockIgnored(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "perm", "perm.md",
		"---\ndescription: has perms\npermission:\n  edit: allow\n---\nBODY")

	r, diags := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "perm:perm")
	if def == nil {
		t.Fatalf("perm:perm not loaded; diags: %+v", diags)
	}
	if len(def.Permissions) != 0 {
		t.Errorf("Permissions = %v, want nil", def.Permissions)
	}
	if !diagsContain(diags, "warning", "permission block ignored") {
		t.Errorf("want a permission-ignored warning, got %+v", diags)
	}
}

// TestClaudeAgentUserGlobalOverridesPluginAgent checks precedence. A user global
// agent named like the plugin agent (the file stem is the native name) replaces
// it.
func TestClaudeAgentUserGlobalOverridesPluginAgent(t *testing.T) {
	home := setupClaudeAgentHome(t)
	installClaudeTestPlugin(t, home, "okf", "context.md", claudeContextAgent)
	writeClaudeTestFile(t, filepath.Join(home, ".config", "opencode", "agents", "okf:context.md"),
		"---\ndescription: user override\nmode: subagent\n---\nUSER_PROMPT")

	r, _ := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "okf:context")
	if def == nil {
		t.Fatal("okf:context not loaded")
	}
	if def.Description != "user override" || def.SystemPrompt != "USER_PROMPT" {
		t.Fatalf("user global did not override plugin agent: %+v", def)
	}
	if def.Tools != nil {
		t.Errorf("overridden def kept plugin Tools=%v", def.Tools)
	}
}

// TestClaudeAgentOcodeNativeParseUnchanged checks that a native file still
// parses as before: name from the file stem, the mode and model kept, and the
// tools key ignored.
func TestClaudeAgentOcodeNativeParseUnchanged(t *testing.T) {
	home := setupClaudeAgentHome(t)
	writeClaudeTestFile(t, filepath.Join(home, ".config", "opencode", "agents", "native.md"),
		"---\ndescription: native\nmode: primary\nmodel: anthropic/claude-haiku-4-5\ntools: Read\n---\nNATIVE")

	r, diags := loadClaudeTestRegistry(t)
	def := findDef(r.All(), "native")
	if def == nil {
		t.Fatalf("native not loaded; diags: %+v", diags)
	}
	if def.Mode != AgentModePrimary || def.Model != "anthropic/claude-haiku-4-5" {
		t.Errorf("native mode/model changed: mode=%q model=%q", def.Mode, def.Model)
	}
	if def.Tools != nil {
		t.Errorf("native agent gained Tools=%v from a tools key; native parsing must not change", def.Tools)
	}
}
