package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/plugins"
)

func parseOptionalFloat(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.Trim(s, "\"'")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func (r *AgentRegistry) LoadMarkdownAgents() []LoadDiagnostic {
	return r.reloadMarkdownAgents(nil)
}

// ReloadMarkdownAgents reloads the markdown registry entries and applies plugin
// enable filtering when enabled is non-nil. It keeps the built-ins and the
// existing precedence order (global → project → plugin project overrides).
func (r *AgentRegistry) ReloadMarkdownAgents(enabled map[string]bool) []LoadDiagnostic {
	return r.reloadMarkdownAgents(enabled)
}

func (r *AgentRegistry) reloadMarkdownAgents(enabled map[string]bool) []LoadDiagnostic {
	// The write lock spans the WHOLE rebuild, not just the final assignment: a
	// reader that arrived mid-reload would otherwise see a half-built list (or
	// race on the slice header). registerBuiltins and addLoaded below are
	// therefore only ever called with the lock held, and take no lock themselves.
	r.mu.Lock()
	defer r.mu.Unlock()

	r.diagnostic = nil
	r.defs = nil
	r.registerBuiltins()

	var globalLoaded []AgentDefinition
	var projectLoaded []AgentDefinition

	home, _ := os.UserHomeDir()
	globalAgentsDir := filepath.Join(home, ".config", "opencode", "agents")
	if runtime.GOOS == "windows" {
		globalAgentsDir = filepath.Join(os.Getenv("APPDATA"), "opencode", "agents")
	}

	// searchPath is one directory the markdown loader scans. plugin is set only
	// for the agents/ dir of a Claude Code-installed plugin, whose files get the
	// <plugin>:<name> names and the Claude Code frontmatter mapping.
	type searchPath struct {
		dir    string
		kind   string
		plugin string
	}
	searchPaths := []searchPath{}

	// Embedded (bundled) plugin agents — lowest precedence. Added first so every
	// disk-based agent definition (global markdown, project markdown, global
	// plugin, project plugin) overrides them by name via addLoaded's
	// last-wins semantics. When enabled is nil, the bundled agents load
	// unfiltered so the normal startup path sees them too.
	for _, bundledAgentsDir := range plugins.LoadBundledPluginAgentsDirPaths(enabled) {
		searchPaths = append(searchPaths, searchPath{dir: bundledAgentsDir, kind: "global"})
	}

	// Agents from plugins Claude Code installed. These sit above the bundled
	// agents and below the user's global dir and everything project-level, so a
	// user or project agent of the same full name still wins.
	for _, cp := range plugins.LoadClaudeCodePluginAgentsDirs(enabled) {
		searchPaths = append(searchPaths, searchPath{dir: cp.Dir, kind: "global", plugin: cp.Plugin})
	}

	searchPaths = append(searchPaths, searchPath{dir: globalAgentsDir, kind: "global"})

	cwd, _ := os.Getwd()
	projectDir := filepath.Join(cwd, ".opencode", "agents")
	searchPaths = append(searchPaths, searchPath{dir: projectDir, kind: "project"})

	// Global plugin agents behave like other global markdown agents: they load
	// before project-local markdown so project files can still override them by
	// name. When enabled is nil, all plugin agents load unfiltered.
	for _, pluginAgentsDir := range plugins.LoadGlobalPluginAgentsDirPaths(enabled) {
		searchPaths = append(searchPaths, searchPath{dir: pluginAgentsDir, kind: "global"})
	}

	// Project-local plugin agents override both global plugin agents and regular
	// project markdown agents by loading last.
	for _, pluginAgentsDir := range plugins.LoadProjectPluginAgentsDirPaths(enabled) {
		searchPaths = append(searchPaths, searchPath{dir: pluginAgentsDir, kind: "project"})
	}

	for _, sp := range searchPaths {
		if sp.dir == "" {
			continue
		}
		entries, err := os.ReadDir(sp.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			path := filepath.Join(sp.dir, entry.Name())
			var def *AgentDefinition
			var diags []LoadDiagnostic
			if sp.plugin != "" {
				def, diags = parseClaudePluginAgentFile(path, sp.plugin)
			} else {
				def, diags = parseAgentFile(path)
			}
			if def != nil {
				if sp.kind == "global" {
					globalLoaded = append(globalLoaded, *def)
				} else {
					projectLoaded = append(projectLoaded, *def)
				}
			}
			r.diagnostic = append(r.diagnostic, diags...)
		}
	}

	for _, def := range globalLoaded {
		r.addLoaded(def)
	}
	for _, def := range projectLoaded {
		r.addLoaded(def)
	}

	return r.diagnostic
}

type parsedFrontmatter struct {
	fields      map[string]string
	permissions map[string]interface{}
}

func parseAgentFile(path string) (*AgentDefinition, []LoadDiagnostic) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []LoadDiagnostic{{Level: "error", File: path, Message: "failed to read file: " + err.Error()}}
	}
	return parseAgentContent(string(data), path)
}

