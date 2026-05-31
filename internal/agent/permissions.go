package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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

// PermissionResponse is returned by an interactive permission callback. Level
// answers the current request. PersistRule/PersistTool additionally tell the
// agent handling the request to update its own PermissionManager, so an
// "always allow" answer applies immediately inside the currently running
// sub-agent as well as wherever the UI persists the setting.
type PermissionResponse struct {
	Level       PermissionLevel
	PersistRule bool
	PersistTool bool
}

type pathPatternEntry struct {
	pattern string
	level   PermissionLevel
}

type PermissionManager struct {
	mode            PermissionMode
	rules           map[string]PermissionLevel
	patterns        []patternRule
	pathPatterns    map[string][]pathPatternEntry // toolName → path-glob patterns
	bashPrefixes    map[string]PermissionLevel
	bashAutoAllow   map[string]bool
	bashPrefixModes map[string]string
	workDir         string
	webfetchDomains map[string]PermissionLevel
}

type patternRule struct {
	pattern string
	level   PermissionLevel
}

const bashInRootPersistPrefix = "__inroot__:"

// bashAutoAllowPrefixes are commands that take filesystem path arguments and
// are auto-allowed only when every path resolves inside the current workdir.
// All entries here MUST be safe to run on any in-root path (read-only, or
// mutating-but-bounded like `sed -i`). Commands that can have side effects
// outside the working tree (network, subprocess execution, package installs)
// MUST NOT be added here — use bashSubcommandAllow instead.
var bashAutoAllowPrefixes = map[string]bool{
	// Text processing (reads inputs, writes to stdout)
	"awk":      true,
	"sed":      true,
	"tr":       true,
	"cat":      true,
	"tac":      true,
	"rev":      true,
	"head":     true,
	"tail":     true,
	"less":     true,
	"more":     true,
	"nl":       true,
	"sort":     true,
	"uniq":     true,
	"cut":      true,
	"paste":    true,
	"join":     true,
	"comm":     true,
	"column":   true,
	"expand":   true,
	"unexpand": true,
	"fold":     true,
	"wc":       true,
	"grep":     true,
	"rg":       true,
	"ag":       true,
	// File / directory inspection
	"ls":       true,
	"tree":     true,
	"file":     true,
	"stat":     true,
	"du":       true,
	"basename": true,
	"dirname":  true,
	"realpath": true,
	"readlink": true,
	"diff":     true,
	"cmp":      true,
	// Hashing
	"md5sum":    true,
	"sha1sum":   true,
	"sha256sum": true,
	"sha512sum": true,
	"shasum":    true,
	"cksum":     true,
	// Binary inspection
	"xxd":      true,
	"hexdump":  true,
	"od":       true,
	"strings":  true,
	// Structured data
	"jq": true,
	"yq": true,
	// Path-aware search (extra flag inspection in canAutoAllowInRoot)
	"find": true,
	"fd":   true,
	"cd":   true,
}

const (
	bashPrefixModeReadOnly = "read_only"
	bashPrefixModeMutating = "mutating"
	bashPrefixModeNever    = "never_auto"
)

// bashAutoAllowDefaultModes defaults every entry in bashAutoAllowPrefixes to
// read_only. Overrides for genuinely mutating commands (sed -i, etc.) are
// listed explicitly below.
var bashAutoAllowDefaultModes = func() map[string]string {
	m := make(map[string]string, len(bashAutoAllowPrefixes))
	for prefix := range bashAutoAllowPrefixes {
		m[prefix] = bashPrefixModeReadOnly
	}
	m["sed"] = bashPrefixModeMutating
	return m
}()

// bashAlwaysAllow are commands that have no filesystem path arguments and no
// meaningful side effects. They auto-allow regardless of workdir.
// Anything that can execute another program (env, command, exec, sudo, etc.)
// MUST NOT be added here.
var bashAlwaysAllow = map[string]bool{
	"pwd":      true,
	"whoami":   true,
	"hostname": true,
	"uname":    true,
	"id":       true,
	"tty":      true,
	"date":     true,
	"true":     true,
	"false":    true,
	":":        true,
	"echo":     true,
	"printf":   true,
	"which":    true,
	"type":     true,
	"locale":   true,
	"tput":     true,
	"groups":   true,
	"users":    true,
	"uptime":   true,
	"arch":     true,
}

