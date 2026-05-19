package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
)

const maxFetchBytes = 2 * 1024 * 1024 // 2 MB

type WebFetchTool struct{}

func (t WebFetchTool) Name() string        { return "webfetch" }
func (t WebFetchTool) Description() string { return "Fetch the content of a URL and return it as LLM-optimized markdown" }
func (t WebFetchTool) Parallel() bool      { return true }
func (t WebFetchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "webfetch",
		"description": "Fetch the content of a URL and return it as LLM-optimized markdown. Preserves headings, links, lists, tables, and code blocks while stripping navigation, scripts, and boilerplate.",
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/plain") || strings.Contains(contentType, "application/json") {
		return string(body), nil
	}

	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
		),
	)

	markdown, err := conv.ConvertString(string(body))
	if err != nil {
		return t.stripHTML(string(body)), nil
	}

	// Truncate if too large for context window
	if len(markdown) > 50000 {
		markdown = markdown[:50000] + "\n\n... [content truncated]"
	}

	return markdown, nil
}

func (t WebFetchTool) stripHTML(html string) string {
	re := regexp.MustCompile("<[^>]*>")
	text := re.ReplaceAllString(html, " ")
	re = regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

type WebSearchTool struct{}

func (t WebSearchTool) Name() string        { return "websearch" }
func (t WebSearchTool) Description() string { return "Search the web using DuckDuckGo" }
func (t WebSearchTool) Parallel() bool      { return true }
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
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
