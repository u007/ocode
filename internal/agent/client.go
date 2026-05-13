package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
)

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolID    string     `json:"tool_call_id,omitempty"`
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
}

type GenericClient struct {
	APIKey   string
	Model    string
	BaseURL  string
	Provider string
}

func (c *GenericClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.Provider == "anthropic" {
		return c.chatAnthropic(messages, tools)
	}
	return c.chatOpenAI(messages, tools)
}

func (c *GenericClient) chatOpenAI(messages []Message, tools []map[string]interface{}) (*Message, error) {
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s error (%d): %s", c.Provider, resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Choices) > 0 {
		return &result.Choices[0].Message, nil
	}
	return nil, fmt.Errorf("no response from %s", c.Provider)
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
		role := m.Role
		if role == "tool" {
			role = "user" // Tool results are 'user' role in Anthropic with tool_result content
		}

		content := []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": m.Content,
			},
		}

		if m.Role == "tool" {
			content = []interface{}{
				map[string]interface{}{
					"type":         "tool_result",
					"tool_use_id":  m.ToolID,
					"content":      m.Content,
				},
			}
		}

		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				content = append(content, map[string]interface{}{
					"type": "tool_use",
					"id":   tc.ID,
					"name": tc.Function.Name,
					"input": func() interface{} {
						var res interface{}
						json.Unmarshal([]byte(tc.Function.Arguments), &res)
						return res
					}(),
				})
			}
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
				"name":        t["name"],
				"description": t["description"],
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
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID      string `json:"id"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			ID    string `json:"id"`
			Name  string `json:"name"`
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
	"openai":     {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"anthropic":  {"ANTHROPIC_API_KEY", "https://api.anthropic.com/v1"},
	"openrouter": {"OPENROUTER_API_KEY", "https://openrouter.ai/api/v1"},
	"google":     {"GOOGLE_OAUTH_ACCESS_TOKEN", "https://generativelanguage.googleapis.com/v1beta/openai"},
	"zai":        {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"z.ai":       {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"zai-coding": {"ZAI_API_KEY", "https://api.z.ai/api/coding/paas/v4"},
	"chutes":     {"CHUTES_API_KEY", "https://api.chutes.ai/v1"},
	"chutes-coding": {"CHUTES_API_KEY", "https://api.chutes.ai/v1"}, // Placeholder if distinct endpoint exists
	"alibaba":    {"DASHSCOPE_API_KEY", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	"alibaba-coding": {"DASHSCOPE_API_KEY", "https://coding-intl.dashscope.aliyuncs.com/v1"},
	"moonshot":   {"MOONSHOT_API_KEY", "https://api.moonshot.cn/v1"},
	"minimax":    {"MINIMAX_API_KEY", "https://api.minimax.chat/v1"},
	"requesty":   {"REQUESTY_API_KEY", "https://router.requesty.ai/v1"},
	"302ai":      {"302AI_API_KEY", "https://api.302.ai/v1"},
	"deepseek":   {"DEEPSEEK_API_KEY", "https://api.deepseek.com/v1"},
	"groq":       {"GROQ_API_KEY", "https://api.groq.com/openai/v1"},
	"mistral":    {"MISTRAL_API_KEY", "https://api.mistral.ai/v1"},
}

func NewClient(cfg *config.Config, model string) LLMClient {
	provider := ""
	apiKey := ""
	baseURL := ""

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
		if baseURL == "" {
			baseURL = info.baseURL
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
	}
}
