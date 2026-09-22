package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/skill"
)

const (
	promptEnvMarker       = "[ocode:environment]"
	promptProviderMarker  = "[ocode:provider]"
	promptModeMarker      = "[ocode:mode]"
	promptContextMarker   = "[ocode:context]"
	promptModelCtxMarker  = "[ocode:model_context]"
	promptSelectionMarker = "[ocode:selection]"
	promptNotesMarker     = "[ocode:notes]"
	promptDocPromptMarker = "[ocode:doc_prompt]"
)

// docPromptContent is the documentation-first development prompt injected
// when DocPromptEnabled is true. It requires inspection and alignment checks
// for nontrivial changes, allows a quick look only for narrowly scoped
// single-file edits, and requires doc updates when the change affects
// documented behavior.
const docPromptContent = `## Documentation-First Development

Before acting, assess scope. For nontrivial, cross-file, behavioral, or unfamiliar changes, you must read relevant docs, check documentation alignment, and build a mental model before acting:
1. **Read existing documentation** — look for README, CLAUDE.md, ARCHITECTURE.md, API docs, style guides, and schema definitions related to your changes.
2. **Check documentation alignment** — if existing docs describe behavior your changes will affect, verify there is no conflict. If there is a conflict, ask the user before proceeding.
3. **Build a mental model** of affected code paths and dependencies.

Only small, self-contained tasks (one file, explicitly scoped by the user, localized, with no public-interface, documented-behavior, schema, dependency, or cross-file implication) may proceed after a quick look at the named area — and even then, if the change affects documented behavior or public interfaces, the documentation obligations below still apply.

If the change affects documented behavior or public interfaces, you must update documentation to reflect your changes:
1. **Update inline documentation** — function/type comments, docstrings.
2. **Update project documentation** — README, API docs, architecture docs, migration notes if your changes affect public APIs, config, setup steps, or data flow.
3. **State explicitly if no doc updates are needed** and explain why.

## Use the Knowledge Bundle via the Context Agent (not direct reads)

When the knowledge bundle is active, OKF documentation (docs/) is ONLY accessible through the **context** sub-agent. The main agent must NOT read docs/ files directly. For any **why / what-did-we-decide / playbook / gotcha** question about THIS project, route the question to the context agent -- via knowledge_lookup (for pure knowledge questions) or task with agent=context (for mixed or code-level work). The context agent searches existing database documents via doc_search/doc_get and manages the bundle via doc_write/doc_deprecate.

**Do NOT read docs/ files directly with the read tool** to answer knowledge questions. The docs/ directory is owned by the knowledge system: retrieval is intentionally routed through the context agent so answers stay curated, verified, and citation-tracked. Only fall back to read on docs/ if the context agent explicitly reports the bundle has no relevant information.

**Context subsumes explore when the bundle is active:** the context sub-agent also handles codebase exploration (where/how/what) and has the full explore toolkit plus doc tools. When the bundle is active, dispatch task with agent=context for code-level or mixed (why+where) questions — explore is hidden from the schema. Pure exploration (no doc involvement) may use grep/glob/read/lsp directly. Priority: doc_search first (get_top: 3), then doc_get as needed, then code tools; for mixed questions you MAY call doc_search and code tools (grep/glob/lsp/read) in parallel in the same batch — if docs answer, ignore the parallel code results.`

// PrepareMessages prepends the stable base prompt fragments for this agent.
// It is safe to call more than once; marked fragments are not duplicated.
// If the environment block in messages is stale (date rolled over), it is
// stripped first so the refreshed block is re-inserted.
func (a *Agent) PrepareMessages(messages []Message, selectionContext string) []Message {
	if a == nil {
		return messages
	}
	// Strip stale env block before marker-dedup so the refreshed date is
	// re-inserted. environmentPrompt() has already updated a.envPromptDate.
	if today := time.Now().Format("Mon Jan 2 2006"); a.envPromptDate != "" && a.envPromptDate != today {
		messages = stripMarker(messages, promptEnvMarker)
	}
	// The selection is per-turn UI state (sidebar file picks). It rides the
	// volatile user-role tail, never the system block: every system-role
	// message is hoisted into the cached system prompt by the provider
	// builders, so a selection change would bust the whole prefix.
	// Preserve an existing selection marker already in messages (the TUI
	// builds selection context via PrepareMessages then re-enters
	// agent.Step, which would otherwise strip it every turn).
	if selectionContext == "" {
		if sel := extractSelectionContext(messages); sel != "" {
			selectionContext = sel
		}
	}
	messages = stripMarker(messages, promptSelectionMarker)
	base := a.BasePromptMessages()
	existing := existingPromptMarkers(messages)
	out := make([]Message, 0, len(base)+len(messages)+1)
	for _, msg := range base {
		marker := promptMarker(msg.Content)
		if marker != "" && existing[marker] {
			continue
		}
		out = append(out, msg)
	}
	out = append(out, messages...)
	if sel := strings.TrimSpace(selectionContext); sel != "" {
		out = append(out, Message{Role: "user", Content: promptSelectionMarker + "\n" + sel})
	}
	return out
}

