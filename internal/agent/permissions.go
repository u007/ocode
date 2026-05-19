package agent

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jamesmercstudio/ocode/internal/config"
)

type PermissionLevel string

const (
	PermissionAllow PermissionLevel = "allow"
	PermissionDeny  PermissionLevel = "deny"
	PermissionAsk   PermissionLevel = "ask"
)

type PermissionMode string

const (
	PermissionModeNormal PermissionMode = "normal"
	PermissionModeYOLO   PermissionMode = "yolo"
	PermissionModeLocked PermissionMode = "locked"
)

type PermissionScope string

const (
	PermissionScopeTool       PermissionScope = "tool"
	PermissionScopeBashPrefix PermissionScope = "bash_prefix"
)

type PermissionRequest struct {
	ToolName string          `json:"tool_name"`
	Args     json.RawMessage `json:"args,omitempty"`
	Command  string          `json:"command,omitempty"`
	Prefix   string          `json:"prefix,omitempty"`
	Scope    PermissionScope `json:"scope"`
	Rule     string          `json:"rule"`
}

type PermissionDecision struct {
	Level   PermissionLevel
	Request *PermissionRequest
}

type PermissionManager struct {
	mode              PermissionMode
	rules             map[string]PermissionLevel
	patterns          []patternRule
	bashPrefixes      map[string]PermissionLevel
	workDir           string
	webfetchDomains   map[string]PermissionLevel
}

type patternRule struct {
	pattern string
	level   PermissionLevel
}

func NewPermissionManager() *PermissionManager {
	pm := &PermissionManager{
		mode:            PermissionModeNormal,
		rules:           make(map[string]PermissionLevel),
		patterns:        make([]patternRule, 0),
		bashPrefixes:    make(map[string]PermissionLevel),
		webfetchDomains: make(map[string]PermissionLevel),
	}
	for _, name := range []string{"read", "glob", "grep", "list", "lsp", "skill", "question"} {
		pm.rules[name] = PermissionAllow
	}
	for _, name := range []string{"write", "edit", "multi_edit", "multiedit", "replace_lines", "patch", "format"} {
		pm.SetRule(name, PermissionAllow)
	}
	for _, name := range []string{"delete", "bash", "webfetch", "websearch", "agent", "task", "mcp_*"} {
		pm.SetRule(name, PermissionAsk)
	}
	return pm
}

func (pm *PermissionManager) Check(toolName string) PermissionLevel {
	if level, ok := pm.rules[toolName]; ok {
		return level
	}

	for _, p := range pm.patterns {
		if matchPattern(p.pattern, toolName) {
			return p.level
		}
	}

	return PermissionAsk
}

func (pm *PermissionManager) LoadFromConfig(cfg map[string]interface{}) {
	if cfg == nil {
		return
	}
	for k, v := range cfg {
		s, ok := v.(string)
		if !ok {
			continue
		}
		level := PermissionLevel(s)
		if level != PermissionAllow && level != PermissionDeny && level != PermissionAsk {
			continue
		}
		if strings.Contains(k, "*") {
			pm.patterns = append(pm.patterns, patternRule{pattern: k, level: level})
		} else {
			pm.rules[k] = level
		}
	}
}

func (pm *PermissionManager) LoadFromOcode(cfg config.PermissionConfig) {
	if cfg.Mode != "" {
		pm.SetMode(PermissionMode(cfg.Mode))
	}
	for k, v := range cfg.Tools {
		level := PermissionLevel(v)
		if validPermissionLevel(level) {
			pm.SetRule(k, level)
		}
	}
	for k, v := range cfg.Bash.Prefixes {
		level := PermissionLevel(v)
		if validPermissionLevel(level) {
			pm.SetBashPrefixRule(k, level)
		}
	}
}

