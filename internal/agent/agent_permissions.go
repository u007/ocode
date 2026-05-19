package agent

func buildPermissionManagerFromAgent(permissions map[string]interface{}) *PermissionManager {
	_, pm := buildPermissionManagerFromAgentWithDiags(permissions)
	return pm
}

func buildPermissionManagerFromAgentWithDiags(permissions map[string]interface{}) ([]LoadDiagnostic, *PermissionManager) {
	pm := NewPermissionManager()
	var diags []LoadDiagnostic

	groupToolMap := map[string][]string{
		"read":      {"read"},
		"edit":      {"write", "edit", "multi_edit", "multiedit", "replace_lines", "patch", "format"},
		"glob":      {"glob"},
		"grep":      {"grep"},
		"bash":      {"bash"},
		"task":      {"task", "agent"},
		"webfetch":  {"webfetch"},
		"websearch": {"websearch"},
		"skill":     {"skill"},
		"question":  {"question"},
		"lsp":       {"lsp"},
	}

	knownGroups := map[string]bool{}
	for g := range groupToolMap {
		knownGroups[g] = true
	}

	for group, val := range permissions {
		if !knownGroups[group] {
			diags = append(diags, LoadDiagnostic{Level: "warning", Message: "unknown permission group: " + group + " — treated as deny"})
			continue
		}

		levelStr, ok := val.(string)
		if !ok {
			diags = append(diags, LoadDiagnostic{Level: "warning", Message: "skipped non-shorthand permission for " + group + " — treated as ask"})
			for _, toolName := range groupToolMap[group] {
				pm.SetRule(toolName, PermissionAsk)
			}
			continue
		}

		level := PermissionLevel(levelStr)
		if !validPermissionLevel(level) {
			continue
		}

		for _, toolName := range groupToolMap[group] {
			pm.SetRule(toolName, level)
		}
	}

	return diags, pm
}
