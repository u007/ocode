package redact

import (
	"regexp"
)

// sensitiveKeywordRe matches sensitive keywords at a word boundary,
// case-insensitively. Keywords: token, pass, password, secret, api_key, api-key, apikey.
var sensitiveKeywordRe = regexp.MustCompile(
	`(?i)\b(?:` +
		`token|pass(?:word)?|secret|api[_-]?key` +
		`)\b`,
)

// sensitivePrefixRe matches env-var-style prefixes like AWS_, OPENAI_, *_TOKEN, etc.
// at a word boundary, case-insensitively.
var sensitivePrefixRe = regexp.MustCompile(
	`(?i)(?:` +
		`(?:AWS|ANTHROPIC|GEMINI|OPENAI)_\w+` + // concrete prefixes
		`|` +
		`\w+(?:_API_KEY|_TOKEN|_SECRET)` + // generic suffix patterns
		`)`,
)

// WarrantsLLMScan returns true when the text contains either a known secret
// value pattern (matched by QuickScan) or a sensitive keyword/prefix that
// suggests the message may contain secrets worth an LLM tier-2 scan.
//
// Used by the "lenient" redaction mode to decide whether to invoke the
// expensive LLM scanner. In "full" mode the scanner runs regardless.
func WarrantsLLMScan(text string) bool {
	if text == "" {
		return false
	}
	// Fast path: known value patterns (AKIA..., sk-..., ghp_..., etc.)
	if QuickScan(text) {
		return true
	}
	// Keyword/prefix check: case-insensitive word-boundary match
	return sensitiveKeywordRe.MatchString(text) || sensitivePrefixRe.MatchString(text)
}
