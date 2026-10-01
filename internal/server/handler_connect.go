package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/crashguard"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// This file exposes the TUI's /connect capability ("Show/Set provider
// API keys") to the web and desktop settings as the Connectors section.
// It manages the BASE credential store (~/.local/share/opencode/auth.json,
// the same file the TUI edits) — per-profile credentials remain under
// /api/profiles/{name}/auth.

// connectFlowKind classifies the interaction shape of a connect flow,
// which is what the web UI renders.
type connectFlowKind string

const (
	connectFlowPasteCode     connectFlowKind = "paste-code"     // Anthropic: paste the full callback URL
	connectFlowLocalCallback connectFlowKind = "local-callback" // OpenAI/Google: localhost redirect
	connectFlowDeviceCode    connectFlowKind = "device-code"    // Copilot: user code + polling
	connectFlowCookies       connectFlowKind = "cookies"        // Grok: x.com cookies
	connectFlowPlugin        connectFlowKind = "plugin"         // plugin AuthMethod.Run
)

// connectFlowState is the lifecycle state of a connect flow.
type connectFlowState string

const (
	connectFlowWaitingInput   connectFlowState = "waiting_input"
	connectFlowWaitingBrowser connectFlowState = "waiting_browser"
	connectFlowRunning        connectFlowState = "running"
	connectFlowComplete       connectFlowState = "complete"
	connectFlowFailed         connectFlowState = "failed"
	connectFlowCancelled      connectFlowState = "cancelled"
)

// connectFlow is one in-flight connector flow. All state transitions go
// through the flow's own mutex; the registry only guards the map.
type connectFlow struct {
	id       string
	provider string
	method   string
	kind     connectFlowKind

	// anthropic paste-code scratch state
	anthropicState    string
	anthropicVerifier string
	// openai manual-mode scratch state. The PKCE verifier and the CSRF state
	// live HERE and never in a response: the client only ever receives AuthURL.
	openaiManual auth.OpenAIManualFlow
	// copilot device-code scratch state
	copilotDevice auth.CopilotDevice

	cancel context.CancelFunc

	mu              sync.Mutex
	state           connectFlowState
	url             string
	userCode        string
	verificationURI string
	instructions    string
	errMsg          string
	account         string
	updatedAt       time.Time
}

func newConnectFlow(provider, method string, kind connectFlowKind, state connectFlowState) *connectFlow {
	return &connectFlow{
		id:        newConnectFlowID(),
		provider:  provider,
		method:    method,
		kind:      kind,
		state:     state,
		updatedAt: time.Now(),
	}
}

func newConnectFlowID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on the supported platforms; fall
		// back to a time-based id rather than refusing to connect.
		return fmt.Sprintf("flow-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (f *connectFlow) setState(state connectFlowState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
	f.updatedAt = time.Now()
}

func (f *connectFlow) getState() connectFlowState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *connectFlow) isWaitingInput() bool {
	return f.getState() == connectFlowWaitingInput
}

func (f *connectFlow) isTerminal() bool {
	switch f.getState() {
	case connectFlowComplete, connectFlowFailed, connectFlowCancelled:
		return true
	}
	return false
}

func (f *connectFlow) succeed(cred auth.Credential) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = connectFlowComplete
	f.account = cred.Account
	f.updatedAt = time.Now()
}

func (f *connectFlow) fail(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = connectFlowFailed
	f.errMsg = msg
	f.updatedAt = time.Now()
}

// snapshot renders the flow for GET /flows/{id}. Tokens are never
// included — only the outcome and the UI payload.
func (f *connectFlow) snapshot() map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]interface{}{
		"flowId":   f.id,
		"provider": f.provider,
		"method":   f.method,
		"kind":     f.kind,
		"state":    f.state,
	}
	if f.url != "" {
		out["url"] = f.url
	}
	if f.userCode != "" {
		out["userCode"] = f.userCode
	}
	if f.verificationURI != "" {
		out["verificationUri"] = f.verificationURI
	}
	if f.instructions != "" {
		out["instructions"] = f.instructions
	}
	if f.errMsg != "" {
		out["error"] = f.errMsg
	}
	if f.account != "" {
		out["account"] = f.account
	}
	return out
}

