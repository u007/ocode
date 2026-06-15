package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/commands"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/redact"
)

type commandSpec struct {
	name          string
	aliases       []string
	usage         string
	help          string
	takesModelArg bool
	handler       func(*model, []string) tea.Cmd
}

var commandSpecs []commandSpec
var commandLookup map[string]*commandSpec
var commandHelpOutput string

var loadedCustomCommands []commands.Command
var customCommandLookup map[string]*commands.Command

func enabledPluginMap(cfg *config.Config) map[string]bool {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	enabled := make(map[string]bool, len(cfg.Plugins))
	for name, p := range cfg.Plugins {
		enabled[name] = p.Enabled
	}
	return enabled
}

func refreshCustomCommands(cfg *config.Config) {
	loadedCustomCommands = commands.LoadCommands(enabledPluginMap(cfg))
	customCommandLookup = make(map[string]*commands.Command, len(loadedCustomCommands))
	for i := range loadedCustomCommands {
		cmd := &loadedCustomCommands[i]
		customCommandLookup["/"+cmd.Name] = cmd
	}
}

func init() {
	commandSpecs = []commandSpec{
		{name: "/models", aliases: []string{"/model"}, usage: "/models [name]", help: "List and switch available models", takesModelArg: true, handler: runModelsCmd},
		{name: "/advisor", usage: "/advisor [provider/model|default]", help: "Set the advisor model (used by the advisor() tool for strategic guidance)", handler: runAdvisorCmd},
		{name: "/connect", help: "Show/Set provider API keys", handler: runConnectCmd},
		{name: "/login", help: "Google Login via OAuth2", handler: runLoginCmd},
		{name: "/session", aliases: []string{"/sessions", "/resume"}, usage: "/session [list|load <id>]", help: "Choose a session to resume", handler: runSessionCmd},
		{name: "/compact", usage: "/compact [focus]", help: "Summarise older context to free tokens; optional focus guides the summary", handler: runCompactCmd},
		{name: "/recap", help: "Summarize conversation in caveman style (uses small model)", handler: runRecapCmd},
		{name: "/changes", help: "Analyze repo changes: diffs, LSP errors, and in-progress specs", handler: runChangesCmd},
		{name: "/lsp", usage: "/lsp [show|open <path>|errors|all]", help: "Show current LSP diagnostics and error counts", handler: runLSPCmd},
		{name: "/undo", help: "Revert last file change", handler: runUndoCmd},
		{name: "/redo", help: "Restore last undone change", handler: runRedoCmd},
		{name: "/export", help: "Save chat as Markdown", handler: runExportCmd},
		{name: "/export-claude", help: "Append chat to Claude Code JSONL", handler: runExportClaudeCmd},
		{name: "/new", aliases: []string{"/clear"}, help: "Start a fresh session", handler: runNewCmd},
		{name: "/thinking", help: "Toggle visibility of agent thoughts", handler: runThinkingCmd},
		{name: "/details", help: "Toggle tool execution details", handler: runDetailsCmd},
		{name: "/init", usage: "/init [focus]", help: "Analyze project and generate AGENTS.md", handler: runInitCmd},
		{name: "/learn", usage: "/learn [focus]", help: "List project-root skills and guide skill creation/update", handler: runLearnCmd},
		{name: "/help", help: "Show this help", handler: runHelpCmd},
		{name: "/themes", aliases: []string{"/theme"}, usage: "/themes [name]", help: "Choose or switch themes", handler: runThemesCmd},
		{name: "/share", help: "Export a shareable session summary", handler: runShareCmd},
		{name: "/title", usage: "/title [text]", help: "Set session title (no arg = reset to auto-generate)", handler: runTitleCmd},
		{name: "/editor", usage: "/editor [command]", help: "Choose default external editor", handler: runEditorCmd},
		{name: "/editor-mode", usage: "/editor-mode [external|tmux-split|tmux-window]", help: "Set editor open mode", handler: runEditorModeCmd},
		{name: "/sidebar", help: "Toggle sidebar placeholder", handler: runSidebarCmd},
		{name: "/skills", help: "List available skills", handler: runSkillsCmd},
		{name: "/context", help: "Show context window token budget", handler: runContextCmd},
		{name: "/commands", help: "List all available commands (built-in + custom)", handler: runCommandsCmd},
		{name: "/mcp", usage: "/mcp [list|enable <server>|disable <server>]", help: "List or toggle MCP servers", handler: runMCPCmd},
		{name: "/mcp-auth", usage: "/mcp-auth <server>", help: "Authenticate with remote MCP server via OAuth", handler: runMCPAuthCmd},
		{name: "/agent", usage: "/agent <name>", help: "Switch agent (build, plan, review, debug, docs)", handler: runAgentCmd},
		{name: "/permissions", usage: "/permissions [auto-add|auto-remove|mode|auto|model|<tool>]", help: "View or set tool, bash auto-allow, and LLM auto-permissions (model test runs tests)", handler: runPermissionsCmd},
		{name: "/yolo", usage: "/yolo [on|off|status]", help: "Toggle YOLO permissions mode", handler: runYoloCmd},
		{name: "/small-model", usage: "/small-model [model]", help: "Show or switch the small model (used for lightweight tasks)", handler: runSmallModelCmd},
		{name: "/github", usage: "/github <action> [args]", help: "GitHub actions (pr, issue, workflow)", handler: runGitHubCmd},
		{name: "/usage", usage: "/usage [hour|day|week|month|last-month|last-3-month|all]", help: "Show LLM token usage summary by model and date range", handler: runUsageCmd},
		{name: "/plugin", usage: "/plugin [list|install <url[@ref]>|remove <name>|enable <name>|disable <name>|info <name>|create <name> [desc]|sync [name]|update [name]|confirm|cancel]", help: "List, install, update, or sync plugins", handler: runPluginCmd},
		{name: "/review", usage: "/review [file|commit|branch|pr]", help: "AI code review with actionable findings", handler: runReviewCmd},
		{name: "/rc", aliases: []string{"/remote-control"}, usage: "/rc [port]", help: "Start web UI to remote-control this session", handler: runRemoteControlCmd},
		{name: "/ide", usage: "/ide [claude|off|status]", help: "Connect to VS Code (Claude Code extension) for live file/selection context", handler: runIDECmd},
		{name: "/max-step", aliases: []string{"/max-steps"}, usage: "/max-step [n]", help: "Show or set the max tool-call steps before auto-summary", handler: runMaxStepCmd},
		{name: "/mask", usage: "/mask [on|off|status|model [name]|list]", help: "Show secret redaction status, manage the tier-2 model, or list secrets", handler: runMaskCmd},
		{name: "/exit", aliases: []string{"/quit", "/q"}, help: "Quit the app", handler: runExitCmd},
	}

	commandLookup = make(map[string]*commandSpec, len(commandSpecs))
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		commandLookup[spec.name] = spec
		for _, alias := range spec.aliases {
			commandLookup[alias] = spec
		}
	}

	commandHelpOutput = buildCommandHelpText(commandSpecs)
	refreshCustomCommands(nil)
}

