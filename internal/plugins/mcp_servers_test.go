package plugins

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// writePluginFixture writes files (relative path → body) into
// root/.opencode/plugins/<name> and returns that plugin directory.
func writePluginFixture(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, ".opencode", "plugins", name)
	for rel, body := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	return dir
}

func mustAbs(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestPluginMCPWrappedShapeAndExpansion(t *testing.T) {
	isolatePluginEnv(t)
	dir := writePluginFixture(t, t.TempDir(), "fsplug", map[string]string{
		"plugin.json": `{"name":"fsplug"}`,
		".mcp.json": `{"mcpServers":{"fs":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/srv",` +
			`"args":["--root","{plugin_dir}/data","$HOME","${HOME}"],` +
			`"env":{"ROOT":"${CLAUDE_PLUGIN_ROOT}/data","HOME_REF":"${HOME}"}}}}`,
	})
	p, ok := readPluginManifest(dir)
	if !ok {
		t.Fatal("plugin with .mcp.json did not load")
	}
	abs := mustAbs(t, dir)
	mc, ok := p.MCPServers["fs"]
	if !ok {
		t.Fatalf("server fs missing: %+v", p.MCPServers)
	}
	wantCmd := []string{abs + "/bin/srv", "--root", abs + "/data", "$HOME", "${HOME}"}
	if !reflect.DeepEqual(mc.Command, wantCmd) {
		t.Fatalf("command = %q, want %q", mc.Command, wantCmd)
	}
	// Only ${CLAUDE_PLUGIN_ROOT} and {plugin_dir} expand; other variables stay.
	wantEnv := map[string]string{"ROOT": abs + "/data", "HOME_REF": "${HOME}"}
	if !reflect.DeepEqual(mc.Environment, wantEnv) {
		t.Fatalf("environment = %v, want %v", mc.Environment, wantEnv)
	}
	if mc.Type != "local" || !mc.Enabled {
		t.Fatalf("type/enabled = %q/%v, want local/true", mc.Type, mc.Enabled)
	}
}

func TestPluginMCPBareShapeKeepsOnlyStdio(t *testing.T) {
	isolatePluginEnv(t)
	dir := writePluginFixture(t, t.TempDir(), "bare", map[string]string{
		"plugin.json": `{"name":"bare"}`,
		".mcp.json": `{"local":{"command":"node","args":["s.js"]},` +
			`"remote":{"url":"https://example.test/mcp"},` +
			`"http":{"type":"http","command":"x"},` +
			`"sse":{"type":"sse","url":"https://example.test/sse"},` +
			`"nocmd":{"type":""},` +
			`"explicit":{"type":"stdio","command":"z"}}`,
	})
	p, ok := readPluginManifest(dir)
	if !ok {
		t.Fatal("plugin did not load")
	}
	var names []string
	for name := range p.MCPServers {
		names = append(names, name)
	}
	if len(names) != 2 || p.MCPServers["local"].Command[0] != "node" || p.MCPServers["explicit"].Command[0] != "z" {
		t.Fatalf("kept servers = %v, want exactly local and explicit", names)
	}
	// A plain user-configured server gets the same default Timeout.
	var userDefault config.MCPConfig
	if err := json.Unmarshal([]byte(`{"command":["x"]}`), &userDefault); err != nil {
		t.Fatal(err)
	}
	if got := p.MCPServers["local"].Timeout; got != userDefault.Timeout || got == 0 {
		t.Fatalf("timeout = %d, want the config default %d", got, userDefault.Timeout)
	}
}

func TestPluginMCPManifestInlineAndPath(t *testing.T) {
	isolatePluginEnv(t)
	root := t.TempDir()
	inline := writePluginFixture(t, root, "inl", map[string]string{
		".claude-plugin/plugin.json": `{"name":"inl","mcpServers":{"inline":{"command":"c"}}}`,
	})
	rel := writePluginFixture(t, root, "rel", map[string]string{
		".claude-plugin/plugin.json": `{"name":"rel","mcpServers":"./servers/mcp.json"}`,
		"servers/mcp.json":           `{"mcpServers":{"fromfile":{"command":"f"}}}`,
	})
	esc := writePluginFixture(t, root, "esc", map[string]string{
		".claude-plugin/plugin.json": `{"name":"esc","mcpServers":"../escape.json"}`,
	})
	// Lives beside the plugin dirs, so ../escape.json from esc resolves to it.
	writeFile(t, filepath.Join(root, ".opencode", "plugins", "escape.json"),
		`{"mcpServers":{"leak":{"command":"l"}}}`)

	if p, ok := readPluginManifest(inline); !ok || p.MCPServers["inline"].Command[0] != "c" {
		t.Fatalf("inline manifest servers = %+v", p.MCPServers)
	}
	if p, ok := readPluginManifest(rel); !ok || p.MCPServers["fromfile"].Command[0] != "f" {
		t.Fatalf("path manifest servers = %+v", p.MCPServers)
	}
	if p, ok := readPluginManifest(esc); !ok || len(p.MCPServers) != 0 {
		t.Fatalf("escaping path must not load servers, got %+v", p.MCPServers)
	}
}

func TestPluginMCPFirstDefinitionWins(t *testing.T) {
	isolatePluginEnv(t)
	dir := writePluginFixture(t, t.TempDir(), "prio", map[string]string{
		".mcp.json": `{"dup":{"command":"first"},"hidden":{"url":"https://r.test/mcp"}}`,
		".claude-plugin/plugin.json": `{"name":"prio","mcpServers":{"dup":{"command":"second"},` +
			`"hidden":{"command":"stdio-second"}}}`,
	})
	p, ok := readPluginManifest(dir)
	if !ok {
		t.Fatal("plugin did not load")
	}
	if got := p.MCPServers["dup"].Command; !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("dup command = %q, want the .mcp.json definition", got)
	}
	// The remote .mcp.json definition claims the name, so the manifest's stdio
	// definition of the same name does not take over.
	if _, ok := p.MCPServers["hidden"]; ok {
		t.Fatal("a later stdio definition must not replace an earlier remote one")
	}
}

