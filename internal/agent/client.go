package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
	"github.com/u007/ocode/internal/redact"
)

const (
	llmRequestTimeout = 5 * time.Minute
	llmMaxRetries     = 3
	llmMaxRetries429  = 5
)

// Context keys for per-call chat parameter overrides (set by hook pipeline).
// Using context avoids mutating shared *GenericClient fields, which would race
// when multiple goroutines call Chat concurrently.
type ctxChatParamKey int

const (
	ctxKeyTemperature ctxChatParamKey = iota
	ctxKeyTopP
	ctxKeyTopK
)

var llmHTTPClient = &http.Client{Timeout: llmRequestTimeout}
var llmRetryBaseDelay = 500 * time.Millisecond

// ErrNoResponseFromOpenAIResponses is returned when the OpenAI Responses API
// returns an empty response (no text content and no tool calls).
var ErrNoResponseFromOpenAIResponses = errors.New("no response from openai responses api")

type Message struct {
	Role             string  `json:"role"`
	Content          string  `json:"content"`
	Images           []Image `json:"images,omitempty"`
	ReasoningContent string  `json:"reasoning_content,omitempty"`
	// Signature carries the thought signature from Gemini Interactions API or
	// Anthropic extended-thinking signature_delta. The signature is an encrypted
	// representation of the model's internal reasoning state and MUST be
	// re-sent on subsequent turns to maintain reasoning continuity.
	Signature           string                   `json:"signature,omitempty"`
	ToolCalls           []ToolCall               `json:"tool_calls,omitempty"`
	ToolID              string                   `json:"tool_call_id,omitempty"`
	OpenAIResponseItems []map[string]interface{} `json:"openai_response_items,omitempty"`
	Model               string                   `json:"-"`
	Usage               *TokenUsage              `json:"-"`
	Spend               *float64                 `json:"-"`
	// Notice carries a user-facing message that should be displayed in the
	// transcript but NOT sent to the LLM. Used by tools that encounter
	// recoverable problems worth surfacing (e.g. LSP server not installed).
	Notice string `json:"notice,omitempty"`
	// DisplayContent, when set, carries the FULL (untruncated) tool result for
	// transcript/UI display. The LLM prompt always uses Content (which may be
	// truncated by TruncateToolResult to protect the context window). This lets
	// a large result render in full in the UI while the model still receives a
	// bounded prefix. It is runtime-only (json:"-") and never sent to the LLM.
	DisplayContent string `json:"-"`
}