// connectFlowTTL bounds how long a flow lives: waiting flows expire
// after 15 minutes (a device flow's user must act within it); terminal
// flows are kept briefly so the final status poll still reports the
// outcome.
const (
	connectFlowTTL         = 15 * time.Minute
	connectFlowTerminalTTL = 2 * time.Minute
)

type connectFlowRegistry struct {
	mu    sync.Mutex
	flows map[string]*connectFlow
}

var connectFlows = &connectFlowRegistry{flows: map[string]*connectFlow{}}

func (r *connectFlowRegistry) add(f *connectFlow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
	r.flows[f.id] = f
}

func (r *connectFlowRegistry) get(id string) *connectFlow {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
	return r.flows[id]
}

func (r *connectFlowRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.flows, id)
}

func (r *connectFlowRegistry) pruneLocked() {
	now := time.Now()
	for id, f := range r.flows {
		f.mu.Lock()
		ttl := connectFlowTTL
		if f.state == connectFlowComplete || f.state == connectFlowFailed || f.state == connectFlowCancelled {
			ttl = connectFlowTerminalTTL
		}
		expired := now.Sub(f.updatedAt) > ttl
		f.mu.Unlock()
		if expired {
			delete(r.flows, id)
		}
	}
}

// Seams for the network-bound auth flows, so handler tests can drive
// flow lifecycles without hitting the network (the same package-var
// pattern as notifyGitAction in internal/tui).
var (
	anthropicAuthorizeFn    = auth.AnthropicAuthorize
	anthropicExchangeFn     = auth.AnthropicExchange
	openaiStartFn           = auth.StartOpenAIOAuth
	openaiManualStartFn     = auth.StartOpenAIOAuthManual
	openaiManualExchangeFn  = auth.ExchangeOpenAIManual
	googleStartFn           = auth.StartGoogleOAuth
	copilotStartFn          = auth.CopilotStartDevice
	copilotPollFn           = auth.CopilotPoll
	copilotFetchAccountFn   = auth.CopilotFetchAccount
	grokSubscriptionLoginFn = auth.GrokSubscriptionLogin
	testCredentialFn        = auth.TestCredential
)

// connectMethodKind labels what a connect method does.
type connectMethodKind string

const (
	connectMethodAPIKey connectMethodKind = "apikey"
	connectMethodOAuth  connectMethodKind = "oauth"
	connectMethodPlugin connectMethodKind = "plugin"
	connectMethodRemove connectMethodKind = "remove"
)

// connectMethodInfo is one selectable way to connect a provider,
// mirroring the TUI /connect method list.
type connectMethodInfo struct {
	ID    string            `json:"id"`
	Label string            `json:"label"`
	Kind  connectMethodKind `json:"kind"`
}

// connectMethodsFor adapts the shared catalog in internal/auth to the
// wire shape. The decision logic (which methods a provider offers) lives
// in auth.MethodsFor so this endpoint and the TUI /connect dialog cannot
// drift — they previously carried verbatim copies.
func connectMethodsFor(p *auth.Provider) []connectMethodInfo {
	shared := auth.MethodsFor(p)
	out := make([]connectMethodInfo, 0, len(shared))
	for _, m := range shared {
		out = append(out, connectMethodInfo{ID: m.ID, Label: m.Label, Kind: connectMethodKind(m.Kind)})
	}
	return out
}

// maskConnectCredential hides a stored credential the same way the
// profile auth endpoints do: first and last characters of an API key,
// or a bare "oauth" marker for token credentials. Keys are never
// returned in full.
func maskConnectCredential(cred auth.Credential) string {
	switch {
	case cred.Key != "":
		if len(cred.Key) > 8 {
			return cred.Key[:4] + "••••" + cred.Key[len(cred.Key)-4:]
		}
		return "••••"
	case cred.AccessToken != "":
		return "oauth ••••"
	default:
		return "••••"
	}
}

// connectProviderView renders one provider for the list and the
// mutation responses: catalog order, connection status, the masked
// stored credential (if any), and the methods it offers.
func connectProviderView(p *auth.Provider) map[string]interface{} {
	symbol, detail := auth.Status(p.ID)
	view := map[string]interface{}{
		"id":            p.ID,
		"label":         p.Label,
		"status":        symbol,
		"statusDetail":  detail,
		"methods":       connectMethodsFor(p),
		"hasCredential": false,
	}
	if cred, ok := auth.Get(p.ID); ok {
		view["hasCredential"] = true
		view["kind"] = string(cred.Kind)
		view["masked"] = maskConnectCredential(cred)
	}
	return view
}

