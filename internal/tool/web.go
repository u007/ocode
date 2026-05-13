package tool

import (
	"encoding/json"
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
	// For now, this requires an external API or a crawler
	// Return a placeholder message
	return "Web search is not yet fully implemented in this clone.", nil
}
