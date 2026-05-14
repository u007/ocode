package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type WebFetchTool struct{}

func (t WebFetchTool) Name() string        { return "webfetch" }
func (t WebFetchTool) Description() string { return "Fetch the content of a URL" }
func (t WebFetchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "webfetch",
		"description": "Fetch the content of a URL and return it as text",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch",
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

	return t.stripHTML(string(body)), nil
}

func (t WebFetchTool) stripHTML(html string) string {
	// Simple regex-based HTML stripping for token efficiency
	re := regexp.MustCompile("<[^>]*>")
	text := re.ReplaceAllString(html, " ")

	// Collapse whitespace
	re = regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

type WebSearchTool struct{}

func (t WebSearchTool) Name() string        { return "websearch" }
func (t WebSearchTool) Description() string { return "Search the web using DuckDuckGo" }
func (t WebSearchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "websearch",
		"description": "Search the web and return snippets and URLs",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
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

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(params.Query))
	resp, err := http.Get(searchURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Very basic extraction of snippets/links from DDG HTML
	text := t.stripHTML(string(body))
	if len(text) > 2000 {
		text = text[:2000] + "..."
	}

	return text, nil
}

func (t WebSearchTool) stripHTML(html string) string {
	re := regexp.MustCompile("<[^>]*>")
	text := re.ReplaceAllString(html, " ")
	re = regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
