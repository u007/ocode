package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// parseClaudePermission splits a Claude Code permission string like
// "Bash(git push *)" or "Bash" into tool and inner pattern.
// Returns tool, pattern, ok. A bare tool name yields pattern "".
func parseClaudePermission(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	idx := strings.Index(raw, "(")
	if idx == -1 {
		return raw, "", true
	}
	if !strings.HasSuffix(raw, ")") {
		return "", "", false
	}
	tool := strings.TrimSpace(raw[:idx])
	pattern := raw[idx+1 : len(raw)-1]
	return tool, pattern, true
}

// normalizeClaudeCommand strips shell quoting that rebuildCommandLine adds so
// that a pattern written as `rm -rf $HOME` matches a rebuilt command
// `rm -rf "$HOME"` and a pattern `curl -s "https://..."` matches a rebuilt
// `curl -s https://...`. Both sides are stripped of double quotes before
// comparison — the quoting is a rebuild artifact, not a semantic difference.
func normalizeClaudeCommand(s string) string {
	return strings.ReplaceAll(s, `"`, "")
}

// claudePatternMatches reports whether command matches a Claude Bash pattern
// where "*" is a wildcard matching any character sequence (including empty).
// Pattern without "*" requires an exact match. Matching is quote-insensitive
// (see normalizeClaudeCommand).
func claudePatternMatches(command, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	command = normalizeClaudeCommand(command)
	pattern = normalizeClaudeCommand(pattern)
	// Build a regex from pattern: escape everything except "*" -> ".*"
	var re strings.Builder
	re.WriteString("^")
	for _, ch := range pattern {
		if ch == '*' {
			re.WriteString(".*")
			continue
		}
		// Escape regexp meta-characters.
		if strings.ContainsRune(`.+?^${}()|[]\`, ch) {
			re.WriteRune('\\')
		}
		re.WriteRune(ch)
	}
	re.WriteString("$")
	matched, _ := regexp.MatchString(re.String(), command)
	return matched
}

// claudeSettingsFile mirrors the shape of .claude/settings.json relevant to
// permissions. Only permissions.allow/ask are read; other keys are ignored.
// permissions.deny is deliberately NOT read: ocode's banned commands come only
// from its own config (/ban), never from Claude Code's.
type claudeSettingsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Ask   []string `json:"ask"`
	} `json:"permissions"`
}

// claudeSettingsPaths returns the ordered list of Claude settings files to
// merge for workDir. Global user settings come first; project files follow so
// they are additive. Merge is union — an ask at any level is in force.
func claudeSettingsPaths(workDir string) []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if wd := strings.TrimSpace(workDir); wd != "" {
		clean := filepath.Clean(wd)
		paths = append(paths, filepath.Join(clean, ".claude", "settings.json"))
		paths = append(paths, filepath.Join(clean, ".claude", "settings.local.json"))
	}
	return paths
}

func loadClaudeSettingsFile(path string) (allow, ask []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var f claudeSettingsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil
	}
	return f.Permissions.Allow, f.Permissions.Ask
}

// addClaudeRule classifies a single Claude permission string into the
// manager's bash-specific allow/ask buckets. Non-Bash tools are ignored.
func (pm *PermissionManager) addClaudeRule(raw string, level PermissionLevel) {
	tool, pattern, ok := parseClaudePermission(raw)
	if !ok {
		return
	}
	if !strings.EqualFold(tool, "bash") {
		return
	}
	pattern = strings.TrimSpace(pattern)
	switch level {
	case PermissionAllow:
		if pattern == "" {
			// Bare "Bash" allow => treat as wildcard allow-all.
			pm.claudeBashAllow = append(pm.claudeBashAllow, "*")
		} else {
			pm.claudeBashAllow = append(pm.claudeBashAllow, pattern)
		}
	case PermissionAsk:
		if pattern == "" {
			if pm.claudeBareAsk == nil {
				pm.claudeBareAsk = make(map[string]bool)
			}
			pm.claudeBareAsk["bash"] = true
		} else {
			pm.claudeBashAsk = append(pm.claudeBashAsk, pattern)
		}
	}
}

// LoadClaudePermissions (re)loads Claude Code's .claude/settings.json allow/
// ask rules for workDir. It merges global (~/.claude/settings.json) with
// project (.claude/settings.json) and local (.claude/settings.local.json).
// Ask wins over allow — matching is enforced at Decide time. Claude's deny
// rules are not loaded.
func (pm *PermissionManager) LoadClaudePermissions(workDir string) {
	if pm == nil {
		return
	}
	// Reset previous Claude rules.
	pm.claudeBashAllow = nil
	pm.claudeBashAsk = nil
	pm.claudeBareAsk = nil

	for _, p := range claudeSettingsPaths(workDir) {
		allow, ask := loadClaudeSettingsFile(p)
		for _, raw := range allow {
			pm.addClaudeRule(raw, PermissionAllow)
		}
		for _, raw := range ask {
			pm.addClaudeRule(raw, PermissionAsk)
		}
	}
}

func (pm *PermissionManager) claudeIsAsk(command string) bool {
	if pm == nil {
		return false
	}
	if pm.claudeBareAsk != nil && pm.claudeBareAsk["bash"] {
		return true
	}
	cmd := strings.TrimSpace(command)
	for _, pat := range pm.claudeBashAsk {
		if claudePatternMatches(cmd, pat) {
			return true
		}
	}
	return false
}

func (pm *PermissionManager) claudeIsAllowed(command string) bool {
	if pm == nil {
		return false
	}
	cmd := strings.TrimSpace(command)
	for _, pat := range pm.claudeBashAllow {
		if claudePatternMatches(cmd, pat) {
			return true
		}
	}
	return false
}