func lookupCommand(name string) *commandSpec {
	return commandLookup[name]
}

func commandNames() []string {
	names := make([]string, 0, len(commandSpecs)+len(loadedCustomCommands))
	for _, spec := range commandSpecs {
		names = append(names, spec.name)
	}
	for _, cmd := range loadedCustomCommands {
		names = append(names, "/"+cmd.Name)
	}
	return names
}

func commandDisplayName(spec commandSpec) string {
	name := spec.name
	if spec.usage != "" {
		name = spec.usage
	}
	if len(spec.aliases) == 0 {
		return name
	}
	parts := append([]string{name}, spec.aliases...)
	return strings.Join(parts, ", ")
}

func autocompleteSlashInput(m *model, text string) []string {
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	if strings.HasSuffix(text, " ") {
		cmd := strings.TrimSpace(text)
		spec := lookupCommand(cmd)
		if spec != nil && spec.takesModelArg {
			return modelSuggestions(m)
		}
		return nil
	}

	prefix := strings.TrimSpace(text)
	suggestions := slashSuggestions(prefix)
	matches := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		matches = append(matches, s.name)
	}
	if prefix == "/m" {
		for i, name := range matches {
			if name == "/models" {
				copy(matches[1:i+1], matches[0:i])
				matches[0] = name
				break
			}
		}
	}
	return matches
}

func modelSuggestions(m *model) []string {
	return agent.AllProviderModels()
}

func commandHelpText() string {
	return commandHelpOutput
}

