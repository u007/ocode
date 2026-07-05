package ocr

import "strings"

// LooksLikeLMStudioBaseURL reports whether a base URL points at a local
// LM Studio server. An empty URL is treated as LM Studio because the
// openai-compat backend defaults to http://localhost:1234/v1.
func LooksLikeLMStudioBaseURL(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	lower := strings.ToLower(baseURL)
	return strings.Contains(lower, "lmstudio") ||
		strings.Contains(lower, "localhost:1234") ||
		strings.Contains(lower, "127.0.0.1:1234")
}