// bashSubcommandAllow maps "<prefix> <subcommand>" (and optionally three-word
// "<prefix> <sub1> <sub2>") strings to true for subcommand-pinned auto-allow.
// Use only for subcommands that are read-only OR project-trusted (operate on
// the working tree but don't reach outside it without an explicit path arg).
//
// Entries here intentionally do NOT path-scope further — a subcommand listed
// here is allowed regardless of args. Do not add subcommands that take an
// arbitrary path and write to it (e.g. `git apply`, `git checkout --`).
var bashSubcommandAllow = map[string]bool{
	// git — read-only subcommands only (no push/reset/checkout/clean/apply)
	"git status":       true,
	"git diff":         true,
	"git log":          true,
	"git show":         true,
	"git blame":        true,
	"git describe":     true,
	"git rev-parse":    true,
	"git rev-list":     true,
	"git ls-files":     true,
	"git ls-tree":      true,
	"git ls-remote":    true,
	"git reflog":       true,
	"git shortlog":     true,
	"git cat-file":     true,
	"git grep":         true,
	"git name-rev":     true,
	"git for-each-ref": true,
	// Intentionally NOT in the list: branch, tag, remote, stash, worktree,
	// submodule, config, fetch, pull, push, reset, checkout, clean, apply,
	// am, cherry-pick, rebase, revert, restore, switch, merge, init, add,
	// commit. Some of these are read-only without args but become destructive
	// with flags (e.g. `git branch -D`, `git tag -d`). Require explicit user
	// approval.
	// gh CLI — viewing only (intentionally omits `gh api` which can POST)
	"gh pr":       true,
	"gh issue":    true,
	"gh run":      true,
	"gh repo":     true,
	"gh auth":     true,
	"gh release":  true,
	"gh workflow": true,
	"gh label":    true,
	"gh search":   true,
	"gh ruleset":  true,
	// Go toolchain
	"go build":    true,
	"go test":     true,
	"go run":      true,
	"go vet":      true,
	"go fmt":      true,
	"go list":     true,
	"go doc":      true,
	"go env":      true,
	"go version":  true,
	"go mod":      true,
	"go tool":     true,
	"go generate": true,
	"go work":     true,
	"gofmt":       true,
	"goimports":   true,
	// Rust toolchain
	"cargo check":    true,
	"cargo build":    true,
	"cargo test":     true,
	"cargo clippy":   true,
	"cargo fmt":      true,
	"cargo doc":      true,
	"cargo tree":     true,
	"cargo metadata": true,
	"cargo version":  true,
	"cargo run":      true,
	// Python / TS type-checkers / formatters (project-scoped tools)
	"pytest":         true,
	"ruff":           true,
	"mypy":           true,
	"basedpyright":   true,
	"tsc":            true,
	"tsgo":           true,
	"eslint":         true,
	"prettier":       true,
	"biome":          true,
	"vitest":         true,
	// Docker — read-only inspection
	"docker ps":              true,
	"docker images":          true,
	"docker logs":            true,
	"docker inspect":         true,
	"docker version":         true,
	"docker info":            true,
	"docker history":         true,
	"docker port":            true,
	"docker top":             true,
	"docker stats":           true,
	"docker compose ps":      true,
	"docker compose logs":    true,
	"docker compose config":  true,
	"docker compose top":     true,
	"docker compose port":    true,
	"docker compose images":  true,
	"docker compose ls":      true,
	// Node package managers — project-trusted script runners + read commands.
	// Same trust model as `make`: scripts can do anything, but they live in
	// the project's manifest.
	"npm run":      true,
	"npm test":     true,
	"npm list":     true,
	"npm ls":       true,
	"npm outdated": true,
	"npm view":     true,
	"npm info":     true,
	"npm audit":    true,
	"npm fund":     true,
	"npm doctor":   true,
	"npm ping":     true,
	"npm search":   true,
	"pnpm run":     true,
	"pnpm test":    true,
	"pnpm list":    true,
	"pnpm ls":      true,
	"pnpm outdated": true,
	"pnpm view":    true,
	"pnpm info":    true,
	"pnpm audit":   true,
	"pnpm why":     true,
	"pnpm doctor":  true,
	"yarn run":     true,
	"yarn test":    true,
	"yarn list":    true,
	"yarn outdated": true,
	"yarn info":    true,
	"yarn audit":   true,
	"yarn why":     true,
	"bun run":      true,
	"bun test":     true,
	// make: project-trusted, all targets (same risk model as before).
	"make": true,
}

// findUnsafeFlags are flags on `find` that can execute subprocesses or delete
// files. Any of these makes the command non-auto-allowable.
var findUnsafeFlags = map[string]bool{
	"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
	"-delete": true, "-fprint": true, "-fprintf": true,
	"-fprint0": true, "-fls": true,
}

// fdUnsafeFlags are flags on `fd` that can execute subprocesses.
var fdUnsafeFlags = map[string]bool{
	"-x": true, "--exec": true, "-X": true, "--exec-batch": true,
}

// pathScopedTools are file tools whose decision depends on the target path
// (workdir scope, sensitive paths). Membership must stay in sync with the
// path-returning cases of extractPathFromArgs.
var pathScopedTools = map[string]bool{
	"read": true, "write": true, "edit": true, "delete": true,
	"multiedit": true, "multi_file_edit": true, "replace_lines": true, "glob": true, "grep": true,
	"list": true, "lsp": true, "apply_patch": true, "format": true, "repo_overview": true,
}

