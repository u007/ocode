package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/commands"
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

func init() {
	commandSpecs = []commandSpec{
		{name: "/model", usage: "/model <name>", help: "Switch LLM model", takesModelArg: true, handler: runModelCmd},
		{name: "/connect", help: "Show/Set provider API keys", handler: runConnectCmd},
		{name: "/login", help: "Google Login via OAuth2", handler: runLoginCmd},
		{name: "/session", usage: "/session <cmd>", help: "Manage sessions (list, load <id>)", handler: runSessionCmd},
		{name: "/compact", help: "Reduce context size by removing tool history", handler: runCompactCmd},
		{name: "/undo", help: "Revert last file change", handler: runUndoCmd},
		{name: "/redo", help: "Restore last undone change", handler: runRedoCmd},
		{name: "/export", help: "Save chat as Markdown", handler: runExportCmd},
		{name: "/new", aliases: []string{"/clear"}, help: "Start a fresh session", handler: runNewCmd},
		{name: "/thinking", help: "Toggle visibility of agent thoughts", handler: runThinkingCmd},
		{name: "/models", help: "List recommended models for active provider", handler: runModelsCmd},
		{name: "/details", help: "Toggle tool execution details", handler: runDetailsCmd},
		{name: "/init", help: "Create default AGENTS.md", handler: runInitCmd},
		{name: "/help", help: "Show this help", handler: runHelpCmd},
		{name: "/themes", help: "List available themes", handler: runThemesCmd},
		{name: "/share", help: "Export a shareable session summary", handler: runShareCmd},
		{name: "/editor", help: "Reopen the external editor", handler: runEditorCmd},
		{name: "/sidebar", help: "Toggle sidebar placeholder", handler: runSidebarCmd},
		{name: "/skills", help: "List available skills", handler: runSkillsCmd},
		{name: "/commands", help: "List all available commands (built-in + custom)", handler: runCommandsCmd},
		{name: "/mcp-auth", usage: "/mcp-auth <server>", help: "Authenticate with remote MCP server via OAuth", handler: runMCPAuthCmd},
		{name: "/agent", usage: "/agent <name>", help: "Switch agent (build, plan, review, debug, docs)", handler: runAgentCmd},
		{name: "/permissions", help: "View or set tool permissions", handler: runPermissionsCmd},
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
	loadedCustomCommands = commands.LoadCommands()
	customCommandLookup = make(map[string]*commands.Command, len(loadedCustomCommands))
	for i := range loadedCustomCommands {
		cmd := &loadedCustomCommands[i]
		customCommandLookup["/"+cmd.Name] = cmd
	}
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
	seen := make(map[string]struct{})
	var matches []string
	for _, spec := range commandSpecs {
		if strings.HasPrefix(spec.name, prefix) {
			if _, ok := seen[spec.name]; !ok {
				matches = append(matches, spec.name)
				seen[spec.name] = struct{}{}
			}
			continue
		}
		for _, alias := range spec.aliases {
			if strings.HasPrefix(alias, prefix) {
				if _, ok := seen[spec.name]; !ok {
					matches = append(matches, spec.name)
					seen[spec.name] = struct{}{}
				}
				break
			}
		}
	}
	for _, cmd := range loadedCustomCommands {
		name := "/" + cmd.Name
		if strings.HasPrefix(name, prefix) {
			if _, ok := seen[name]; !ok {
				matches = append(matches, name)
				seen[name] = struct{}{}
			}
		}
	}
	return matches
}

func modelSuggestions(m *model) []string {
	provider := "openai"
	if m != nil && m.agent != nil {
		provider = m.agent.GetProvider()
	}
	return providerModels(provider)
}

func providerModels(provider string) []string {
	return agent.ProviderModels(provider)
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
	b.WriteString("!command       : Run a shell command\n")
	b.WriteString("@path          : Add file content to context\n")
	b.WriteString("Ctrl+P         : Open command palette\n")
	b.WriteString("Ctrl+X         : Leader key for quick actions (u:undo, r:redo, n:new, l:list, c:compact)\n")
	return b.String()
}

func runModelCmd(m *model, args []string) tea.Cmd {
	m.handleModelCmd(args)
	return nil
}

func runConnectCmd(m *model, args []string) tea.Cmd {
	m.handleConnectCmd(args)
	return nil
}

func runLoginCmd(m *model, args []string) tea.Cmd {
	return m.handleLoginCmd(args)
}

func runSessionCmd(m *model, args []string) tea.Cmd {
	m.handleSessionCmd(args)
	return nil
}

func runCompactCmd(m *model, args []string) tea.Cmd {
	m.handleCompactCmd(args)
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

func runNewCmd(m *model, args []string) tea.Cmd {
	m.handleNewCmd(args)
	return nil
}

func runThinkingCmd(m *model, args []string) tea.Cmd {
	m.handleThinkingCmd(args)
	return nil
}

func runModelsCmd(m *model, args []string) tea.Cmd {
	m.handleModelsCmd(args)
	return nil
}

func runDetailsCmd(m *model, args []string) tea.Cmd {
	m.handleDetailsCmd(args)
	return nil
}

func runInitCmd(m *model, args []string) tea.Cmd {
	m.handleInitCmd(args)
	return nil
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

func runEditorCmd(m *model, args []string) tea.Cmd {
	return m.handleEditorCmd(args)
}

func runSidebarCmd(m *model, args []string) tea.Cmd {
	m.toggleSidebar()
	return nil
}

func runMCPAuthCmd(m *model, args []string) tea.Cmd {
	if len(args) < 2 {
		return func() tea.Msg {
			return statusMsg{text: "Usage: /mcp-auth <server-name>"}
		}
	}
	serverName := args[1]
	return func() tea.Msg {
		err := m.handleMCPAuth(serverName)
		if err != nil {
			return statusMsg{text: fmt.Sprintf("MCP auth failed: %s", err.Error())}
		}
		return statusMsg{text: fmt.Sprintf("MCP authentication successful for %s", serverName)}
	}
}

func runExitCmd(m *model, args []string) tea.Cmd {
	return tea.Quit
}

func runAgentCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		var b strings.Builder
		b.WriteString("Available agents:\n")
		for _, spec := range agent.DefaultAgents {
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
	if len(args) == 0 {
		if m.agent == nil || m.agent.Permissions() == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No permission rules configured."})
			return nil
		}
		rules := m.agent.Permissions().Rules()
		if len(rules) == 0 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No permission rules configured. All tools allowed by default."})
			return nil
		}
		var b strings.Builder
		b.WriteString("Permission rules:\n")
		for toolName, level := range rules {
			b.WriteString(fmt.Sprintf("  %-20s %s\n", toolName, level))
		}
		b.WriteString("\nUsage: /permissions <tool> <allow|deny|ask>")
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		return nil
	}
	if len(args) >= 2 {
		toolName := args[0]
		level := agent.PermissionLevel(args[1])
		if level != agent.PermissionAllow && level != agent.PermissionDeny && level != agent.PermissionAsk {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid permission level. Use: allow, deny, or ask."})
			return nil
		}
		if m.agent != nil && m.agent.Permissions() != nil {
			m.agent.Permissions().SetRule(toolName, level)
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Set %s permission for %q to %s.", level, toolName, level)})
		return nil
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /permissions [<tool> <allow|deny|ask>]"})
	return nil
}

func runSkillsCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		m.handleSkillsCmd(args)
		return nil
	}
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