func (pm *PermissionManager) Decide(toolName string, args json.RawMessage) PermissionDecision {
	if pm.mode == PermissionModeLocked {
		if isReadOnlyTool(toolName) {
			return PermissionDecision{Level: PermissionAllow}
		}
		return PermissionDecision{Level: PermissionDeny}
	}

	if toolName == "bash" {
		command := bashCommand(args)
		if isHardBlockedCommand(command) {
			return PermissionDecision{Level: PermissionDeny}
		}
		prefix, ok := bashPrefix(command)
		if pm.mode == PermissionModeYOLO {
			return PermissionDecision{Level: PermissionAllow}
		}
		if ok {
			if level, exists := pm.bashPrefixes[prefix]; exists {
				if level == PermissionAsk {
					return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
				}
				return PermissionDecision{Level: level}
			}
		}
		// Check safe bash commands after prefix rules
		if isSafeBashCommand(command) {
			return PermissionDecision{Level: PermissionAllow}
		}
		level := pm.Check(toolName)
		if level == PermissionAsk {
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
		}
		return PermissionDecision{Level: level}
	}

	if pm.mode == PermissionModeYOLO {
		return PermissionDecision{Level: PermissionAllow}
	}

	// Path-scoped file tools
	fileTools := map[string]bool{
		"read": true, "write": true, "edit": true, "delete": true,
		"multiedit": true, "replace_lines": true, "glob": true, "grep": true,
		"list": true, "lsp": true, "patch": true, "format": true, "todowrite": true,
	}
	if fileTools[toolName] {
		path := extractPathFromArgs(toolName, args)
		if path != "" {
			if !isWithinWorkDir(pm, path) {
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".out_of_scope",
				}}
			}
			if isSensitivePath(path) {
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".sensitive_path",
				}}
			}
			if toolName == "delete" {
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".delete",
				}}
			}
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// Webfetch domain tracking
	if toolName == "webfetch" {
		path := extractPathFromArgs(toolName, args)
		domain := extractDomainFromURL(path)
		if domain != "" {
			if level, exists := pm.webfetchDomains[domain]; exists {
				return PermissionDecision{Level: level}
			}
			return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
				ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "webfetch.domain." + domain,
			}}
		}
	}

	level := pm.Check(toolName)
	if level == PermissionAsk {
		return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName}}
	}
	return PermissionDecision{Level: level}
}

func bashPermissionRequest(args json.RawMessage, command, prefix string) *PermissionRequest {
	scope := PermissionScopeTool
	rule := "tool.bash"
	if prefix != "" {
		scope = PermissionScopeBashPrefix
		rule = "bash.prefix." + prefix
	}
	return &PermissionRequest{ToolName: "bash", Args: args, Command: command, Prefix: prefix, Scope: scope, Rule: rule}
}

func isWithinWorkDir(pm *PermissionManager, rawPath string) bool {
	if pm.workDir == "" {
		return true
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// File may not exist yet (e.g., write creating new file); check directory
		dir := filepath.Dir(absPath)
		resolved, err = filepath.EvalSymlinks(dir)
		if err != nil {
			return false
		}
		resolved = filepath.Join(resolved, filepath.Base(absPath))
	}
	workDirSep := pm.workDir + string(filepath.Separator)
	return resolved == pm.workDir || strings.HasPrefix(resolved, workDirSep)
}

func isSensitivePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)

	// Exact match sensitive filenames
	sensitiveBases := []string{".env", ".netrc", ".npmrc", ".pypirc"}
	for _, s := range sensitiveBases {
		if base == s {
			return true
		}
	}

	// .env.* variants
	if strings.HasPrefix(base, ".env.") {
		return true
	}

	// SSH keys
	sshKeyBases := []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"}
	for _, k := range sshKeyBases {
		if base == k {
			return true
		}
	}

	// Certificate/key files
	certSuffixes := []string{".pem", ".key", ".p12", ".pfx", ".secrets"}
	for _, suffix := range certSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}

	// Paths under sensitive directories
	sensitiveDirs := []string{".git/", ".github/workflows/", ".aws/"}
	for _, dir := range sensitiveDirs {
		if strings.Contains("/"+clean+"/", "/"+dir) || strings.HasPrefix(clean, dir) {
			return true
		}
	}

	return false
}

func extractPathFromArgs(toolName string, args json.RawMessage) string {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	switch toolName {
	case "read", "write", "delete", "edit", "multiedit", "replace_lines", "format", "lsp", "patch", "todowrite":
		return params.Path
	case "glob", "list":
		if params.Pattern != "" {
			return params.Pattern
		}
		return params.Path
	case "grep":
		return params.Path
	case "webfetch":
		return params.URL
	default:
		return ""
	}
}

func isSafeBashCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}

	// Safe command prefixes
	safePrefixes := []string{
		"git ", "git\t",
		"ls ", "ls\t", "ls -",
		"pwd", "pwd ",
		"echo ", "echo\t",
		"cat ", "cat\t",
		"grep ", "rg ", "ag ",
		"find ", // but check for -exec
		"wc ", "sort ", "uniq ", "head ", "tail ",
		"which ", "type ", "env ",
		"go build", "go test", "go run", "go vet", "go fmt",
		"npm run ", "yarn run ", "bun run ", "pnpm run ",
		"make ",
	}

	for _, prefix := range safePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			// Special check: find with -exec is not safe
			if strings.HasPrefix(trimmed, "find ") && strings.Contains(trimmed, " -exec ") {
				return false
			}
			// Special check: make without shell metachars
			if strings.HasPrefix(trimmed, "make ") {
				for _, meta := range []string{"|", "&", ";", "$(", "`"} {
					if strings.Contains(trimmed, meta) {
						return false
					}
				}
			}
			return true
		}
	}

	return false
}

func extractDomainFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		hostname = parsed.Host
	}
	return hostname
}