// extractSelectionContext pulls the content of an existing
// promptSelectionMarker block from messages, so a caller that already
// ran PrepareMessages with a selection context can re-enter
// PrepareMessages (e.g. agent.Step) without losing it.
func extractSelectionContext(messages []Message) string {
	for _, msg := range messages {
		if promptMarker(msg.Content) == promptSelectionMarker {
			trimmed := strings.TrimPrefix(msg.Content, promptSelectionMarker)
			return strings.TrimSpace(trimmed)
		}
	}
	return ""
}

// stripMarker removes the first message with the given marker from messages.
func stripMarker(messages []Message, marker string) []Message {
	for i, msg := range messages {
		if promptMarker(msg.Content) == marker {
			return append(messages[:i:i], messages[i+1:]...)
		}
	}
	return messages
}

// BasePromptMessages returns the base system fragments shared by TUI, CLI,
// server, ACP, and subagent entrypoints. Everything here is system-role and
// therefore part of the cached prefix; per-turn state must not be added.
func (a *Agent) BasePromptMessages() []Message {
	var msgs []Message
	if env := a.environmentPrompt(); env != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptEnvMarker + "\n" + env})
	}
	if a.client != nil {
		if pp := modelFamilyPrompt(a.client.GetProvider(), a.client.GetModel()); pp != "" {
			msgs = append(msgs, Message{Role: "system", Content: promptProviderMarker + "\n" + pp})
		}
	}
	agentPrompt := a.Mode().SystemPrompt()
	if a.spec != nil && strings.TrimSpace(a.spec.SystemPrompt) != "" {
		agentPrompt = strings.TrimSpace(a.spec.SystemPrompt)
	}
	// Harness identity line (opencode parity): real opencode's system prompt
	// opens by naming itself (anthropic.txt: "You are OpenCode, the best
	// coding agent on the planet." / default.txt: "You are opencode, an
	// interactive CLI tool that helps users with software engineering
	// tasks."). Under the opencode harness the mode fragment carries that
	// opening, so the model identifies as opencode while ocode's mode
	// workflow still governs behavior. ocode harness adds nothing — the
	// agent answers as ocode. Lives inside the cached system block: the
	// harness only changes via /fake-agent, an explicit act that SHOULD
	// re-cache the prefix.
	if agentPrompt != "" && ActiveHarness() == HarnessOpencode {
		agentPrompt = "You are opencode, an interactive CLI tool that helps users with software engineering tasks.\n" + agentPrompt
	}
	if agentPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptModeMarker + "\n" + agentPrompt})
	}
	ctx := a.getPreloadedContext()
	if ctx == "" {
		enabled := make(map[string]bool)
		if a.config != nil {
			for name, p := range a.config.Plugins {
				enabled[name] = p.Enabled
			}
		}
		activeModel := ""
		if a.client != nil {
			activeModel = a.client.GetModel()
		}
		root := a.workDir
		if root == "" {
			if cwd, err := os.Getwd(); err == nil {
				root = cwd
			}
		}
		ctx = LoadContext(enabled, a.MemoryEnabled(), a.discoveryConfigEnabled(), activeModel, root)
	}
	if strings.TrimSpace(ctx) != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptContextMarker + "\nContext and rules:\n" + ctx})
	}
	if a.client != nil {
		// Anchor the search at the agent's workDir (the real project) — the
		// process cwd can be "/" for desktop/web launches and would silently
		// find nothing (or the wrong project's file). Cache shared with
		// ModelContextInfo/ModelContextContent.
		a.loadModelContextCached()
		if a.preloadedModelContext != "" {
			msgs = append(msgs, Message{Role: "system", Content: promptModelCtxMarker + "\nModel-specific context:\n" + a.preloadedModelContext})
		}
	}
	if a.DocPromptEnabled() {
		msgs = append(msgs, Message{Role: "system", Content: promptDocPromptMarker + "\n" + docPromptContent})
	}
	// Notes protocol fragment. Gate strictly on bus presence:
	// a child not in a group has no bus, and the prompt must
	// be byte-identical to the non-group case (zero overhead
	// on the common path). The fragment is part of the stable
	// prefix; it does not change per loop. See
	// append_stable.go for the cache-stability contract.
	if a.noteBus != nil && a.noteAgentID != "" {
		if n := a.notesProtocolPrompt(a.noteAgentID); n != "" {
			msgs = append(msgs, Message{Role: "system", Content: promptNotesMarker + "\n" + n})
		}
	}
	return msgs
}