// claudeAgentToolGrants maps a Claude Code tool name in a plugin agent's tools
// field to the ocode tool it grants. A name not listed here grants nothing.
var claudeAgentToolGrants = map[string]string{
	"Read":      "read",
	"Grep":      "grep",
	"Glob":      "glob",
	"Bash":      "bash",
	"Edit":      "edit",
	"Write":     "write",
	"MultiEdit": "multiedit",
	"WebFetch":  "webfetch",
	"WebSearch": "websearch",
	"Skill":     "skill",
	"LS":        "list",
}

// parseClaudePluginAgentFile parses an agent file shipped by a Claude Code
// installed plugin. The body and the required fields follow parseAgentContent.
// The name is "<plugin>:<name>", where name is the frontmatter name when set and
// the file stem otherwise. Mode defaults to sub-agent. A model alias (no "/")
// becomes empty, which inherits the parent's model. The tools allowlist sets
// Tools, which the child enforces through isToolAllowed. Permissions stays empty
// on purpose: applying it writes rules onto the parent's shared PermissionManager.
func parseClaudePluginAgentFile(path, plugin string) (*AgentDefinition, []LoadDiagnostic) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []LoadDiagnostic{{Level: "error", File: path, Message: "failed to read file: " + err.Error()}}
	}
	content := string(data)
	def, diags := parseAgentContent(content, path)
	if def == nil {
		return nil, diags
	}
	fm, _, _ := splitFrontmatter(content)

	name := strings.Trim(strings.TrimSpace(fm.fields["name"]), "\"'")
	if name == "" {
		name = def.Name
	}
	def.Name = plugin + ":" + name

	if strings.TrimSpace(fm.fields["mode"]) == "" {
		def.Mode = AgentModeSubagent
	}

	if def.Model != "" && !strings.Contains(def.Model, "/") {
		if def.Model != "inherit" {
			diags = append(diags, LoadDiagnostic{Level: "warning", File: path, Message: "model " + def.Model + " is a Claude Code alias, not an ocode model id; using the parent model"})
		}
		def.Model = ""
	}

	if len(def.Permissions) > 0 {
		diags = append(diags, LoadDiagnostic{Level: "warning", File: path, Message: "permission block ignored for a Claude Code plugin agent; use tools"})
		def.Permissions = nil
	}

	// A present tools field is an allowlist. An empty Tools means "all tools",
	// so a field that grants nothing must not load the agent unrestricted.
	raw, hasTools := fm.fields["tools"]
	if !hasTools {
		return def, diags
	}
	tools, toolDiags := mapClaudeTools(raw, path)
	diags = append(diags, toolDiags...)
	if len(tools) == 0 {
		diags = append(diags, LoadDiagnostic{Level: "error", File: path, Message: "tools grants no tool ocode knows; agent not loaded"})
		return nil, diags
	}
	def.Tools = tools
	return def, diags
}

// mapClaudeTools turns a Claude Code tools value, a comma list or the flow form
// ["Read", "Grep"], into ocode tool names. Names with no mapping are dropped
// with a warning.
func mapClaudeTools(raw, path string) ([]string, []LoadDiagnostic) {
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")

	var tools []string
	var diags []LoadDiagnostic
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.Trim(strings.TrimSpace(part), "\"'")
		if name == "" {
			continue
		}
		mapped, ok := claudeAgentToolGrants[name]
		if !ok {
			diags = append(diags, LoadDiagnostic{Level: "warning", File: path, Message: "tool " + name + " has no ocode equivalent; dropped (grants nothing)"})
			continue
		}
		if !seen[mapped] {
			seen[mapped] = true
			tools = append(tools, mapped)
		}
	}
	return tools, diags
}