func NewPermissionManager() *PermissionManager {
	pm := &PermissionManager{
		mode:            PermissionModeNormal,
		rules:           make(map[string]PermissionLevel),
		patterns:        make([]patternRule, 0),
		pathPatterns:    make(map[string][]pathPatternEntry),
		bashPrefixes:    make(map[string]PermissionLevel),
		bashAutoAllow:   make(map[string]bool),
		bashPrefixModes: make(map[string]string),
		webfetchDomains: make(map[string]PermissionLevel),
	}
	for k, v := range bashAutoAllowPrefixes {
		pm.bashAutoAllow[k] = v
	}
	for k, v := range bashAutoAllowDefaultModes {
		pm.bashPrefixModes[k] = v
	}
	for _, name := range []string{"read", "glob", "grep", "list", "lsp", "skill", "question", "todoread", "todowrite", "advisor", "task", "task_status", "agent_status", "repo_overview", "plan_enter", "plan_exit", "wait", "bash_output", "kill_shell"} {
		pm.rules[name] = PermissionAllow
	}
	for _, name := range []string{"write", "edit", "multiedit", "multi_file_edit", "replace_lines", "apply_patch", "format"} {
		pm.SetRule(name, PermissionAllow)
	}
	for _, name := range []string{"delete", "bash", "webfetch", "websearch", "repo_clone", "mcp_*"} {
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
	for toolName, val := range cfg {
		switch v := val.(type) {
		case string:
			pm.SetRule(toolName, PermissionLevel(v))
		case map[string]interface{}:
			for pattern, levelVal := range v {
				if levelStr, ok := levelVal.(string); ok {
					level := PermissionLevel(levelStr)
					if validPermissionLevel(level) {
						pm.SetPathRule(toolName, pattern, level)
					}
				}
			}
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
	for _, prefix := range cfg.Bash.AutoAllowPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		pm.bashAutoAllow[prefix] = true
		if _, ok := pm.bashPrefixModes[prefix]; !ok {
			pm.bashPrefixModes[prefix] = bashPrefixModeReadOnly
		}
	}
	for prefix, mode := range cfg.Bash.PrefixModes {
		mode = strings.TrimSpace(mode)
		if mode != bashPrefixModeReadOnly && mode != bashPrefixModeMutating && mode != bashPrefixModeNever {
			continue
		}
		pm.bashPrefixModes[prefix] = mode
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
		if pm.mode == PermissionModeYOLO {
			return PermissionDecision{Level: PermissionAllow}
		}

		// Parse the compound command
		parsedCmds, err := parseShellCommandLine(command)
		if err != nil {
			// Parsing error (unbalanced quotes, etc.): fallback to asking for safety
			level := pm.Check(toolName)
			if level == PermissionAsk {
				return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, "")}
			}
			return PermissionDecision{Level: level}
		}

		// Evaluate each constituent command, environment variable, and redirection
		var finalDecision *PermissionDecision
		for _, cmd := range parsedCmds {
			dec := pm.decideSingleCommand(args, cmd)
			if dec.Level == PermissionDeny {
				return dec
			}
			if dec.Level == PermissionAsk {
				// Keep track of the first Ask decision to return if none are Deny
				if finalDecision == nil {
					finalDecision = &dec
				}
			}
		}

		if finalDecision != nil {
			return *finalDecision
		}
		return PermissionDecision{Level: PermissionAllow}
	}

	if pm.mode == PermissionModeYOLO {
		return PermissionDecision{Level: PermissionAllow}
	}

	if pathScopedTools[toolName] {
		path := extractPathFromArgs(toolName, args)
		if path != "" {
			// Check path-based permission patterns first (e.g., opencode.json
			// "permission" entries with glob patterns). An explicit "allow" or
			// "deny" overrides the normal workdir/sensitive checks.
			if level := pm.CheckPathPatterns(toolName, path); level != "" {
				if level == PermissionAsk {
					return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
						ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".path_pattern",
					}}
				}
				return PermissionDecision{Level: level}
			}
			// Relative paths and glob patterns (non-absolute) are implicitly within workDir
			if filepath.IsAbs(path) && !isWithinWorkDir(pm, path) {
				// Check tool-level rule first — an explicit "allow" (from "always
				// allow this rule/tool") overrides the out-of-scope gate so the user
				// isn't asked repeatedly for the same permitted tool.
				if pm.Check(toolName) == PermissionAllow {
					return PermissionDecision{Level: PermissionAllow}
				}
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".out_of_scope",
				}}
			}
			if isSensitivePath(path) {
				if pm.Check(toolName) == PermissionAllow {
					return PermissionDecision{Level: PermissionAllow}
				}
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".sensitive_path",
				}}
			}
			if toolName == "delete" {
				if pm.Check(toolName) == PermissionAllow {
					return PermissionDecision{Level: PermissionAllow}
				}
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
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	switch toolName {
	case "read", "write", "delete", "edit", "multiedit", "multi_file_edit", "replace_lines", "format", "lsp", "apply_patch", "grep", "repo_overview":
		if params.Path != "" {
			return params.Path
		}
		return params.FilePath
	case "glob", "list":
		if params.Pattern != "" {
			return params.Pattern
		}
		return params.Path
	case "webfetch":
		return params.URL
	default:
		return ""
	}
}