type Image struct {
	Path     string `json:"path,omitempty"`
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type ToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Signature carries the Gemini Interactions API function_call step signature.
	// Unlike thought signatures (stored on Message), each tool call has its own
	// encrypted signature that must be re-sent on subsequent turns.
	Signature string `json:"signature,omitempty"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type LLMClient interface {
	Chat(messages []Message, tools []map[string]interface{}) (*Message, error)
	GetProvider() string
	GetModel() string
}

type GenericClient struct {
	APIKey         string
	Model          string
	BaseURL        string
	Provider       string
	MaxImageDim    int
	UseOAuth       bool   // when true, treat APIKey as a bearer OAuth token
	AccountID      string // cached chatgpt_account_id from OAuth credential
	ThinkingBudget int    // >0 enables extended thinking for Anthropic models that support it
	// Temperature, when non-nil, is added to the request payload for providers
	// that accept it. Pointer so we can distinguish "unset" from explicit zero.
	Temperature *float64
	// TopP, when non-nil, is added to the request payload for providers that
	// accept it. Pointer for the same reason as Temperature.
	TopP *float64
	// TopK, when non-nil, is added to the request payload for models that
	// support it (e.g. MiniMax M2.x). Pointer for the same reason.
	TopK *float64
	// OnDelta, when non-nil, is invoked from inside Chat for each streamed
	// reasoning or text token. kind is "reasoning" or "text". Callers MUST set
	// this only around their own Chat call (via SetOnDelta / chatWithDelta) and
	// clear it after; subagents share the same *GenericClient and a stale
	// callback would leak deltas across agent boundaries. Access is serialised
	// via deltaMu so concurrent Chat callers can swap the field without racing
	// the streaming reader goroutines that fire it.
	OnDelta func(kind, text string)

	// OnUsage, when non-nil, is invoked from inside Chat when the provider
	// streams token usage information (e.g. Anthropic message_delta). Follows
	// the same pattern as OnDelta — set per-call via SetOnUsage, cleared after.
	OnUsage func(inputTokens, outputTokens int64)

	// deltaMu protects reads and writes to OnDelta and OnUsage. Refactoring
	// the callback per-call would require changing the LLMClient interface and
	// every call site; mutex-guarding the field is the lighter alternative
	// noted as acceptable in the original review.
	deltaMu sync.Mutex

	// UseWebSocket enables WebSocket transport for OpenAI Responses API.
	UseWebSocket bool

	// RetryNotifier, if set, is invoked from ChatWithContext before each retry
	// sleep. Fires on the calling goroutine — keep handlers fast and
	// non-blocking (e.g. push to a buffered channel). Set per-call and cleared
	// after; subagents share the same *GenericClient and a stale callback would
	// leak across agent boundaries.
	RetryNotifier func(attempt, maxRetries int, delay time.Duration, err error)

	// Redaction is an optional hook for the chokepoint safety net.
	// When set, ChatWithContext scans all message contents for known-format
	// secrets and redacts them before sending to the provider.
	Redaction *redact.NetHook
}

// SetOnDelta installs (or clears, with nil) the streaming-token callback on
// the client. Safe to call concurrently with active Chat invocations on the
// same *GenericClient.
func (c *GenericClient) SetOnDelta(fn func(kind, text string)) {
	c.deltaMu.Lock()
	c.OnDelta = fn
	c.deltaMu.Unlock()
}

// onDelta returns the currently-installed delta callback (or nil) under the
// mutex. Streaming reader paths call this once per chunk to avoid racing with
// SetOnDelta in chatWithDelta.
func (c *GenericClient) onDelta() func(kind, text string) {
	c.deltaMu.Lock()
	defer c.deltaMu.Unlock()
	return c.OnDelta
}

// SetOnUsage installs (or clears, with nil) the streaming-usage callback on
// the client. Safe to call concurrently with active Chat invocations on the
// same *GenericClient.
func (c *GenericClient) SetOnUsage(fn func(inputTokens, outputTokens int64)) {
	c.deltaMu.Lock()
	c.OnUsage = fn
	c.deltaMu.Unlock()
}

// onUsage returns the currently-installed usage callback (or nil) under the
// mutex. Streaming reader paths call this once per chunk to avoid racing with
// SetOnUsage in chatWithDelta.
func (c *GenericClient) onUsage() func(inputTokens, outputTokens int64) {
	c.deltaMu.Lock()
	defer c.deltaMu.Unlock()
	return c.OnUsage
}

// applyGenerationParams adds temperature / top_p / top_k to a request payload
// if configured. When the struct fields are nil, model-ID-based defaults may
// apply. Centralised so every provider branch picks them up. Skipped when the
// active model is a reasoning family (o1/o3/o4/gpt-5/etc.) or when the
// Anthropic extended-thinking budget is set — both APIs reject the sampling
// tunables in those cases.
func (c *GenericClient) applyGenerationParams(ctx context.Context, payload map[string]interface{}) {
	if !c.samplingTunable() {
		return
	}
	// Model-ID-based defaults when struct fields are nil. These match the
	// conventions in opencode's provider/transform.ts.
	temp := c.Temperature
	if temp == nil {
		if v := defaultTemperature(c.Model); v != nil {
			temp = v
		}
	}
	topP := c.TopP
	if topP == nil {
		if v := defaultTopP(c.Model); v != nil {
			topP = v
		}
	}
	topK := c.TopK
	if topK == nil {
		if v := defaultTopK(c.Model); v != nil {
			topK = v
		}
	}
	// Per-call overrides from context (set by hook pipeline in chatWithDelta)
	// take priority over struct fields. This avoids mutating the shared
	// *GenericClient, which would race under concurrent Chat callers.
	if ctx != nil {
		if v := ctx.Value(ctxKeyTemperature); v != nil {
			if t, ok := v.(*float64); ok {
				temp = t
			}
		}
		if v := ctx.Value(ctxKeyTopP); v != nil {
			if t, ok := v.(*float64); ok {
				topP = t
			}
		}
		if v := ctx.Value(ctxKeyTopK); v != nil {
			if t, ok := v.(*float64); ok {
				topK = t
			}
		}
	}
	if temp != nil {
		payload["temperature"] = *temp
	}
	if topP != nil {
		payload["top_p"] = *topP
	}
	if topK != nil {
		payload["top_k"] = *topK
	}
}

// defaultTemperature returns a model-ID-specific temperature default, or nil
// when the model has no known preference. Matches opencode's transform.ts
// and models.dev entries for newer model families.
func defaultTemperature(modelID string) *float64 {
	m := strings.ToLower(modelID)
	if strings.Contains(m, "north-mini-code") {
		return floatPtr(1.0)
	}
	if strings.Contains(m, "qwen") {
		return floatPtr(0.55)
	}
	if strings.Contains(m, "claude") {
		return nil
	}
	if strings.Contains(m, "gemini") {
		return floatPtr(1.0)
	}
	if strings.Contains(m, "glm-4.5") || strings.Contains(m, "glm-4.6") || strings.Contains(m, "glm-4.7") ||
		strings.Contains(m, "glm-5") {
		return floatPtr(1.0)
	}
	if strings.Contains(m, "minimax-m2") {
		return floatPtr(1.0)
	}
	if strings.Contains(m, "deepseek-v4") {
		return floatPtr(0.6)
	}
	if strings.Contains(m, "grok") {
		// Grok reasoning variants (grok-4, grok-4.3, grok-4.20)
		if strings.Contains(m, "non-reasoning") {
			return floatPtr(0.7)
		}
		return nil // reasoning models use reasoning_effort
	}
	if strings.Contains(m, "gemma") {
		return floatPtr(0.8)
	}
	if strings.Contains(m, "mistral") || strings.Contains(m, "codestral") || strings.Contains(m, "devstral") {
		return floatPtr(0.7)
	}
	if strings.Contains(m, "cohere") || strings.Contains(m, "command") {
		return floatPtr(0.75)
	}
	if strings.Contains(m, "llama") {
		return floatPtr(0.7)
	}
	if strings.Contains(m, "nemotron") {
		return floatPtr(0.7)
	}
	if strings.Contains(m, "minimax-m3") {
		return floatPtr(1.0)
	}
	if strings.Contains(m, "mimo") {
		return floatPtr(0.6)
	}
	if strings.Contains(m, "kimi-k2") {
		// kimi-k2-thinking, kimi-k2.5, kimi-k2p5, kimi-k2-5,
		// kimi-k2.6, kimi-k2.7, kimi-k2.7-code
		if strings.Contains(m, "thinking") || strings.Contains(m, "k2.") ||
			strings.Contains(m, "k2p") || strings.Contains(m, "k2-5") ||
			strings.Contains(m, "k2.6") || strings.Contains(m, "k2.7") ||
			strings.Contains(m, "k2-6") || strings.Contains(m, "k2-7") {
			return floatPtr(1.0)
		}
		return floatPtr(0.6)
	}
	return nil
}

// defaultTopP returns a model-ID-specific top_p default, or nil when the model
// has no known preference. Matches opencode's transform.ts and models.dev
// entries for newer model families.
func defaultTopP(modelID string) *float64 {
	m := strings.ToLower(modelID)
	if strings.Contains(m, "qwen") {
		return floatPtr(1)
	}
	if strings.Contains(m, "minimax-m2") ||
		strings.Contains(m, "gemini") ||
		strings.Contains(m, "kimi-k2.5") ||
		strings.Contains(m, "kimi-k2p5") ||
		strings.Contains(m, "kimi-k2-5") ||
		strings.Contains(m, "kimi-k2.6") ||
		strings.Contains(m, "kimi-k2.7") ||
		strings.Contains(m, "kimi-k2-6") ||
		strings.Contains(m, "kimi-k2-7") {
		return floatPtr(0.95)
	}
	return nil
}

// defaultTopK returns a model-ID-specific top_k default, or nil when the model
// has no known preference. Matches opencode's transform.ts.
func defaultTopK(modelID string) *float64 {
	m := strings.ToLower(modelID)
	if strings.Contains(m, "minimax-m2") {
		if strings.Contains(m, "m2.") || strings.Contains(m, "m25") || strings.Contains(m, "m21") {
			return floatPtr(40)
		}
		return floatPtr(20)
	}
	if strings.Contains(m, "gemini") {
		return floatPtr(64)
	}
	return nil
}

// samplingTunable reports whether the active client/model accepts temperature
// and top_p. Reasoning-family OpenAI models (o1/o3/o4/gpt-5*) and any
// thinking-enabled session must omit these fields or the API hard-rejects.
func (c *GenericClient) samplingTunable() bool {
	if c.ThinkingBudget > 0 {
		return false
	}
	if isReasoningOnlyModel(c.Model) {
		return false
	}
	return true
}

// isReasoningOnlyModel returns true for models that reject temperature/top_p.
// Kept narrow on purpose: only the OpenAI o-series and gpt-5* families are
// known-bad today. Extend as new families surface the same constraint.
func isReasoningOnlyModel(modelID string) bool {
	m := strings.ToLower(strings.TrimSpace(modelID))
	if m == "" {
		return false
	}
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if i := strings.LastIndex(m, ":"); i >= 0 {
		m = m[i+1:]
	}
	return strings.HasPrefix(m, "o1") ||
		strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "o4") ||
		strings.HasPrefix(m, "gpt-5") ||
		strings.Contains(m, "mimo")
}

// maybeStripMaxTokensForGateway removes max_tokens from an outgoing payload
// when the provider is cloudflare-gateway and the model is an o-series reasoning
// model. Cloudflare's gateway forwards to OpenAI, which rejects max_tokens for
// o-series; it accepts only max_completion_tokens (not yet supported here).
// floatPtr is a small helper that returns a pointer to a float64 value.
// Used to produce *float64 literals for model-ID-based defaults.
func floatPtr(v float64) *float64 { return &v }

func maybeStripMaxTokensForGateway(provider, model string, payload map[string]interface{}) {
	if provider != "cloudflare-gateway" {
		return
	}
	if isReasoningOnlyModel(model) {
		delete(payload, "max_tokens")
	}
}

func (c *GenericClient) GetProvider() string {
	return c.Provider
}

func (c *GenericClient) GetModel() string {
	return c.Model
}

func (c *GenericClient) isGoogleProvider() bool {
	return c.Provider == "google" || c.Provider == "google-vertex"
}

func (c *GenericClient) usesAnthropicMessagesAPI() bool {
	if c.Provider == "anthropic" {
		return true
	}
	// opencode-go routes per-model: minimax uses /v1/messages (Anthropic API);
	// everything else (deepseek, glm, kimi, mimo, qwen) uses
	// /v1/chat/completions (OpenAI).
	if c.Provider == "opencode" || c.Provider == "opencode-go" {
		return strings.HasPrefix(c.Model, "minimax-")
	}
	baseURL := strings.ToLower(strings.TrimRight(c.BaseURL, "/"))
	return strings.HasSuffix(baseURL, "/anthropic") || strings.Contains(baseURL, "/anthropic/")
}

func (c *GenericClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return c.ChatWithContext(context.Background(), messages, tools)
}

// ChatWithContext is like Chat but honours ctx cancellation so that in-flight
// HTTP requests are interrupted when the caller's context is cancelled (e.g.
// when the user presses Escape).
func (c *GenericClient) ChatWithContext(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	// Chokepoint safety net: scan all messages for known-format secrets
	if c.Redaction != nil && c.Redaction.Enabled && c.Redaction.Registry != nil {
		messages = c.applyRedactionSafetyNet(messages)
	}

	var lastErr error
	attempts := 0
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempts = attempt + 1
		var (
			msg *Message
			err error
		)
		if c.usesAnthropicMessagesAPI() {
			msg, err = c.chatAnthropic(ctx, messages, tools)
		} else if c.Provider == "copilot" {
			msg, err = c.chatCopilot(ctx, messages, tools)
		} else if c.isGoogleProvider() {
			msg, err = c.chatGoogle(ctx, messages, tools)
		} else {
			msg, err = c.chatOpenAI(ctx, messages, tools)
		}
		if err == nil {
			return msg, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			break
		}

		// Determine retry strategy for this error.
		is429 := isRateLimitError(err)
		isRetryable := is429 || isRetryableLLMClientError(err)
		if !isRetryable {
			break
		}

		maxRetries := llmMaxRetries
		if is429 {
			maxRetries = llmMaxRetries429
		}
		if attempt >= maxRetries {
			break
		}

		if is429 {
			// Linear backoff: 3s → 5s across 6 retries.
			// attempt=0 → 3.0s, 1 → 3.4s, … 5 → 5.0s.
			delay := 3*time.Second + time.Duration(attempt)*400*time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			if c.RetryNotifier != nil {
				c.RetryNotifier(attempt, maxRetries, delay, lastErr)
			}
			time.Sleep(delay)
		} else {
			delay := time.Duration(attempt+1) * llmRetryBaseDelay
			if c.RetryNotifier != nil {
				c.RetryNotifier(attempt, maxRetries, delay, lastErr)
			}
			time.Sleep(delay)
		}
	}
	return nil, fmt.Errorf("llm request failed after %d attempt(s): %w", attempts, lastErr)
}

// isRateLimitError returns true when err is an HTTP 429 (Too Many Requests)
// error originating from any of the provider chat methods. All of them format
// the error as "<provider> error (429): …", so we detect the status code in
// the formatted string.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, " (429)")
}

func isRetryableLLMClientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, ErrNoResponseFromOpenAIResponses) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "eof")
}

// chatCopilot exchanges the stored GitHub OAuth token (held in APIKey) for a short-lived
// Copilot API token, then calls the Copilot chat completions endpoint with the headers
// the service requires.
func (c *GenericClient) chatCopilot(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("copilot: no github token configured (run /connect → GitHub Copilot)")
	}
	tokenCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	apiToken, err := auth.CopilotExchangeAPIToken(tokenCtx, c.APIKey)
	if err != nil {
		return nil, fmt.Errorf("copilot token exchange: %w", err)
	}

	url := c.BaseURL + "/chat/completions"
	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
		"stream":   true,
	}
	c.applyGenerationParams(ctx, payload)
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Editor-Version", "vscode/1.95.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.35.0")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")
	req.Header.Set("Openai-Intent", "conversation-panel")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("copilot error (%d): %s", resp.StatusCode, string(body))
		emitDebug("error", msg)
		return nil, fmt.Errorf("%s", msg)
	}
	msg, usageRaw, err := parseOpenAIChatCompletionsStream(resp.Body, c.onDelta(), c.onUsage())
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("no response from copilot")
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	usage, err := usageForProvider("openai", usageRaw)
	if err != nil {
		return nil, err
	}
	msg.Usage = usage
	if usage != nil {
		msg.Spend = usage.Spend(msg.Model)
		usage.DebugLog(msg.Model)
	}
	return msg, nil
}

func (c *GenericClient) chatOpenAI(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.UseOAuth && c.Provider == "openai" {
		if plugin, ok := providerplugin.Get("openai"); ok && plugin.ModelAllowed(c.Model) {
			return c.chatOpenAIResponses(ctx, messages, tools)
		}
	}
	// Use WebSocket transport if enabled for OpenAI
	if c.UseWebSocket && SupportsWebSocket(c.Provider) {
		return c.chatOpenAIWebSocket(ctx, messages, tools)
	}
	url := c.BaseURL + "/chat/completions"

	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
		"stream":   true,
	}
	c.applyGenerationParams(ctx, payload)
	maybeStripMaxTokensForGateway(c.Provider, c.Model, payload)
	if providerSupportsReasoningEffort(c.Provider) && c.ThinkingBudget > 0 {
		payload["reasoning_effort"] = reasoningEffortForBudget(c.ThinkingBudget)
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	emitDebug("LLM", fmt.Sprintf("chatOpenAI: url=%s apiKey=%s model=%q", url, maskKey(c.APIKey), c.Model))

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("%s error (%d): %s", c.Provider, resp.StatusCode, string(body))
		emitDebug("ERROR", fmt.Sprintf("chatOpenAI: status=%d apiKey=%s url=%s", resp.StatusCode, maskKey(c.APIKey), url))
		return nil, fmt.Errorf("%s", msg)
	}

	msg, usageRaw, err := parseOpenAIChatCompletionsStream(resp.Body, c.onDelta(), c.onUsage())
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("no response from %s", c.Provider)
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	usage, err := usageForProvider(c.Provider, usageRaw)
	if err != nil {
		return nil, err
	}
	msg.Usage = usage
	if usage != nil {
		msg.Spend = usage.Spend(msg.Model)
		usage.DebugLog(msg.Model)
	}
	return msg, nil
}

// chatGoogle speaks the Gemini Interactions API (v1beta/interactions).
// Unlike the OpenAI-compatible endpoint, this API uses an SSE format with
// explicit event types (interaction.created, step.start, step.delta,
// step.stop, interaction.completed) and requires thought signatures to be
// preserved across turns for reasoning continuity.
func (c *GenericClient) chatGoogle(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	url := c.BaseURL
	if url == "" {
		url = "https://generativelanguage.googleapis.com/v1beta/interactions"
	} else {
		// Allow user-supplied base URL; strip trailing /openai if present
		// (the OpenAI-compatible endpoint prefix) and ensure we reach the
		// Interactions API at /v1beta/interactions.
		url = strings.TrimRight(url, "/")
		url = strings.TrimSuffix(url, "/openai")
		if !strings.HasSuffix(url, "/interactions") {
			// Avoid duplicating /v1beta if the base URL already includes it
			// (e.g. "https://generativelanguage.googleapis.com/v1beta" after
			// stripping the default OpenAI-compat suffix).
			if strings.HasSuffix(url, "/v1beta") {
				url += "/interactions"
			} else {
				url += "/v1beta/interactions"
			}
		}
	}

	// Convert ocode messages to Interactions API input steps.
	inputSteps, err := c.convertToGoogleSteps(messages)
	if err != nil {
		return nil, fmt.Errorf("google: convert steps: %w", err)
	}

	payload := map[string]interface{}{
		"model":  c.Model,
		"input":  inputSteps,
		"stream": true,
	}

	if len(tools) > 0 {
		payload["tools"] = googleTools(tools)
	}

	// Build generation_config
	genConfig := map[string]interface{}{}
	c.applyGenerationParams(ctx, genConfig)
	if c.ThinkingBudget > 0 {
		genConfig["thinking_level"] = googleThinkingLevel(c.ThinkingBudget)
	}
	if len(genConfig) > 0 {
		payload["generation_config"] = genConfig
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("x-goog-api-key", c.APIKey)
	}
	emitDebug("LLM", fmt.Sprintf("chatGoogle: url=%s model=%q", url, c.Model))

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("%s error (%d): %s", c.Provider, resp.StatusCode, string(body))
		emitDebug("ERROR", fmt.Sprintf("chatGoogle: status=%d url=%s", resp.StatusCode, url))
		return nil, fmt.Errorf("%s", msg)
	}

	msg, usageRaw, err := parseGoogleInteractionsStream(resp.Body, c.onDelta(), c.onUsage())
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("no response from %s", c.Provider)
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	usage, err := usageForProvider(c.Provider, usageRaw)
	if err != nil {
		return nil, err
	}
	msg.Usage = usage
	if usage != nil {
		msg.Spend = usage.Spend(msg.Model)
		usage.DebugLog(msg.Model)
	}
	return msg, nil
}

// convertToGoogleSteps converts ocode Message history into the Interactions API
// input Step array. Each ocode Message may map to one or more Steps:
//
//	assistant + ReasoningContent + Signature → thought step (with signature)
//	assistant + Content → model_output step
//	assistant + ToolCalls → function_call steps
//	user → user_input step(s)
//	tool → function_result step
func (c *GenericClient) convertToGoogleSteps(messages []Message) ([]map[string]interface{}, error) {
	var steps []map[string]interface{}

	for _, m := range messages {
		switch m.Role {
		case "system":
			// System instructions are handled as system_instruction in the request,
			// not as input steps. Skip them here.
			continue

		case "user":
			content := buildGoogleContent(m)
			steps = append(steps, map[string]interface{}{
				"type":    "user_input",
				"content": content,
			})

		case "assistant":
			// Emit thought step first if we have reasoning content and a signature.
			if m.ReasoningContent != "" {
				thoughtContent := []map[string]interface{}{
					{"type": "text", "text": m.ReasoningContent},
				}
				step := map[string]interface{}{
					"type":    "thought",
					"summary": thoughtContent,
				}
				if m.Signature != "" {
					step["signature"] = m.Signature
				}
				steps = append(steps, step)
			}

			// Emit model_output step for text content.
			if m.Content != "" {
				steps = append(steps, map[string]interface{}{
					"type": "model_output",
					"content": []map[string]interface{}{
						{"type": "text", "text": m.Content},
					},
				})
			}

			// Emit function_call steps for tool calls.
			for _, tc := range m.ToolCalls {
				var args interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					args = map[string]interface{}{}
				}
				step := map[string]interface{}{
					"type":      "function_call",
					"id":        tc.ID,
					"name":      tc.Function.Name,
					"arguments": args,
				}
				if tc.Signature != "" {
					step["signature"] = tc.Signature
				}
				steps = append(steps, step)
			}

		case "tool":
			// Gemini Interactions API expects result as content array.
			result := []map[string]interface{}{
				{"type": "text", "text": m.Content},
			}
			step := map[string]interface{}{
				"type":    "function_result",
				"call_id": m.ToolID,
				"result":  result,
			}
			// Include tool name if available (optional but helpful).
			for _, tc := range msgToolCalls(messages, m.ToolID) {
				step["name"] = tc.Function.Name
				break
			}
			steps = append(steps, step)
		}
	}

	return steps, nil
}

// msgToolCalls searches the messages slice for a tool call that matches the given
// tool ID, returning all matching tool calls (typically one). Used to look up
// the function name when constructing function_result steps.
func msgToolCalls(messages []Message, toolID string) []ToolCall {
	var calls []ToolCall
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.ID == toolID {
				calls = append(calls, tc)
			}
		}
	}
	return calls
}

// buildGoogleContent creates the content array for a user step, handling images.
func buildGoogleContent(m Message) []map[string]interface{} {
	var content []map[string]interface{}
	if m.Content != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": m.Content,
		})
	}
	for _, img := range m.Images {
		content = append(content, map[string]interface{}{
			"type":     "image",
			"data":     img.Data,
			"mimeType": img.MIMEType,
		})
	}
	return content
}

// googleStreamEvent is a raw SSE frame from the Interactions API.
type googleStreamEvent struct {
	Event string
	Data  json.RawMessage
}

// parseGoogleSSE reads the Interactions API SSE stream and returns a channel
// of typed events.
func parseGoogleSSE(body io.Reader, out chan<- googleStreamEvent) {
	defer close(out)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var currentEvent string
	var dataBuf strings.Builder
	done := false

	flushData := func() {
		if dataBuf.Len() > 0 {
			out <- googleStreamEvent{
				Event: currentEvent,
				Data:  json.RawMessage(dataBuf.String()),
			}
			dataBuf.Reset()
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			flushData()
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				flushData()
				done = true
				break
			}
			dataBuf.WriteString(data)
		} else if line == "" {
			flushData()
			currentEvent = ""
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("google SSE scanner error: %v", err)
	}
	flushData()
	_ = done
}

// stepAccum accumulates data for a single Interactions API step during streaming.
type stepAccum struct {
	typ         string
	text        strings.Builder
	thoughtText strings.Builder
	signature   string
	callID      string
	callName    string
	callArgs    strings.Builder
	isError     bool
}

// parseGoogleInteractionsStream parses the Gemini Interactions API SSE stream
// and assembles a single assistant Message from the steps.
func parseGoogleInteractionsStream(body io.Reader, onDelta func(kind, text string), onUsage func(inputTokens, outputTokens int64)) (*Message, json.RawMessage, error) {
	eventCh := make(chan googleStreamEvent, 64)
	go parseGoogleSSE(body, eventCh)

	msg := &Message{Role: "assistant"}

	// Per-step accumulators
	var currentStep *stepAccum
	var usageRaw json.RawMessage

	for evt := range eventCh {
		switch evt.Event {
		case "interaction.created":
			// Extract model name from the interaction object.
			var created struct {
				Interaction struct {
					Model string `json:"model"`
				} `json:"interaction"`
			}
			if json.Unmarshal(evt.Data, &created) == nil && created.Interaction.Model != "" {
				msg.Model = created.Interaction.Model
			}

		case "step.start":
			if currentStep != nil {
				finalizeStep(msg, currentStep, onDelta)
			}
			var start struct {
				Index int                    `json:"index"`
				Step  map[string]interface{} `json:"step"`
			}
			if err := json.Unmarshal(evt.Data, &start); err != nil {
				continue
			}
			stepType, _ := start.Step["type"].(string)
			currentStep = &stepAccum{typ: stepType}

			// Extract signature from thought or function_call steps.
			// Gemini attaches signatures to these step types for reasoning
			// continuity across multi-turn tool-calling interactions.
			if stepType == "thought" || stepType == "function_call" {
				if sig, ok := start.Step["signature"].(string); ok {
					currentStep.signature = sig
				}
			}
			// function_call steps carry the tool name and call id in the
			// step.start frame; arguments stream later via arguments_delta.
			// Without capturing these here the finalized tool call has an
			// empty name and the agent rejects it ("tool \"\" is not allowed").
			if stepType == "function_call" {
				if name, ok := start.Step["name"].(string); ok {
					currentStep.callName = name
				}
				if id, ok := start.Step["id"].(string); ok {
					currentStep.callID = id
				}
			}
			// thought steps may have initial summary content embedded.
			if stepType == "thought" {
				_ = start.Step["summary"] // processed via deltas
			}

		case "step.delta":
			if currentStep == nil {
				continue
			}
			// The delta payload has a "type" discriminator in a nested structure.
			// We need to extract the delta object and dispatch by its type field.
			var deltaFrame struct {
				Index int             `json:"index"`
				Delta json.RawMessage `json:"delta"`
			}
			if err := json.Unmarshal(evt.Data, &deltaFrame); err != nil {
				continue
			}

			// Determine delta type from the raw delta object.
			var deltaType struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(deltaFrame.Delta, &deltaType); err != nil {
				continue
			}

			switch deltaType.Type {
			case "text":
				var td struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(deltaFrame.Delta, &td) == nil {
					currentStep.text.WriteString(td.Text)
					if onDelta != nil {
						onDelta("text", td.Text)
					}
				}

			case "thought_summary":
				var tsd struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				if json.Unmarshal(deltaFrame.Delta, &tsd) == nil {
					currentStep.thoughtText.WriteString(tsd.Content.Text)
					if onDelta != nil {
						onDelta("reasoning", tsd.Content.Text)
					}
				}

			case "thought_signature":
				var tsig struct {
					Signature string `json:"signature"`
				}
				if json.Unmarshal(deltaFrame.Delta, &tsig) == nil {
					currentStep.signature = tsig.Signature
				}

			case "arguments_delta":
				var ad struct {
					Arguments string `json:"arguments"`
				}
				if json.Unmarshal(deltaFrame.Delta, &ad) == nil {
					currentStep.callArgs.WriteString(ad.Arguments)
					if onDelta != nil {
						onDelta("text", ad.Arguments) // emit tool arguments as text
					}
				}
			}

		case "step.stop":
			if currentStep != nil {
				finalizeStep(msg, currentStep, onDelta)
				currentStep = nil
			}

		case "interaction.completed":
			// Extract usage from the interaction object.
			var completed struct {
				Interaction struct {
					Usage json.RawMessage `json:"usage"`
				} `json:"interaction"`
			}
			if json.Unmarshal(evt.Data, &completed) == nil && len(completed.Interaction.Usage) > 0 {
				usageRaw = completed.Interaction.Usage
				if onUsage != nil {
					var u struct {
						InputTokens  int64 `json:"total_input_tokens"`
						OutputTokens int64 `json:"total_output_tokens"`
					}
					if err := json.Unmarshal(usageRaw, &u); err == nil {
						if u.InputTokens > 0 || u.OutputTokens > 0 {
							onUsage(u.InputTokens, u.OutputTokens)
						}
					}
				}
			}

		case "error":
			var errEvt struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Status  string `json:"status"`
				} `json:"error"`
			}
			if json.Unmarshal(evt.Data, &errEvt) == nil {
				return nil, nil, fmt.Errorf("google api error: %s (code=%d, status=%s)", errEvt.Error.Message, errEvt.Error.Code, errEvt.Error.Status)
			}
		}
	}

	// If no content and no tool calls, this is an empty heartbeat.
	if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 {
		return nil, nil, nil
	}
	return msg, usageRaw, nil
}

// finalizeStep accumulates a completed step's data into the result Message.
func finalizeStep(msg *Message, step *stepAccum, onDelta func(kind, text string)) {
	switch step.typ {
	case "thought":
		if step.thoughtText.Len() > 0 {
			if msg.ReasoningContent != "" {
				msg.ReasoningContent += "\n"
			}
			msg.ReasoningContent += step.thoughtText.String()
		}
		if step.signature != "" {
			msg.Signature = step.signature
		}

	case "model_output":
		msg.Content += step.text.String()

	case "function_call":
		args := step.callArgs.String()
		if args == "" {
			args = "{}"
		} else if !json.Valid([]byte(args)) {
			// Fragments didn't assemble; attempt to fix by wrapping.
			if !strings.HasPrefix(args, "{") {
				args = "{" + args
			}
			if !strings.HasSuffix(args, "}") {
				args += "}"
			}
			if !json.Valid([]byte(args)) {
				args = "{}"
			}
		}
		tc := ToolCall{
			ID:   step.callID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      step.callName,
				Arguments: args,
			},
		}
		if step.signature != "" {
			tc.Signature = step.signature
		}
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
}

// parseOpenAIChatCompletionsStream parses a Server-Sent Events stream in the
// OpenAI chat-completions format and returns the assembled assistant Message
// plus the raw usage payload (last `data:` line with non-empty usage). Used by
// both chatOpenAI and chatCopilot since Copilot speaks the same dialect.
//
// SSE chunk shape (abridged):
//
//	{"choices":[{"delta":{"role":"assistant","content":"hi","reasoning_content":"…",
//	  "tool_calls":[{"index":0,"id":"…","function":{"name":"…","arguments":"…"}}]}}],
//	 "model":"…","usage":{…}}
//
// Tool call arguments are streamed as partial JSON fragments, concatenated by
// the per-choice `tool_calls[i].index`.
func parseOpenAIChatCompletionsStream(body io.Reader, onDelta func(kind, text string), onUsage func(inputTokens, outputTokens int64)) (*Message, json.RawMessage, error) {
	msg := &Message{Role: "assistant"}
	toolByIdx := map[int]*ToolCall{}
	var toolOrder []int
	var usageRaw json.RawMessage
	thinkSplitter := inlineThinkingSplitter{}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					Role             string `json:"role"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" && msg.Model == "" {
			msg.Model = chunk.Model
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			usageRaw = chunk.Usage
			if onUsage != nil {
				var usage struct {
					PromptTokens     *int64 `json:"prompt_tokens"`
					CompletionTokens *int64 `json:"completion_tokens"`
				}
				if err := json.Unmarshal(chunk.Usage, &usage); err == nil {
					in, out := int64(0), int64(0)
					if usage.PromptTokens != nil {
						in = *usage.PromptTokens
					}
					if usage.CompletionTokens != nil {
						out = *usage.CompletionTokens
					}
					if in > 0 || out > 0 {
						onUsage(in, out)
					}
				}
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		hasExplicitReasoning := ch.Delta.ReasoningContent != "" || ch.Delta.Reasoning != ""
		if ch.Delta.Content != "" {
			if hasExplicitReasoning {
				msg.Content += ch.Delta.Content
				if onDelta != nil {
					onDelta("text", ch.Delta.Content)
				}
			} else {
				for _, part := range thinkSplitter.Feed(ch.Delta.Content) {
					if part.text == "" {
						continue
					}
					if part.kind == "reasoning" {
						msg.ReasoningContent += part.text
					} else {
						msg.Content += part.text
					}
					if onDelta != nil {
						onDelta(part.kind, part.text)
					}
				}
			}
		}
		// Some providers stream reasoning under `reasoning_content` (DeepSeek,
		// official OpenAI for o-series via chat/completions). Others use
		// `reasoning` (Grok 4 family). Accept both.
		reasoning := ch.Delta.ReasoningContent
		if reasoning == "" {
			reasoning = ch.Delta.Reasoning
		}
		if reasoning != "" {
			msg.ReasoningContent += reasoning
			if onDelta != nil {
				onDelta("reasoning", reasoning)
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			existing, ok := toolByIdx[tc.Index]
			if !ok {
				existing = &ToolCall{Type: "function"}
				toolByIdx[tc.Index] = existing
				toolOrder = append(toolOrder, tc.Index)
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				existing.Function.Arguments += tc.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, nil, fmt.Errorf("openai stream error: SSE line exceeded 16MB buffer (likely huge reasoning block): %w", err)
		}
		return nil, nil, fmt.Errorf("openai stream error: %w", err)
	}
	for _, part := range thinkSplitter.Flush() {
		if part.text == "" {
			continue
		}
		if part.kind == "reasoning" {
			msg.ReasoningContent += part.text
		} else {
			msg.Content += part.text
		}
		if onDelta != nil {
			onDelta(part.kind, part.text)
		}
	}
	sort.Ints(toolOrder)
	for _, idx := range toolOrder {
		tc := *toolByIdx[idx]
		if tc.Function.Arguments != "" && !json.Valid([]byte(tc.Function.Arguments)) {
			emitDebug("AGENT", fmt.Sprintf("openai: invalid tool arguments for %s (id=%s, %d bytes); falling back to {}", tc.Function.Name, tc.ID, len(tc.Function.Arguments)))
			tc.Function.Arguments = "{}"
		}
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
	// Skip empty keep-alive / heartbeat chunks: some providers send a `data:`
	// frame with no content, reasoning, or tool calls just to keep the SSE
	// connection alive. Returning (nil, nil, nil) lets the caller treat this
	// as "no response" without surfacing it as an error.
	if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 {
		return nil, nil, nil
	}
	return msg, usageRaw, nil
}

type inlineDeltaPart struct {
	kind string
	text string
}

type inlineThinkingSplitter struct {
	inThinking bool
	carry      string
}

var (
	inlineThinkOpenTags  = []string{"<thinking>", "<think>", "<thought>"}
	inlineThinkCloseTags = []string{"</thinking>", "</think>", "</thought>"}
)

func (s *inlineThinkingSplitter) Feed(fragment string) []inlineDeltaPart {
	data := s.carry + fragment
	s.carry = ""
	if data == "" {
		return nil
	}
	parts := make([]inlineDeltaPart, 0, 4)
	for len(data) > 0 {
		if s.inThinking {
			idx, tagLen := firstTagIndex(data, inlineThinkCloseTags)
			if idx >= 0 {
				if idx > 0 {
					parts = append(parts, inlineDeltaPart{kind: "reasoning", text: data[:idx]})
				}
				data = data[idx+tagLen:]
				s.inThinking = false
				continue
			}
			safeLen := len(data) - (len("</thinking>") - 1)
			if safeLen > 0 {
				parts = append(parts, inlineDeltaPart{kind: "reasoning", text: data[:safeLen]})
				s.carry = data[safeLen:]
				break
			}
			s.carry = data
			break
		}

		idx, tagLen := firstTagIndex(data, inlineThinkOpenTags)
		if idx >= 0 {
			if idx > 0 {
				parts = append(parts, inlineDeltaPart{kind: "text", text: data[:idx]})
			}
			data = data[idx+tagLen:]
			s.inThinking = true
			continue
		}
		safeLen := len(data) - (len("<thinking>") - 1)
		if safeLen > 0 {
			parts = append(parts, inlineDeltaPart{kind: "text", text: data[:safeLen]})
			s.carry = data[safeLen:]
			break
		}
		s.carry = data
		break
	}
	return parts
}

func (s *inlineThinkingSplitter) Flush() []inlineDeltaPart {
	if s.carry == "" {
		return nil
	}
	kind := "text"
	if s.inThinking {
		kind = "reasoning"
	}
	out := []inlineDeltaPart{{kind: kind, text: s.carry}}
	s.carry = ""
	return out
}

func firstTagIndex(s string, tags []string) (idx int, tagLen int) {
	best := -1
	bestLen := 0
	for _, tag := range tags {
		i := strings.Index(s, tag)
		if i < 0 {
			continue
		}
		if best == -1 || i < best || (i == best && len(tag) > bestLen) {
			best = i
			bestLen = len(tag)
		}
	}
	if best < 0 {
		return -1, 0
	}
	return best, bestLen
}

// hasToolResultBlock reports whether any block in the content slice is a tool_result.
// Used to prevent merging tool_result user messages with text user messages, which
// strict Anthropic-compatible providers (e.g. Minimax) reject.
func hasToolResultBlock(content []interface{}) bool {
	for _, b := range content {
		bm, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if btype, _ := bm["type"].(string); btype == "tool_result" {
			return true
		}
	}
	return false
}

// googleTools converts the ocode generic tool descriptors into the Gemini
// Interactions API tool format. Each tool gets flattened: name/description/
// parameters (or input_schema) are hoisted from the nested function object.
func googleTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if t["type"] == "function" {
			// Flatten "function" wrapper: extract inner fields.
			if fn, ok := t["function"].(map[string]interface{}); ok {
				gt := map[string]interface{}{
					"type": "function",
					"name": fn["name"],
				}
				if desc, ok := fn["description"]; ok {
					gt["description"] = desc
				}
				if params, ok := fn["parameters"]; ok {
					gt["parameters"] = params
				} else if schema, ok := fn["input_schema"]; ok {
					gt["parameters"] = schema
				}
				out = append(out, gt)
				continue
			}
		}
		// Already flat (name/description at top level).
		gt := map[string]interface{}{
			"type": "function",
			"name": t["name"],
		}
		if desc, ok := t["description"]; ok {
			gt["description"] = desc
		}
		if params, ok := t["parameters"]; ok {
			gt["parameters"] = params
		} else if schema, ok := t["input_schema"]; ok {
			gt["parameters"] = schema
		}
		out = append(out, gt)
	}
	return out
}

func openAITools(tools []map[string]interface{}) []map[string]interface{} {
	openAITools := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if t["type"] == "function" {
			openAITools = append(openAITools, t)
			continue
		}
		openAITools = append(openAITools, map[string]interface{}{
			"type":     "function",
			"function": t,
		})
	}
	return openAITools
}