func parseAgentContent(content, source string) (*AgentDefinition, []LoadDiagnostic) {
	fm, body, parseErr := splitFrontmatter(content)
	if parseErr != "" {
		return nil, []LoadDiagnostic{{Level: "error", File: source, Message: parseErr}}
	}

	body = strings.TrimLeft(body, "\n")
	if strings.TrimSpace(body) == "" {
		return nil, []LoadDiagnostic{{Level: "error", File: source, Message: "missing prompt body"}}
	}

	var diags []LoadDiagnostic

	name := strings.TrimSuffix(filepath.Base(source), ".md")

	modeStr := fm.fields["mode"]
	if modeStr == "" {
		modeStr = "all"
	}
	mode := AgentMode(modeStr)
	if mode != AgentModePrimary && mode != AgentModeSubagent && mode != AgentModeAll {
		return nil, []LoadDiagnostic{{Level: "error", File: source, Message: "invalid mode: " + modeStr}}
	}

	desc := fm.fields["description"]
	if desc == "" {
		hidden := fm.fields["hidden"] == "true"
		if !hidden {
			return nil, []LoadDiagnostic{{Level: "error", File: source, Message: "missing description"}}
		}
	}

	hidden := fm.fields["hidden"] == "true"

	model := strings.TrimSpace(fm.fields["model"])
	color := strings.TrimSpace(fm.fields["color"])
	expectedOutput := strings.TrimSpace(fm.fields["expected_output"])
	temperature := parseOptionalFloat(fm.fields["temperature"])
	topP := parseOptionalFloat(fm.fields["top_p"])
	// Surface invalid numeric values as warnings rather than silently dropping.
	if raw := strings.TrimSpace(fm.fields["temperature"]); raw != "" && temperature == nil {
		diags = append(diags, LoadDiagnostic{Level: "warning", File: source, Message: "invalid temperature (not a number): " + raw})
	}
	if raw := strings.TrimSpace(fm.fields["top_p"]); raw != "" && topP == nil {
		diags = append(diags, LoadDiagnostic{Level: "warning", File: source, Message: "invalid top_p (not a number): " + raw})
	}

	var maxSteps int
	if stepsStr, ok := fm.fields["steps"]; ok && stepsStr != "" {
		if n, err := strconv.Atoi(stepsStr); err == nil && n > 0 {
			maxSteps = n
		}
	} else if maxStepsStr, ok := fm.fields["maxSteps"]; ok && maxStepsStr != "" {
		if n, err := strconv.Atoi(maxStepsStr); err == nil && n > 0 {
			maxSteps = n
		}
	}

	def := &AgentDefinition{
		Name:           name,
		Description:    desc,
		SystemPrompt:   body,
		Mode:           mode,
		Hidden:         hidden,
		Permissions:    fm.permissions,
		Source:         source,
		MaxSteps:       maxSteps,
		Model:          model,
		Color:          color,
		ExpectedOutput: expectedOutput,
		Temperature:    temperature,
		TopP:           topP,
	}

	return def, diags
}

func splitFrontmatter(content string) (*parsedFrontmatter, string, string) {
	fm := &parsedFrontmatter{
		fields:      make(map[string]string),
		permissions: make(map[string]interface{}),
	}

	lines := strings.Split(content, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		fm.fields = make(map[string]string)
		return fm, content, ""
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return fm, content, ""
	}

	var permCollect map[string]interface{}
	var permIndent int = -1

	for i := 1; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if permCollect != nil {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			if permIndent < 0 {
				permIndent = indent
			}
			if indent >= permIndent && trimmed != "" {
				idx := strings.Index(trimmed, ":")
				if idx != -1 {
					k := strings.TrimSpace(trimmed[:idx])
					v := strings.TrimSpace(trimmed[idx+1:])
					if v != "" {
						permCollect[k] = v
					}
				}
				continue
			}
			permCollect = nil
			permIndent = -1
		}

		idx := strings.Index(trimmed, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		if val == "" && (key == "permission" || key == "permissions") {
			permCollect = fm.permissions
			permIndent = -1
			continue
		}

		fm.fields[key] = val
	}

	body := strings.Join(lines[end+1:], "\n")

	return fm, body, ""
}
