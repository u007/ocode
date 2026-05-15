package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
)

// connectStage tracks which step of the auth flow the dialog is on.
type connectStage int

const (
	connectStageProvider connectStage = iota
	connectStageMethod
	connectStageKeyInput
	connectStagePasteCode    // Anthropic: user pastes authorization code
	connectStageWaitBrowser  // OpenAI: localhost callback is running
	connectStageDeviceCode   // Copilot: show user_code, await poll
	connectStageMessage
)

type connectDialog struct {
	stage       connectStage
	providerIdx int
	methodIdx   int
	provider    *auth.Provider
	methods     []connectMethod
	keyInput    textinput.Model
	codeInput   textinput.Model
	message     string
	messageOK   bool

	// OAuth-flow scratch state.
	anthropicFlow auth.AnthropicFlow
	anthropicMode string // "max" | "console"
	copilotDevice auth.CopilotDevice
	cancelFlow    context.CancelFunc
}

type connectMethod struct {
	id    string // "apikey" | "oauth_max" | "oauth_console" | "oauth" | "remove" | "cancel"
	label string
}

func (m *model) openConnectDialog() {
	ti := textinput.New()
	ti.Placeholder = "paste API key and press Enter"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 512
	ti.Focus()

	codeIn := textinput.New()
	codeIn.Placeholder = "paste authorization code (or URL) and press Enter"
	codeIn.CharLimit = 2048

	m.connect = &connectDialog{
		stage:       connectStageProvider,
		providerIdx: 0,
		keyInput:    ti,
		codeInput:   codeIn,
	}
	m.showConnect = true
}

func (m *model) closeConnectDialog() {
	if m.connect != nil && m.connect.cancelFlow != nil {
		m.connect.cancelFlow()
	}
	m.showConnect = false
	m.connect = nil
}

// buildMethods returns the available methods for the current provider.
func (m *model) buildMethods() []connectMethod {
	if m.connect == nil || m.connect.provider == nil {
		return nil
	}
	p := m.connect.provider
	out := []connectMethod{{id: "apikey", label: "API Key"}}
	switch p.OAuthFlow {
	case "anthropic":
		out = append(out,
			connectMethod{id: "oauth_max", label: "Claude Pro/Max (OAuth)"},
			connectMethod{id: "oauth_console", label: "Anthropic Console (OAuth → API key)"},
		)
	case "openai":
		out = append(out, connectMethod{id: "oauth", label: "ChatGPT login (OAuth)"})
	case "google":
		out = append(out, connectMethod{id: "oauth", label: "Google (OAuth)"})
	case "copilot":
		out = append(out, connectMethod{id: "oauth", label: "GitHub device flow"})
	}
	if _, ok := auth.Get(p.ID); ok {
		out = append(out, connectMethod{id: "remove", label: "Remove stored credential"})
	}
	out = append(out, connectMethod{id: "cancel", label: "Cancel"})
	return out
}

