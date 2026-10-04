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
	connectFlowCommitting     connectFlowState = "committing"
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

// beginInput moves a waiting_input flow to running AND installs its cancel
// function in ONE locked step.
//
// These used to be two separate lock acquisitions — isWaitingInput() to check,
// then setState(running) — so two concurrent POSTs to the flow's input endpoint
// both passed the check and both started an exchange. The cancel func was
// separately assigned without the lock at all, racing the cancel handler's
// unlocked read. Doing the compare-and-set and the store together removes both:
// the loser of the race gets false and is told the flow already started.
func (f *connectFlow) beginInput(cancel context.CancelFunc) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state != connectFlowWaitingInput {
		return false
	}
	f.cancel = cancel
	f.state = connectFlowRunning
	f.updatedAt = time.Now()
	return true
}

// setCancel installs the cancel func for a flow that was never waiting for
// input. If the flow was already cancelled the func is invoked immediately, so
// a cancel that arrived before the func existed is not lost.
func (f *connectFlow) setCancel(cancel context.CancelFunc) {
	f.mu.Lock()
	if f.state == connectFlowCancelled {
		f.mu.Unlock()
		cancel()
		return
	}
	f.cancel = cancel
	f.mu.Unlock()
}

// takeCancel removes and returns the flow's cancel func. Taking it (rather than
// reading it) makes a repeated cancel a no-op instead of a double call.
func (f *connectFlow) takeCancel() context.CancelFunc {
	f.mu.Lock()
	defer f.mu.Unlock()
	cancel := f.cancel
	f.cancel = nil
	return cancel
}