// notesProtocolPrompt returns the stable prompt fragment
// that teaches a grouped child the notes bus protocol. The
// fragment names the agent's own id, shows the wire format,
// and states the two cardinal rules (leads-not-facts,
// cross-agent-value only). The wording is intentionally
// short — the [ocode:notes] marker is part of the stable
// prefix, so a long block costs cache tokens on every loop.
//
// "seq" and "by" attributes are filled by the system. The
// parser ignores any "by" the wire carries, and the bus
// stamps "seq" on append, so a child that authors them
// anyway is breaking the protocol AND will not be
// impersonated either way. The prompt states this so the
// child does not try.
func (a *Agent) notesProtocolPrompt(id string) string {
	return "Shared notes bus — protocol\n" +
		"You are agent " + id + ".\n" +
		"\n" +
		"Other agents in this group (a1, a2, ...) can read your notes and you can read theirs. The " +
		"shared bus is your only way to coordinate with them mid-task.\n" +
		"\n" +
		"EMIT — share only findings that have cross-agent value:\n" +
		"  <oc-note at=\"symbol-or-snippet\">caveman text</oc-note>\n" +
		"Do NOT author seq= or by= attributes. The system fills them.\n" +
		"Keep own-report-only findings OUT of the bus — they belong in your final report.\n" +
		"\n" +
		"RESOLVE — when a peer note turns out to be wrong or already addressed:\n" +
		"  <oc-resolve ref=\"N\"/>\n" +
		"N is the seq of the note you are resolving (you will see it on the wire as seq=\"N\").\n" +
		"\n" +
		"READ — notes you receive are LEADS, not facts. Weaker models may author them. " +
		"Always verify a received note against the actual code or document before acting on it. " +
		"A lead that turns out to be wrong is normal; correct it in your own report and resolve it on the bus."
}

func envHash(cwd, root, projectHost string) string {
	return strings.Join([]string{cwd, root, projectHost, os.Getenv("NVM_DIR"), os.Getenv("PYENV_ROOT"), os.Getenv("PATH")}, "|")
}