// renderConnect draws the bordered dialog for the current stage.
func (m model) renderConnect() string {
	if m.connect == nil {
		return ""
	}

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true)

	var header, body, hint string

	switch m.connect.stage {
	case connectStageProvider:
		header = headerStyle.Render("Connect provider")
		var b strings.Builder
		for i, p := range auth.Providers {
			sym, detail := auth.Status(p.ID)
			line := fmt.Sprintf("%s  %-20s %s", sym, p.Label, hintStyle.Render(detail))
			if i == m.connect.providerIdx {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#1A1B26")).
					Background(lipgloss.Color("#7AA2F7")).
					Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		body = b.String()
		hint = hintStyle.Render("↑/↓ select · Enter continue · Esc cancel")

	case connectStageMethod:
		header = headerStyle.Render("Method: " + m.connect.provider.Label)
		var b strings.Builder
		for i, opt := range m.connect.methods {
			line := opt.label
			if i == m.connect.methodIdx {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#1A1B26")).
					Background(lipgloss.Color("#7AA2F7")).
					Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		body = b.String()
		hint = hintStyle.Render("↑/↓ select · Enter confirm · Esc back")

	case connectStageKeyInput:
		header = headerStyle.Render("API key: " + m.connect.provider.Label)
		body = m.connect.keyInput.View() + "\n"
		hint = hintStyle.Render("Enter save · Esc cancel · stored at ~/.config/ocode/auth.json (0600)")

	case connectStagePasteCode:
		header = headerStyle.Render("Anthropic OAuth: " + strings.ToUpper(m.connect.anthropicMode))
		body = fmt.Sprintf(
			"1. A browser tab was opened. Sign in.\n"+
				"2. Anthropic will redirect to a page showing an authorization code.\n"+
				"3. Copy the code (or the full callback URL) and paste below.\n\n"+
				"%s\n",
			m.connect.codeInput.View(),
		)
		hint = hintStyle.Render("Enter exchange · Esc cancel")

	case connectStageWaitBrowser:
		header = headerStyle.Render("OpenAI ChatGPT OAuth")
		body = "Opening browser… complete sign-in in the tab.\n\nThis dialog will update automatically when login finishes.\n"
		hint = hintStyle.Render("Esc cancel")

	case connectStageDeviceCode:
		header = headerStyle.Render("GitHub Copilot — device flow")
		body = fmt.Sprintf(
			"1. Open: %s\n2. Enter the code: %s\n\nWaiting for authorization…\n",
			lipgloss.NewStyle().Underline(true).Render(m.connect.copilotDevice.VerificationURI),
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ECE6A")).Render(m.connect.copilotDevice.UserCode),
		)
		hint = hintStyle.Render("Esc cancel")

	case connectStageMessage:
		header = headerStyle.Render("Connect")
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
		if !m.connect.messageOK {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F7768E"))
		}
		body = style.Render(m.connect.message) + "\n"
		hint = hintStyle.Render("Enter close")
	}

	width := m.width - 4
	if width < 60 {
		width = 60
	}
	return borderStyle.Width(width).Render(header + "\n\n" + body + "\n" + hint)
}

// updateConnectDialog handles key events while the connect dialog is open.
func (m model) updateConnectDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.connect
	if d == nil {
		m.showConnect = false
		return m, nil
	}

	keyStr := msg.String()

	switch d.stage {
	case connectStageProvider:
		switch keyStr {
		case "esc":
			m.closeConnectDialog()
			return m, nil
		case "up":
			if d.providerIdx > 0 {
				d.providerIdx--
			}
			return m, nil
		case "down":
			if d.providerIdx < len(auth.Providers)-1 {
				d.providerIdx++
			}
			return m, nil
		case "enter":
			d.provider = &auth.Providers[d.providerIdx]
			d.methods = m.buildMethods()
			d.methodIdx = 0
			d.stage = connectStageMethod
			return m, nil
		}
		return m, nil

	case connectStageMethod:
		switch keyStr {
		case "esc":
			d.stage = connectStageProvider
			return m, nil
		case "up":
			if d.methodIdx > 0 {
				d.methodIdx--
			}
			return m, nil
		case "down":
			if d.methodIdx < len(d.methods)-1 {
				d.methodIdx++
			}
			return m, nil
		case "enter":
			return m.applyConnectMethod()
		}
		return m, nil

	case connectStageKeyInput:
		switch keyStr {
		case "esc":
			d.stage = connectStageMethod
			return m, nil
		case "enter":
			key := strings.TrimSpace(d.keyInput.Value())
			if key == "" {
				d.message = "API key cannot be empty."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			if err := auth.Set(d.provider.ID, auth.Credential{Kind: auth.KindAPIKey, Key: key}); err != nil {
				d.message = fmt.Sprintf("Failed to save: %v", err)
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			if d.provider.EnvVar != "" {
				_ = os.Setenv(d.provider.EnvVar, key)
			}
			m.rebuildAgentClient()
			d.message = fmt.Sprintf("%s connected. Testing connection…", d.provider.Label)
			d.messageOK = true
			d.stage = connectStageMessage
			return m, m.testConnection(d.provider.ID)
		}
		var cmd tea.Cmd
		d.keyInput, cmd = d.keyInput.Update(msg)
		return m, cmd

	case connectStagePasteCode:
		switch keyStr {
		case "esc":
			d.stage = connectStageMethod
			return m, nil
		case "enter":
			input := strings.TrimSpace(d.codeInput.Value())
			code, state, ok := auth.ParseAnthropicCallback(input)
			if !ok {
				d.message = "Could not parse code+state from input."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			if state != d.anthropicFlow.State {
				d.message = "State mismatch — possible CSRF; restart the flow."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			return m, m.exchangeAnthropic(code)
		}
		var cmd tea.Cmd
		d.codeInput, cmd = d.codeInput.Update(msg)
		return m, cmd

	case connectStageWaitBrowser, connectStageDeviceCode:
		if keyStr == "esc" {
			if d.cancelFlow != nil {
				d.cancelFlow()
			}
			d.stage = connectStageMethod
			return m, nil
		}
		return m, nil

	case connectStageMessage:
		if keyStr == "enter" || keyStr == "esc" {
			m.closeConnectDialog()
		}
		return m, nil
	}
	return m, nil
}

func (m model) applyConnectMethod() (tea.Model, tea.Cmd) {
	d := m.connect
	if d == nil || d.provider == nil || d.methodIdx >= len(d.methods) {
		return m, nil
	}
	switch d.methods[d.methodIdx].id {
	case "apikey":
		d.keyInput.SetValue("")
		d.keyInput.Focus()
		d.stage = connectStageKeyInput
		return m, nil

	case "oauth":
		switch d.provider.OAuthFlow {
		case "google":
			d.stage = connectStageWaitBrowser
			return m, m.startGoogleOAuth(d.provider.ID)
		case "openai":
			d.stage = connectStageWaitBrowser
			return m, m.startOpenAIOAuth()
		case "copilot":
			return m, m.startCopilotDevice()
		}
		d.message = fmt.Sprintf("OAuth for %s not implemented.", d.provider.Label)
		d.messageOK = false
		d.stage = connectStageMessage
		return m, nil

	case "oauth_max", "oauth_console":
		mode := "max"
		if d.methods[d.methodIdx].id == "oauth_console" {
			mode = "console"
		}
		flow, err := auth.AnthropicAuthorize(mode)
		if err != nil {
			d.message = fmt.Sprintf("Failed to start OAuth: %v", err)
			d.messageOK = false
			d.stage = connectStageMessage
			return m, nil
		}
		d.anthropicFlow = flow
		d.anthropicMode = mode
		d.codeInput.SetValue("")
		d.codeInput.Focus()
		d.stage = connectStagePasteCode
		auth.OpenURL(flow.URL)
		return m, nil

	case "remove":
		if err := auth.Remove(d.provider.ID); err != nil {
			d.message = fmt.Sprintf("Failed to remove: %v", err)
			d.messageOK = false
		} else {
			if d.provider.EnvVar != "" {
				_ = os.Unsetenv(d.provider.EnvVar)
			}
			d.message = fmt.Sprintf("Removed credential for %s.", d.provider.Label)
			d.messageOK = true
		}
		d.stage = connectStageMessage
		return m, nil

	case "cancel":
		m.closeConnectDialog()
		return m, nil
	}
	return m, nil
}

// startGoogleOAuth kicks off the Google OAuth flow and reports back via connectOAuthFinishedMsg.
func (m model) startGoogleOAuth(providerID string) tea.Cmd {
	return func() tea.Msg {
		token, err := auth.LoginWithGoogle()
		if err != nil {
			return connectOAuthFinishedMsg{provider: providerID, err: err}
		}
		return connectOAuthFinishedMsg{provider: providerID, cred: auth.Credential{
			Kind:        auth.KindOAuth,
			AccessToken: token,
		}}
	}
}

// startOpenAIOAuth runs the ChatGPT OAuth flow.
func (m model) startOpenAIOAuth() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.connect.cancelFlow = cancel
	return func() tea.Msg {
		cred, err := auth.OpenAILogin(ctx)
		return connectOAuthFinishedMsg{provider: "openai", cred: cred, err: err}
	}
}

// exchangeAnthropic swaps the pasted authorization code for tokens.
func (m model) exchangeAnthropic(code string) tea.Cmd {
	flow := m.connect.anthropicFlow
	state := flow.State
	verifier := flow.Verifier
	return func() tea.Msg {
		cred, err := auth.AnthropicExchange(code, state, verifier)
		return connectOAuthFinishedMsg{provider: "anthropic", cred: cred, err: err}
	}
}

// startCopilotDevice requests a device code, shows it, then polls.
func (m model) startCopilotDevice() tea.Cmd {
	dev, err := auth.CopilotStartDevice()
	if err != nil {
		m.connect.message = fmt.Sprintf("Failed to start device flow: %v", err)
		m.connect.messageOK = false
		m.connect.stage = connectStageMessage
		return nil
	}
	m.connect.copilotDevice = dev
	m.connect.stage = connectStageDeviceCode
	auth.OpenURL(dev.VerificationURI)

	ctx, cancel := context.WithCancel(context.Background())
	m.connect.cancelFlow = cancel
	return func() tea.Msg {
		cred, err := auth.CopilotPoll(ctx, dev)
		if err == nil && cred.AccessToken != "" {
			cred.Account = auth.CopilotFetchAccount(cred.AccessToken)
		}
		return connectOAuthFinishedMsg{provider: "copilot", cred: cred, err: err}
	}
}

// testConnection runs a cheap probe against the saved credential.
func (m model) testConnection(providerID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return connectTestFinishedMsg{provider: providerID, err: auth.TestCredential(ctx, providerID)}
	}
}

// rebuildAgentClient re-creates the agent client so a newly-set API key is picked up.
func (m *model) rebuildAgentClient() {
	if m.config == nil || m.config.Model == "" {
		return
	}
	var mcpNames []string
	var tools = m.getInitialTools()
	if m.agent != nil {
		mcpNames = m.agent.MCPToolNames()
		tools = m.agent.GetTools()
	}
	client := agent.NewClient(m.config, m.config.Model)
	if client == nil {
		return
	}
	m.agent = agent.NewAgent(client, tools, m.config)
	if len(mcpNames) > 0 {
		m.agent.RestoreMCPToolNames(mcpNames)
	}
}