// requiresJSONObjectArguments returns true for providers whose OpenAI-compatible
// API rejects tool call arguments as a JSON-encoded string and requires them as
// a parsed JSON object instead (e.g. Alibaba DashScope).
func requiresJSONObjectArguments(provider string) bool {
	return provider == "alibaba" ||
		provider == "alibaba-coding"
}

// isGLMModel reports whether the model is a Zhipu/Z.AI GLM model. GLM's
// OpenAI-compatible endpoint is stricter than OpenAI: it emits reasoning_content
// in responses but rejects it as a request field, and it rejects an empty-string
// content on an assistant message that carries tool_calls (error 1214,
// "messages parameter is illegal"). Detected by model name since GLM is served
// through several providers (openrouter, z-ai, ...).
func isGLMModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "glm")
}

// providerSupportsReasoningEffort returns true for providers that accept the
// OpenAI-style reasoning_effort parameter to enable/control chain-of-thought.
func providerSupportsReasoningEffort(provider string) bool {
	return provider == "openai" ||
		provider == "openrouter" ||
		provider == "google" ||
		strings.HasPrefix(provider, "xiaomi")
}

// googleThinkingLevel maps ThinkingBudget to Gemini's thinking_level values.
// Maps to the same budget thresholds as reasoningEffortForBudget for consistency.
func googleThinkingLevel(budget int) string {
	switch {
	case budget >= 16000:
		return "high"
	case budget >= 8000:
		return "medium"
	default:
		return "low"
	}
}

