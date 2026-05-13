package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type WebFetchTool struct{}

func (t WebFetchTool) Name() string        { return "webfetch" }
func (t WebFetchTool) Description() string { return "Fetch web content" }
func (t WebFetchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "webfetch",
		"description": "Fetch web content from a URL",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t WebFetchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	resp, err := http.Get(params.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

type WebSearchTool struct{}

func (t WebSearchTool) Name() string        { return "websearch" }
func (t WebSearchTool) Description() string { return "Search the web for information" }
func (t WebSearchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "websearch",
		"description": "Search the web for information",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t WebSearchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Use DDG html search for a functional free version
	url := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", params.Query)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read search response: %w", err)
	}

	return string(body), nil
}