// handleConnectList lists every provider with its connection status,
// mirroring the TUI /connect provider stage. The catalog order is the
// display order (it is fixed in internal/auth, so the listing is
// deterministic).
func (h *Handler) handleConnectList(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]interface{}, 0, len(auth.Providers))
	for i := range auth.Providers {
		p := &auth.Providers[i]
		out = append(out, connectProviderView(p))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": out})
}

// handleConnectSet saves an API-key credential for a provider in the
// base store, mirroring the TUI's key-input stage (including the
// Cloudflare account/gateway extras). The provider env var is set on
// this process afterwards so the key takes effect immediately — env
// vars outrank the store in the credential resolution order.
func (h *Handler) handleConnectSet(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	p := auth.FindProvider(providerID)
	if p == nil {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	var req struct {
		APIKey    string `json:"apiKey"`
		Key       string `json:"key"`
		AccountID string `json:"accountId"`
		BaseURL   string `json:"baseUrl"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		key = strings.TrimSpace(req.Key)
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "apiKey or key required")
		return
	}
	cred := auth.Credential{Kind: auth.KindAPIKey, Key: key}
	switch p.ID {
	case "cloudflare-workers":
		if strings.TrimSpace(req.AccountID) == "" {
			writeError(w, http.StatusBadRequest, "accountId required for Cloudflare Workers AI")
			return
		}
		cred.AccountID = strings.TrimSpace(req.AccountID)
		cred.BaseURL = auth.CloudflareWorkersBaseURL(cred.AccountID)
	case "cloudflare-gateway":
		if strings.TrimSpace(req.BaseURL) == "" {
			writeError(w, http.StatusBadRequest, "baseUrl required for Cloudflare AI Gateway")
			return
		}
		cred.BaseURL = strings.TrimSpace(req.BaseURL)
	}
	if err := auth.Set(p.ID, cred); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save: %v", err))
		return
	}
	if p.EnvVar != "" {
		_ = os.Setenv(p.EnvVar, key)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "provider": connectProviderView(p)})
}

// handleConnectRemove deletes a provider's stored credential and
// unsets its env var, mirroring the TUI's "Remove stored credential".
func (h *Handler) handleConnectRemove(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	p := auth.FindProvider(providerID)
	if p == nil {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	if err := auth.Remove(providerID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove: %v", err))
		return
	}
	if p.EnvVar != "" {
		_ = os.Unsetenv(p.EnvVar)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "provider": connectProviderView(p)})
}

// handleConnectTest probes a saved credential, mirroring the TUI's
// "Testing connection…" step after a save.
func (h *Handler) handleConnectTest(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	p := auth.FindProvider(providerID)
	if p == nil {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err := testCredentialFn(ctx, providerID)
	resp := map[string]interface{}{"ok": err == nil}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleConnectOAuthStart begins an OAuth (or plugin) connect flow and
// returns a flow id plus the shape-specific payload (authorize URL,
// device code, paste instructions). The flow then completes in the
// background; the client polls GET /flows/{id}.
func (h *Handler) handleConnectOAuthStart(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	p := auth.FindProvider(providerID)
	if p == nil {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	var req struct {
		Method string `json:"method"`
		// Mode selects how a loopback OAuth flow completes: "auto" binds the
		// callback port, "manual" takes a pasted redirect instead. Only the
		// client can know whether the browser shares the server's host, so it
		// chooses; anything unrecognised is treated as "auto", which is the
		// historical behaviour.
		Mode string `json:"mode"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	methods := connectMethodsFor(p)
	var method *connectMethodInfo
	for i := range methods {
		if methods[i].ID == req.Method {
			method = &methods[i]
			break
		}
	}
	if method == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("method %q is not offered by %s", req.Method, p.Label))
		return
	}
	if method.Kind != connectMethodOAuth && method.Kind != connectMethodPlugin {
		writeError(w, http.StatusBadRequest, "method is not an OAuth flow")
		return
	}

	switch {
	case method.ID == "oauth_max" || method.ID == "oauth_console":
		h.startAnthropicConnectFlow(w, p, method.ID)
	case method.ID == "oauth" && p.OAuthFlow == "openai":
		mode := req.Mode
		if mode != "manual" {
			mode = "auto"
		}
		h.startOpenAIConnectFlow(w, p, mode)
	case method.ID == "oauth" && p.OAuthFlow == "google":
		h.startGoogleConnectFlow(w, p)
	case method.ID == "oauth" && p.OAuthFlow == "copilot":
		h.startCopilotConnectFlow(w, p)
	case method.ID == "grok_subscription":
		h.startGrokConnectFlow(w, p)
	case strings.HasPrefix(method.ID, "plugin_"):
		h.startPluginConnectFlow(w, p, strings.TrimPrefix(method.ID, "plugin_"))
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("OAuth for %s not implemented.", p.Label))
	}
}