func buildCommandHelpText(specs []commandSpec) string {
	var b strings.Builder
	b.WriteString("Available Commands:\n")
	for _, spec := range specs {
		if spec.help == "" {
			continue
		}
		fmt.Fprintf(&b, "%-20s : %s\n", commandDisplayName(spec), spec.help)
	}
	b.WriteString("\nShortcuts:\n")
	b.WriteString("!command       : Prefix the input with ! to run a shell command (double-esc exits shell mode)\n")
	b.WriteString("@path          : Reference a file (attach an image, or pass the path to the model)\n")
	b.WriteString("Enter          : Send message\n")
	b.WriteString("Shift+Enter    : New line in input\n")
	b.WriteString("Up/Down        : Navigate input history\n")
	b.WriteString("Tab            : Autocomplete slash commands\n")
	b.WriteString("Shift+Tab      : Toggle agent strip focus (cycle through running agents)\n")
	b.WriteString("Ctrl+P         : Search and open files\n")
	b.WriteString("Ctrl+X         : Leader key for quick actions (u:undo, r:redo, n:new, l:list, c:compact, t:thinking level)\n")
	b.WriteString("Ctrl+D         : Cycle thinking effort level (off -> low -> med -> high)\n")
	b.WriteString("Ctrl+B         : Move running bash command to background\n")
	b.WriteString("Ctrl+G         : Open process list\n")
	b.WriteString("Ctrl+O         : Toggle YOLO permissions mode\n")
	b.WriteString("Ctrl+Y         : Retry last LLM timeout or I/O error\n")
	b.WriteString("Ctrl+C         : Clear input / Cancel / Quit (double-tap to quit)\n")
	b.WriteString("Esc            : Close popup / Exit shell mode / Cancel detail view\n")
	b.WriteString("\nPermission examples:\n")
	b.WriteString("/permissions bash:git allow\n")
	b.WriteString("/permissions auto-add jq\n")
	b.WriteString("/permissions auto-remove jq\n")
	b.WriteString("/permissions mode sed mutating\n")
	return b.String()
}

func runModelCmd(m *model, args []string) tea.Cmd {
	return m.handleModelCmd(args)
}

func runConnectCmd(m *model, args []string) tea.Cmd {
	m.handleConnectCmd(args)
	return nil
}

func runLoginCmd(m *model, args []string) tea.Cmd {
	return m.handleLoginCmd(args)
}

func runSessionCmd(m *model, args []string) tea.Cmd {
	return m.handleSessionCmd(args)
}

func runCompactCmd(m *model, args []string) tea.Cmd {
	m.handleCompactCmd(args)
	return nil
}

func runRecapCmd(m *model, args []string) tea.Cmd {
	return m.handleRecapCmd(args)
}

func runLSPCmd(m *model, args []string) tea.Cmd {
	m.handleLSPCmd(args)
	return nil
}

func runUndoCmd(m *model, args []string) tea.Cmd {
	m.handleUndoCmd(args)
	return nil
}

func runRedoCmd(m *model, args []string) tea.Cmd {
	m.handleRedoCmd(args)
	return nil
}

func runExportCmd(m *model, args []string) tea.Cmd {
	m.handleExportCmd(args)
	return nil
}

func runExportClaudeCmd(m *model, args []string) tea.Cmd {
	m.handleExportClaudeCmd(args)
	return nil
}

func runNewCmd(m *model, args []string) tea.Cmd {
	return m.handleNewCmd(args)
}

func runThinkingCmd(m *model, args []string) tea.Cmd {
	m.handleThinkingCmd(args)
	return nil
}

func runModelsCmd(m *model, args []string) tea.Cmd {
	return m.handleModelsCmd(args)
}

func runAdvisorCmd(m *model, args []string) tea.Cmd {
	return m.handleAdvisorCmd(args)
}

func runDetailsCmd(m *model, args []string) tea.Cmd {
	m.handleDetailsCmd(args)
	return nil
}

func runInitCmd(m *model, args []string) tea.Cmd {
	return m.handleInitCmd(args)
}

func runLearnCmd(m *model, args []string) tea.Cmd {
	return m.handleLearnCmd(args)
}

func runHelpCmd(m *model, args []string) tea.Cmd {
	m.handleHelpCmd(args)
	return nil
}

func runThemesCmd(m *model, args []string) tea.Cmd {
	m.handleThemesCmd(args)
	return nil
}

func runShareCmd(m *model, args []string) tea.Cmd {
	m.handleShareCmd(args)
	return nil
}

func runTitleCmd(m *model, args []string) tea.Cmd {
	return m.handleTitleCmd(args)
}

func runEditorCmd(m *model, args []string) tea.Cmd {
	return m.handleEditorCmd(args)
}

func runSidebarCmd(m *model, args []string) tea.Cmd {
	m.toggleSidebar()
	return nil
}

