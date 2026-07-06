package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/u007/ocode/internal/knowledge"
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
// when DocPromptEnabled is true. It instructs the agent to consult existing
// docs before implementing and to update them afterward.
const docPromptContent = `## Documentation-First Development

Before writing any code:
1. **Read existing documentation** — look for README, CLAUDE.md, ARCHITECTURE.md, API docs, style guides, and schema definitions related to your changes.
2. **Check documentation alignment** — if existing docs describe behavior your changes will affect, verify there is no conflict. If there is a conflict, ask the user before proceeding.
3. **Build a mental model** of affected code paths and dependencies.

After implementing, update documentation to reflect your changes:
1. **Update inline documentation** — function/type comments, docstrings.
2. **Update project documentation** — README, API docs, architecture docs, migration notes if your changes affect public APIs, config, setup steps, or data flow.
3. **State explicitly if no doc updates are needed** and explain why.`

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
	base := a.BasePromptMessages(selectionContext)
	if len(base) == 0 {
		return messages
	}
	existing := existingPromptMarkers(messages)
	out := make([]Message, 0, len(base)+len(messages))
	for _, msg := range base {
		marker := promptMarker(msg.Content)
		if marker != "" && existing[marker] {
			continue
		}
		out = append(out, msg)
	}
	out = append(out, messages...)
	return out
}

// stripMarker removes the first system message with the given marker from messages.
func stripMarker(messages []Message, marker string) []Message {
	for i, msg := range messages {
		if msg.Role == "system" && promptMarker(msg.Content) == marker {
			return append(messages[:i:i], messages[i+1:]...)
		}
	}
	return messages
}

// BasePromptMessages returns the base system fragments shared by TUI, CLI,
// server, ACP, and subagent entrypoints.
func (a *Agent) BasePromptMessages(selectionContext string) []Message {
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
		ctx = LoadContext(enabled, a.MemoryEnabled(), a.discoveryConfigEnabled())
	}
	if strings.TrimSpace(ctx) != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptContextMarker + "\nContext and rules:\n" + ctx})
	}
	if a.client != nil {
		mc := a.preloadedModelContext
		if mc == "" {
			mc = LoadModelContext(a.client.GetModel())
			a.preloadedModelContext = mc
		}
		if mc != "" {
			msgs = append(msgs, Message{Role: "system", Content: promptModelCtxMarker + "\nModel-specific context:\n" + mc})
		}
	}
	if a.DocPromptEnabled() {
		msgs = append(msgs, Message{Role: "system", Content: promptDocPromptMarker + "\n" + docPromptContent})
		// Inject the [ocode:knowledge] index when the knowledge bundle is active.
		wd := a.workDir
		if wd == "" {
			wd, _ = os.Getwd()
		}
		if bundle, ok := knowledge.DetectBundle(wd); ok {
			indexPath := filepath.Join(bundle.Root, "index.md")
			if content, err := os.ReadFile(indexPath); err == nil {
				msgs = append(msgs, Message{Role: "system", Content: "[ocode:knowledge]\n" + string(content)})
			}
		}
	}
	if sel := strings.TrimSpace(selectionContext); sel != "" {
		msgs = append(msgs, Message{Role: "system", Content: promptSelectionMarker + "\n" + sel})
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

func (a *Agent) environmentPrompt() string {
	today := time.Now().Format("Mon Jan 2 2006")
	if a.envPromptDate == today && a.envPromptStr != "" {
		return a.envPromptStr
	}
	cwd, _ := os.Getwd()
	// Use the workDir override if set (e.g., via /cd command)
	if a.workDir != "" {
		cwd = a.workDir
	}
	root := findWorkspaceRoot(cwd)
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

	lines := []string{
		fmt.Sprintf("You are powered by the model named %s.", modelID),
		"Here is some useful information about the environment you are running in:",
		"<env>",
		fmt.Sprintf("  Working directory: %s", cwd),
		fmt.Sprintf("  Workspace root folder: %s", root),
		fmt.Sprintf("  Is directory a git repo: %s", yesNo(isGitRepo(root))),
	}
	if isGitRepo(root) {
		lines = append(lines, "  Git worktree directory: .worktrees/ (gitignored, project root)")
	}
	lines = append(lines,
		fmt.Sprintf("  Platform: %s", runtime.GOOS),
		fmt.Sprintf("  Today's date: %s", today),
		fmt.Sprintf("  Global opencode config (MCP servers, model, providers): %s", globalOpencodeCfg),
		fmt.Sprintf("  Global ocode config (permissions, extra paths, settings): %s", globalOcodeCfg),
		fmt.Sprintf("  Project ocode settings: %s", filepath.Join(root, ".ocode", "settings.json")),
		fmt.Sprintf("  Project opencode config: %s", filepath.Join(root, "opencode.json")),
		"  Skills search paths (checked in order):",
	)
	lines = append(lines, skillDirLines...)
	lines = append(lines, "</env>")
	result := strings.Join(lines, "\n")
	a.envPromptDate = today
	a.envPromptStr = result
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