func (h *Handler) startAnthropicConnectFlow(w http.ResponseWriter, p *auth.Provider, methodID string) {
	mode := "max"
	if methodID == "oauth_console" {
		mode = "console"
	}
	flow, err := anthropicAuthorizeFn(mode)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to start OAuth: %v", err))
		return
	}
	f := newConnectFlow(p.ID, methodID, connectFlowPasteCode, connectFlowWaitingInput)
	f.url = flow.URL
	f.anthropicState = flow.State
	f.anthropicVerifier = flow.Verifier
	f.instructions = "Open the URL, sign in, then paste the full callback URL here."
	connectFlows.add(f)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowPasteCode,
		"url":          f.url,
		"instructions": f.instructions,
	})
}

// startOpenAIConnectFlow begins the ChatGPT login.
//
// mode "auto" (the default) binds 127.0.0.1:1455 and finishes when the provider
// redirects the BROWSER back there — which only works when the browser and ocode
// share a host. mode "manual" binds nothing and finishes when the user pastes
// the redirect back, so it works from a `serve --remote` server or a second
// device. The CLIENT picks the mode and sends it, because only the client knows
// whether the browser is on the server's machine: `remoteMode` lives on
// *Server, and this route is registered as s.handler.*, so the handler cannot
// read it.
func (h *Handler) startOpenAIConnectFlow(w http.ResponseWriter, p *auth.Provider, mode string) {
	if mode == "manual" {
		h.startOpenAIManualFlow(w, p)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	authURL, finish, err := openaiStartFn(ctx)
	if err != nil {
		cancel()
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to start OAuth: %v", err))
		return
	}
	f := newConnectFlow(p.ID, "oauth", connectFlowLocalCallback, connectFlowWaitingBrowser)
	f.url = authURL
	f.cancel = cancel
	f.instructions = "Sign in in the new tab; ocode finishes the sign-in automatically when the page returns to localhost."
	connectFlows.add(f)
	crashguard.Go(func() {
		cred, err := finish()
		completeConnectFlow(f, p.ID, cred, err)
	})
	// `state` is reported for symmetry with the manual flow, so a client can
	// branch on the completion mode without a second status poll.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowLocalCallback,
		"state":        connectFlowWaitingBrowser,
		"url":          f.url,
		"instructions": f.instructions,
		"note":         "The sign-in page redirects back to localhost, so this flow needs your browser on the same machine as ocode. From another machine, request the manual mode instead.",
	})
}

// startOpenAIManualFlow registers a ChatGPT flow that waits for the user to
// paste back the redirect URL. It binds no port, so the browser may be anywhere.
func (h *Handler) startOpenAIManualFlow(w http.ResponseWriter, p *auth.Provider) {
	flow, err := openaiManualStartFn()
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to start OAuth: %v", err))
		return
	}
	f := newConnectFlow(p.ID, "oauth", connectFlowLocalCallback, connectFlowWaitingInput)
	f.url = flow.AuthURL
	f.openaiManual = flow
	f.instructions = "Open the URL, sign in, then paste the URL your browser was redirected to (it will not load — copy it from the address bar)."
	connectFlows.add(f)
	// The response carries the authorize URL only; the verifier and state stay
	// in the flow. See the leak guard in handler_connect_secrets_test.go.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowLocalCallback,
		"state":        connectFlowWaitingInput,
		"url":          f.url,
		"instructions": f.instructions,
		"note":         "Nothing is listening on a port, so this works even when ocode runs on another machine.",
	})
}