func runMCPAuthCmd(m *model, args []string) tea.Cmd {
	if len(args) < 1 {
		return func() tea.Msg {
			return statusMsg{text: "Usage: /mcp-auth <server-name>"}
		}
	}
	serverName := args[0]
	return func() tea.Msg {
		err := m.handleMCPAuth(serverName)
		if err != nil {
			return statusMsg{text: fmt.Sprintf("MCP auth failed: %s", err.Error())}
		}
		return statusMsg{text: fmt.Sprintf("MCP authentication successful for %s", serverName)}
	}
}

func runMCPCmd(m *model, args []string) tea.Cmd {
	if m.config == nil || len(m.config.MCP) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No MCP servers configured in opencode config."})
		return nil
	}
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "list", "ls", "status":
		m.messages = append(m.messages, message{role: roleAssistant, text: m.renderMCPList()})
		return nil
	case "enable", "on", "disable", "off":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /mcp enable <server> or /mcp disable <server>"})
			return nil
		}
		name := args[1]
		mcpCfg, ok := m.config.MCP[name]
		if !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("MCP server %q not found.", name)})
			return nil
		}
		enabled := action == "enable" || action == "on"
		if err := config.SaveMCPEnabled(name, enabled); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to update MCP config: %v", err)})
			return nil
		}
		mcpCfg.Enabled = enabled
		m.config.MCP[name] = mcpCfg
		listenCmd := m.rebuildAgentWithExternalTools()
		state := "enabled"
		if !enabled {
			state = "disabled"
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("MCP server %q %s.", name, state)})
		return listenCmd
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /mcp [list|enable <server>|disable <server>]"})
		return nil
	}
}

func runPluginCmd(m *model, args []string) tea.Cmd {
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "list", "ls", "":
		m.messages = append(m.messages, message{role: roleAssistant, text: m.renderPluginList()})
		return nil

	case "info":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin info <name>"})
			return nil
		}
		name := args[1]
		p, ok := m.config.Plugins[name]
		if !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		text := fmt.Sprintf("Plugin: %s\nSource: %s\nDir: %s\nEnabled: %v", name, p.Source, p.Dir, p.Enabled)
		for _, pl := range plugins.LoadPlugins(nil) {
			if pl.Name == name {
				if pl.Description != "" {
					text += "\nDescription: " + pl.Description
				}
				if len(pl.Tools) > 0 {
					text += "\nTools: " + strings.Join(pl.Tools, ", ")
				}
				if len(pl.Commands) > 0 {
					text += "\nCommands: " + strings.Join(pl.Commands, ", ")
				}
				break
			}
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: text})
		return nil

	case "enable", "on", "disable", "off":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin enable <name> or /plugin disable <name>"})
			return nil
		}
		name := args[1]
		enabled := action == "enable" || action == "on"
		// Builtin opt-in tools (disabled by default, persisted in ocode config).
		if name == "ast" {
			if err := config.SaveOcodeASTPlugin(enabled); err != nil {
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to update ast plugin: %v", err)})
				return nil
			}
			m.config.Ocode.Plugins.AST = enabled
			state := "enabled"
			if !enabled {
				state = "disabled"
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q %s (LSP-backed semantic navigation).", name, state)})
			return m.rebuildAgentWithExternalTools()
		}
		if _, ok := m.config.Plugins[name]; !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		if err := config.SavePluginEnabled(name, enabled); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to update plugin config: %v", err)})
			return nil
		}
		p := m.config.Plugins[name]
		p.Enabled = enabled
		m.config.Plugins[name] = p
		refreshCustomCommands(m.config)
		state := "enabled"
		if !enabled {
			state = "disabled"
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q %s.", name, state)})
		return m.rebuildAgentWithExternalTools()

	case "create":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin create <name> [description]"})
			return nil
		}
		name := args[1]
		desc := ""
		if len(args) > 2 {
			desc = strings.Join(args[2:], " ")
		}
		return func() tea.Msg { return pluginCreateMsg{name: name, description: desc} }

	case "install":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin install <github.com/user/repo[@ref]>"})
			return nil
		}
		source := args[1]
		ref := ""
		if at := strings.LastIndex(source, "@"); at > 0 {
			ref = source[at+1:]
			source = source[:at]
		}
		return func() tea.Msg { return pluginInstallMsg{source: source, ref: ref} }

	case "remove":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin remove <name>"})
			return nil
		}
		return func() tea.Msg { return pluginRemoveMsg{name: args[1]} }

	case "update", "upgrade":
		if len(args) < 2 {
			// Update all plugins.
			return func() tea.Msg { return pluginUpdateAllMsg{} }
		}
		name := args[1]
		cfg, ok := m.config.Plugins[name]
		if !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		return func() tea.Msg {
			return pluginUpdateMsg{name: name, source: cfg.Source, ref: cfg.Ref}
		}

	case "sync":
		if len(args) < 2 {
			// Sync all plugins.
			return func() tea.Msg { return pluginSyncAllMsg{} }
		}
		name := args[1]
		cfg, ok := m.config.Plugins[name]
		if !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		return func() tea.Msg {
			return pluginSyncMsg{name: name, source: cfg.Source, ref: cfg.Ref}
		}

	case "confirm":
		if m.pendingPluginInstall == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No pending plugin install."})
			return nil
		}
		pending := m.pendingPluginInstall
		m.pendingPluginInstall = nil
		return func() tea.Msg {
			if err := plugins.RunOnInstall(pending.dirName, pending.p); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			if err := plugins.AutoRegisterMCP(pending.dirName, pending.p); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			cfg := config.PluginConfig{Source: pending.source, Ref: pending.ref, Dir: pending.dirName, Enabled: true}
			if err := config.SavePlugin(pending.p.Name, cfg); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			return pluginInstalledMsg{name: pending.p.Name, source: pending.source, ref: pending.ref, dir: pending.dirName}
		}

	case "cancel":
		if m.pendingPluginInstall == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No pending plugin install."})
			return nil
		}
		pending := m.pendingPluginInstall
		m.pendingPluginInstall = nil
		if err := plugins.Remove(pending.dirName); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Warning: could not clean up plugin dir %s: %v", pending.dirName, err)})
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: "Plugin install cancelled."})
		return nil

	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin [list|install <url[@ref]>|remove <name>|enable <name>|disable <name>|info <name>|create <name> [desc]|sync [name]|update [name]|confirm|cancel]"})
		return nil
	}
}