func reasoningEffortForBudget(budget int) string {
	switch {
	case budget >= 16000:
		return "high"
	case budget >= 8000:
		return "medium"
	default:
		return "low"
	}
}

// hoistSystemMessages moves any system messages that appear after a non-system
// message to the front of the list, preserving their relative order. Some
// OpenAI-compatible providers (DeepSeek, chutes-routed DeepSeek) reject
// requests where a system message appears mid-conversation; the compaction
// summary is inserted as a system message after the first user turn, which
// trips that check. Leading system messages are left untouched.
func hoistSystemMessages(messages []Message) []Message {
	sawNonSystem := false
	needsHoist := false
	for _, m := range messages {
		if m.Role != "system" {
			sawNonSystem = true
			continue
		}
		if sawNonSystem {
			needsHoist = true
			break
		}
	}
	if !needsHoist {
		return messages
	}
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			out = append(out, m)
		}
	}
	for _, m := range messages {
		if m.Role != "system" {
			out = append(out, m)
		}
	}
	return out
}

// mergeLeadingSystemMessages merges consecutive system messages at the front
// of the messages slice into a single system message. Some OpenAI-compatible
// providers (Chutes-routed DeepSeek in particular) reject multiple system
// messages or require exactly one system message at position 0. Merging them
// eliminates that ambiguity.
func mergeLeadingSystemMessages(messages []Message) []Message {
	if len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}
	// Count leading system messages.
	end := 0
	for end < len(messages) && messages[end].Role == "system" {
		end++
	}
	if end <= 1 {
		return messages // zero or one system message — nothing to merge
	}
	// Merge their content.
	var b strings.Builder
	for i := 0; i < end; i++ {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(messages[i].Content)
	}
	out := make([]Message, 0, len(messages)-end+1)
	out = append(out, Message{Role: "system", Content: b.String()})
	out = append(out, messages[end:]...)
	return out
}

// collectAndRemoveSystemMessages extracts all system-role messages from the
// slice, merges them into a single string (separated by blank lines), and
// returns the merged text plus the slice with all system messages removed.
// Useful for providers like Anthropic that pass system as a top-level field.
func collectAndRemoveSystemMessages(messages []Message) (string, []Message) {
	var parts []string
	var rest []Message
	for _, m := range messages {
		if m.Role == "system" {
			parts = append(parts, m.Content)
		} else {
			rest = append(rest, m)
		}
	}
	if len(parts) == 0 {
		return "", messages
	}
	return strings.Join(parts, "\n\n"), rest
}

// repairToolCallSequence rewrites message history into a provider-valid
// assistant(tool_calls) -> tool* sequence.
//
// OpenAI-compatible providers (DeepSeek in particular) are strict about two
// invariants:
//   - every assistant tool_call must receive a matching role=tool result
//   - every role=tool message must appear immediately after the assistant that
//     requested it
//
// Real sessions can violate that when a tool/sub-agent is interrupted, when an
// out-of-band system notification is injected between the assistant and the
// eventual tool result, or when a session is persisted mid-turn. Rather than
// sending invalid history, we pull matching tool results back next to their
// assistant, synthesise stubs for missing results, and downgrade any remaining
// stray tool messages into system notes.
func repairToolCallSequence(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}

	consumed := make([]bool, len(messages))
	out := make([]Message, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		if consumed[i] {
			continue
		}
		m := messages[i]
		consumed[i] = true

		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			if m.Role == "tool" {
				emitDebug("AGENT", fmt.Sprintf("repair: downgrading stray tool result for tool_call_id=%s", m.ToolID))
				out = append(out, Message{
					Role:    "system",
					Content: fmt.Sprintf("[stray tool result for tool_call_id=%s was recorded out of sequence and downgraded to a note; the assistant turn referencing this call received a synthesised 'interrupted' stub — this note holds the real output]\n\n%s", m.ToolID, m.Content),
				})
				continue
			}
			out = append(out, m)
			continue
		}

		out = append(out, m)

		pending := make(map[string]ToolCall, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				pending[tc.ID] = tc
			}
		}

		var matched []Message
		for j := i + 1; j < len(messages); j++ {
			if consumed[j] {
				continue
			}
			next := messages[j]
			if next.Role == "assistant" || next.Role == "user" {
				break
			}
			if next.Role != "tool" {
				continue
			}
			if _, ok := pending[next.ToolID]; !ok {
				continue
			}
			matched = append(matched, next)
			consumed[j] = true
			delete(pending, next.ToolID)
		}

		out = append(out, matched...)

		for _, tc := range m.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if _, ok := pending[tc.ID]; !ok {
				continue
			}
			emitDebug("AGENT", fmt.Sprintf("repair: synthesising missing tool result for tool_call_id=%s (%s)", tc.ID, tc.Function.Name))
			out = append(out, Message{
				Role:    "tool",
				ToolID:  tc.ID,
				Content: fmt.Sprintf("[tool execution interrupted: no result was recorded inline for tool_call_id=%s; treat this call as failed. If a later system note references this id, that note holds the real output captured out-of-sequence.]", tc.ID),
			})
		}
	}

	return out
}

