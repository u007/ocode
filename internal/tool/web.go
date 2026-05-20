package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
)

const maxFetchBytes = 2 * 1024 * 1024 // 2 MB
const webFetchTimeout = 30 * time.Second

var webFetchClient = &http.Client{
	Timeout: webFetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("stopped after 5 redirects")
		}
		return nil
	},
}

type WebFetchTool struct{}

func (t WebFetchTool) Name() string        { return "webfetch" }
func (t WebFetchTool) Description() string { return "Fetch the content of a URL and return it as LLM-optimized markdown" }
func (t WebFetchTool) Parallel() bool      { return true }
func (t WebFetchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "webfetch",
		"description": "Fetch the content of a URL and return it as LLM-optimized markdown. Preserves headings, links, lists, tables, and code blocks while stripping navigation, scripts, and boilerplate. HTTP URLs are upgraded to HTTPS. Redirects are followed up to 5 hops.",
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

	fetchURL := params.URL
	if strings.HasPrefix(fetchURL, "http://") {
		fetchURL = "https://" + strings.TrimPrefix(fetchURL, "http://")
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", fetchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Ocode-WebFetch/1.0")
	req.Header.Set("Accept", "text/markdown, text/html, */*;q=0.8")

	resp, err := webFetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, fetchURL)
	}

	finalURL := resp.Request.URL.String()
	if finalURL != fetchURL {
		return fmt.Sprintf("URL %s redirected to %s. Fetch the new URL to get the content.", fetchURL, finalURL), nil
	}

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
		"description": "Search the web and return results with titles, URLs, and snippets. Follow up with webfetch to read a specific page.",
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
	req, err := http.NewRequestWithContext(context.Background(), "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Ocode-WebSearch/1.0")

	resp, err := webFetchClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", err
	}

	results := extractDDGResults(string(body))
	if len(results) == 0 {
		return "No search results found.", nil
	}

	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			b.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
	}
	return b.String(), nil
}

type ddgResult struct {
	Title   string
	URL     string
	Snippet string
}

func extractDDGResults(html string) []ddgResult {
	var results []ddgResult

	re := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(html, -1)

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		rawURL := m[1]
		title := stripHTMLInline(m[2])

		if len(results) >= 10 {
			break
		}

		results = append(results, ddgResult{
			Title: title,
			URL:   rawURL,
		})
	}

	snippetRe := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	snippets := snippetRe.FindAllStringSubmatch(html, -1)
	for i := 0; i < len(results) && i < len(snippets); i++ {
		if len(snippets[i]) > 1 {
			results[i].Snippet = stripHTMLInline(snippets[i][1])
		}
	}

	return results
}

func stripHTMLInline(html string) string {
	re := regexp.MustCompile("<[^>]*>")
	text := re.ReplaceAllString(html, "")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#x27;", "'")
	text = strings.ReplaceAll(text, "&#x2F;", "/")
	return strings.TrimSpace(text)
}