func runExitCmd(m *model, args []string) tea.Cmd {
	m.cleanupCurrentSession()
	return tea.Quit
}

func runAgentCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		var b strings.Builder
		b.WriteString("Available agents:\n")
		for _, spec := range agent.PrimaryAgentSpecs() {
			current := ""
			if m.agent != nil && m.agent.Spec() != nil && m.agent.Spec().Name == spec.Name {
				current = " (active)"
			}
			b.WriteString(fmt.Sprintf("  %-10s %s%s\n", spec.Name, spec.Description, current))
		}
		b.WriteString("\nUse '/agent <name>' to switch. Press Tab to cycle.")
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		return nil
	}
	m.switchAgent(args[0])
	return nil
}

func runPermissionsCmd(m *model, args []string) tea.Cmd {
	usage := "Usage: /permissions [<tool> <allow|deny|ask> | bash:<prefix> <allow|deny|ask> | auto-add <prefix> | auto-remove <prefix> | mode <prefix> <read_only|mutating|never_auto> | auto <on|off|status> | model [test|<provider/model>|auto]]"
	if len(args) == 0 {
		if m.agent == nil || m.agent.Permissions() == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No permission manager configured.\n\n" + usage})
			return nil
		}
		rules := m.agent.Permissions().Rules()
		if len(rules) == 0 {
			autoEnabled := m.agent.Permissions().AutoPermissionEnabled()
			autoStatus := map[bool]string{true: "on", false: "off"}[autoEnabled]
			msg := fmt.Sprintf("Permission mode: %s\nLLM auto-allow: %s\n\nNo permission rules configured. All tools allowed by default.\n\n%s", m.agent.Permissions().Mode(), autoStatus, usage)
			m.messages = append(m.messages, message{role: roleAssistant, text: msg})
			return nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Permission mode: %s\n", m.agent.Permissions().Mode()))
		autoEnabled := m.agent.Permissions().AutoPermissionEnabled()
		b.WriteString(fmt.Sprintf("LLM auto-allow: %s\n\n", map[bool]string{true: "on", false: "off"}[autoEnabled]))
		b.WriteString("Tool permission rules:\n")
		ruleNames := make([]string, 0, len(rules))
		for toolName := range rules {
			ruleNames = append(ruleNames, toolName)
		}
		sort.Strings(ruleNames)
		for _, toolName := range ruleNames {
			level := rules[toolName]
			b.WriteString(fmt.Sprintf("  %-20s %s\n", toolName, level))
		}
		prefixRules := m.agent.Permissions().BashPrefixRules()
		if len(prefixRules) > 0 {
			b.WriteString("\nBash prefix rules:\n")
			prefixNames := make([]string, 0, len(prefixRules))
			for prefix := range prefixRules {
				prefixNames = append(prefixNames, prefix)
			}
			sort.Strings(prefixNames)
			for _, prefix := range prefixNames {
				level := prefixRules[prefix]
				b.WriteString(fmt.Sprintf("  %-20s %s\n", prefix, level))
			}
		}
		auto := m.agent.Permissions().BashAutoAllowPrefixes()
		sort.Strings(auto)
		if len(auto) > 0 {
			b.WriteString("\nBash auto-allow prefixes:\n")
			for _, prefix := range auto {
				b.WriteString(fmt.Sprintf("  %s\n", prefix))
			}
		}
		modes := m.agent.Permissions().BashPrefixModes()
		if len(modes) > 0 {
			modeNames := make([]string, 0, len(modes))
			for prefix := range modes {
				modeNames = append(modeNames, prefix)
			}
			sort.Strings(modeNames)
			b.WriteString("\nBash prefix modes:\n")
			for _, prefix := range modeNames {
				b.WriteString(fmt.Sprintf("  %-20s %s\n", prefix, modes[prefix]))
			}
		}
		b.WriteString("\n" + usage)
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		return nil
	}
	if m.agent == nil || m.agent.Permissions() == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No permission manager configured."})
		return nil
	}
	if len(args) >= 1 {
		action := strings.ToLower(args[0])
		switch action {
		case "auto-add":
			if len(args) < 2 {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			prefix := strings.TrimSpace(args[1])
			if prefix == "" {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			m.agent.Permissions().SetBashAutoAllowPrefix(prefix, true)
			m.persistPermissions()
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Added bash auto-allow prefix %q.", prefix)})
			return nil
		case "auto-remove":
			if len(args) < 2 {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			prefix := strings.TrimSpace(args[1])
			if prefix == "" {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			m.agent.Permissions().SetBashAutoAllowPrefix(prefix, false)
			m.persistPermissions()
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Removed bash auto-allow prefix %q.", prefix)})
			return nil
		case "mode":
			if len(args) < 3 {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			prefix := strings.TrimSpace(args[1])
			mode := strings.TrimSpace(args[2])
			if !m.agent.Permissions().SetBashPrefixMode(prefix, mode) {
				m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid mode. Use read_only, mutating, or never_auto."})
				return nil
			}
			m.persistPermissions()
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Set bash prefix mode for %q to %q.", prefix, mode)})
			return nil
		case "auto":
			if len(args) < 2 {
				m.messages = append(m.messages, message{role: roleAssistant, text: usage})
				return nil
			}
			sub := strings.ToLower(args[1])
			switch sub {
			case "on", "true", "yes", "1":
				m.agent.Permissions().SetAutoPermissionEnabled(true)
				m.persistPermissions()
				m.messages = append(m.messages, message{role: roleAssistant, text: "LLM auto-allow enabled."})
			case "off", "false", "no", "0":
				m.agent.Permissions().SetAutoPermissionEnabled(false)
				m.persistPermissions()
				m.messages = append(m.messages, message{role: roleAssistant, text: "LLM auto-allow disabled."})
			case "status":
				enabled := m.agent.Permissions().AutoPermissionEnabled()
				status := map[bool]string{true: "on", false: "off"}[enabled]
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("LLM auto-allow is %s.", status)})
			default:
				m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /permissions auto <on|off|status>"})
			}
			return nil
		case "model":
			return m.handlePermissionModelCmd(args[1:])
		}
	}
	if len(args) >= 2 {
		toolName := args[0]
		level := agent.PermissionLevel(args[1])
		if level != agent.PermissionAllow && level != agent.PermissionDeny && level != agent.PermissionAsk {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid permission level. Use: allow, deny, or ask."})
			return nil
		}
		if strings.HasPrefix(toolName, "bash:") {
			m.agent.Permissions().SetBashPrefixRule(strings.TrimPrefix(toolName, "bash:"), level)
		} else {
			m.agent.Permissions().SetRule(toolName, level)
		}
		m.persistPermissions()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Set %s permission for %q to %s.", level, toolName, level)})
		return nil
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: usage})
	return nil
}

func runYoloCmd(m *model, args []string) tea.Cmd {
	if m.agent == nil || m.agent.Permissions() == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No permission manager configured."})
		return nil
	}
	mode := m.agent.Permissions().Mode()
	if len(args) == 0 {
		if mode == agent.PermissionModeYOLO {
			m.agent.Permissions().SetMode(agent.PermissionModeNormal)
			mode = agent.PermissionModeNormal
		} else {
			m.agent.Permissions().SetMode(agent.PermissionModeYOLO)
			mode = agent.PermissionModeYOLO
		}
		m.persistPermissions()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Permission mode: %s", mode)})
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "true", "yes", "yolo":
		m.agent.Permissions().SetMode(agent.PermissionModeYOLO)
	case "off", "false", "no", "normal":
		m.agent.Permissions().SetMode(agent.PermissionModeNormal)
	case "locked", "lock":
		m.agent.Permissions().SetMode(agent.PermissionModeLocked)
	case "status":
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Permission mode: %s", mode)})
		return nil
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /yolo [on|off|status]"})
		return nil
	}
	m.persistPermissions()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Permission mode: %s", m.agent.Permissions().Mode())})
	return nil
}

func runSmallModelCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		m.handleSmallModelCmd(args)
		return nil
	}
}

func runSkillsCmd(m *model, args []string) tea.Cmd {
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "install", "upgrade", "update":
		// I/O-bound: run off the Update goroutine, communicate back via message
		// so model state is only mutated in Update — no data race.
		rest := args[1:]
		return func() tea.Msg {
			return skillsOutputMsg{text: m.runInstaller(sub, rest)}
		}
	default:
		m.handleSkillsCmd(args)
		return nil
	}
}

func runContextCmd(m *model, args []string) tea.Cmd {
	m.handleContextCmd(args)
	return nil
}

func runCommandsCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		b.WriteString("Built-in commands:\n")
		for _, spec := range commandSpecs {
			if spec.help == "" {
				continue
			}
			name := spec.name
			if spec.usage != "" {
				name = spec.usage
			}
			b.WriteString(fmt.Sprintf("  %-22s %s\n", name, spec.help))
		}
		if len(loadedCustomCommands) > 0 {
			b.WriteString("\nCustom commands:\n")
			for _, cmd := range loadedCustomCommands {
				desc := cmd.Description
				if desc == "" {
					desc = "(no description)"
				}
				b.WriteString(fmt.Sprintf("  /%-22s %s\n", cmd.Name, desc))
			}
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		return nil
	}
}

func runGitHubCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		if len(args) < 2 {
			var b strings.Builder
			b.WriteString("GitHub commands:\n")
			b.WriteString("  /github pr <owner> <repo> <number>     — Get PR diff and details\n")
			b.WriteString("  /github issue list <owner> <repo> [state] — List issues\n")
			b.WriteString("  /github issue get <owner> <repo> <number> — Get issue details\n")
			b.WriteString("  /github workflow <name>                — Generate workflow (test/lint/build/deploy)\n")
			m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
			return nil
		}

		action := args[1]
		switch action {
		case "pr":
			if len(args) < 5 {
				return statusMsg{text: "Usage: /github pr <owner> <repo> <number>"}
			}
			owner, repo := args[2], args[3]
			prNum, err := strconv.Atoi(args[4])
			if err != nil {
				return statusMsg{text: "Invalid PR number"}
			}
			return func() tea.Msg {
				pr, err := m.handleGitHubPR(owner, repo, prNum)
				if err != nil {
					return statusMsg{text: fmt.Sprintf("GitHub PR error: %s", err.Error())}
				}
				m.messages = append(m.messages, message{role: roleAssistant, text: pr})
				return nil
			}
		case "issue":
			if len(args) < 3 {
				return statusMsg{text: "Usage: /github issue <list|get> ..."}
			}
			subAction := args[2]
			switch subAction {
			case "list":
				if len(args) < 5 {
					return statusMsg{text: "Usage: /github issue list <owner> <repo> [state]"}
				}
				state := "open"
				if len(args) >= 6 {
					state = args[5]
				}
				return func() tea.Msg {
					result, err := m.handleGitHubIssueList(args[3], args[4], state)
					if err != nil {
						return statusMsg{text: fmt.Sprintf("GitHub issue error: %s", err.Error())}
					}
					m.messages = append(m.messages, message{role: roleAssistant, text: result})
					return nil
				}
			case "get":
				if len(args) < 6 {
					return statusMsg{text: "Usage: /github issue get <owner> <repo> <number>"}
				}
				num, err := strconv.Atoi(args[5])
				if err != nil {
					return statusMsg{text: "Invalid issue number"}
				}
				return func() tea.Msg {
					result, err := m.handleGitHubIssueGet(args[3], args[4], num)
					if err != nil {
						return statusMsg{text: fmt.Sprintf("GitHub issue error: %s", err.Error())}
					}
					m.messages = append(m.messages, message{role: roleAssistant, text: result})
					return nil
				}
			default:
				return statusMsg{text: "Unknown issue action: " + subAction}
			}
		case "workflow":
			if len(args) < 3 {
				return statusMsg{text: "Usage: /github workflow <test|lint|build|deploy>"}
			}
			return func() tea.Msg {
				result := m.handleGitHubWorkflow(args[2])
				m.messages = append(m.messages, message{role: roleAssistant, text: result})
				return nil
			}
		default:
			return statusMsg{text: "Unknown GitHub action: " + action}
		}
	}
}