func TestPluginMCPMalformedFileLoadsPluginAndLogsOnce(t *testing.T) {
	isolatePluginEnv(t)
	root := t.TempDir()
	writePluginFixture(t, root, "broken", map[string]string{
		"plugin.json": `{"name":"broken","description":"still loads"}`,
		".mcp.json":   `{not json`,
	})
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	var found *Plugin
	for _, p := range LoadAllPluginsForProject(root) {
		if p.Name == "broken" {
			pp := p
			found = &pp
		}
	}
	if found == nil {
		t.Fatal("plugin with a malformed .mcp.json must still load")
	}
	if len(found.MCPServers) != 0 {
		t.Fatalf("malformed file must yield no servers, got %+v", found.MCPServers)
	}
	// readPluginManifest runs on every discovery pass; the parse error is
	// reported once per process.
	readPluginManifest(found.Dir)
	if n := strings.Count(buf.String(), "ignoring MCP servers"); n != 1 {
		t.Fatalf("parse error logged %d times, want 1: %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), found.Dir) {
		t.Fatalf("log line should name the plugin dir: %q", buf.String())
	}
}

func TestMergeMCPServers(t *testing.T) {
	isolatePluginEnv(t)
	root := t.TempDir()
	writePluginFixture(t, root, "alpha", map[string]string{
		"plugin.json": `{"name":"alpha"}`,
		".mcp.json": `{"mcpServers":{"a1":{"command":"a1"},"shared":{"command":"alpha-shared"},` +
			`"userdis":{"command":"alpha-userdis"}}}`,
	})
	writePluginFixture(t, root, "beta", map[string]string{
		"plugin.json": `{"name":"beta"}`,
		".mcp.json":   `{"mcpServers":{"b1":{"command":"b1"},"shared":{"command":"beta-shared"}}}`,
	})
	writePluginFixture(t, root, "gamma", map[string]string{
		"plugin.json": `{"name":"gamma"}`,
		".mcp.json":   `{"mcpServers":{"g1":{"command":"g1"}}}`,
	})

	t.Run("enabled plugin added, disabled plugin skipped, user entries win", func(t *testing.T) {
		cfg := &config.Config{
			Plugins: map[string]config.PluginConfig{
				"alpha": {Enabled: true},
				"beta":  {Enabled: true},
				"gamma": {Enabled: false},
			},
			MCP: map[string]config.MCPConfig{
				"a1":      {Command: []string{"mine"}, Enabled: true},
				"userdis": {Command: []string{"mine-off"}, Enabled: false},
			},
		}
		got := MergeMCPServers(cfg, root)
		want := map[string]string{"b1": "beta", "shared": "alpha"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("merged = %v, want %v", got, want)
		}
		if _, ok := cfg.MCP["g1"]; ok {
			t.Fatal("disabled plugin gamma must not contribute g1")
		}
		if c := cfg.MCP["a1"].Command; !reflect.DeepEqual(c, []string{"mine"}) {
			t.Fatalf("user enabled entry overwritten: %q", c)
		}
		if mc := cfg.MCP["userdis"]; mc.Enabled || mc.Command[0] != "mine-off" {
			t.Fatalf("user disabled entry overwritten: %+v", mc)
		}
		if c := cfg.MCP["shared"].Command; !reflect.DeepEqual(c, []string{"alpha-shared"}) {
			t.Fatalf("first plugin in precedence order must win a shared name, got %q", c)
		}
	})

	t.Run("project-local plugins need an explicit enable", func(t *testing.T) {
		// alpha, beta and gamma live under the project's .opencode/plugins, so
		// with no config entry none of their servers start.
		if got := MergeMCPServers(&config.Config{}, root); got != nil {
			t.Fatalf("project plugins merged without an explicit enable: %v", got)
		}
		cfg := &config.Config{Plugins: map[string]config.PluginConfig{
			"alpha": {Enabled: true},
			"beta":  {Enabled: true},
		}}
		got := MergeMCPServers(cfg, root)
		want := map[string]string{"a1": "alpha", "userdis": "alpha", "shared": "alpha", "b1": "beta"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("merged = %v, want %v", got, want)
		}
		if _, ok := cfg.MCP["g1"]; ok {
			t.Fatal("gamma has no explicit entry, so g1 must not merge")
		}
		if cfg.MCP == nil || !cfg.MCP["b1"].Enabled {
			t.Fatalf("cfg.MCP not initialised with plugin servers: %+v", cfg.MCP)
		}
	})

	t.Run("explicit disabled entry keeps project plugin off", func(t *testing.T) {
		cfg := &config.Config{Plugins: map[string]config.PluginConfig{"alpha": {Enabled: false}}}
		if got := MergeMCPServers(cfg, root); got["shared"] == "alpha" || got["a1"] == "alpha" {
			t.Fatalf("disabled project plugin merged: %v", got)
		}
	})

	t.Run("copy on write leaves an aliased map untouched", func(t *testing.T) {
		shared := map[string]config.MCPConfig{"keep": {Command: []string{"k"}}}
		enable := map[string]config.PluginConfig{"alpha": {Enabled: true}, "beta": {Enabled: true}}
		cfgA := &config.Config{MCP: shared, Plugins: enable}
		cfgB := &config.Config{MCP: shared, Plugins: enable}
		MergeMCPServers(cfgA, root)
		if len(shared) != 1 {
			t.Fatalf("shared map was modified: %v", shared)
		}
		if _, ok := cfgB.MCP["b1"]; ok {
			t.Fatal("alias config picked up merged servers")
		}
		if _, ok := cfgA.MCP["b1"]; !ok {
			t.Fatal("merged config is missing b1")
		}
		if _, ok := cfgA.MCP["keep"]; !ok {
			t.Fatal("merged config dropped the user's entry")
		}
	})

	t.Run("deterministic across runs and nil safe", func(t *testing.T) {
		enabled := func() *config.Config {
			return &config.Config{Plugins: map[string]config.PluginConfig{
				"alpha": {Enabled: true}, "beta": {Enabled: true},
			}}
		}
		first := MergeMCPServers(enabled(), root)
		for i := 0; i < 5; i++ {
			if again := MergeMCPServers(enabled(), root); !reflect.DeepEqual(first, again) {
				t.Fatalf("run %d = %v, first = %v", i, again, first)
			}
		}
		if got := MergeMCPServers(nil, root); got != nil {
			t.Fatalf("nil config merged to %v, want nil", got)
		}
	})
}

func TestMergeMCPServersNonProjectPluginsFollowDefaultEnabled(t *testing.T) {
	home := isolatePluginEnv(t)
	root := t.TempDir()

	// Global ocode plugin: default-enabled, no config entry needed.
	globalDir := filepath.Join(home, ".config", "opencode", "plugins", "gp")
	writeFile(t, filepath.Join(globalDir, "plugin.json"), `{"name":"gp"}`)
	writeFile(t, filepath.Join(globalDir, ".mcp.json"), `{"gsrv":{"command":"g"}}`)

	// Claude Code install: default-enabled through Claude Code's own state.
	ccDir := filepath.Join(home, "cc-cache", "ccp")
	writeFile(t, filepath.Join(ccDir, ".claude-plugin", "plugin.json"), `{"name":"ccp"}`)
	writeFile(t, filepath.Join(ccDir, ".mcp.json"), `{"csrv":{"command":"c"}}`)
	installInClaudeCode(t, home, map[string]string{"ccp@market": ccDir})

	// Project-local plugin: gated.
	local := writePluginFixture(t, root, "local", map[string]string{
		"plugin.json": `{"name":"local"}`,
		".mcp.json":   `{"lsrv":{"command":"l"}}`,
	})

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	got := MergeMCPServers(&config.Config{}, root)
	want := map[string]string{"gsrv": "gp", "csrv": "ccp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v (project plugin must stay gated)", got, want)
	}

	byName := map[string]Plugin{}
	for _, p := range LoadAllPluginsForProject(root) {
		byName[p.Name] = p
	}
	if !byName["local"].ProjectScoped {
		t.Fatal("plugin under the project's .opencode/plugins must be ProjectScoped")
	}
	if byName["gp"].ProjectScoped || byName["ccp"].ProjectScoped {
		t.Fatal("global and Claude Code plugins must not be ProjectScoped")
	}

	// The gate is logged once per plugin dir, naming the enable command.
	MergeMCPServers(&config.Config{}, root)
	if n := strings.Count(buf.String(), "/plugin enable local"); n != 1 {
		t.Fatalf("gated plugin logged %d times, want 1: %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), local) {
		t.Fatalf("gate log should name the plugin dir: %q", buf.String())
	}
}

func TestMCPProvidedByPluginMessage(t *testing.T) {
	got := MCPProvidedByPluginMessage("fs", "fsplug")
	want := `MCP server "fs" is provided by plugin "fsplug"; disable the plugin to turn it off, or add your own "fs" entry to opencode.json to override it`
	if got != want {
		t.Fatalf("message = %q\nwant      %q", got, want)
	}
}

func TestMergeMCPServersRequiresAny(t *testing.T) {
	home := isolatePluginEnv(t)
	root := t.TempDir()

	// A bare name is resolved through PATH; a path is a file that must exist.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "ocode-req-test-bin"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	pathBinary := filepath.Join(t.TempDir(), "tool-bin")
	writeFile(t, pathBinary, "#!/bin/sh\n")
	// "~/" paths expand to the home directory isolatePluginEnv set.
	writeFile(t, filepath.Join(home, ".local", "bin", "homebin"), "#!/bin/sh\n")

	writePluginFixture(t, root, "req", map[string]string{
		"plugin.json": `{"name":"req"}`,
		".mcp.json": `{"mcpServers":{` +
			`"by-path":{"command":"by-path","requiresAny":["no-such-binary-xyz","` + pathBinary + `"]},` +
			`"by-name":{"command":"by-name","requiresAny":["ocode-req-test-bin"]},` +
			`"by-home":{"command":"by-home","requiresAny":["~/.local/bin/homebin"]},` +
			`"unmet":{"command":"unmet","requiresAny":["no-such-binary-xyz","` + filepath.Join(home, "missing") + `"]},` +
			`"unmet-dir":{"command":"unmet-dir","requiresAny":["` + binDir + `"]},` +
			`"plain":{"command":"plain"}}}`,
	})
	cfg := &config.Config{Plugins: map[string]config.PluginConfig{"req": {Enabled: true}}}
	got := MergeMCPServers(cfg, root)

	want := map[string]string{"by-path": "req", "by-name": "req", "by-home": "req", "plain": "req"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
	for _, name := range []string{"unmet", "unmet-dir"} {
		if _, ok := cfg.MCP[name]; ok {
			t.Fatalf("server %s with no resolvable requiresAny was merged", name)
		}
	}
	if _, ok := cfg.MCP["plain"]; !ok {
		t.Fatal("server without requiresAny was not merged")
	}
}

func TestRequiresAnyIsIgnoredByParsingAndKeptOnPlugin(t *testing.T) {
	isolatePluginEnv(t)
	root := t.TempDir()
	dir := writePluginFixture(t, root, "req", map[string]string{
		"plugin.json": `{"name":"req"}`,
		".mcp.json":   `{"mcpServers":{"x":{"command":"x","requiresAny":["no-such-binary-xyz"],"someFutureKey":true}}}`,
	})
	servers, requires := pluginMCPServers(dir, nil)
	if _, ok := servers["x"]; !ok {
		t.Fatalf("server with an unmet requirement was dropped at parse time: %+v", servers)
	}
	if !reflect.DeepEqual(requires["x"], []string{"no-such-binary-xyz"}) {
		t.Fatalf("requires = %v", requires)
	}
	if _, ok := requires["plain"]; ok {
		t.Fatal("a server without requiresAny got a requirement entry")
	}
}

func TestMergeSharedMCPServersSkipsProjectLocalPlugins(t *testing.T) {
	isolatePluginEnv(t)
	root := t.TempDir()
	writePluginFixture(t, root, "projplug", map[string]string{
		"plugin.json": `{"name":"projplug"}`,
		".mcp.json":   `{"mcpServers":{"proj-srv":{"command":"proj"}}}`,
	})
	global := filepath.Join(t.TempDir(), "globalplug")
	writeFile(t, filepath.Join(global, "plugin.json"), `{"name":"globalplug"}`)
	writeFile(t, filepath.Join(global, ".mcp.json"), `{"mcpServers":{"glob-srv":{"command":"glob"}}}`)

	enabled := func() *config.Config {
		return &config.Config{Plugins: map[string]config.PluginConfig{"projplug": {Enabled: true}}}
	}

	// The full merge includes the explicitly enabled project-local plugin.
	full := enabled()
	if got := MergeMCPServers(full, root); got["proj-srv"] != "projplug" {
		t.Fatalf("MergeMCPServers = %v, want proj-srv from projplug", got)
	}

	// The shared merge must not carry the project's servers into the shared config.
	shared := enabled()
	if got := MergeSharedMCPServers(shared, root); len(got) != 0 {
		t.Fatalf("MergeSharedMCPServers = %v, want no project-local servers", got)
	}
	if _, ok := shared.MCP["proj-srv"]; ok {
		t.Fatal("the shared config received a project-local plugin server")
	}
}
