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

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/lsp"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
	"github.com/u007/ocode/internal/tool"
)

// connectStage tracks which step of the auth flow the dialog is on.
type connectStage int

const (
	connectStageProvider connectStage = iota
	connectStageMethod
	connectStageKeyInput
	connectStagePasteCode   // Anthropic: user pastes authorization code
	connectStageWaitBrowser // OpenAI: localhost callback is running
	connectStageDeviceCode  // Copilot: show user_code, await poll
	connectStageAccountID   // Cloudflare Workers: collect account ID before API key
	connectStageGatewayURL  // Cloudflare AI Gateway: collect gateway URL before API key
	connectStageGrokCookies // Grok subscription: collect x.com auth_token + ct0 cookies
	connectStageMessage
)

type connectDialog struct {
	stage              connectStage
	providerIdx        int
	methodIdx          int
	provider           *auth.Provider
	methods            []connectMethod
	keyInput           textinput.Model
	codeInput          textinput.Model
	accountIDInput     textinput.Model
	accountID          string
	gatewayURLInput    textinput.Model
	gatewayURL         string
	grokAuthTokenInput textinput.Model
	grokCt0Input       textinput.Model
	grokCookieField    int // 0 = auth_token, 1 = ct0
	message            string
	messageOK          bool

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
	m.input.Blur()

	ti := textinput.New()
	ti.Placeholder = "paste API key and press Enter"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 512
	ti.Focus()

	codeIn := textinput.New()
	codeIn.Placeholder = "paste authorization code (or URL) and press Enter"
	codeIn.CharLimit = 2048

	acctIn := textinput.New()
	acctIn.Placeholder = "Cloudflare Account ID (from dash.cloudflare.com)"
	acctIn.CharLimit = 64

	gwIn := textinput.New()
	gwIn.Placeholder = "Cloudflare AI Gateway URL (e.g. https://gateway.ai.cloudflare.com/v1/account/gateway)"
	gwIn.CharLimit = 256

	grokAt := textinput.New()
	grokAt.Placeholder = "x.com cookie: auth_token"
	grokAt.CharLimit = 2048
	grokAt.EchoMode = textinput.EchoPassword
	grokAt.EchoCharacter = '•'
	grokCt0 := textinput.New()
	grokCt0.Placeholder = "x.com cookie: ct0"
	grokCt0.CharLimit = 2048
	grokCt0.EchoMode = textinput.EchoPassword
	grokCt0.EchoCharacter = '•'

	m.connect = &connectDialog{
		stage:              connectStageProvider,
		providerIdx:        0,
		keyInput:           ti,
		codeInput:          codeIn,
		accountIDInput:     acctIn,
		gatewayURLInput:    gwIn,
		grokAuthTokenInput: grokAt,
		grokCt0Input:       grokCt0,
	}
	m.showConnect = true
}

func (m *model) closeConnectDialog() {
	if m.connect != nil && m.connect.cancelFlow != nil {
		m.connect.cancelFlow()
	}
	m.showConnect = false
	m.connect = nil
	m.input.Focus()
}