func (h *Handler) startGoogleConnectFlow(w http.ResponseWriter, p *auth.Provider) {
	authURL, finish, err := googleStartFn()
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to start OAuth: %v", err))
		return
	}
	f := newConnectFlow(p.ID, "oauth", connectFlowLocalCallback, connectFlowWaitingBrowser)
	f.url = authURL
	f.instructions = "Sign in in the new tab; ocode finishes the sign-in automatically when the page returns to localhost."
	connectFlows.add(f)
	crashguard.Go(func() {
		token, err := finish()
		var cred auth.Credential
		if err == nil {
			cred = auth.Credential{Kind: auth.KindOAuth, AccessToken: token}
		}
		completeConnectFlow(f, p.ID, cred, err)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowLocalCallback,
		"url":          f.url,
		"instructions": f.instructions,
		"note":         "The sign-in page redirects back to localhost, so this flow needs your browser on the same machine as ocode.",
	})
}

func (h *Handler) startCopilotConnectFlow(w http.ResponseWriter, p *auth.Provider) {
	dev, err := copilotStartFn()
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to start device flow: %v", err))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := newConnectFlow(p.ID, "oauth", connectFlowDeviceCode, connectFlowWaitingBrowser)
	f.userCode = dev.UserCode
	f.verificationURI = dev.VerificationURI
	f.copilotDevice = dev
	f.cancel = cancel
	f.instructions = "Open the URL, enter the code, and authorize ocode."
	connectFlows.add(f)
	crashguard.Go(func() {
		cred, err := copilotPollFn(ctx, dev)
		if err == nil && cred.AccessToken != "" {
			cred.Account = copilotFetchAccountFn(cred.AccessToken)
		}
		completeConnectFlow(f, p.ID, cred, err)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":          f.id,
		"kind":            connectFlowDeviceCode,
		"userCode":        f.userCode,
		"verificationUri": f.verificationURI,
		"instructions":    f.instructions,
	})
}

func (h *Handler) startGrokConnectFlow(w http.ResponseWriter, p *auth.Provider) {
	f := newConnectFlow(p.ID, "grok_subscription", connectFlowCookies, connectFlowWaitingInput)
	f.instructions = "Paste your x.com cookies (auth_token and ct0) to connect your Grok subscription."
	connectFlows.add(f)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowCookies,
		"instructions": f.instructions,
	})
}

func (h *Handler) startPluginConnectFlow(w http.ResponseWriter, p *auth.Provider, label string) {
	plugin, ok := providerplugin.Get(p.ID)
	if !ok {
		writeError(w, http.StatusBadGateway, "plugin no longer available")
		return
	}
	methods := plugin.AuthMethods()
	var am *providerplugin.AuthMethod
	for i := range methods {
		if methods[i].Label == label && methods[i].Run != nil {
			am = &methods[i]
			break
		}
	}
	if am == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("auth method %q not found", label))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := newConnectFlow(p.ID, "plugin_"+label, connectFlowPlugin, connectFlowRunning)
	f.cancel = cancel
	f.instructions = fmt.Sprintf("Running %s…", label)
	connectFlows.add(f)
	crashguard.Go(func() {
		result, err := am.Run(ctx)
		var cred auth.Credential
		if err == nil {
			cred = pluginAuthResultCredential(result)
		}
		completeConnectFlow(f, p.ID, cred, err)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"flowId":       f.id,
		"kind":         connectFlowPlugin,
		"instructions": f.instructions,
	})
}

// pluginAuthResultCredential maps a plugin auth result to a stored
// credential. The TUI's runPluginAuth always builds an OAuth
// credential; api-typed results are additionally mapped to an API key
// so plugins that return plain keys store correctly.
func pluginAuthResultCredential(result providerplugin.AuthResult) auth.Credential {
	if result.Type == "api" && result.Key != "" {
		return auth.Credential{Kind: auth.KindAPIKey, Key: result.Key, AccountID: result.AccountID}
	}
	return auth.Credential{
		Kind:         auth.KindOAuth,
		AccessToken:  result.Access,
		RefreshToken: result.Refresh,
		ExpiresAt:    result.Expires / 1000,
		AccountID:    result.AccountID,
		Account:      result.AccountID,
	}
}