func runUsageCmd(m *model, args []string) tea.Cmd {
	return m.handleUsageCmd(args)
}

func runEditorModeCmd(m *model, args []string) tea.Cmd {
	return m.handleEditorModeCmd(args)
}

func runReviewCmd(m *model, args []string) tea.Cmd {
	return m.handleReviewCmd(args)
}

func runRemoteControlCmd(m *model, args []string) tea.Cmd {
	return m.handleRemoteControlCmd(args)
}

func runIDECmd(m *model, args []string) tea.Cmd {
	return m.handleIDECmd(args)
}

func maskStatusText(m *model, includeHint bool) string {
	activeModel := m.currentModelName()
	if activeModel == "" {
		activeModel = "(not configured)"
	}
	state := "disabled"
	if m.redactionEnabled {
		state = "enabled"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Active model: %s\n", activeModel))
	b.WriteString(fmt.Sprintf("Secret redaction: %s\n", state))

	if m.llmScanner != nil {
		baseURL := m.llmScanner.BaseURL
		if len(baseURL) > 40 {
			baseURL = baseURL[:37] + "..."
		}
		b.WriteString(fmt.Sprintf("Tier-2 scanner: active (model=%s, base_url=%s)", m.redactionModel, baseURL))
	} else if m.redactionModel != "" {
		b.WriteString(fmt.Sprintf("Tier-2 scanner: inactive (model=%s, base_url not configured)", m.redactionModel))
	} else {
		b.WriteString("Tier-2 scanner: not configured")
	}

	if includeHint {
		b.WriteString("\n\nTry: /mask [on|off|status|model [name]|list]")
	}
	return b.String()
}

func runMaskCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: maskStatusText(m, true)})
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "true", "yes", "enable":
		if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) { rc.Enabled = true }); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error: %v", err)})
			return nil
		}
		m.redactionEnabled = true
		m.messages = append(m.messages, message{role: roleAssistant, text: "Secret redaction: enabled"})
	case "off", "false", "no", "disable":
		if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) { rc.Enabled = false }); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error: %v", err)})
			return nil
		}
		m.redactionEnabled = false
		m.messages = append(m.messages, message{role: roleAssistant, text: "Secret redaction: disabled"})
	case "status":
		m.messages = append(m.messages, message{role: roleAssistant, text: maskStatusText(m, false)})
	case "model":
		// Show or set the tier-2 scanning model
		if len(args) > 1 {
			// Set model
			if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) { rc.Model = args[1] }); err != nil {
				m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error: %v", err)})
				return nil
			}
			m.redactionModel = args[1]
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Tier-2 model: %s", args[1])})
		} else {
			// Open the model picker to select the tier-2 scanning model
			m.openRedactionModelPicker()
		}
	case "list":
		// List registered secrets
		if m.redactionRegistry == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No secrets registered in this session."})
		} else {
			entries := m.redactionRegistry.All()
			if len(entries) == 0 {
				m.messages = append(m.messages, message{role: roleAssistant, text: "No secrets registered in this session."})
			} else {
				var b strings.Builder
				b.WriteString(fmt.Sprintf("Registered secrets (%d):\n", len(entries)))
				for i, e := range entries {
					preview := redact.MaskedPreview(e.Value)
					source := e.Source
					if source == "" {
						source = "(unknown)"
					}
					b.WriteString(fmt.Sprintf("  %d. [%s] %s (source=%s)\n", i+1, e.Kind, preview, source))
				}
				m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
			}
		}
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /mask [on|off|status|model [name]|list]"})
	}
	return nil
}

func runMaxStepCmd(m *model, args []string) tea.Cmd {
	m.handleMaxStepCmd(args)
	return nil
}
