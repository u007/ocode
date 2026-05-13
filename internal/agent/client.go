package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

type OpenAIClient struct {
	APIKey string
	Model  string
}

func (c *OpenAIClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	url := "https://api.openai.com/v1/chat/completions"

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
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai error (%d): %s", resp.StatusCode, string(body))
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

	return nil, fmt.Errorf("no response from openai")
}

func NewClient(provider string, model string) LLMClient {
	// Simple factory based on provider or model prefix
	if provider == "openai" || (provider == "" && len(model) >= 3 && model[:3] == "gpt") {
		return &OpenAIClient{
			APIKey: os.Getenv("OPENAI_API_KEY"),
			Model:  model,
		}
	}

	// Default/fallback (could implement Anthropic here too)
	return nil
}
