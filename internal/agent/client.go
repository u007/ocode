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
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
)

const (
	llmRequestTimeout = 5 * time.Minute
	llmMaxRetries     = 3
)

var llmHTTPClient = &http.Client{Timeout: llmRequestTimeout}
var llmRetryBaseDelay = 500 * time.Millisecond

type Message struct {
	Role             string      `json:"role"`
	Content          string      `json:"content"`
	Images           []Image     `json:"images,omitempty"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	ToolID           string      `json:"tool_call_id,omitempty"`
	Model            string      `json:"-"`
	Usage            *TokenUsage `json:"-"`
	Spend            *float64    `json:"-"`
}

type Image struct {
	Path     string `json:"path,omitempty"`
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
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
	var lastErr error
	for attempt := 0; attempt <= llmMaxRetries; attempt++ {
		var (
			msg *Message
			err error
		)
		if c.Provider == "anthropic" {
			msg, err = c.chatAnthropic(messages, tools)
		} else if c.Provider == "copilot" {
			msg, err = c.chatCopilot(messages, tools)
		} else {
			msg, err = c.chatOpenAI(messages, tools)
		}
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if !isRetryableLLMClientError(err) || attempt == llmMaxRetries {
			break
		}
		time.Sleep(time.Duration(attempt+1) * llmRetryBaseDelay)
	}
	return nil, fmt.Errorf("llm request failed after %d attempt(s): %w", llmMaxRetries+1, lastErr)
}

func isRetryableLLMClientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
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
	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
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

	openAIMessages, err := c.convertToOpenAIMessages(messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"model":    c.Model,
		"messages": openAIMessages,
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
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

func (c *GenericClient) convertToOpenAIMessages(messages []Message) ([]map[string]interface{}, error) {
	var result []map[string]interface{}
	for _, m := range messages {
		if m.Role == "tool" {
			result = append(result, map[string]interface{}{
				"role":         "tool",
				"content":      m.Content,
				"tool_call_id": m.ToolID,
			})
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

		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.Role == "assistant" && m.ReasoningContent != "" {
			msg["reasoning_content"] = m.ReasoningContent
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				calls = append(calls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				})
			}
			msg["tool_calls"] = calls
		}
		result = append(result, msg)
	}
	return result, nil
}

func (c *GenericClient) buildOpenAIContentWithImages(m Message) ([]map[string]interface{}, error) {
	var content []map[string]interface{}
	if len(m.Images) > 0 {
		if m.Content != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": m.Content,
			})
		}
		for _, img := range m.Images {
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
				img, err := NewImage(filePath)
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
func (c *GenericClient) chatOpenAIResponses(messages []Message, tools []map[string]interface{}) (*Message, error) {
	accountID := jwtClaim(c.APIKey, "https://api.openai.com/auth", "chatgpt_account_id")

	// Map messages → Responses API input items.
	instructions := make([]string, 0, 1)
	input := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			instructions = append(instructions, m.Content)
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
			for _, img := range m.Images {
				parts = append(parts, map[string]interface{}{
					"type":      "input_image",
					"image_url": "data:" + img.MIMEType + ";base64," + img.Data,
				})
			}
			content = parts
		}
		item := map[string]interface{}{"role": m.Role, "content": content}
		input = append(input, item)
	}

	payload := map[string]interface{}{
		"model":        c.Model,
		"instructions": strings.Join(instructions, "\n\n"),
		"input":        input,
		"store":        false,
		"stream":       true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := c.openAIResponsesURL()
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

	// Parse SSE stream to accumulate the full response.
	var fullText string
	var resultModel string
	var lastEvent string
	scanner := bufio.NewScanner(resp.Body)
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
			Type  string `json:"type"`
			Model string `json:"model"`
			Delta string `json:"delta"`
			Text  string `json:"text"`
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
		case "response.output_text.done":
			fullText += payload.Text
		case "response.completed":
			if payload.Model != "" {
				resultModel = payload.Model
			}
		}
	}

	if fullText == "" {
		return nil, fmt.Errorf("no response from openai responses api")
	}

	msg := &Message{
		Role:    "assistant",
		Content: fullText,
		Model:   resultModel,
	}
	if msg.Model == "" {
		msg.Model = c.Model
	}
	return msg, nil
}

func (c *GenericClient) openAIResponsesURL() string {
	if c.UseOAuth && c.Provider == "openai" {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return strings.TrimRight(c.BaseURL, "/") + "/responses"
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
		role := m.Role
		if role == "tool" {
			role = "user"
		}

		var content []interface{}

		if m.Role == "tool" {
			content = []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": m.ToolID,
					"content":     m.Content,
				},
			}
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
						"text": m.Content,
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
				img, err := NewImage(filePath)
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

var providers = map[string]providerInfo{
	"openai":         {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"anthropic":      {"ANTHROPIC_API_KEY", "https://api.anthropic.com/v1"},
	"openrouter":     {"OPENROUTER_API_KEY", "https://openrouter.ai/api/v1"},
	"google":         {"GOOGLE_API_KEY", "https://generativelanguage.googleapis.com/v1beta/openai"},
	"zai":            {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"z.ai":           {"ZAI_API_KEY", "https://api.z.ai/v1"},
	"zai-coding":     {"ZAI_API_KEY", "https://api.z.ai/api/coding/paas/v4"},
	"chutes":         {"CHUTES_API_KEY", "https://llm.chutes.ai/v1"},
	"chutes-coding":  {"CHUTES_API_KEY", "https://llm.chutes.ai/v1"}, // Placeholder if distinct endpoint exists
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
	"opencode":       {"OPENCODE_API_KEY", "https://opencode.ai/zen/v1"},
	"opencode-go":    {"OPENCODE_API_KEY", "https://opencode.ai/zen/go/v1"},
	"copilot":        {"GITHUB_COPILOT_TOKEN", "https://api.githubcopilot.com"},
}

func NewClient(cfg *config.Config, model string) LLMClient {
	provider := ""
	apiKey := ""
	baseURL := ""
	useOAuth := false

	// Handle provider/model (opencode) and provider:model formats.
	if parts := strings.SplitN(model, ":", 2); len(parts) == 2 {
		provider = parts[0]
		model = parts[1]
	} else if parts := strings.SplitN(model, "/", 2); len(parts) == 2 {
		if _, ok := providers[parts[0]]; ok {
			provider = parts[0]
			model = parts[1]
		}
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