// buildMethods returns the available methods for the current provider.
func (m *model) buildMethods() []connectMethod {
	if m.connect == nil || m.connect.provider == nil {
		return nil
	}
	p := m.connect.provider
	out := []connectMethod{{id: "apikey", label: "API Key"}}
	if plugin, ok := providerplugin.Get(p.ID); ok {
		for _, am := range plugin.AuthMethods() {
			if am.Run == nil {
				continue
			}
			id := "plugin_" + am.Label
			// Grok's x.com subscription needs cookies collected by the TUI,
			// so it gets a dedicated method id handled outside the generic
			// plugin dispatch.
			if p.ID == "grok" && strings.Contains(am.Label, "Subscription") {
				id = "grok_subscription"
			}
			out = append(out, connectMethod{id: id, label: am.Label})
		}
	} else {
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

	var header, body, hint string

	switch m.connect.stage {
	case connectStageProvider:
		header = m.styles.Header.Render("Connect provider")
		var b strings.Builder
		for i, p := range auth.Providers {
			sym, detail := auth.Status(p.ID)
			line := fmt.Sprintf("%s  %-20s %s", sym, p.Label, hintStyle.Render(detail))
			if i == m.connect.providerIdx {
				line = m.styles.Selected.Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		rawLines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		sb := renderListScrollbar(len(rawLines), len(auth.Providers), 0, len(rawLines))
		sbLines := strings.Split(sb, "\n")
		for i, line := range rawLines {
			sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
			if i < len(sbLines) {
				sbCol = sbLines[i]
			}
			rawLines[i] = line + sbCol
		}
		body = strings.Join(rawLines, "\n") + "\n"
		hint = hintStyle.Render("↑/↓ select · Enter continue · Esc cancel")

	case connectStageMethod:
		header = m.styles.Header.Render("Method: " + m.connect.provider.Label)
		var b strings.Builder
		for i, opt := range m.connect.methods {
			line := opt.label
			if i == m.connect.methodIdx {
				line = m.styles.Selected.Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		rawLines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		sb := renderListScrollbar(len(rawLines), len(m.connect.methods), 0, len(rawLines))
		sbLines := strings.Split(sb, "\n")
		for i, line := range rawLines {
			sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
			if i < len(sbLines) {
				sbCol = sbLines[i]
			}
			rawLines[i] = line + sbCol
		}
		body = strings.Join(rawLines, "\n") + "\n"
		hint = hintStyle.Render("↑/↓ select · Enter confirm · Esc back")

	case connectStageKeyInput:
		header = m.styles.Header.Render("API key: " + m.connect.provider.Label)
		body = m.connect.keyInput.View() + "\n"
		hint = hintStyle.Render("Enter save · Esc cancel · stored at ~/.config/ocode/auth.json (0600)")

	case connectStageAccountID:
		header = m.styles.Header.Render("Connect " + m.connect.provider.Label)
		body = m.connect.accountIDInput.View()
		hint = hintStyle.Render("Enter your Cloudflare Account ID, then press Enter")

	case connectStageGatewayURL:
		header = m.styles.Header.Render("Connect " + m.connect.provider.Label)
		body = m.connect.gatewayURLInput.View()
		hint = hintStyle.Render("Enter your Cloudflare AI Gateway URL, then press Enter")

	case connectStageGrokCookies:
		header = m.styles.Header.Render("Grok Subscription — x.com cookies")
		body = "Paste the x.com session cookies from your browser after signing in to grok.com.\n\n" +
			m.connect.grokAuthTokenInput.View() + "\n" +
			m.connect.grokCt0Input.View() + "\n"
		hint = hintStyle.Render("Enter each field · after ct0 press Enter to connect · Esc back")

	case connectStagePasteCode:
		header = m.styles.Header.Render("Anthropic OAuth: " + strings.ToUpper(m.connect.anthropicMode))
		body = fmt.Sprintf(
			"1. A browser tab was opened. Sign in.\n"+
				"2. Anthropic will redirect to a page showing an authorization code.\n"+
				"3. Copy the code (or the full callback URL) and paste below.\n\n"+
				"%s\n",
			m.connect.codeInput.View(),
		)
		hint = hintStyle.Render("Enter exchange · Esc cancel")

	case connectStageWaitBrowser:
		header = m.styles.Header.Render("OpenAI ChatGPT OAuth")
		body = "Opening browser… complete sign-in in the tab.\n\nThis dialog will update automatically when login finishes.\n"
		hint = hintStyle.Render("Esc cancel")

	case connectStageDeviceCode:
		header = m.styles.Header.Render("GitHub Copilot — device flow")
		body = fmt.Sprintf(
			"1. Open: %s\n2. Enter the code: %s\n\nWaiting for authorization…\n",
			lipgloss.NewStyle().Underline(true).Render(m.connect.copilotDevice.VerificationURI),
			m.styles.Success.Bold(true).Render(m.connect.copilotDevice.UserCode),
		)
		hint = hintStyle.Render("Esc cancel")

	case connectStageMessage:
		header = m.styles.Header.Render("Connect")
		s := m.styles.Success
		if !m.connect.messageOK {
			s = m.styles.Error
		}
		body = s.Render(m.connect.message) + "\n"
		hint = hintStyle.Render("Enter close")
	}

	width := m.width - 4
	if width < 60 {
		width = 60
	}
	return borderStyle.Width(width).Render(header + "\n\n" + body + "\n" + hint)
}

func (m model) connectRowForY(y int) (int, bool) {
	if !m.showConnect || m.connect == nil {
		return 0, false
	}

	var count int
	switch m.connect.stage {
	case connectStageProvider:
		count = len(auth.Providers)
	case connectStageMethod:
		count = len(m.connect.methods)
	default:
		return 0, false
	}

	idx := y - 3 // border top + header + blank line
	if idx < 0 || idx >= count {
		return 0, false
	}
	return idx, true
}

func (m model) selectConnectRow(index int) (tea.Model, tea.Cmd) {
	d := m.connect
	if d == nil {
		m.showConnect = false
		return m, nil
	}

	switch d.stage {
	case connectStageProvider:
		if index < 0 || index >= len(auth.Providers) {
			return m, nil
		}
		d.providerIdx = index
		d.provider = &auth.Providers[d.providerIdx]
		d.methods = m.buildMethods()
		d.methodIdx = 0
		d.stage = connectStageMethod
		return m, nil
	case connectStageMethod:
		if index < 0 || index >= len(d.methods) {
			return m, nil
		}
		d.methodIdx = index
		return m.applyConnectMethod()
	default:
		return m, nil
	}
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
			return m.selectConnectRow(d.providerIdx)
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
			return m.selectConnectRow(d.methodIdx)
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
			cred := auth.Credential{Kind: auth.KindAPIKey, Key: key}
			if d.provider.ID == "cloudflare-workers" && d.accountID != "" {
				cred.AccountID = d.accountID
				cred.BaseURL = auth.CloudflareWorkersBaseURL(d.accountID)
			}
			if d.provider.ID == "cloudflare-gateway" && d.gatewayURL != "" {
				cred.BaseURL = d.gatewayURL
			}
			if err := auth.Set(d.provider.ID, cred); err != nil {
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

	case connectStageAccountID:
		switch keyStr {
		case "esc":
			d.stage = connectStageMethod
			return m, nil
		case "enter":
			id := strings.TrimSpace(d.accountIDInput.Value())
			if id == "" {
				d.message = "Account ID cannot be empty."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			d.accountID = id
			d.keyInput.SetValue("")
			d.keyInput.Focus()
			d.stage = connectStageKeyInput
			return m, nil
		}
		var cmd tea.Cmd
		d.accountIDInput, cmd = d.accountIDInput.Update(msg)
		return m, cmd

	case connectStageGatewayURL:
		switch keyStr {
		case "esc":
			d.stage = connectStageMethod
			return m, nil
		case "enter":
			url := strings.TrimSpace(d.gatewayURLInput.Value())
			if url == "" {
				d.message = "Gateway URL cannot be empty."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			d.gatewayURL = url
			d.keyInput.SetValue("")
			d.keyInput.Focus()
			d.stage = connectStageKeyInput
			return m, nil
		}
		var cmd tea.Cmd
		d.gatewayURLInput, cmd = d.gatewayURLInput.Update(msg)
		return m, cmd

	case connectStageGrokCookies:
		switch keyStr {
		case "esc":
			d.stage = connectStageMethod
			return m, nil
		case "enter":
			if d.grokCookieField == 0 {
				if strings.TrimSpace(d.grokAuthTokenInput.Value()) == "" {
					d.message = "x.com auth_token cannot be empty."
					d.messageOK = false
					d.stage = connectStageMessage
					return m, nil
				}
				d.grokCookieField = 1
				d.grokCt0Input.Focus()
				return m, nil
			}
			authToken := strings.TrimSpace(d.grokAuthTokenInput.Value())
			ct0 := strings.TrimSpace(d.grokCt0Input.Value())
			if authToken == "" || ct0 == "" {
				d.message = "Both x.com cookies (auth_token and ct0) are required."
				d.messageOK = false
				d.stage = connectStageMessage
				return m, nil
			}
			d.message = "Exchanging x.com cookies for a Grok subscription token…"
			d.messageOK = true
			d.stage = connectStageMessage
			return m, m.startGrokSubscription(authToken, ct0)
		}
		var cmd tea.Cmd
		if d.grokCookieField == 0 {
			d.grokAuthTokenInput, cmd = d.grokAuthTokenInput.Update(msg)
		} else {
			d.grokCt0Input, cmd = d.grokCt0Input.Update(msg)
		}
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
		if d.provider != nil && d.provider.ID == "cloudflare-workers" {
			d.accountIDInput.SetValue("")
			d.accountIDInput.Focus()
			d.stage = connectStageAccountID
			return m, nil
		}
		if d.provider != nil && d.provider.ID == "cloudflare-gateway" {
			d.gatewayURLInput.SetValue("")
			d.gatewayURLInput.Focus()
			d.stage = connectStageGatewayURL
			return m, nil
		}
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

	case "grok_subscription":
		d.grokCookieField = 0
		d.grokAuthTokenInput.SetValue("")
		d.grokCt0Input.SetValue("")
		d.grokAuthTokenInput.Focus()
		d.stage = connectStageGrokCookies
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

	default:
		if strings.HasPrefix(d.methods[d.methodIdx].id, "plugin_") {
			return m, m.runPluginAuth(d.methods[d.methodIdx].id)
		}
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

// startGrokSubscription exchanges the supplied x.com cookies for a Grok
// subscription SSO token and reports back via connectOAuthFinishedMsg.
func (m model) startGrokSubscription(authToken, ct0 string) tea.Cmd {
	return func() tea.Msg {
		cred, err := auth.GrokSubscriptionLogin(context.Background(), authToken, ct0)
		if err != nil {
			return connectOAuthFinishedMsg{provider: "grok", err: err}
		}
		return connectOAuthFinishedMsg{provider: "grok", cred: cred, err: nil}
	}
}

// runPluginAuth executes a plugin's auth method and stores the result.
func (m model) runPluginAuth(methodID string) tea.Cmd {
	d := m.connect
	providerID := d.provider.ID
	return func() tea.Msg {
		plugin, ok := providerplugin.Get(providerID)
		if !ok {
			return connectOAuthFinishedMsg{provider: providerID, err: fmt.Errorf("plugin not found for %s", providerID)}
		}
		label := strings.TrimPrefix(methodID, "plugin_")
		for _, am := range plugin.AuthMethods() {
			if am.Label == label && am.Run != nil {
				result, err := am.Run(context.Background())
				if err != nil {
					return connectOAuthFinishedMsg{provider: providerID, err: err}
				}
				cred := auth.Credential{Kind: auth.KindOAuth, AccessToken: result.Access, RefreshToken: result.Refresh, ExpiresAt: result.Expires / 1000, AccountID: result.AccountID, Account: result.AccountID}
				return connectOAuthFinishedMsg{provider: providerID, cred: cred, err: nil}
			}
		}
		return connectOAuthFinishedMsg{provider: providerID, err: fmt.Errorf("auth method %q not found", label)}
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
	var tools []tool.Tool
	var lspMgr *lsp.Manager
	tools, lspMgr = m.getInitialTools()
	if m.agent != nil {
		mcpNames = m.agent.MCPToolNames()
		tools = m.agent.GetTools()
		// Keep the same LSP manager the original agent was using so the
		// reconnect preserves diagnostic state (re-fetching the manager
		// here would create a NEW one and the old one's stored
		// diagnostics would be lost).
		lspMgr = m.lspMgr
	}
	client := agent.NewClient(m.config, m.config.Model)
	if client == nil {
		return
	}
	m.agent = agent.NewAgent(client, tools, m.config, lspMgr)
	// Apply the session-level advisor toggle so the rebuilt agent respects
	// the same runtime state as the previous agent.
	if m.advisorEnabledSet {
		m.agent.SetAdvisorEnabled(m.advisorEnabled)
	}
	if len(mcpNames) > 0 {
		m.agent.RestoreMCPToolNames(mcpNames)
	}
	m.syncRedactionRuntime()
	// Rewire the changes tab's registry accessor to the rebuilt agent;
	// installAgent isn't called here so this must be done explicitly, or
	// the Changes tab keeps reading the old/nil agent's registry.
	m.changes = m.changes.withRegistry(m.agent.Changes)
}