// sanitizeAPIText removes null bytes and control characters from text that
// some API JSON parsers reject even when Go's json.Marshal escapes them as
// \\uXXXX. Keeps tab (0x09), newline (0x0A), and carriage return (0x0D).
func sanitizeAPIText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0: // null byte
			continue
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r': // control chars
			continue
		case r == '\uFEFF': // BOM
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (c *GenericClient) convertToOpenAIMessages(messages []Message) ([]map[string]interface{}, error) {
	messages = repairToolCallSequence(messages)
	messages = hoistSystemMessages(messages)
	messages = mergeLeadingSystemMessages(messages)
	var result []map[string]interface{}
	// OpenAI tool-role messages are string-only and must stay contiguous under
	// the assistant that made the tool calls, so image bytes cannot ride a tool
	// result. Buffer any images returned by tool calls and flush them as a
	// single user message right before the next non-tool message (typically the
	// assistant turn) — a user message after a run of tool results is valid.
	var pendingImageBlocks []map[string]interface{}
	vision := c.supportsVision()
	flushPendingImages := func() {
		if len(pendingImageBlocks) == 0 {
			return
		}
		content := make([]map[string]interface{}, 0, len(pendingImageBlocks)+1)
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": "Image(s) returned by the preceding read tool call(s):",
		})
		content = append(content, pendingImageBlocks...)
		result = append(result, map[string]interface{}{"role": "user", "content": content})
		pendingImageBlocks = nil
	}
	for _, m := range messages {
		if m.Role != "tool" {
			flushPendingImages()
		}
		if m.Role == "tool" {
			result = append(result, map[string]interface{}{
				"role":         "tool",
				"content":      sanitizeAPIText(m.Content),
				"tool_call_id": m.ToolID,
			})
			if vision {
				for _, img := range m.Images {
					pendingImageBlocks = append(pendingImageBlocks, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:" + img.MIMEType + ";base64," + img.Data,
						},
					})
				}
			}
			continue
		}

		if m.Role == "user" && (len(m.Images) > 0 || strings.Contains(m.Content, "@")) {
			content, err := c.buildOpenAIContentWithImages(m)
			if err != nil {
				return nil, err
			}
			if content != nil {
				result = append(result, map[string]interface{}{
					"role":    m.Role,
					"content": content,
				})
				continue
			}
		}

		glm := isGLMModel(c.Model)
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": sanitizeAPIText(m.Content),
		}
		// GLM rejects an empty-string content alongside tool_calls; omit it so the
		// field defaults to null on its side (error 1214 otherwise).
		if glm && m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) > 0 {
			delete(msg, "content")
		}
		// GLM emits reasoning_content but rejects it as a request field; only echo
		// it back to providers that accept it.
		if !glm && m.Role == "assistant" && m.ReasoningContent != "" {
			msg["reasoning_content"] = sanitizeAPIText(m.ReasoningContent)
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				var argsVal interface{} = tc.Function.Arguments
				if requiresJSONObjectArguments(c.Provider) {
					var parsed interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
						argsVal = parsed
					} else {
						argsVal = map[string]interface{}{}
					}
				}
				calls = append(calls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": argsVal,
					},
				})
			}
			msg["tool_calls"] = calls
		}
		result = append(result, msg)
	}
	// GLM rejects a request whose message sequence ends on an assistant message
	// (it has nothing to respond to → error 1214). This happens when the history
	// Flush images from a trailing run of tool results (conversation ends on the
	// tool turn — e.g. building the request right after a read).
	flushPendingImages()

	// ends on an assistant turn (resumed/interrupted session, or assistant text
	// with no tool calls). Append a synthetic user turn — the same thing a manual
	// "continue" does. Other providers tolerate a trailing assistant message.
	if isGLMModel(c.Model) && len(result) > 0 {
		if last, ok := result[len(result)-1]["role"].(string); ok && last == "assistant" {
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": "continue",
			})
		}
	}
	return result, nil
}

// imageStubForTextModel is the text placeholder substituted for an image when
// the active model has no vision support, mirroring the read tool's text-only
// degradation. A text-only provider (e.g. DeepSeek) 400s on an image_url block,
// so we never emit the base64 — the model instead learns an image was attached
// and how to view it.
func imageStubForTextModel(path string) string {
	if path != "" {
		return fmt.Sprintf("[image omitted: %s — active model has no vision support; switch to a vision-capable model or OCR the image first]", path)
	}
	return "[image omitted — active model has no vision support; switch to a vision-capable model or OCR the image first]"
}

func (c *GenericClient) buildOpenAIContentWithImages(m Message) ([]map[string]interface{}, error) {
	vision := c.supportsVision()
	var content []map[string]interface{}
	if len(m.Images) > 0 {
		if m.Content != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": sanitizeAPIText(m.Content),
			})
		}
		for _, img := range m.Images {
			if !vision {
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": imageStubForTextModel(img.Path),
				})
				continue
			}
			content = append(content, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "data:" + img.MIMEType + ";base64," + img.Data,
				},
			})
		}
		return content, nil
	}

	text := m.Content
	parts := strings.Fields(text)
	hasImage := false
	var textParts []string

	for _, part := range parts {
		if strings.HasPrefix(part, "@") {
			filePath := strings.TrimPrefix(part, "@")
			if IsImageFile(filePath) {
				if !vision {
					// Text-only model: never load/embed base64. Replace the
					// @path token with a stub and force the rebuilt-content path
					// (hasImage) so the stub is emitted instead of falling
					// through to plain text that would send the raw @path.
					textParts = append(textParts, imageStubForTextModel(filePath))
					hasImage = true
					continue
				}
				img, err := NewImageWithMaxDim(filePath, c.MaxImageDim)
				if err != nil {
					textParts = append(textParts, part)
					continue
				}
				if !hasImage {
					if len(textParts) > 0 {
						content = append(content, map[string]interface{}{
							"type": "text",
							"text": strings.Join(textParts, " "),
						})
						textParts = nil
					}
					hasImage = true
				}
				content = append(content, map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": "data:" + img.MIMEType + ";base64," + img.Data,
					},
				})
				continue
			}
		}
		textParts = append(textParts, part)
	}

	if len(textParts) > 0 {
		if hasImage {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": strings.Join(textParts, " "),
			})
		} else {
			return nil, nil
		}
	}

	if !hasImage {
		return nil, nil
	}

	return content, nil
}

