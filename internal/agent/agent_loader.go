package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (r *AgentRegistry) LoadMarkdownAgents() []LoadDiagnostic {
	var globalLoaded []AgentDefinition
	var projectLoaded []AgentDefinition

	home, _ := os.UserHomeDir()
	searchPaths := []struct {
		dir  string
		kind string
	}{
		{filepath.Join(home, ".config", "opencode", "agents"), "global"},
	}

	if runtime.GOOS == "windows" {
		searchPaths[0].dir = filepath.Join(os.Getenv("APPDATA"), "opencode", "agents")
	}

	cwd, _ := os.Getwd()
	projectDir := filepath.Join(cwd, ".opencode", "agents")
	searchPaths = append(searchPaths, struct {
		dir  string
		kind string
	}{projectDir, "project"})

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
			def, diags := parseAgentFile(path)
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

	knownUnsupported := map[string]bool{
		"model": true, "temperature": true, "top_p": true, "steps": true, "maxSteps": true,
	}
	for k := range fm.fields {
		if knownUnsupported[k] {
			diags = append(diags, LoadDiagnostic{Level: "warning", File: source, Message: "ignored unsupported field: " + k})
		}
	}

	def := &AgentDefinition{
		Name:         name,
		Description:  desc,
		SystemPrompt: body,
		Mode:         mode,
		Hidden:       hidden,
		Permissions:  fm.permissions,
		Source:       source,
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