// completeConnectFlow records the outcome of a background flow: on
// success it stores the credential through the targeted auth.Set saver
// (the same one the TUI uses); on failure it records the error. It
// never returns tokens to any caller.
func completeConnectFlow(f *connectFlow, providerID string, cred auth.Credential, err error) {
	if err != nil {
		f.fail(err.Error())
		return
	}
	if setErr := auth.Set(providerID, cred); setErr != nil {
		f.fail(setErr.Error())
		return
	}
	f.succeed(cred)
}

// handleConnectFlowInput supplies the interactive input for a
// waiting_input flow: a pasted Anthropic callback URL, or Grok's
// x.com cookies.
func (h *Handler) handleConnectFlowInput(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("flowId")
	f := connectFlows.get(flowID)
	if f == nil {
		writeError(w, http.StatusNotFound, "unknown flow")
		return
	}
	if !f.isWaitingInput() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("flow is %s, not waiting for input", f.getState()))
		return
	}
	var req struct {
		Code      string `json:"code"`
		AuthToken string `json:"authToken"`
		Ct0       string `json:"ct0"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	switch f.kind {
	case connectFlowPasteCode:
		code, state, ok := auth.ParseAnthropicCallback(req.Code)
		if !ok {
			writeError(w, http.StatusBadRequest, "could not parse code+state from input.")
			return
		}
		if state != f.anthropicState {
			writeError(w, http.StatusBadRequest, "state mismatch — possible CSRF; restart the flow.")
			return
		}
		f.setState(connectFlowRunning)
		verifier := f.anthropicVerifier
		crashguard.Go(func() {
			cred, err := anthropicExchangeFn(code, state, verifier)
			completeConnectFlow(f, f.provider, cred, err)
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowRunning})

	case connectFlowLocalCallback:
		// Only a MANUAL-mode flow reaches here, and the separation is enforced
		// upstream, not here: the isWaitingInput() guard above already rejected
		// the auto flow, which stays waiting_browser because it owns a loopback
		// listener. A local-callback flow is waiting_input ONLY if a manual
		// starter created it, and every manual starter must set openaiManual.
		// If you add another loopback mode, give it its own state or its own
		// kind — do not make an auto flow wait for input, or a paste can drive it.
		if strings.TrimSpace(req.Code) == "" {
			writeError(w, http.StatusBadRequest, "paste the URL your browser was redirected to.")
			return
		}
		f.setState(connectFlowRunning)
		flowState, pasted := f.openaiManual, req.Code
		crashguard.Go(func() {
			cred, err := openaiManualExchangeFn(flowState, pasted)
			completeConnectFlow(f, f.provider, cred, err)
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowRunning})

	case connectFlowCookies:
		if strings.TrimSpace(req.AuthToken) == "" || strings.TrimSpace(req.Ct0) == "" {
			writeError(w, http.StatusBadRequest, "Both x.com cookies (auth_token and ct0) are required.")
			return
		}
		f.setState(connectFlowRunning)
		ctx, cancel := context.WithCancel(context.Background())
		f.cancel = cancel
		authToken, ct0 := req.AuthToken, req.Ct0
		crashguard.Go(func() {
			cred, err := grokSubscriptionLoginFn(ctx, authToken, ct0)
			completeConnectFlow(f, f.provider, cred, err)
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowRunning})

	default:
		writeError(w, http.StatusBadRequest, "this flow does not accept pasted input")
	}
}

// handleConnectFlowStatus reports a flow's current state (poll this
// until it reaches a terminal state).
func (h *Handler) handleConnectFlowStatus(w http.ResponseWriter, r *http.Request) {
	f := connectFlows.get(r.PathValue("flowId"))
	if f == nil {
		writeError(w, http.StatusNotFound, "unknown flow")
		return
	}
	writeJSON(w, http.StatusOK, f.snapshot())
}

// handleConnectFlowCancel aborts a running flow (its background
// context is cancelled) and drops it from the registry.
func (h *Handler) handleConnectFlowCancel(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("flowId")
	f := connectFlows.get(flowID)
	if f == nil {
		writeError(w, http.StatusNotFound, "unknown flow")
		return
	}
	if f.isTerminal() {
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": f.getState()})
		return
	}
	if f.cancel != nil {
		f.cancel()
	}
	f.setState(connectFlowCancelled)
	connectFlows.remove(flowID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowCancelled})
}