// chatOpenAIResponses calls the OpenAI Responses API using a ChatGPT OAuth token.
// ChatGPT OAuth tokens use the Codex backend; API keys use api.openai.com.
// The chatgpt_account_id claim is extracted from the JWT and sent as ChatGPT-Account-ID.
func (c *GenericClient) chatOpenAIResponses(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	accountID := c.AccountID
	// AccountID is a cached copy of the chatgpt_account_id JWT claim. When the
	// credential was imported from an external store (e.g. another CLI) the
	// cache can be empty; recover it from the bearer token so the codex backend
	// still receives ChatGPT-Account-ID. Matches the two-level lookup in
	// auth.extractOpenAIAccountID.
	if accountID == "" {
		if id := jwtClaim(c.APIKey, "https://api.openai.com/auth", "chatgpt_account_id"); id != "" {
			accountID = id
		} else if id := jwtClaim(c.APIKey, "chatgpt_account_id"); id != "" {
			accountID = id
		}
	}

	// Map messages → Responses API input items.
	instructions := make([]string, 0, 1)
	input := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			instructions = append(instructions, m.Content)
			continue
		}
		if m.Role == "tool" {
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": m.ToolID,
				"output":  m.Content,
			})
			continue
		}
		content := interface{}(m.Content)
		if m.Role == "user" && len(m.Images) > 0 {
			parts := make([]map[string]interface{}, 0, len(m.Images)+1)
			if m.Content != "" {
				parts = append(parts, map[string]interface{}{
					"type": "input_text",
					"text": m.Content,
				})
			}
			vision := c.supportsVision()
			for _, img := range m.Images {
				if !vision {
					parts = append(parts, map[string]interface{}{
						"type": "input_text",
						"text": imageStubForTextModel(img.Path),
					})
					continue
				}
				parts = append(parts, map[string]interface{}{
					"type":      "input_image",
					"image_url": "data:" + img.MIMEType + ";base64," + img.Data,
				})
			}
			content = parts
		}
		// Skip role items with no content; assistant tool-call-only turns are
		// emitted below as stored Responses output items or function_call items.
		if m.Content != "" || (m.Role == "user" && len(m.Images) > 0) {
			input = append(input, map[string]interface{}{"type": "message", "role": m.Role, "content": content})
		}
		if m.Role == "assistant" {
			if len(m.OpenAIResponseItems) > 0 {
				input = append(input, m.OpenAIResponseItems...)
			}
			presentCallIDs := openAIResponseFunctionCallIDs(m.OpenAIResponseItems)
			for _, tc := range m.ToolCalls {
				if _, ok := presentCallIDs[tc.ID]; ok {
					continue
				}
				input = append(input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
		}
	}
	input = dedupeOpenAIResponseInputItems(input)

	// Ensure every function_call has a matching function_call_output.
	// OpenAI Responses API returns 400 if a call_id has no output.
	outputIDs := make(map[string]bool)
	for _, item := range input {
		if item["type"] == "function_call_output" {
			if id, ok := item["call_id"].(string); ok {
				outputIDs[id] = true
			}
		}
	}
	for _, item := range input {
		if item["type"] == "function_call" {
			if id, ok := item["call_id"].(string); ok && !outputIDs[id] {
				toolName := ""
				if name, ok := item["name"].(string); ok {
					toolName = name
				}
				emitDebug("API", fmt.Sprintf("auto-filling missing output for call %s (%s)", id, toolName))
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  "error: tool result missing",
				})
				outputIDs[id] = true
			}
		}
	}

	payload := map[string]interface{}{
		"model":        normalizeOpenAICodexModel(c.Model),
		"instructions": strings.Join(instructions, "\n\n"),
		"input":        input,
		"store":        false,
		"stream":       true,
		"include":      []string{"reasoning.encrypted_content"},
		"text":         map[string]interface{}{"verbosity": "medium"},
	}
	c.applyGenerationParams(ctx, payload)
	if c.ThinkingBudget > 0 {
		payload["reasoning"] = map[string]interface{}{
			"effort":  reasoningEffortForBudget(c.ThinkingBudget),
			"summary": "auto",
		}
	}

	if len(tools) > 0 {
		respTools := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fn := t
			if t["type"] == "function" {
				if f, ok := t["function"].(map[string]interface{}); ok {
					fn = f
				}
			}
			respTools = append(respTools, map[string]interface{}{
				"type":        "function",
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
		}
		payload["tools"] = respTools
	}

	// Plugin params
	if plugin, ok := providerplugin.Get("openai"); ok && c.UseOAuth {
		for k, v := range plugin.RequestParams(providerplugin.RequestContext{}) {
			if v == nil {
				delete(payload, k)
			} else {
				payload[k] = v
			}
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := c.openAIResponsesURL()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}
	// Plugin headers
	if plugin, ok := providerplugin.Get("openai"); ok && c.UseOAuth {
		pluginHeaders := plugin.RequestHeaders(providerplugin.RequestContext{
			Provider:  c.Provider,
			Model:     c.Model,
			SessionID: os.Getenv("OPENCODE_SESSION_ID"),
		})
		for k, vs := range pluginHeaders {
			req.Header[k] = vs
		}
	}

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized && c.UseOAuth {
			return nil, fmt.Errorf("ChatGPT session expired — run /connect to re-authenticate")
		}
		msg := fmt.Sprintf("openai responses error (%d): %s", resp.StatusCode, string(body))
		emitDebug("error", msg)
		return nil, fmt.Errorf("%s", msg)
	}

	// Parse SSE stream to accumulate the full response.
	var fullText string
	var reasoningText string
	var resultModel string
	var lastEvent string
	var toolCalls []ToolCall
	var responseItems []map[string]interface{}
	var responseUsage json.RawMessage
	onDelta := c.onDelta()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			lastEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			if line == "" {
				lastEvent = ""
			}
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var payload struct {
			Type     string                 `json:"type"`
			Model    string                 `json:"model"`
			Delta    string                 `json:"delta"`
			Text     string                 `json:"text"`
			Item     map[string]interface{} `json:"item"`
			Response map[string]interface{} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		eventType := payload.Type
		if eventType == "" {
			eventType = lastEvent
		}
		if payload.Model != "" {
			resultModel = payload.Model
		}
		switch eventType {
		case "response.output_text.delta":
			fullText += payload.Delta
			if onDelta != nil && payload.Delta != "" {
				onDelta("text", payload.Delta)
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			reasoningText += payload.Delta
			if onDelta != nil && payload.Delta != "" {
				onDelta("reasoning", payload.Delta)
			}
		case "response.reasoning_summary_text.done", "response.reasoning_text.done":
			if reasoningText == "" {
				reasoningText = payload.Text
			}
		case "response.output_text.done":
			// done carries the full part text; only use it when no deltas
			// streamed, otherwise the body would be appended twice.
			if fullText == "" {
				fullText = payload.Text
			}
		case "response.content_part.done":
			if fullText == "" {
				fullText = payload.Text
			}
		case "response.output_item.done":
			itemType, _ := payload.Item["type"].(string)
			if itemText := openAIResponseItemText(payload.Item); itemText != "" && fullText == "" {
				fullText = itemText
			}
			if itemType == "reasoning" || itemType == "function_call" {
				responseItems = append(responseItems, payload.Item)
			}
			if itemType == "function_call" {
				id, _ := payload.Item["call_id"].(string)
				if id == "" {
					id, _ = payload.Item["id"].(string)
				}
				name, _ := payload.Item["name"].(string)
				arguments, _ := payload.Item["arguments"].(string)
				toolCalls = append(toolCalls, ToolCall{
					ID:   id,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      name,
						Arguments: arguments,
					},
				})
			}
		case "response.completed", "response.done":
			if payload.Model != "" {
				resultModel = payload.Model
			}
			if payload.Response != nil {
				if model, _ := payload.Response["model"].(string); model != "" {
					resultModel = model
				}
				if text := openAIResponseText(payload.Response); text != "" && fullText == "" {
					fullText = text
				}
				if usageRaw, err := json.Marshal(payload.Response["usage"]); err == nil && string(usageRaw) != "null" {
					responseUsage = usageRaw
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("openai responses stream error: SSE line exceeded 16MB buffer (likely huge reasoning block): %w", err)
		}
		return nil, fmt.Errorf("openai responses stream error: %w", err)
	}

	if fullText == "" && len(toolCalls) == 0 {
		return nil, ErrNoResponseFromOpenAIResponses
	}

	msg := &Message{
		Role:                "assistant",
		Content:             fullText,
		ReasoningContent:    reasoningText,
		Model:               resultModel,
		ToolCalls:           toolCalls,
		OpenAIResponseItems: responseItems,
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	if len(responseUsage) > 0 {
		usage, err := parseOpenAIResponsesUsage(responseUsage)
		if err != nil {
			emitDebug("error", fmt.Sprintf("parse openai responses usage: %v", err))
		} else if usage != nil {
			msg.Usage = usage
			msg.Spend = usage.Spend(msg.Model)
			usage.DebugLog(msg.Model)
		}
	}
	return msg, nil
}

func openAIResponseFunctionCallIDs(items []map[string]interface{}) map[string]struct{} {
	callIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		if itemType, _ := item["type"].(string); itemType != "function_call" {
			continue
		}
		if callID, _ := item["call_id"].(string); callID != "" {
			callIDs[callID] = struct{}{}
		}
	}
	return callIDs
}

func dedupeOpenAIResponseInputItems(input []map[string]interface{}) []map[string]interface{} {
	seenIDs := make(map[string]struct{})
	seenCallIDs := make(map[string]struct{})
	out := make([]map[string]interface{}, 0, len(input))
	for _, item := range input {
		itemType, _ := item["type"].(string)
		if id, _ := item["id"].(string); id != "" {
			if _, ok := seenIDs[id]; ok {
				emitDebug("API", fmt.Sprintf("dropping duplicate responses input item id=%s type=%s", id, itemType))
				continue
			}
			seenIDs[id] = struct{}{}
		}
		if itemType == "function_call" || itemType == "function_call_output" {
			if callID, _ := item["call_id"].(string); callID != "" {
				key := itemType + ":" + callID
				if _, ok := seenCallIDs[key]; ok {
					emitDebug("API", fmt.Sprintf("dropping duplicate responses input %s call_id=%s", itemType, callID))
					continue
				}
				seenCallIDs[key] = struct{}{}
			}
		}
		out = append(out, item)
	}
	return out
}

func (c *GenericClient) openAIResponsesURL() string {
	if c.UseOAuth && c.Provider == "openai" {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return strings.TrimRight(c.BaseURL, "/") + "/responses"
}

// normalizeOpenAICodexModel mirrors the Codex/opencode OAuth bridge behavior for
// legacy model aliases while preserving unknown/new model IDs. The ChatGPT Codex
// backend currently expects newer canonical IDs for the old GPT-5 aliases, but
// users may still select future IDs from models.dev; those pass through.
func normalizeOpenAICodexModel(model string) string {
	switch model {
	case "gpt-5", "gpt-5-mini", "gpt-5-nano":
		return "gpt-5.1"
	case "gpt-5-codex":
		return "gpt-5.1-codex"
	case "codex-mini-latest", "gpt-5-codex-mini", "gpt-5-codex-mini-medium", "gpt-5-codex-mini-high":
		return "gpt-5.1-codex-mini"
	}
	for _, suffix := range []string{"-none", "-low", "-medium", "-high", "-xhigh"} {
		if strings.HasPrefix(model, "gpt-5.1") && strings.HasSuffix(model, suffix) {
			return strings.TrimSuffix(model, suffix)
		}
		if strings.HasPrefix(model, "gpt-5.2") && strings.HasSuffix(model, suffix) {
			return strings.TrimSuffix(model, suffix)
		}
	}
	return model
}

func openAIResponseText(response map[string]interface{}) string {
	if text, _ := response["output_text"].(string); text != "" {
		return text
	}
	if output, ok := response["output"].([]interface{}); ok {
		var b strings.Builder
		for _, raw := range output {
			item, _ := raw.(map[string]interface{})
			if item == nil {
				continue
			}
			b.WriteString(openAIResponseItemText(item))
		}
		return b.String()
	}
	return ""
}

func openAIResponseItemText(item map[string]interface{}) string {
	if item == nil {
		return ""
	}
	if text, _ := item["text"].(string); text != "" {
		return text
	}
	if content, ok := item["content"].([]interface{}); ok {
		var b strings.Builder
		for _, raw := range content {
			part, _ := raw.(map[string]interface{})
			if part == nil {
				continue
			}
			partType, _ := part["type"].(string)
			if partType == "output_text" || partType == "text" || partType == "input_text" || partType == "" {
				if text, _ := part["text"].(string); text != "" {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}

// jwtClaim extracts a nested string field from a JWT payload without verifying the signature.
// path is a chain of keys: jwtClaim(token, "https://api.openai.com/auth", "chatgpt_account_id")
func jwtClaim(token string, keys ...string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	padded := parts[1]
	switch len(padded) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	data, err := base64.URLEncoding.DecodeString(padded)
	if err != nil {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	var cur interface{} = payload
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func (c *GenericClient) chatAnthropic(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	url := c.BaseURL + "/messages"

	messages = repairToolCallSequence(messages)

	system, messages := collectAndRemoveSystemMessages(messages)

	anthropicMsgs, err := c.buildAnthropicMessages(messages)
	if err != nil {
		return nil, err
	}

	// Build system payload with cache_control for prompt caching
	var systemPayload interface{}
	if system != "" {
		systemPayload = []interface{}{
			map[string]interface{}{
				"type":          "text",
				"text":          system,
				"cache_control": map[string]interface{}{"type": "ephemeral"},
			},
		}
	}

	// Add cache_control to first user message content for prompt caching
	for i := range anthropicMsgs {
		if anthropicMsgs[i]["role"] == "user" {
			if content, ok := anthropicMsgs[i]["content"].([]interface{}); ok && len(content) > 0 {
				if last, ok := content[len(content)-1].(map[string]interface{}); ok {
					last["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			}
			break
		}
	}

	maxTokens := 4096
	if c.ThinkingBudget > 0 {
		// thinking budget counts against max_tokens; ensure room for response
		maxTokens = c.ThinkingBudget + 4096
	}

	payload := map[string]interface{}{
		"model":      c.Model,
		"system":     systemPayload,
		"messages":   anthropicMsgs,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	// applyGenerationParams self-skips when ThinkingBudget>0 — Anthropic's
	// Messages API rejects temperature/top_p alongside extended thinking.
	c.applyGenerationParams(ctx, payload)

	if c.ThinkingBudget > 0 {
		payload["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": c.ThinkingBudget,
		}
	}

	if len(tools) > 0 {
		var anthropicTools []map[string]interface{}
		for _, t := range tools {
			anthropicTools = append(anthropicTools, map[string]interface{}{
				"name":         t["name"],
				"description":  t["description"],
				"input_schema": t["parameters"],
			})
		}
		// Add cache_control to last tool for prompt caching
		if len(anthropicTools) > 0 {
			anthropicTools[len(anthropicTools)-1]["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
		payload["tools"] = anthropicTools
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if c.UseOAuth {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if c.ThinkingBudget > 0 {
			req.Header.Set("anthropic-beta", "oauth-2025-04-20,interleaved-thinking-2025-05-14")
		} else {
			req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		}
	} else {
		req.Header.Set("x-api-key", c.APIKey)
		if c.ThinkingBudget > 0 {
			req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		}
	}

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("anthropic error (%d): %s", resp.StatusCode, string(body))
		emitDebug("error", msg)
		return nil, fmt.Errorf("%s", msg)
	}

	// Streaming parser. Anthropic emits one of:
	//   message_start / content_block_start / content_block_delta /
	//   content_block_stop / message_delta / message_stop / ping / error.
	// Blocks are addressed by index; tool_use inputs arrive as partial_json
	// deltas that we accumulate into a per-block string and json-parse at stop.
	// signature_delta tokens are silently dropped — the current non-streaming
	// path also discards them (see TODO.md: extended thinking signatures are
	// not yet preserved across turns, so interleaved-thinking multi-turn flows
	// remain at parity with the previous behavior).
	type anthropicBlock struct {
		typ       string
		text      string
		toolID    string
		toolName  string
		toolJSON  string
		signature string
	}
	blocks := map[int]*anthropicBlock{}
	resMsg := &Message{Role: "assistant"}
	var resultModel string
	var resultUsage json.RawMessage
	// Capture onDelta and onUsage once at stream start. Contract: streaming
	// callbacks are bound for the lifetime of one Chat call. SetOnDelta /
	// SetOnUsage invoked mid-stream do not take effect until the next Chat;
	// chatWithDelta sets them before the call and defer-clears them after.
	// parseOpenAIChatCompletionsStream follows the same contract via its
	// function parameters.
	onDelta := c.onDelta()
	rawOnUsage := c.onUsage()
	// Anthropic's message_start and message_delta events carry CUMULATIVE
	// input_tokens / output_tokens snapshots. Subscribers (e.g. AgentRun.AddUsage)
	// expect incremental deltas, so unwrap the cumulative-to-delta conversion
	// here and only forward positive increments.
	var lastInputReported, lastOutputReported int64
	var onUsage func(int64, int64)
	if rawOnUsage != nil {
		onUsage = func(in, out int64) {
			dIn := in - lastInputReported
			dOut := out - lastOutputReported
			if dIn < 0 {
				dIn = 0
			}
			if dOut < 0 {
				dOut = 0
			}
			if in > lastInputReported {
				lastInputReported = in
			}
			if out > lastOutputReported {
				lastOutputReported = out
			}
			if dIn > 0 || dOut > 0 {
				rawOnUsage(dIn, dOut)
			}
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				Model string          `json:"model"`
				Usage json.RawMessage `json:"usage"`
			} `json:"message"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				Signature   string `json:"signature"`
			} `json:"delta"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message.Model != "" {
				resultModel = ev.Message.Model
			}
			if len(ev.Message.Usage) > 0 {
				resultUsage = ev.Message.Usage
				emitDebug("TOKENS", fmt.Sprintf("message_start usage from provider=%s model=%s: %s", c.Provider, c.Model, string(ev.Message.Usage)))
				if onUsage != nil {
					var u struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					}
					if err := json.Unmarshal(ev.Message.Usage, &u); err == nil {
						if u.InputTokens > 0 || u.OutputTokens > 0 {
							onUsage(u.InputTokens, u.OutputTokens)
						}
					}
				}
			}
		case "content_block_start":
			blocks[ev.Index] = &anthropicBlock{
				typ:      ev.ContentBlock.Type,
				toolID:   ev.ContentBlock.ID,
				toolName: ev.ContentBlock.Name,
				text:     ev.ContentBlock.Text,
			}
		case "content_block_delta":
			b, ok := blocks[ev.Index]
			if !ok {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				b.text += ev.Delta.Text
				if onDelta != nil && ev.Delta.Text != "" {
					onDelta("text", ev.Delta.Text)
				}
			case "thinking_delta":
				b.text += ev.Delta.Thinking
				if onDelta != nil && ev.Delta.Thinking != "" {
					onDelta("reasoning", ev.Delta.Thinking)
				}
			case "input_json_delta":
				b.toolJSON += ev.Delta.PartialJSON
			case "signature_delta":
				b.signature += ev.Delta.Signature
			}
		case "content_block_stop":
			// finalization happens below in index order; nothing to do here.
		case "message_delta":
			if len(ev.Usage) > 0 {
				// message_delta carries cumulative output_tokens; merge by
				// preferring it over message_start's input_tokens.
				emitDebug("TOKENS", fmt.Sprintf("message_delta usage from provider=%s model=%s: %s", c.Provider, c.Model, string(ev.Usage)))
				resultUsage = mergeAnthropicUsage(resultUsage, ev.Usage)
				if onUsage != nil {
					var u struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					}
					if err := json.Unmarshal(ev.Usage, &u); err == nil {
						if u.InputTokens > 0 || u.OutputTokens > 0 {
							onUsage(u.InputTokens, u.OutputTokens)
						}
					}
				}
			}
		case "message_stop":
			// drained below.
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("anthropic stream error: SSE line exceeded 16MB buffer (likely huge reasoning block): %w", err)
		}
		return nil, fmt.Errorf("anthropic stream error: %w", err)
	}

	// Walk blocks in index order so multi-block responses preserve ordering.
	indices := make([]int, 0, len(blocks))
	for idx := range blocks {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		b := blocks[idx]
		switch b.typ {
		case "text":
			resMsg.Content += b.text
		case "thinking":
			resMsg.ReasoningContent += b.text
		case "tool_use":
			args := b.toolJSON
			if args == "" {
				args = "{}"
			} else if !json.Valid([]byte(args)) {
				// Streamed input_json_delta fragments did not assemble into
				// valid JSON (typically the stream was truncated mid-tool).
				// Fall back to an empty object so the tool call is still
				// dispatched and the model can react to the error.
				emitDebug("AGENT", fmt.Sprintf("anthropic: truncated tool_use input_json for %s (id=%s, %d bytes); falling back to {}", b.toolName, b.toolID, len(args)))
				args = "{}"
			}
			resMsg.ToolCalls = append(resMsg.ToolCalls, ToolCall{
				ID:   b.toolID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      b.toolName,
					Arguments: args,
				},
			})
		}
	}
	if resultModel != "" {
		resMsg.Model = resultModel
	}
	usage, err := usageForProvider(c.Provider, resultUsage)
	if err != nil {
		return nil, err
	}
	if resMsg.Model == "" {
		resMsg.Model = c.Model
	}
	// Some providers (e.g. minimax-m3 via opencode-go) omit input_tokens from
	// their streaming usage events. Fill it in from a character-based estimate
	// of the messages we sent so spend and context-window display are accurate.
	if usage != nil && usage.PromptTokens == nil {
		emitDebug("TOKENS", fmt.Sprintf("provider=%s model=%s returned no input_tokens in usage (raw=%s); estimating from message content", c.Provider, c.Model, string(resultUsage)))
		estimated := int64(messagesTokens(messages, charsPerTokenFor(c.Provider, c.Model)))
		if system != "" {
			estimated += int64((len(system) + charsPerToken - 1) / charsPerToken)
		}
		usage.PromptTokens = &estimated
	}
	resMsg.Usage = usage
	if usage != nil {
		resMsg.Spend = usage.Spend(resMsg.Model)
	}

	if resMsg.Content != "" || len(resMsg.ToolCalls) > 0 {
		return resMsg, nil
	}
	return nil, fmt.Errorf("no response from anthropic")
}

// supportsVision reports whether the client's active model can accept image
// input. Serialization gates image blocks on this so a mid-session switch to a
// text-only model does not re-send baked-in read images and get the turn
// rejected.
func (c *GenericClient) supportsVision() bool {
	m := c.Model
	if c.Provider != "" {
		m = c.Provider + "/" + c.Model
	}
	return ModelSupportsVision(m)
}

// buildAnthropicMessages converts the ocode message slice (already stripped of
// system messages) into the Anthropic messages array. Extracted from
// chatAnthropic so the tool_result / image-block serialization is unit-testable
// without an HTTP round trip.
func (c *GenericClient) buildAnthropicMessages(messages []Message) ([]map[string]interface{}, error) {
	vision := c.supportsVision()
	var anthropicMsgs []map[string]interface{}
	for _, m := range messages {
		role := m.Role
		if role == "tool" {
			role = "user"
		}

		var content []interface{}

		if m.Role == "tool" {
			toolResult := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": m.ToolID,
			}
			if len(m.Images) > 0 && vision {
				// Anthropic tool_result.content accepts an array of text + image
				// blocks — carry the read image inline so the model can see it.
				blocks := make([]interface{}, 0, len(m.Images)+1)
				if txt := sanitizeAPIText(m.Content); txt != "" {
					blocks = append(blocks, map[string]interface{}{"type": "text", "text": txt})
				}
				for _, img := range m.Images {
					blocks = append(blocks, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": img.MIMEType,
							"data":       img.Data,
						},
					})
				}
				toolResult["content"] = blocks
			} else {
				toolResult["content"] = sanitizeAPIText(m.Content)
			}
			content = []interface{}{toolResult}
		} else {
			if m.Role == "user" && (len(m.Images) > 0 || strings.Contains(m.Content, "@")) {
				imgBlocks, err := c.buildAnthropicImageContent(m)
				if err != nil {
					return nil, err
				}
				if imgBlocks != nil {
					content = imgBlocks
				}
			}
			if content == nil {
				if m.Content != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": sanitizeAPIText(m.Content),
					})
				}
			}
			for _, tc := range m.ToolCalls {
				var input interface{}
				json.Unmarshal([]byte(tc.Function.Arguments), &input) //nolint:errcheck
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
		}

		if len(content) == 0 {
			continue
		}

		if n := len(anthropicMsgs); n > 0 && anthropicMsgs[n-1]["role"] == role {
			prev, _ := anthropicMsgs[n-1]["content"].([]interface{})
			// Don't merge when either side has tool_result blocks — mixing tool_result
			// with text in the same user message is rejected by strict providers (e.g. Minimax).
			if !hasToolResultBlock(prev) && !hasToolResultBlock(content) {
				anthropicMsgs[n-1]["content"] = append(prev, content...)
				continue
			}
		}
		anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
			"role":    role,
			"content": content,
		})
	}
	return anthropicMsgs, nil
}

func (c *GenericClient) buildAnthropicImageContent(m Message) ([]interface{}, error) {
	var content []interface{}
	if len(m.Images) > 0 {
		if m.Content != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": m.Content,
			})
		}
		for _, img := range m.Images {
			content = append(content, map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type":       "base64",
					"media_type": img.MIMEType,
					"data":       img.Data,
				},
			})
		}
		return content, nil
	}

	text := m.Content
	parts := strings.Fields(text)
	hasImage := false
	var textParts []string

	for _, part := range parts {
		if strings.HasPrefix(part, "@") {
			filePath := strings.TrimPrefix(part, "@")
			if IsImageFile(filePath) {
				isImage, mimeType, err := DetectImage(filePath)
				if err != nil || !isImage {
					textParts = append(textParts, part)
					continue
				}
				img, err := NewImageWithMaxDim(filePath, c.MaxImageDim)
				if err != nil {
					return nil, err
				}
				if !hasImage {
					if len(textParts) > 0 {
						content = append(content, map[string]interface{}{
							"type": "text",
							"text": strings.Join(textParts, " "),
						})
						textParts = nil
					}
					hasImage = true
				}
				content = append(content, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": mimeType,
						"data":       img.Data,
					},
				})
				continue
			}
		}
		textParts = append(textParts, part)
	}

	if len(textParts) > 0 {
		if hasImage {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": strings.Join(textParts, " "),
			})
		} else {
			return nil, nil
		}
	}

	if !hasImage {
		return nil, nil
	}

	return content, nil
}