func (a *Agent) environmentPrompt() string {
	today := time.Now().Format("Mon Jan 2 2006")
	cwd, _ := os.Getwd()
	// Use the workDir override if set (e.g., via /cd command)
	if a.workDir != "" {
		cwd = a.workDir
	}
	root := findWorkspaceRoot(cwd)
	if a.envPromptDate == today && a.envPromptStr != "" && a.envPromptCwd == cwd && a.envPromptRoot == root && a.envPromptEnvHash == envHash(cwd, root, a.projectHost) && a.envPromptHarness == ActiveHarness() {
		return a.envPromptStr
	}
	provider, model := "", ""
	if a.client != nil {
		provider = a.client.GetProvider()
		model = a.client.GetModel()
	}
	modelID := strings.Trim(strings.TrimPrefix(provider+"/"+model, "/"), "/")
	if modelID == "" {
		modelID = "unknown"
	}
	// Resolve config and skill paths for the LLM.
	home, _ := os.UserHomeDir()
	var globalConfigBase string
	if runtime.GOOS == "windows" {
		globalConfigBase = filepath.Join(os.Getenv("APPDATA"), "opencode")
	} else {
		globalConfigBase = filepath.Join(home, ".config", "opencode")
	}
	globalOpencodeCfg := filepath.Join(globalConfigBase, "opencode.json")
	globalOcodeCfg := filepath.Join(globalConfigBase, "ocodeconfig.json")

	skillDirs := skill.SkillSearchPathsForRoot(root)
	var skillDirLines []string
	for _, d := range skillDirs {
		skillDirLines = append(skillDirLines, "    - "+d)
	}

	modelLines := []string{fmt.Sprintf("You are powered by the model named %s.", modelID)}
	if ActiveHarness() == HarnessOpencode {
		// Real opencode appends this second sentence
		// (packages/opencode/src/session/system.ts); it names the raw
		// provider/model pair so a provider-side harness check can key on it.
		modelLines = append(modelLines, fmt.Sprintf("The exact model ID is %s.", modelID))
	}
	lines := append(modelLines,
		"Here is some useful information about the environment you are running in:",
		"<env>",
		fmt.Sprintf("  Working directory: %s", cwd),
		fmt.Sprintf("  Workspace root folder: %s", root),
		fmt.Sprintf("  Is directory a git repo: %s", yesNo(isGitRepo(root))),
	)
	if isGitRepo(root) {
		lines = append(lines, "  Git worktree directory: .worktrees/ (gitignored, project root)")
	}
	// A per-project remote (SSH/WSL) project runs its chat agent on the local
	// machine, so the <env> block would otherwise present a remote project root
	// next to the local machine's config/session/skill/runtime paths with no
	// indication they belong to different machines. Say it explicitly.
	// Empty for local projects → byte-identical prompt (cache-stable).
	if a.projectHost != "" {
		lines = append(lines, fmt.Sprintf(
			"  Project host: %s (remote project — this agent runs on that host, so the project files, shell, home, and the config/session/runtime paths below all live on it)",
			a.projectHost,
		))
	}
	lines = append(lines,
		fmt.Sprintf("  Platform: %s", runtime.GOOS),
		fmt.Sprintf("  Today's date: %s", today),
		fmt.Sprintf("  Global opencode config (MCP servers, model, providers): %s", globalOpencodeCfg),
		fmt.Sprintf("  Global ocode config (permissions, extra paths, settings): %s", globalOcodeCfg),
		fmt.Sprintf("  Project ocode settings: %s", filepath.Join(root, ".ocode", "settings.json")),
		fmt.Sprintf("  Project opencode config: %s", filepath.Join(root, "opencode.json")),
	)
	// Project slug + session history location, so the model can answer
	// "where is this session's history stored" without spawning a shell to
	// recompute the SHA-256 slug hash itself.
	slug := paths.ProjectSlug(root)
	if sessionsDir, err := paths.ProjectSessionsDir(slug); err == nil {
		lines = append(lines, fmt.Sprintf("  Project slug (session storage id): %s", slug))
		lines = append(lines, fmt.Sprintf("  Session history directory: %s", sessionsDir))
		if a.sessionID != "" {
			lines = append(lines, fmt.Sprintf("  Current session file: %s", filepath.Join(sessionsDir, a.sessionID+".ojsonl")))
		}
	}
	lines = append(lines, "  Skills search paths (checked in order):")
	lines = append(lines, skillDirLines...)
	if rp := runtimePathsSection(root, cwd); len(rp) > 0 {
		lines = append(lines, rp...)
	}
	lines = append(lines, "</env>")
	result := strings.Join(lines, "\n")
	a.envPromptDate = today
	a.envPromptStr = result
	a.envPromptCwd = cwd
	a.envPromptRoot = root
	a.envPromptEnvHash = envHash(cwd, root, a.projectHost)
	a.envPromptHarness = ActiveHarness()
	return result
}

func existingPromptMarkers(messages []Message) map[string]bool {
	existing := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if marker := promptMarker(msg.Content); marker != "" {
			existing[marker] = true
		}
	}
	return existing
}

func promptMarker(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	if strings.HasPrefix(line, "[ocode:") && strings.HasSuffix(line, "]") {
		return line
	}
	return ""
}

func findWorkspaceRoot(start string) string {
	if start == "" {
		return ""
	}
	curr := start
	for {
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, "opencode.json")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, ".opencode")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return start
		}
		curr = parent
	}
}

func isGitRepo(root string) bool {
	if root == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

func mustGetwd() string {
	w, _ := os.Getwd()
	return w
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