func matchPattern(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.SplitN(pattern, "*", 2)
	if len(parts) == 2 {
		if parts[1] == "" {
			return strings.HasPrefix(name, parts[0])
		}
		if parts[0] == "" {
			return strings.HasSuffix(name, parts[1])
		}
		return strings.HasPrefix(name, parts[0]) && strings.HasSuffix(name, parts[1])
	}
	return false
}

func (pm *PermissionManager) SetRule(toolName string, level PermissionLevel) {
	if !validPermissionLevel(level) {
		return
	}
	if strings.Contains(toolName, "*") {
		pm.patterns = append(pm.patterns, patternRule{pattern: toolName, level: level})
	} else {
		pm.rules[toolName] = level
	}
}

func (pm *PermissionManager) SetBashPrefixRule(prefix string, level PermissionLevel) {
	if prefix == "" || !validPermissionLevel(level) {
		return
	}
	pm.bashPrefixes[prefix] = level
}

func (pm *PermissionManager) SetMode(mode PermissionMode) {
	switch mode {
	case PermissionModeNormal, PermissionModeYOLO, PermissionModeLocked:
		pm.mode = mode
	}
}

func (pm *PermissionManager) SetWorkDir(path string) {
	pm.workDir = filepath.Clean(path)
}

func (pm *PermissionManager) SetWebfetchDomain(domain string, level PermissionLevel) {
	if validPermissionLevel(level) {
		pm.webfetchDomains[domain] = level
	}
}

func (pm *PermissionManager) Mode() PermissionMode {
	if pm.mode == "" {
		return PermissionModeNormal
	}
	return pm.mode
}

func (pm *PermissionManager) Rules() map[string]PermissionLevel {
	result := make(map[string]PermissionLevel)
	for k, v := range pm.rules {
		result[k] = v
	}
	for _, p := range pm.patterns {
		result[p.pattern] = p.level
	}
	return result
}

func (pm *PermissionManager) BashPrefixRules() map[string]PermissionLevel {
	result := make(map[string]PermissionLevel)
	for k, v := range pm.bashPrefixes {
		result[k] = v
	}
	return result
}

func (pm *PermissionManager) ExportConfig() config.PermissionConfig {
	tools := make(map[string]string)
	for k, v := range pm.Rules() {
		tools[k] = string(v)
	}
	prefixes := make(map[string]string)
	for k, v := range pm.BashPrefixRules() {
		prefixes[k] = string(v)
	}
	return config.PermissionConfig{Mode: string(pm.Mode()), Tools: tools, Bash: config.BashPermissionConfig{Prefixes: prefixes}}
}

func validPermissionLevel(level PermissionLevel) bool {
	return level == PermissionAllow || level == PermissionDeny || level == PermissionAsk
}

func isReadOnlyTool(name string) bool {
	switch name {
	case "read", "glob", "grep", "list", "lsp", "webfetch", "websearch", "skill", "question":
		return true
	default:
		return false
	}
}

func bashCommand(args json.RawMessage) string {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	return strings.TrimSpace(params.Command)
}

func bashPrefix(command string) (string, bool) {
	if command == "" || shellCompound(command) {
		return "", false
	}
	fields := splitShellFields(command)
	if len(fields) == 0 {
		return "", false
	}
	if fields[0] == "sudo" || strings.Contains(fields[0], "=") {
		return "", false
	}
	return fields[0], true
}

func shellCompound(command string) bool {
	for _, token := range []string{"&&", "||", ";", "|", "`", "$(", ">", "<"} {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func splitShellFields(command string) []string {
	var fields []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		}
		if unicode.IsSpace(r) && !inSingle && !inDouble {
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func isHardBlockedCommand(command string) bool {
	compact := strings.Join(splitShellFields(command), " ")
	if compact == "rm -rf /" || compact == "rm -fr /" || strings.Contains(command, ":(){ :|:& };:") {
		return true
	}
	// Hard-block destructive and exfiltration patterns
	blockedPatterns := []string{
		"| bash", "| sh", "| python", "| perl",  // pipe to shell
		"dd if=", "mkfs",                         // disk/partition write
		"; sudo", "&& sudo", "| sudo",            // privilege escalation chains
	}
	for _, p := range blockedPatterns {
		if strings.Contains(command, p) {
			return true
		}
	}
	return false
}

func IsAllowedPlanWritePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	if base == "PLAN.md" || strings.HasSuffix(base, ".plan.md") {
		return true
	}
	if strings.HasPrefix(clean, "plans/") || strings.Contains(clean, "/plans/") {
		return true
	}
	if strings.HasPrefix(clean, "docs/plans/") || strings.Contains(clean, "/docs/plans/") {
		return true
	}
	return false
}

func IsAllowedReviewWritePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	if base == "REVIEW.md" || strings.HasSuffix(base, ".review.md") {
		return true
	}
	if strings.HasPrefix(clean, "reviews/") || strings.Contains(clean, "/reviews/") {
		return true
	}
	return false
}