type providerInfo struct {
	envKey  string
	baseURL string
}

// keyOptionalProviders are providers that can serve requests without an API
// key — local servers and free tiers. For every other (keyed) provider,
// NewClient refuses to build a client when no credential is available, so a
// model switch / ResolveSmallModel fallback / compactSummaryClient skips it
// instead of returning a client that 401s on its first request.
var keyOptionalProviders = map[string]bool{
	"opencode":    true, // free tier (mimo-v2.5-free etc.)
	"opencode-go": true,
	"lmstudio":    true, // local server, no key
}

var providers = map[string]providerInfo{
	"openai":                {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"anthropic":             {"ANTHROPIC_API_KEY", "https://api.anthropic.com/v1"},
	"openrouter":            {"OPENROUTER_API_KEY", "https://openrouter.ai/api/v1"},
	"google":                {"GOOGLE_API_KEY", "https://generativelanguage.googleapis.com/v1beta/openai"},
	"zai":                   {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"z.ai":                  {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"zai-coding":            {"ZAI_API_KEY", "https://api.z.ai/api/coding/paas/v4"},
	"chutes":                {"CHUTES_API_KEY", "https://llm.chutes.ai/v1"},
	"chutes-coding":         {"CHUTES_API_KEY", "https://llm.chutes.ai/v1"}, // Placeholder if distinct endpoint exists
	"alibaba":               {"DASHSCOPE_API_KEY", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	"alibaba-coding":        {"DASHSCOPE_API_KEY", "https://coding-intl.dashscope.aliyuncs.com/v1"},
	"moonshot":              {"MOONSHOT_API_KEY", "https://api.moonshot.cn/v1"},
	"minimax":               {"MINIMAX_API_KEY", "https://api.minimax.chat/v1"},
	"requesty":              {"REQUESTY_API_KEY", "https://router.requesty.ai/v1"},
	"deepinfra":             {"DEEPINFRA_API_KEY", "https://api.deepinfra.com/v1/openai"},
	"nvidia":                {"NVIDIA_API_KEY", "https://integrate.api.nvidia.com/v1"},
	"302ai":                 {"302AI_API_KEY", "https://api.302.ai/v1"},
	"deepseek":              {"DEEPSEEK_API_KEY", "https://api.deepseek.com/v1"},
	"groq":                  {"GROQ_API_KEY", "https://api.groq.com/openai/v1"},
	"mistral":               {"MISTRAL_API_KEY", "https://api.mistral.ai/v1"},
	"novita-ai":             {"NOVITA_API_KEY", "https://api.novita.ai/openai/v1"},
	"opencode":              {"OPENCODE_API_KEY", "https://opencode.ai/zen/v1"},
	"opencode-go":           {"OPENCODE_API_KEY", "https://opencode.ai/zen/go/v1"},
	"copilot":               {"GITHUB_COPILOT_TOKEN", "https://api.githubcopilot.com"},
	"lmstudio":              {"", "http://localhost:1234/v1"},
	"cloudflare-workers":    {"CLOUDFLARE_API_KEY", ""},
	"cloudflare-gateway":    {"CLOUDFLARE_GATEWAY_KEY", ""},
	"codex":                 {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"xiaomi":                {"XIAOMI_API_KEY", "https://xiaomimimo.com/v1"},
	"xiaomi-token-plan-sgp": {"XIAOMI_API_KEY", "https://token-plan-sgp.xiaomimimo.com/v1"},
	"xiaomi-token-plan-ams": {"XIAOMI_API_KEY", "https://token-plan-ams.xiaomimimo.com/v1"},
	"xiaomi-token-plan-cn":  {"XIAOMI_API_KEY", "https://token-plan-cn.xiaomimimo.com/v1"},
}

// maskKey returns a safe-to-log representation of an API key: first 4 chars +
// "…" if present, or "(empty)" if blank. Never leaks the full key.
func maskKey(key string) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "…" + key[len(key)-4:]
}

func NewClient(cfg *config.Config, model string) LLMClient {
	emitDebug("AGENT", fmt.Sprintf("NewClient: building client for model %q", model))
	provider := ""
	apiKey := ""
	baseURL := ""
	useOAuth := false
	accountID := ""

	// Handle provider/model and provider:model formats.
	// Check slash first so that OpenRouter models like
	// "openrouter/openai/gpt-oss-120b:free" parse correctly — the colon
	// is part of the model name, not a provider separator.
	if parts := strings.SplitN(model, "/", 2); len(parts) == 2 {
		if _, ok := providers[parts[0]]; ok {
			provider = parts[0]
			model = parts[1]
		} else if cfg != nil {
			if _, ok := cfg.Provider[parts[0]]; ok {
				provider = parts[0]
				model = parts[1]
			}
		}
	}
	if provider == "" {
		if parts := strings.SplitN(model, ":", 2); len(parts) == 2 {
			provider = parts[0]
			model = parts[1]
		}
	}
	emitDebug("AGENT", fmt.Sprintf("NewClient: resolved provider=%q model=%q", provider, model))

	// OPENCODE_AUTH_TOKEN env var — highest priority override. When set it
	// bypasses config, per-provider env vars, and stored credentials.
	if v := os.Getenv("OPENCODE_AUTH_TOKEN"); v != "" {
		apiKey = v
		emitDebug("AGENT", fmt.Sprintf("NewClient: OPENCODE_AUTH_TOKEN override — apiKey=%s", maskKey(apiKey)))
	}

	// Use config for provider details if available
	if cfg != nil && provider != "" {
		if p, ok := cfg.Provider[provider]; ok {
			if pMap, ok := p.(map[string]interface{}); ok {
				if opts, ok := pMap["options"].(map[string]interface{}); ok {
					if b, ok := opts["baseURL"].(string); ok {
						baseURL = b
					}
					if apiKey == "" {
						if a, ok := opts["apiKey"].(string); ok {
							apiKey = auth.ResolveEnvVarRef(a)
						}
					}
				}
			}
		}
	}
	emitDebug("AGENT", fmt.Sprintf("NewClient: after config check — provider=%q apiKey=%s baseURL=%q", provider, maskKey(apiKey), baseURL))

	// Apply defaults from provider map
	if info, ok := providers[provider]; ok {
		if apiKey == "" {
			apiKey = os.Getenv(info.envKey)
			if provider == "google" && apiKey == "" {
				apiKey = os.Getenv("GOOGLE_API_KEY")
			}
			emitDebug("AGENT", fmt.Sprintf("NewClient: env check — envKey=%q apiKey=%s", info.envKey, maskKey(apiKey)))
		}
		if apiKey == "" {
			// Fall back to stored credential. Copilot stores a long-lived GH OAuth
			// token under AccessToken; for other providers prefer Key, then OAuth token.
			if cred, ok := auth.Get(provider); ok {
				switch cred.Kind {
				case auth.KindAPIKey:
					apiKey = cred.Key
				case auth.KindOAuth:
					if tok, refreshed := auth.OAuthAccessToken(provider); refreshed {
						apiKey = tok
					} else {
						apiKey = cred.AccessToken
					}
					useOAuth = true
					accountID = cred.AccountID
				}
				emitDebug("AGENT", fmt.Sprintf("NewClient: credential check — kind=%s apiKey=%s useOAuth=%v", cred.Kind, maskKey(apiKey), useOAuth))
			} else {
				emitDebug("AGENT", fmt.Sprintf("NewClient: no stored credential for provider %q", provider))
			}
		}
		if baseURL == "" {
			baseURL = info.baseURL
		}
		if override := auth.GetBaseURL(provider); override != "" {
			baseURL = override
		}
	}

	// LM Studio base URL can be overridden via env var.
	if provider == "lmstudio" {
		if override := os.Getenv("LMSTUDIO_BASE_URL"); override != "" {
			baseURL = normalizeLMStudioBaseURL(override)
		}
	}

	// Heuristics for unknown providers
	if provider == "" {
		if strings.HasPrefix(model, "gpt") {
			provider = "openai"
		} else if strings.HasPrefix(model, "claude") {
			provider = "anthropic"
		}
		if info, ok := providers[provider]; ok {
			apiKey = os.Getenv(info.envKey)
			baseURL = info.baseURL
		}
	}

	if baseURL == "" {
		emitDebug("AGENT", fmt.Sprintf("NewClient: no baseURL for provider %q; returning nil", provider))
		return nil
	}

	// A keyed provider with no credential (env var, stored API key, or OAuth)
	// can't authenticate. Refuse to build the client so callers — model switch,
	// ResolveSmallModel fallback, compactSummaryClient — skip it and surface a
	// clear failure instead of a deferred 401 on the first request. Providers in
	// keyOptionalProviders (local servers, free tiers) are allowed through.
	if apiKey == "" && provider != "" && !keyOptionalProviders[provider] {
		emitDebug("AGENT", fmt.Sprintf("NewClient: no API key for provider %q (useOAuth=%v); refusing to build client (would 401)", provider, useOAuth))
		return nil
	}

	thinkingBudget := 0
	if cfg != nil {
		thinkingBudget = cfg.ThinkingBudget
	}

	emitDebug("AGENT", fmt.Sprintf("NewClient: OK — provider=%q model=%q apiKey=%s useOAuth=%v ws=%v", provider, model, maskKey(apiKey), useOAuth, cfg != nil && cfg.UseWebSocket))
	maxImageDim := 0
	if cfg != nil {
		maxImageDim = cfg.Ocode.MaxImageDim
	}
	return &GenericClient{
		APIKey:         apiKey,
		Model:          model,
		BaseURL:        baseURL,
		Provider:       provider,
		MaxImageDim:    ResolveImageMaxDim(maxImageDim),
		UseOAuth:       useOAuth,
		AccountID:      accountID,
		ThinkingBudget: thinkingBudget,
		UseWebSocket:   cfg != nil && cfg.UseWebSocket,
	}
}

// ModelSupportsThinking returns true when the resolved model supports
// provider-level reasoning / extended thinking.
func ModelSupportsThinking(modelID string) bool {
	if reasoning, ok := modelSupportsThinkingFromRegistry(modelID); ok {
		return reasoning
	}

	model := strings.ToLower(modelID)
	provider := ""
	if p, m, ok := splitModelID(model); ok {
		provider = p
		model = m
	}

	if strings.Contains(model, "non-reasoning") {
		return false
	}

	// Fallback heuristics for when models.dev is unavailable. The registry's
	// explicit reasoning flag wins whenever present.
	switch provider {
	case "anthropic", "":
		if strings.Contains(model, "claude-3-7") ||
			strings.Contains(model, "claude-opus-4") ||
			strings.Contains(model, "claude-sonnet-4") ||
			strings.Contains(model, "claude-haiku-4") {
			return true
		}
	}

	return strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4") ||
		strings.HasPrefix(model, "gpt-5") ||
		strings.Contains(model, "gemini-2.5") ||
		strings.Contains(model, "gemini-3") ||
		strings.Contains(model, "gemma-4") ||
		strings.Contains(model, "deepseek-reasoner") ||
		strings.Contains(model, "deepseek-v4") ||
		strings.HasPrefix(model, "qwq") ||
		strings.HasPrefix(model, "qwen3") ||
		strings.HasPrefix(model, "glm-5") ||
		strings.HasPrefix(model, "glm-4.5") ||
		strings.HasPrefix(model, "glm-4.6") ||
		strings.HasPrefix(model, "glm-4.7") ||
		strings.Contains(model, "mimo") ||
		(strings.Contains(model, "grok-4") && strings.Contains(model, "reasoning"))
}

func modelSupportsThinkingFromRegistry(modelID string) (bool, bool) {
	data := registrySnapshotIfReady()
	if data == nil {
		return false, false
	}

	if provider, model, ok := splitModelID(modelID); ok {
		if entry, ok := data[provider]; ok {
			if m, ok := entry.Models[model]; ok {
				return m.Reasoning, true
			}
		}
	}

	for _, entry := range data {
		if m, ok := entry.Models[modelID]; ok {
			return m.Reasoning, true
		}
	}

	return false, false
}

// normalizeLMStudioBaseURL accepts user-supplied overrides in any of the
// common shapes (with or without trailing slash, with or without a /v1 suffix)
// and returns a canonical base URL ending in /v1. This avoids the foot-gun
// where a naive override like "http://host:1234/v1" would otherwise be
// concatenated into "http://host:1234/v1/v1" and every request would 404.
func normalizeLMStudioBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	s = strings.TrimRight(s, "/")
	if strings.HasSuffix(s, "/v1") {
		return s
	}
	return s + "/v1"
}

// mergeAnthropicUsage merges an incremental usage payload from message_delta
// over the cumulative usage seen so far (typically from message_start). Anthropic
// streams input_tokens up front and updates output_tokens at the end; the merged
// object keeps the highest seen value for each key so usageForProvider sees both.
func mergeAnthropicUsage(base, incoming json.RawMessage) json.RawMessage {
	if len(base) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return base
	}
	var b, i map[string]interface{}
	if err := json.Unmarshal(base, &b); err != nil {
		return incoming
	}
	if err := json.Unmarshal(incoming, &i); err != nil {
		return base
	}
	for k, v := range i {
		if existing, ok := b[k]; ok {
			// Keep larger numeric value; non-numeric falls through to overwrite.
			ef, eok := existing.(float64)
			nf, nok := v.(float64)
			if eok && nok && ef > nf {
				continue
			}
		}
		b[k] = v
	}
	merged, err := json.Marshal(b)
	if err != nil {
		return incoming
	}
	return merged
}

// chatOpenAIWebSocket uses WebSocket transport for OpenAI Responses API.
func (c *GenericClient) chatOpenAIWebSocket(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	wsClient := NewWebSocketClient(c.BaseURL, c.APIKey)
	defer wsClient.Close()

	// Connect
	if err := wsClient.Connect(ctx); err != nil {
		// Fallback to HTTP on connection failure
		emitDebug("websocket", fmt.Sprintf("WebSocket connect failed, falling back to HTTP: %v", err))
		return c.chatOpenAIHTTP(ctx, messages, tools)
	}

	// Build request payload
	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
		"stream":   true,
	}
	c.applyGenerationParams(ctx, payload)
	maybeStripMaxTokensForGateway(c.Provider, c.Model, payload)
	if providerSupportsReasoningEffort(c.Provider) && c.ThinkingBudget > 0 {
		payload["reasoning_effort"] = reasoningEffortForBudget(c.ThinkingBudget)
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
	}

	// Send via WebSocket
	if err := wsClient.Send(ctx, &WSMessage{
		Type:    "response.create",
		Payload: mustMarshal(payload),
	}); err != nil {
		// Fallback to HTTP on send failure
		emitDebug("websocket", fmt.Sprintf("WebSocket send failed, falling back to HTTP: %v", err))
		return c.chatOpenAIHTTP(ctx, messages, tools)
	}

	// Receive streaming responses
	return c.receiveWebSocketStream(ctx, wsClient)
}

// chatOpenAIHTTP is the original HTTP SSE implementation.
func (c *GenericClient) chatOpenAIHTTP(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	url := c.BaseURL + "/chat/completions"

	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
		"stream":   true,
	}
	c.applyGenerationParams(ctx, payload)
	maybeStripMaxTokensForGateway(c.Provider, c.Model, payload)
	if providerSupportsReasoningEffort(c.Provider) && c.ThinkingBudget > 0 {
		payload["reasoning_effort"] = reasoningEffortForBudget(c.ThinkingBudget)
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	emitDebug("LLM", fmt.Sprintf("chatOpenAIHTTP: url=%s apiKey=%s model=%q", url, maskKey(c.APIKey), c.Model))

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("%s error (%d): %s", c.Provider, resp.StatusCode, string(body))
		emitDebug("ERROR", fmt.Sprintf("chatOpenAIHTTP: status=%d apiKey=%s url=%s", resp.StatusCode, maskKey(c.APIKey), url))
		return nil, fmt.Errorf("%s", msg)
	}

	msg, usageRaw, err := parseOpenAIChatCompletionsStream(resp.Body, c.onDelta(), c.onUsage())
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("no response from %s", c.Provider)
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	usage, err := usageForProvider(c.Provider, usageRaw)
	if err != nil {
		return nil, err
	}
	msg.Usage = usage
	if usage != nil {
		msg.Spend = usage.Spend(msg.Model)
		usage.DebugLog(msg.Model)
	}
	return msg, nil
}