// beginCommit claims the exclusive right to persist this flow's credential.
//
// It succeeds only from a state a background exchange can legitimately finish
// in — running (plugin, and every paste-code/cookie flow after beginInput) or
// waiting_browser (OpenAI auto, Copilot and Google, whose exchanges start
// immediately and block until the browser returns). It then moves the flow to
// committing, so exactly one caller can reach auth.Set and a cancel arriving
// afterwards cannot strand a half-written credential.
//
// A cancelled or already-finished flow loses the claim and its credential is
// DISCARDED. Without this check a flow the user cancelled still had its
// credential saved, because the Anthropic, Google and manual-OpenAI exchanges
// take no context and kept running to completion regardless of the cancel.
// waiting_input is deliberately NOT claimable: no exchange was ever started for
// such a flow, so a completion there means a bug, not a credential to keep.
func (f *connectFlow) beginCommit() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.state {
	case connectFlowRunning, connectFlowWaitingBrowser:
		f.state = connectFlowCommitting
		f.updatedAt = time.Now()
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

// Completion modes for a loopback OAuth flow. "auto" makes the SERVER bind
// the callback port and finish when the provider redirects the browser back to
// it, which needs the browser on the server's machine. "manual" binds nothing
// and takes a pasted redirect instead, so it works from a `serve --remote` host
// or a second device.
const (
	connectModeAuto   = "auto"
	connectModeManual = "manual"
)

// oauthFlowTakesMode reports whether a method's start handler honours a
// caller-chosen completion mode.
//
// Exactly one flow qualifies: the OpenAI loopback login, shared by every
// provider whose catalog entry declares OAuthFlow "openai" (openai AND codex —
// keying this on the provider id would silently downgrade codex). Everything
// else has a single shape and MUST NOT advertise a choice: an Anthropic
// paste-code flow that offered "open the page on this machine" beside "paste the
// redirect back" would render a control the server ignores, and a device-code or
// plugin flow has no loopback port to choose between.
//
// This is the SINGLE source for both the advertised `modes` below and the start
// handler's dispatch, so the two cannot drift: a method that advertises a mode
// the start handler ignores would strand a client in a flow that never finishes.
func oauthFlowTakesMode(p *auth.Provider, methodID string) bool {
	return methodID == "oauth" && p.OAuthFlow == "openai"
}

// connectMethodInfo is one selectable way to connect a provider,
// mirroring the TUI /connect method list.
type connectMethodInfo struct {
	ID    string            `json:"id"`
	Label string            `json:"label"`
	Kind  connectMethodKind `json:"kind"`
	// Modes lists the completion modes this method accepts, or is omitted when
	// the method has exactly one shape. A client MUST NOT offer a choice the
	// server does not honour; see oauthFlowTakesMode.
	Modes []string `json:"modes,omitempty"`
}

// connectMethodsFor adapts the shared catalog in internal/auth to the
// wire shape. The decision logic (which methods a provider offers) lives
// in auth.MethodsFor so this endpoint and the TUI /connect dialog cannot
// drift — they previously carried verbatim copies.
func connectMethodsFor(p *auth.Provider) []connectMethodInfo {
	shared := auth.MethodsFor(p)
	out := make([]connectMethodInfo, 0, len(shared))
	for _, m := range shared {
		info := connectMethodInfo{ID: m.ID, Label: m.Label, Kind: connectMethodKind(m.Kind)}
		if oauthFlowTakesMode(p, m.ID) {
			info.Modes = []string{connectModeAuto, connectModeManual}
		}
		out = append(out, info)
	}
	return out
}

// maskConnectCredential hides a stored credential the same way the
// profile auth endpoints do: first and last characters of an API key,
// or a bare "oauth" marker for token credentials. Keys are never
// returned in full.
//
// The HEAD is never revealed. A 4+4 window on a short key discloses most of
// it — a 9-character key rendered as "1234••••6789" handed over eight of nine
// characters. Only the trailing 4 are shown, and only once the key is at least
// 16 characters long, which caps the disclosure at a quarter of the key (and
// far less on a real one). Shorter keys are masked whole.
func maskConnectCredential(cred auth.Credential) string {
	switch {
	case cred.Key != "":
		if len(cred.Key) >= 16 {
			return "••••••••••••" + cred.Key[len(cred.Key)-4:]
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
	case oauthFlowTakesMode(p, method.ID):
		mode := req.Mode
		if mode != connectModeManual {
			mode = connectModeAuto
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
	if mode == connectModeManual {
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
	f.instructions = "Sign in in the new tab; ocode finishes the sign-in automatically when the page returns to localhost."
	connectFlows.add(f)
	// setCancel, not a bare assignment: the flow is already reachable through
	// the registry here, so a cancel can race the write.
	f.setCancel(cancel)
	crashguard.Go(func() {
		cred, err := finish()
		completeConnectFlow(ctx, f, p.ID, cred, err)
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
	// auth.StartGoogleOAuth takes no context, so the token exchange cannot be
	// interrupted. Run it cancellably: cancel returns promptly, and the token is
	// discarded rather than saved behind the user's back. This is also what
	// gives the flow a cancel func at all — the Google flow previously had none,
	// so DELETE /flows/{id} was a complete no-op for it.
	ctx, cancel := context.WithCancel(context.Background())
	f.setCancel(cancel)
	runConnectExchange(ctx, f, p.ID, func() (auth.Credential, error) {
		token, err := finish()
		if err != nil {
			return auth.Credential{}, err
		}
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: token}, nil
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
	f.instructions = "Open the URL, enter the code, and authorize ocode."
	connectFlows.add(f)
	f.setCancel(cancel)
	crashguard.Go(func() {
		cred, err := copilotPollFn(ctx, dev)
		if err == nil && cred.AccessToken != "" {
			cred.Account = copilotFetchAccountFn(cred.AccessToken)
		}
		completeConnectFlow(ctx, f, p.ID, cred, err)
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
	f.instructions = fmt.Sprintf("Running %s…", label)
	connectFlows.add(f)
	f.setCancel(cancel)
	crashguard.Go(func() {
		result, err := am.Run(ctx)
		var cred auth.Credential
		if err == nil {
			cred = pluginAuthResultCredential(result)
		}
		completeConnectFlow(ctx, f, p.ID, cred, err)
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

// completeConnectFlow records the outcome of a background flow: on success it
// stores the credential through the targeted auth.Set saver (the same one the
// TUI uses); on failure it records the error. It never returns tokens to any
// caller.
//
// A flow that was cancelled does not save anything. ctx is the flow's own
// context and is checked as well as the state, because a cancel and a finishing
// exchange can race: ctx.Err() closes the window between the cancel signal and
// the cancelled state being recorded.
func completeConnectFlow(ctx context.Context, f *connectFlow, providerID string, cred auth.Credential, err error) {
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		f.fail(err.Error())
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	if !f.beginCommit() {
		return
	}
	if setErr := auth.Set(providerID, cred); setErr != nil {
		f.fail(setErr.Error())
		return
	}
	f.succeed(cred)
}

// connectExchangeResult is one exchange's outcome. The channel carrying it is
// buffered so the goroutine can always exit, even when nobody is left to read.
type connectExchangeResult struct {
	cred auth.Credential
	err  error
}

// runConnectExchange runs a blocking exchange that takes NO context — the
// Anthropic code exchange, the Google token exchange and the manual OpenAI
// exchange are all shaped that way — so that cancelling the flow still returns
// promptly.
//
// It is fully ASYNCHRONOUS: it returns immediately, because every caller is an
// HTTP handler that must reply with an auth URL (or a "running"
// acknowledgement) without waiting on the provider. The wait therefore lives on
// its own goroutine.
//
// The network call cannot be interrupted, so the inner goroutine is left to
// finish on its own; what cancel controls is whether its credential is USED. On
// a cancellation the result is dropped and the flow is marked cancelled, so a
// connect the user walked away from cannot complete behind their back.
func runConnectExchange(ctx context.Context, f *connectFlow, providerID string, exchange func() (auth.Credential, error)) {
	done := make(chan connectExchangeResult, 1)
	crashguard.Go(func() {
		cred, err := exchange()
		done <- connectExchangeResult{cred: cred, err: err}
	})
	crashguard.Go(func() {
		select {
		case res := <-done:
			completeConnectFlow(ctx, f, providerID, res.cred, res.err)
		case <-ctx.Done():
			f.markCancelled()
		}
	})
}

// markCancelled records a cancellation raised from inside the flow rather than
// by the cancel endpoint. It never overwrites a terminal state, so a credential
// that already committed keeps its outcome.
func (f *connectFlow) markCancelled() {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.state {
	case connectFlowComplete, connectFlowFailed, connectFlowCancelled, connectFlowCommitting:
		return
	}
	f.state = connectFlowCancelled
	f.updatedAt = time.Now()
}

// handleConnectFlowInput supplies the interactive input for a
// waiting_input flow: a pasted Anthropic callback URL, or Grok's
// x.com cookies.
//
// There is deliberately no up-front isWaitingInput() check here. It used to
// guard the endpoint, but it was a SEPARATE lock acquisition from the
// setState(running) that followed, so two concurrent POSTs (a double-click, or
// a retried client) both passed it and both started an exchange. beginInput now
// performs the waiting_input -> running compare-and-set under one lock and is
// the authority for every branch; a caller that loses the race gets a 409.
func (h *Handler) handleConnectFlowInput(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("flowId")
	f := connectFlows.get(flowID)
	if f == nil {
		writeError(w, http.StatusNotFound, "unknown flow")
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
		verifier := f.anthropicVerifier
		ctx, cancel := context.WithCancel(context.Background())
		if !f.beginInput(cancel) {
			cancel()
			writeError(w, http.StatusConflict, fmt.Sprintf("flow is %s, it already started", f.getState()))
			return
		}
		// auth.AnthropicExchange takes no context, so run it cancellably.
		runConnectExchange(ctx, f, f.provider, func() (auth.Credential, error) {
			return anthropicExchangeFn(code, state, verifier)
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowRunning})

	case connectFlowLocalCallback:
		// Only a MANUAL-mode flow reaches here, and the separation is enforced
		// by the state machine rather than by a separate check: beginInput only
		// succeeds from waiting_input, and an auto flow stays waiting_browser
		// because it owns a loopback listener. A local-callback flow is
		// waiting_input ONLY if a manual starter created it, and every manual
		// starter must set openaiManual. If you add another loopback mode, give
		// it its own state or its own kind — do not make an auto flow wait for
		// input, or a paste can drive it.
		if strings.TrimSpace(req.Code) == "" {
			writeError(w, http.StatusBadRequest, "paste the URL your browser was redirected to.")
			return
		}
		flowState, pasted := f.openaiManual, req.Code
		ctx, cancel := context.WithCancel(context.Background())
		if !f.beginInput(cancel) {
			cancel()
			writeError(w, http.StatusConflict, fmt.Sprintf("flow is %s, it already started", f.getState()))
			return
		}
		// auth.ExchangeOpenAIManual takes no context, so run it cancellably.
		runConnectExchange(ctx, f, f.provider, func() (auth.Credential, error) {
			return openaiManualExchangeFn(flowState, pasted)
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowRunning})

	case connectFlowCookies:
		if strings.TrimSpace(req.AuthToken) == "" || strings.TrimSpace(req.Ct0) == "" {
			writeError(w, http.StatusBadRequest, "Both x.com cookies (auth_token and ct0) are required.")
			return
		}
		authToken, ct0 := req.AuthToken, req.Ct0
		ctx, cancel := context.WithCancel(context.Background())
		// beginInput installs cancel under the flow lock, so the cancel
		// endpoint can no longer race an unsynchronised write to f.cancel.
		if !f.beginInput(cancel) {
			cancel()
			writeError(w, http.StatusConflict, fmt.Sprintf("flow is %s, it already started", f.getState()))
			return
		}
		crashguard.Go(func() {
			cred, err := grokSubscriptionLoginFn(ctx, authToken, ct0)
			completeConnectFlow(ctx, f, f.provider, cred, err)
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
//
// Once a flow has claimed the right to persist its credential (committing) the
// cancel is refused with 409 rather than allowed to interleave with auth.Set —
// the credential is already on its way to disk and reporting "cancelled" would
// be a lie.
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
	if f.getState() == connectFlowCommitting {
		writeError(w, http.StatusConflict, "this flow is saving its credential and can no longer be cancelled")
		return
	}
	// takeCancel reads f.cancel under the flow lock. It used to be read with no
	// lock at all, racing the unlocked write that the input handler performed.
	if cancel := f.takeCancel(); cancel != nil {
		cancel()
	}
	f.setState(connectFlowCancelled)
	connectFlows.remove(flowID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"flowId": f.id, "state": connectFlowCancelled})
}