// matchSubcommandAllow returns true when the command matches an entry in
// bashSubcommandAllow at the longest possible token length (3 → 2 → 1).
// Leading dashed flags on the command itself (e.g. `git --no-pager log`)
// would not match — that's intentional: we accept a small loss of coverage
// in exchange for not having to parse every tool's option grammar.
func matchSubcommandAllow(command string) bool {
	fields := splitShellFields(command)
	if len(fields) == 0 {
		return false
	}
	// Three-word match (e.g. "docker compose ps").
	if len(fields) >= 3 {
		key := fields[0] + " " + fields[1] + " " + fields[2]
		if bashSubcommandAllow[key] {
			return true
		}
	}
	// Two-word match (e.g. "git status").
	if len(fields) >= 2 {
		key := fields[0] + " " + fields[1]
		if bashSubcommandAllow[key] {
			return true
		}
	}
	// Single-word match (e.g. "make", "tsc"). These accept any args.
	return bashSubcommandAllow[fields[0]]
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

// matchPathPattern matches a path against a glob pattern that may contain
// "**" (recursive wildcard, matches zero or more path segments) and "*"
// (single-segment wildcard, does not match "/"). Standard filepath.Match
// patterns like "?" are also supported.
func matchPathPattern(pattern, path string) bool {
	// Normalise to forward slashes, but do NOT filepath.Clean the pattern —
	// "**" is not a real filesystem element and Clean would interpret it as a
	// literal directory name.
	pattern = filepath.ToSlash(pattern)
	cleanPath := filepath.ToSlash(filepath.Clean(path))

	// Split into segments so we can handle "**" correctly.
	patSegs := strings.Split(pattern, "/")
	pathSegs := strings.Split(cleanPath, "/")

	return matchPathSegments(patSegs, pathSegs)
}

// matchPathSegments recursively matches path segments against pattern segments,
// supporting "**" as a recursive wildcard (matches zero or more segments).
func matchPathSegments(pattern, path []string) bool {
	// If both are empty, it's a match.
	if len(pattern) == 0 && len(path) == 0 {
		return true
	}

	// If pattern is empty but path isn't, no match.
	if len(pattern) == 0 {
		return false
	}

	// Handle "**" at current position
	if pattern[0] == "**" {
		// Try matching zero or more path segments against the rest of the pattern.
		// Try zero first, then one, two, etc.
		for i := 0; i <= len(path); i++ {
			if matchPathSegments(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}

	// If path is empty but pattern isn't (and we didn't match ** above), no match.
	if len(path) == 0 {
		return false
	}

	// Match current segment with filepath.Match (handles *, ?, [a-z], etc.)
	matched, err := filepath.Match(pattern[0], path[0])
	if err != nil || !matched {
		return false
	}

	// Recurse to the next segment
	return matchPathSegments(pattern[1:], path[1:])
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

func (pm *PermissionManager) SetPathRule(toolName, pattern string, level PermissionLevel) {
	if toolName == "" || pattern == "" || !validPermissionLevel(level) {
		return
	}
	pm.pathPatterns[toolName] = append(pm.pathPatterns[toolName], pathPatternEntry{pattern: pattern, level: level})
}

// CheckPathPatterns returns the first matching permission level from path-based
// rules for the given tool and target path, or empty string if no rule matches.
func (pm *PermissionManager) CheckPathPatterns(toolName, targetPath string) PermissionLevel {
	entries, ok := pm.pathPatterns[toolName]
	if !ok {
		return ""
	}
	for _, entry := range entries {
		if matchPathPattern(entry.pattern, targetPath) {
			return entry.level
		}
	}
	// Also check wildcard tool entries (e.g., "mcp_*" → matches any MCP tool)
	for pattern, entries := range pm.pathPatterns {
		if matchPattern(pattern, toolName) {
			for _, entry := range entries {
				if matchPathPattern(entry.pattern, targetPath) {
					return entry.level
				}
			}
		}
	}
	return ""
}

func (pm *PermissionManager) SetBashPrefixRule(prefix string, level PermissionLevel) {
	if prefix == "" || !validPermissionLevel(level) || strings.HasPrefix(prefix, bashInRootPersistPrefix) {
		return
	}
	pm.bashPrefixes[prefix] = level
}

func (pm *PermissionManager) BashAutoAllowPrefixes() []string {
	result := make([]string, 0, len(pm.bashAutoAllow))
	for k, v := range pm.bashAutoAllow {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		if v {
			result = append(result, k)
		}
	}
	return result
}

func (pm *PermissionManager) SetBashAutoAllowPrefix(prefix string, enabled bool) {
	if strings.TrimSpace(prefix) == "" {
		return
	}
	if enabled {
		pm.bashAutoAllow[prefix] = true
		if _, ok := pm.bashPrefixModes[prefix]; !ok {
			pm.bashPrefixModes[prefix] = bashPrefixModeReadOnly
		}
		return
	}
	delete(pm.bashAutoAllow, prefix)
}

func (pm *PermissionManager) BashPrefixModes() map[string]string {
	result := make(map[string]string, len(pm.bashPrefixModes))
	for k, v := range pm.bashPrefixModes {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		result[k] = v
	}
	return result
}

func (pm *PermissionManager) SetBashPrefixMode(prefix, mode string) bool {
	if strings.TrimSpace(prefix) == "" {
		return false
	}
	if mode != bashPrefixModeReadOnly && mode != bashPrefixModeMutating && mode != bashPrefixModeNever {
		return false
	}
	pm.bashPrefixModes[prefix] = mode
	if _, ok := pm.bashAutoAllow[prefix]; !ok && mode != bashPrefixModeNever {
		pm.bashAutoAllow[prefix] = true
	}
	return true
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

func (pm *PermissionManager) Clone() *PermissionManager {
	if pm == nil {
		return nil
	}

	clone := &PermissionManager{
		mode:            pm.Mode(),
		rules:           make(map[string]PermissionLevel, len(pm.rules)),
		patterns:        append([]patternRule(nil), pm.patterns...),
		pathPatterns:    make(map[string][]pathPatternEntry, len(pm.pathPatterns)),
		bashPrefixes:    make(map[string]PermissionLevel, len(pm.bashPrefixes)),
		bashAutoAllow:   make(map[string]bool, len(pm.bashAutoAllow)),
		bashPrefixModes: make(map[string]string, len(pm.bashPrefixModes)),
		workDir:         pm.workDir,
		webfetchDomains: make(map[string]PermissionLevel, len(pm.webfetchDomains)),
	}
	for k, v := range pm.rules {
		clone.rules[k] = v
	}
	for k, v := range pm.bashPrefixes {
		clone.bashPrefixes[k] = v
	}
	for k, v := range pm.bashAutoAllow {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		clone.bashAutoAllow[k] = v
	}
	for k, v := range pm.bashPrefixModes {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		clone.bashPrefixModes[k] = v
	}
	for k, v := range pm.webfetchDomains {
		clone.webfetchDomains[k] = v
	}
	for toolName, entries := range pm.pathPatterns {
		clone.pathPatterns[toolName] = append([]pathPatternEntry(nil), entries...)
	}
	return clone
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
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
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
	autoAllow := make([]string, 0, len(pm.bashAutoAllow))
	for k, v := range pm.bashAutoAllow {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		if v {
			autoAllow = append(autoAllow, k)
		}
	}
	modes := make(map[string]string, len(pm.bashPrefixModes))
	for k, v := range pm.bashPrefixModes {
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		modes[k] = v
	}
	return config.PermissionConfig{
		Mode:  string(pm.Mode()),
		Tools: tools,
		Bash: config.BashPermissionConfig{
			Prefixes:          prefixes,
			AutoAllowPrefixes: autoAllow,
			PrefixModes:       modes,
		},
	}
}

func validPermissionLevel(level PermissionLevel) bool {
	return level == PermissionAllow || level == PermissionDeny || level == PermissionAsk
}

func isReadOnlyTool(name string) bool {
	switch name {
	case "read", "glob", "grep", "list", "lsp", "webfetch", "websearch", "skill", "question", "todoread", "todowrite":
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

func bashInRootKey(prefix, workDir string) string {
	cleanWorkDir := filepath.Clean(workDir)
	return bashInRootPersistPrefix + prefix + ":" + cleanWorkDir
}

func canAutoAllowWithMode(pm *PermissionManager, command, prefix string) bool {
	if !canAutoAllowInRoot(pm, command, prefix) {
		return false
	}
	mode := pm.bashPrefixModes[prefix]
	switch mode {
	case bashPrefixModeNever:
		return false
	case bashPrefixModeMutating:
		return true
	default:
		pm.bashPrefixes[bashInRootKey(prefix, pm.workDir)] = PermissionAllow
		return true
	}
}

func canAutoAllowInRoot(pm *PermissionManager, command, prefix string) bool {
	if pm == nil || pm.workDir == "" {
		return false
	}
	if !pm.bashAutoAllow[prefix] {
		return false
	}
	if shellCompound(command) {
		return false
	}
	fields := splitShellFields(command)
	if len(fields) == 0 || fields[0] != prefix {
		return false
	}
	// find/fd: reject if any field is an unsafe flag (executes subprocesses
	// or deletes files). These flags would let an in-root path argument
	// trigger arbitrary actions, defeating the workdir scope.
	if prefix == "find" {
		for _, f := range fields[1:] {
			if findUnsafeFlags[f] {
				return false
			}
		}
	}
	if prefix == "fd" {
		for _, f := range fields[1:] {
			if fdUnsafeFlags[f] {
				return false
			}
		}
	}
	paths := extractBashCommandPaths(prefix, fields)
	if prefix == "cd" && len(paths) == 0 {
		home := os.Getenv("HOME")
		if home != "" {
			paths = append(paths, home)
		}
	}
	// Zero paths means the command operates on stdin or the current directory
	// only (e.g. `grep "foo"`, `find`, `ls`). That's still inside the workdir
	// by definition, so allow.
	for _, p := range paths {
		resolved := resolvePath(p, pm.workDir)
		if !isWithinWorkDir(pm, resolved) {
			return false
		}
	}
	return true
}

func extractBashCommandPaths(prefix string, fields []string) []string {
	var paths []string
	sedScriptConsumed := false
	for i := 1; i < len(fields); i++ {
		arg := fields[i]
		if arg == "--" {
			for j := i + 1; j < len(fields); j++ {
				if strings.TrimSpace(fields[j]) != "" {
					paths = append(paths, fields[j])
				}
			}
			break
		}
		if strings.HasPrefix(arg, "-") {
			switch prefix {
			case "awk":
				if arg == "-f" {
					i++
				}
			case "sed":
				if arg == "-e" || arg == "-f" {
					if arg == "-e" {
						sedScriptConsumed = true
					}
					i++
				}
			case "grep", "rg", "head", "tail":
				if arg == "-e" || arg == "-f" || arg == "--file" {
					i++
				}
			}
			continue
		}
		if prefix == "awk" && i == 1 {
			continue
		}
		if prefix == "sed" && !sedScriptConsumed {
			sedScriptConsumed = true
			continue
		}
		if isLikelyPathArg(arg) {
			paths = append(paths, arg)
		}
	}
	return paths
}

// isLikelyPathArg returns true if arg looks like a filesystem path. Bare
// identifiers (e.g. literal patterns passed to grep, awk's variable names)
// return false so they don't get treated as out-of-workdir paths and
// inadvertently block the auto-allow.
func isLikelyPathArg(arg string) bool {
	if arg == "" {
		return false
	}
	// Glob metacharacters.
	if strings.ContainsAny(arg, "*?[") {
		return true
	}
	// Absolute or explicitly-rooted relative paths.
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, "~/") ||
		arg == "." || arg == ".." || arg == "~" {
		return true
	}
	// Contains a path separator → almost certainly a path.
	if strings.Contains(arg, string(filepath.Separator)) {
		return true
	}
	// Looks like a filename (has an extension dot in the middle).
	if dot := strings.Index(arg, "."); dot > 0 && dot < len(arg)-1 {
		return true
	}
	return false
}

func isHardBlockedCommand(command string) bool {
	compact := strings.Join(splitShellFields(command), " ")
	if compact == "rm -rf /" || compact == "rm -fr /" || strings.Contains(command, ":(){ :|:& };:") {
		return true
	}
	// Hard-block destructive and exfiltration patterns
	blockedPatterns := []string{
		"| bash", "| sh", "| python", "| perl", // pipe to shell
		"dd if=", "mkfs", // disk/partition write
		"; sudo", "&& sudo", "| sudo", // privilege escalation chains
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

// Shell parsing and validation structures and functions

type shellTokenType int

const (
	tokWord shellTokenType = iota
	tokOp                  // &&, ||, ;, &, |
	tokRedir               // >, >>, <, 2>, &>, etc.
	tokSubst               // $(...) or `...`
	tokLeftParen           // (
	tokRightParen          // )
)

type shellToken struct {
	typ   shellTokenType
	value string
}

type parsedShellCommand struct {
	envVars      []string // "KEY=VAL"
	cmdWords     []string // command and its arguments (e.g. ["go", "test", "./..."])
	redirections []string // target paths
}

func parseShellCommandLine(commandLine string) ([]parsedShellCommand, error) {
	tokens, err := tokenizeShell(commandLine)
	if err != nil {
		return nil, err
	}

	var commands []parsedShellCommand
	var currentTokens []shellToken

	emitCommand := func() {
		if len(currentTokens) > 0 {
			cmd := parseSingleCommandTokens(currentTokens)
			if cmd != nil {
				commands = append(commands, *cmd)
			}
			currentTokens = nil
		}
	}

	for _, tok := range tokens {
		if tok.typ == tokOp {
			emitCommand()
		} else if tok.typ == tokLeftParen || tok.typ == tokRightParen {
			emitCommand()
		} else if tok.typ == tokSubst {
			subCmds, err := parseShellCommandLine(tok.value)
			if err == nil {
				commands = append(commands, subCmds...)
			}
			currentTokens = append(currentTokens, tok)
		} else {
			currentTokens = append(currentTokens, tok)
		}
	}
	emitCommand()

	return commands, nil
}

func tokenizeShell(input string) ([]shellToken, error) {
	var tokens []shellToken
	var current strings.Builder

	runes := []rune(input)
	n := len(runes)

	inSingle := false
	inDouble := false
	escaped := false

	emitWord := func() {
		if current.Len() > 0 {
			tokens = append(tokens, shellToken{typ: tokWord, value: current.String()})
			current.Reset()
		}
	}

	for i := 0; i < n; i++ {
		r := runes[i]

		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingle {
			if inDouble {
				if i+1 < n && (runes[i+1] == '"' || runes[i+1] == '\\' || runes[i+1] == '$' || runes[i+1] == '`') {
					escaped = true
					continue
				} else {
					current.WriteRune(r)
					continue
				}
			} else {
				escaped = true
				continue
			}
		}

		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				current.WriteRune(r)
			}
			continue
		}

		if inDouble {
			if r == '$' && i+1 < n && runes[i+1] == '(' {
				emitWord()
				sub, endIdx, err := parseParenthesis(runes, i+1)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, shellToken{typ: tokSubst, value: sub})
				i = endIdx
				continue
			}
			if r == '`' {
				emitWord()
				sub, endIdx, err := parseBackticks(runes, i)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, shellToken{typ: tokSubst, value: sub})
				i = endIdx
				continue
			}
			if r == '"' {
				inDouble = false
			} else {
				current.WriteRune(r)
			}
			continue
		}

		switch r {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '`':
			emitWord()
			sub, endIdx, err := parseBackticks(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, shellToken{typ: tokSubst, value: sub})
			i = endIdx
		case '$':
			if i+1 < n && runes[i+1] == '(' {
				emitWord()
				sub, endIdx, err := parseParenthesis(runes, i+1)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, shellToken{typ: tokSubst, value: sub})
				i = endIdx
			} else {
				current.WriteRune(r)
			}
		case '&':
			emitWord()
			if i+1 < n && runes[i+1] == '&' {
				tokens = append(tokens, shellToken{typ: tokOp, value: "&&"})
				i++
			} else {
				tokens = append(tokens, shellToken{typ: tokOp, value: "&"})
			}
		case '|':
			emitWord()
			if i+1 < n && runes[i+1] == '|' {
				tokens = append(tokens, shellToken{typ: tokOp, value: "||"})
				i++
			} else {
				tokens = append(tokens, shellToken{typ: tokOp, value: "|"})
			}
		case ';':
			emitWord()
			tokens = append(tokens, shellToken{typ: tokOp, value: ";"})
		case '(':
			emitWord()
			tokens = append(tokens, shellToken{typ: tokLeftParen, value: "("})
		case ')':
			emitWord()
			tokens = append(tokens, shellToken{typ: tokRightParen, value: ")"})
		case '>':
			emitWord()
			if i+1 < n && runes[i+1] == '>' {
				tokens = append(tokens, shellToken{typ: tokRedir, value: ">>"})
				i++
			} else {
				tokens = append(tokens, shellToken{typ: tokRedir, value: ">"})
			}
		case '<':
			emitWord()
			tokens = append(tokens, shellToken{typ: tokRedir, value: "<"})
		case '1', '2':
			if i+1 < n && runes[i+1] == '>' {
				emitWord()
				if i+2 < n && runes[i+2] == '>' {
					tokens = append(tokens, shellToken{typ: tokRedir, value: string(r) + ">>"})
					i += 2
				} else {
					tokens = append(tokens, shellToken{typ: tokRedir, value: string(r) + ">"})
					i++
				}
			} else {
				current.WriteRune(r)
			}
		default:
			if unicode.IsSpace(r) {
				emitWord()
			} else {
				current.WriteRune(r)
			}
		}
	}
	emitWord()
	return tokens, nil
}

func parseParenthesis(runes []rune, start int) (string, int, error) {
	depth := 1
	inSingle := false
	inDouble := false
	escaped := false
	var content strings.Builder

	n := len(runes)
	for i := start + 1; i < n; i++ {
		r := runes[i]
		if escaped {
			content.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			content.WriteRune(r)
			escaped = true
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
			}
			content.WriteRune(r)
			continue
		}
		if inDouble {
			if r == '"' {
				inDouble = false
			}
			content.WriteRune(r)
			continue
		}

		switch r {
		case '\'':
			inSingle = true
			content.WriteRune(r)
		case '"':
			inDouble = true
			content.WriteRune(r)
		case '(':
			depth++
			content.WriteRune(r)
		case ')':
			depth--
			if depth == 0 {
				return content.String(), i, nil
			}
			content.WriteRune(r)
		default:
			content.WriteRune(r)
		}
	}
	return "", 0, fmt.Errorf("unbalanced parenthesis in command substitution")
}

func parseBackticks(runes []rune, start int) (string, int, error) {
	inSingle := false
	inDouble := false
	escaped := false
	var content strings.Builder

	n := len(runes)
	for i := start + 1; i < n; i++ {
		r := runes[i]
		if escaped {
			content.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			content.WriteRune(r)
			escaped = true
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
			}
			content.WriteRune(r)
			continue
		}
		if inDouble {
			if r == '"' {
				inDouble = false
			}
			content.WriteRune(r)
			continue
		}

		switch r {
		case '\'':
			inSingle = true
			content.WriteRune(r)
		case '"':
			inDouble = true
			content.WriteRune(r)
		case '`':
			return content.String(), i, nil
		default:
			content.WriteRune(r)
		}
	}
	return "", 0, fmt.Errorf("unbalanced backticks in command substitution")
}

func parseSingleCommandTokens(tokens []shellToken) *parsedShellCommand {
	var cmd parsedShellCommand
	var remaining []shellToken

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.typ == tokRedir {
			if i+1 < len(tokens) && tokens[i+1].typ == tokWord {
				cmd.redirections = append(cmd.redirections, tokens[i+1].value)
				i++
			}
		} else {
			remaining = append(remaining, tok)
		}
	}

	if len(remaining) == 0 {
		return nil
	}

	idx := 0
	for idx < len(remaining) {
		tok := remaining[idx]
		if tok.typ == tokWord && strings.Contains(tok.value, "=") {
			cmd.envVars = append(cmd.envVars, tok.value)
			idx++
		} else {
			break
		}
	}

	for idx < len(remaining) {
		tok := remaining[idx]
		if tok.typ == tokWord {
			cmd.cmdWords = append(cmd.cmdWords, tok.value)
		} else if tok.typ == tokSubst {
			cmd.cmdWords = append(cmd.cmdWords, "$("+tok.value+")")
		}
		idx++
	}

	if len(cmd.cmdWords) == 0 && len(cmd.envVars) == 0 && len(cmd.redirections) == 0 {
		return nil
	}

	return &cmd
}