// receiveWebSocketStream receives streaming responses from WebSocket.
func (c *GenericClient) receiveWebSocketStream(ctx context.Context, wsClient *WebSocketClient) (*Message, error) {
	msg := &Message{Role: "assistant"}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			wsMsg, err := wsClient.Receive(ctx)
			if err != nil {
				return nil, fmt.Errorf("websocket receive: %w", err)
			}

			switch wsMsg.Type {
			case "response.output_text.delta":
				var delta struct {
					Delta string `json:"delta"`
				}
				if err := json.Unmarshal(wsMsg.Payload, &delta); err == nil && delta.Delta != "" {
					msg.Content += delta.Delta
					if fn := c.onDelta(); fn != nil {
						fn("text", delta.Delta)
					}
				}

			case "response.completed":
				// Stream completed
				if msg.Model == "" {
					msg.Model = c.Model
				}
				return msg, nil

			case "error":
				return nil, fmt.Errorf("websocket error: %s", wsMsg.Error)

			default:
				// Handle other message types
				emitDebug("websocket", fmt.Sprintf("unexpected message type: %s", wsMsg.Type))
			}
		}
	}
}

// mustMarshal marshals a value to JSON, panicking on error.
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// applyRedactionSafetyNet scans all message contents for known-format secrets
// and returns a new slice with redacted content. This is the chokepoint that
// catches secrets in system prompts, context files, and LSP diagnostic injections.
func (c *GenericClient) applyRedactionSafetyNet(messages []Message) []Message {
	if c.Redaction == nil || !c.Redaction.Enabled || c.Redaction.Registry == nil {
		return messages
	}

	result := make([]Message, len(messages))
	for i, msg := range messages {
		result[i] = msg
		if msg.Content == "" {
			continue
		}

		// Scan for known-format secrets (file mode - no keyword/entropy)
		spans := redact.Detect(msg.Content, nil, redact.DetectOpts{FileContent: true})
		if len(spans) == 0 {
			continue
		}

		// Register and substitute
		for _, span := range spans {
			value := msg.Content[span.Start:span.End]
			c.Redaction.Registry.GetOrAssign(value, span.Kind, "net")
		}
		result[i].Content = c.Redaction.Registry.Substitute(msg.Content)
	}

	return result
}
