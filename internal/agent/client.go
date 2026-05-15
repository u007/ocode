package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
)

var llmHTTPClient = &http.Client{Timeout: 120 * time.Second}

type Message struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
	ToolID    string      `json:"tool_call_id,omitempty"`
	Model     string      `json:"-"`
	Usage     *TokenUsage `json:"-"`
	Spend     *float64    `json:"-"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
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
	APIKey   string
	Model    string
	BaseURL  string
	Provider string
	UseOAuth bool // when true, treat APIKey as a bearer OAuth token
}

func (c *GenericClient) GetProvider() string {
	return c.Provider
}

func (c *GenericClient) GetModel() string {
	return c.Model
}

func (c *GenericClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.Provider == "anthropic" {
		return c.chatAnthropic(messages, tools)
	}
	if c.Provider == "copilot" {
		return c.chatCopilot(messages, tools)
	}
	return c.chatOpenAI(messages, tools)
}

// chatCopilot exchanges the stored GitHub OAuth token (held in APIKey) for a short-lived
// Copilot API token, then calls the Copilot chat completions endpoint with the headers
// the service requires.
func (c *GenericClient) chatCopilot(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("copilot: no github token configured (run /connect → GitHub Copilot)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	apiToken, err := auth.CopilotExchangeAPIToken(ctx, c.APIKey)
	if err != nil {
		return nil, fmt.Errorf("copilot token exchange: %w", err)
	}

	url := c.BaseURL + "/chat/completions"
	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
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
		return nil, fmt.Errorf("copilot error (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from copilot")
	}
	msg := &result.Choices[0].Message
	msg.Model = result.Model
	if msg.Model == "" {
		msg.Model = c.Model
	}
	usage, err := usageForProvider("openai", result.Usage)
	if err != nil {
		return nil, err
	}
	msg.Usage = usage
	if usage != nil {
		msg.Spend = usage.Spend(msg.Model)
	}
	return msg, nil
}

func (c *GenericClient) chatOpenAI(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.UseOAuth && c.Provider == "openai" {
		return c.chatOpenAIResponses(messages, tools)
	}
	url := c.BaseURL + "/chat/completions"
	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s error (%d): %s", c.Provider, resp.StatusCode, string(body))
	}

	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Choices) > 0 {
		msg := &result.Choices[0].Message
		msg.Model = result.Model
		if msg.Model == "" {
			msg.Model = c.Model
		}
		usage, err := usageForProvider(c.Provider, result.Usage)
		if err != nil {
			return nil, err
		}
		msg.Usage = usage
		if usage != nil {
			msg.Spend = usage.Spend(msg.Model)
		}
		return msg, nil
	}
	return nil, fmt.Errorf("no response from %s", c.Provider)
}

// chatOpenAIResponses calls the OpenAI Responses API using a ChatGPT OAuth token.
// The token must have api.connectors.invoke scope (obtained via the login flow).
// The chatgpt_account_id claim is extracted from the JWT and sent as ChatGPT-Account-ID.
func (c *GenericClient) chatOpenAIResponses(messages []Message, tools []map[string]interface{}) (*Message, error) {
	accountID := jwtClaim(c.APIKey, "https://api.openai.com/auth", "chatgpt_account_id")

	// Map messages → Responses API input items.
	input := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		item := map[string]interface{}{"role": m.Role, "content": m.Content}
		input = append(input, item)
	}

	payload := map[string]interface{}{
		"model": c.Model,
		"input": input,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := "https://api.openai.com/v1/responses"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai responses error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Find the first message-type output item.
	for _, out := range result.Output {
		if out.Type != "message" {
			continue
		}
		var text string
		for _, c := range out.Content {
			if c.Type == "output_text" {
				text += c.Text
			}
		}
		msg := &Message{
			Role:    out.Role,
			Content: text,
			Model:   result.Model,
		}
		if msg.Model == "" {
			msg.Model = c.Model
		}
		usage, err := usageForProvider(c.Provider, result.Usage)
		if err != nil {
			return nil, err
		}
		msg.Usage = usage
		if usage != nil {
			msg.Spend = usage.Spend(msg.Model)
		}
		return msg, nil
	}
	return nil, fmt.Errorf("no response from openai responses api")
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

func (c *GenericClient) chatAnthropic(messages []Message, tools []map[string]interface{}) (*Message, error) {
	url := c.BaseURL + "/messages"

	var system string
	var anthropicMsgs []map[string]interface{}
	for _, m := range messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		// Anthropic roles: user, assistant
		// tool results must be sent as role=user with tool_result content blocks.
		role := m.Role
		if role == "tool" {
			role = "user"
		}

		var content []interface{}

		if m.Role == "tool" {
			// Tool result block only — no extra text block.
			content = []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": m.ToolID,
					"content":     m.Content,
				},
			}
		} else {
			// For user/assistant messages: text first, then tool_use blocks.
			if m.Content != "" {
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": m.Content,
				})
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

		anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
			"role":    role,
			"content": content,
		})
	}

	payload := map[string]interface{}{
		"model":      c.Model,
		"system":     system,
		"messages":   anthropicMsgs,
		"max_tokens": 4096,
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
		payload["tools"] = anthropicTools
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if c.UseOAuth {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		req.Header.Set("x-api-key", c.APIKey)
	}

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   json.RawMessage `json:"usage"`
		Content []struct {
			Type  string      `json:"type"`
			Text  string      `json:"text"`
			ID    string      `json:"id"`
			Name  string      `json:"name"`
			Input interface{} `json:"input"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	resMsg := &Message{Role: "assistant"}
	for _, block := range result.Content {
		if block.Type == "text" {
			resMsg.Content += block.Text
		} else if block.Type == "tool_use" {
			args, _ := json.Marshal(block.Input)
			resMsg.ToolCalls = append(resMsg.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}
	usage, err := usageForProvider(c.Provider, result.Usage)
	if err != nil {
		return nil, err
	}
	resMsg.Model = result.Model
	if resMsg.Model == "" {
		resMsg.Model = c.Model
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

type providerInfo struct {
	envKey  string
	baseURL string
}

var providers = map[string]providerInfo{
	"openai":         {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"anthropic":      {"ANTHROPIC_API_KEY", "https://api.anthropic.com/v1"},
	"openrouter":     {"OPENROUTER_API_KEY", "https://openrouter.ai/api/v1"},
	"google":         {"GOOGLE_API_KEY", "https://generativelanguage.googleapis.com/v1beta/openai"},
	"zai":            {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"z.ai":           {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"zai-coding":     {"ZAI_API_KEY", "https://api.z.ai/api/coding/paas/v4"},
	"chutes":         {"CHUTES_API_KEY", "https://api.chutes.ai/v1"},
	"chutes-coding":  {"CHUTES_API_KEY", "https://api.chutes.ai/v1"}, // Placeholder if distinct endpoint exists
	"alibaba":        {"DASHSCOPE_API_KEY", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	"alibaba-coding": {"DASHSCOPE_API_KEY", "https://coding-intl.dashscope.aliyuncs.com/v1"},
	"moonshot":       {"MOONSHOT_API_KEY", "https://api.moonshot.cn/v1"},
	"minimax":        {"MINIMAX_API_KEY", "https://api.minimax.chat/v1"},
	"requesty":       {"REQUESTY_API_KEY", "https://router.requesty.ai/v1"},
	"deepinfra":      {"DEEPINFRA_API_KEY", "https://api.deepinfra.com/v1/openai"},
	"nvidia":         {"NVIDIA_API_KEY", "https://integrate.api.nvidia.com/v1"},
	"302ai":          {"302AI_API_KEY", "https://api.302.ai/v1"},
	"deepseek":       {"DEEPSEEK_API_KEY", "https://api.deepseek.com/v1"},
	"groq":           {"GROQ_API_KEY", "https://api.groq.com/openai/v1"},
	"mistral":        {"MISTRAL_API_KEY", "https://api.mistral.ai/v1"},
	"opencode":       {"OPENCODE_API_KEY", "https://api.opencode.ai/v1"},
	"opencode-go":    {"OPENCODE_API_KEY", "https://api.opencode.ai/v1/go"},
	"copilot":        {"GITHUB_COPILOT_TOKEN", "https://api.githubcopilot.com"},
}

func NewClient(cfg *config.Config, model string) LLMClient {
	provider := ""
	apiKey := ""
	baseURL := ""
	useOAuth := false

	// Handle provider:model format
	if parts := strings.SplitN(model, ":", 2); len(parts) == 2 {
		provider = parts[0]
		model = parts[1]
	}

	// Use config for provider details if available
	if cfg != nil && provider != "" {
		if p, ok := cfg.Provider[provider]; ok {
			if pMap, ok := p.(map[string]interface{}); ok {
				if opts, ok := pMap["options"].(map[string]interface{}); ok {
					if b, ok := opts["baseURL"].(string); ok {
						baseURL = b
					}
					if a, ok := opts["apiKey"].(string); ok {
						apiKey = a
						if strings.HasPrefix(apiKey, "{env:") && strings.HasSuffix(apiKey, "}") {
							envVar := strings.TrimSuffix(strings.TrimPrefix(apiKey, "{env:"), "}")
							apiKey = os.Getenv(envVar)
						}
					}
				}
			}
		}
	}

	// Apply defaults from provider map
	if info, ok := providers[provider]; ok {
		if apiKey == "" {
			apiKey = os.Getenv(info.envKey)
			if provider == "google" && apiKey == "" {
				apiKey = os.Getenv("GOOGLE_API_KEY")
			}
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
				}
			}
		}
		if baseURL == "" {
			baseURL = info.baseURL
		}
		if override := auth.GetBaseURL(provider); override != "" {
			baseURL = override
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
		return nil
	}

	return &GenericClient{
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  baseURL,
		Provider: provider,
		UseOAuth: useOAuth,
	}
}