func rebuildCommandLine(fields []string) string {
	var parts []string
	for _, f := range fields {
		if f == "" {
			parts = append(parts, `""`)
			continue
		}
		needsQuote := false
		for _, r := range f {
			if unicode.IsSpace(r) || strings.ContainsRune(`'"&|;><()*?[$`, r) {
				needsQuote = true
				break
			}
		}
		if needsQuote {
			escaped := strings.ReplaceAll(f, `"`, `\"`)
			parts = append(parts, `"`+escaped+`"`)
		} else {
			parts = append(parts, f)
		}
	}
	return strings.Join(parts, " ")
}

func resolvePath(path string, workDir string) string {
	if strings.HasPrefix(path, "~") {
		home := os.Getenv("HOME")
		if home != "" {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(home, path[2:])
			}
		}
	}
	if !filepath.IsAbs(path) {
		return filepath.Join(workDir, path)
	}
	return path
}

func (pm *PermissionManager) decideSingleCommand(args json.RawMessage, cmd parsedShellCommand) PermissionDecision {
	// Check env variables for path values
	for _, env := range cmd.envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			val := parts[1]
			if isLikelyPathArg(val) {
				resolved := resolvePath(val, pm.workDir)
				if !isWithinWorkDir(pm, resolved) {
					return PermissionDecision{
						Level:   PermissionAsk,
						Request: envVarPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), env, false),
					}
				}
				if isSensitivePath(resolved) {
					return PermissionDecision{
						Level:   PermissionAsk,
						Request: envVarPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), env, true),
					}
				}
			}
		}
	}

	// Check redirections
	for _, path := range cmd.redirections {
		if path == "/dev/null" || path == "/dev/stdout" || path == "/dev/stderr" {
			continue
		}
		resolved := resolvePath(path, pm.workDir)
		if !isWithinWorkDir(pm, resolved) {
			return PermissionDecision{
				Level:   PermissionAsk,
				Request: redirectionPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), path, false),
			}
		}
		if isSensitivePath(resolved) {
			return PermissionDecision{
				Level:   PermissionAsk,
				Request: redirectionPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), path, true),
			}
		}
	}

	if len(cmd.cmdWords) == 0 {
		return PermissionDecision{Level: PermissionAllow}
	}

	command := rebuildCommandLine(cmd.cmdWords)
	if isHardBlockedCommand(command) {
		return PermissionDecision{Level: PermissionDeny}
	}

	prefix := cmd.cmdWords[0]

	// 1. Persisted in-root rule
	if level, exists := pm.bashPrefixes[bashInRootKey(prefix, pm.workDir)]; exists {
		if level == PermissionAllow && canAutoAllowInRoot(pm, command, prefix) {
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// 2. Explicit prefix rule
	if level, exists := pm.bashPrefixes[prefix]; exists {
		if level == PermissionAsk {
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
		}
		return PermissionDecision{Level: level}
	}

	// 3. Path-scoped auto-allow
	if bashAutoAllowPrefixes[prefix] {
		if canAutoAllowWithMode(pm, command, prefix) {
			return PermissionDecision{Level: PermissionAllow}
		}
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
	}

	// 4. Argless commands
	if bashAlwaysAllow[prefix] {
		return PermissionDecision{Level: PermissionAllow}
	}

	// 5. Subcommand-pinned allowlist
	if matchSubcommandAllow(command) {
		return PermissionDecision{Level: PermissionAllow}
	}

	// 6. Fall through to tool-level rule
	level := pm.Check("bash")
	if level == PermissionAsk {
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
	}
	return PermissionDecision{Level: level}
}

func redirectionPermissionRequest(args json.RawMessage, command, path string, isSensitive bool) *PermissionRequest {
	rule := "bash.redirection.out_of_scope"
	if isSensitive {
		rule = "bash.redirection.sensitive_path"
	}
	return &PermissionRequest{
		ToolName: "bash",
		Args:     args,
		Command:  command,
		Prefix:   "",
		Scope:    PermissionScopeTool,
		Rule:     rule,
	}
}

func envVarPermissionRequest(args json.RawMessage, command, envVar string, isSensitive bool) *PermissionRequest {
	rule := "bash.env.out_of_scope"
	if isSensitive {
		rule = "bash.env.sensitive_path"
	}
	return &PermissionRequest{
		ToolName: "bash",
		Args:     args,
		Command:  command,
		Prefix:   "",
		Scope:    PermissionScopeTool,
		Rule:     rule,
	}
}
