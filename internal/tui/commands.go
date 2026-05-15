package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type commandSpec struct {
	name          string
	aliases       []string
	usage         string
	help          string
	takesModelArg bool
	handler       func(*model, []string) tea.Cmd
}

var commandSpecs = []commandSpec{
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
	{name: "/exit", aliases: []string{"/quit", "/q"}, help: "Quit the app", handler: runExitCmd},
}

var commandLookup = func() map[string]*commandSpec {
	lookup := make(map[string]*commandSpec, len(commandSpecs))
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		lookup[spec.name] = spec
		for _, alias := range spec.aliases {
			lookup[alias] = spec
		}
	}
	return lookup
}()

var commandHelpOutput string

func init() {
	commandHelpOutput = buildCommandHelpText(commandSpecs)
}

func lookupCommand(name string) *commandSpec {
	return commandLookup[name]
}

func commandNames() []string {
	names := make([]string, 0, len(commandSpecs))
	for _, spec := range commandSpecs {
		names = append(names, spec.name)
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
	switch provider {
	case "anthropic":
		return []string{"claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"}
	case "google":
		return []string{"gemini-1.5-pro", "gemini-1.5-flash"}
	default:
		return []string{"gpt-4o", "gpt-4o-mini", "o1-preview"}
	}
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

func runExitCmd(m *model, args []string) tea.Cmd {
	return tea.Quit
}
