package redact

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Span represents a detected secret span in text.
type Span struct {
	Start int
	End   int
	Kind  string
}

// DetectOpts controls detector behavior.
type DetectOpts struct {
	// FileContent disables keyword+entropy heuristics and restricts
	// detection to known-format matches and custom words only.
	FileContent bool
}

// Known-format patterns compiled once at package init.
var (
	// AWS Access Key: AKIA... (20 uppercase alphanumeric chars)
	awsKeyRe = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

	// GitHub tokens: ghp_, gho_, ghb_, ghu_, ghs_, ghm_, ghc_ or github_pat_
	githubTokenRe = regexp.MustCompile(`(?:ghp_|gho_|ghb_|ghu_|ghs_|ghm_|ghc_|github_pat_)[a-zA-Z0-9]{36,}`)

	// Slack tokens: xoxb-, xoxa-, xoxp-, xoxr-, xoxs-, xoxv-
	slackTokenRe = regexp.MustCompile(`xox[abprsv]-[a-zA-Z0-9]{10,}`)

	// Stripe live keys: sk_live_, pk_live_, rk_live_
	stripeKeyRe = regexp.MustCompile(`(?:sk_live_|pk_live_|rk_live_)[a-zA-Z0-9]{24,}`)

	// JWT: three base64url segments separated by dots
	jwtRe = regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`)

	// OpenAI keys: sk-... (20+ chars after prefix)
	openAIKeyRe = regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)

	// Anthropic keys: sk-ant-...
	anthropicKeyRe = regexp.MustCompile(`sk-ant-[a-zA-Z0-9]{20,}`)

	// PEM private key blocks
	pemKeyRe = regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----[\s\S]*?-----END\s+(?:RSA\s+)?PRIVATE\s+KEY-----`)

	// URL with credentials: scheme://user:pass@host
	urlCredsRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/]+:[^\s@]+@[^\s]+`)

	// Git SHA (40 hex chars) - false positive guard
	gitSHARe = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

	// sha512- integrity hash
	sha512IntegrityRe = regexp.MustCompile(`sha512-[a-zA-Z0-9+/=]{80,}`)

	// Base64 image chunk
	base64ImageRe = regexp.MustCompile(`[a-zA-Z0-9+/]{100,}=`)
)

// keyword-adjacent entropy patterns (chat mode only)
var keywordRe *regexp.Regexp

func init() {
	keywords := []string{
		"password", "passwd", "secret", "token",
		"api_key", "apikey", "api-key",
		"authorization:", "bearer",
	}
	// Build pattern: look for any of the keywords followed by non-alphanumeric and then a high-entropy candidate
	kwPattern := `(?i)(` + strings.Join(keywords, "|") + `)\s*[:=]\s*["']?([a-zA-Z0-9_\-./+]{16,})["']?`
	keywordRe = regexp.MustCompile(kwPattern)
}

// Detect finds secret spans in text.
func Detect(text string, customWords []string, opts DetectOpts) []Span {
	var spans []Span

	// Known format detectors (both modes)
	addSpans(&spans, awsKeyRe.FindAllStringIndex(text, -1), "aws_key")
	addSpans(&spans, githubTokenRe.FindAllStringIndex(text, -1), "github_token")
	addSpans(&spans, slackTokenRe.FindAllStringIndex(text, -1), "slack_token")
	addSpans(&spans, stripeKeyRe.FindAllStringIndex(text, -1), "stripe_key")
	addSpans(&spans, jwtRe.FindAllStringIndex(text, -1), "jwt")
	addSpans(&spans, openAIKeyRe.FindAllStringIndex(text, -1), "openai_key")
	addSpans(&spans, anthropicKeyRe.FindAllStringIndex(text, -1), "anthropic_key")
	addSpans(&spans, pemKeyRe.FindAllStringIndex(text, -1), "pem_key")
	addSpans(&spans, urlCredsRe.FindAllStringIndex(text, -1), "url_credentials")

	// Custom words (both modes)
	for _, word := range customWords {
		if word == "" {
			continue
		}
		// Simple case-insensitive search
		lower := strings.ToLower(text)
		wordLower := strings.ToLower(word)
		start := 0
		for {
			idx := strings.Index(lower[start:], wordLower)
			if idx == -1 {
				break
			}
			absStart := start + idx
			absEnd := absStart + len(word)
			spans = append(spans, Span{Start: absStart, End: absEnd, Kind: "custom"})
			start = absEnd
		}
	}

	// Chat mode: keyword+entropy heuristics
	if !opts.FileContent {
		addSpansFromKeywordMatches(&spans, text, keywordRe.FindAllStringSubmatchIndex(text, -1))
	}

	// False positive filtering: remove spans that match known safe patterns
	spans = filterFalsePositives(spans, text)

	return spans
}

// addSpans appends spans derived from index pairs to the slice.
func addSpans(spans *[]Span, idxPairs [][]int, kind string) {
	for _, pair := range idxPairs {
		if len(pair) == 2 {
			*spans = append(*spans, Span{Start: pair[0], End: pair[1], Kind: kind})
		}
	}
}

// addSpansFromKeywordMatches appends spans for keyword-detected candidates.
func addSpansFromKeywordMatches(spans *[]Span, text string, matches [][]int) {
	for _, m := range matches {
		if len(m) < 6 {
			continue
		}
		// m[0]:m[1] = full match, m[2]:m[3] = keyword, m[4]:m[5] = candidate value
		keyword := text[m[2]:m[3]]
		candidate := text[m[4]:m[5]]

		// Check entropy threshold
		entropy := shannonEntropy(candidate)
		if entropy < 3.0 {
			continue
		}

		// Only the value part (capture group 2), not the full keyword=value span
		*spans = append(*spans, Span{Start: m[4], End: m[5], Kind: "keyword_entropy: " + keyword})
	}
}

// filterFalsePositives removes spans that are known-safe patterns.
func filterFalsePositives(spans []Span, text string) []Span {
	// Collect safe spans to exclude
	var safeSpans []Span

	// Git SHA
	for _, m := range gitSHARe.FindAllStringIndex(text, -1) {
		safeSpans = append(safeSpans, Span{Start: m[0], End: m[1], Kind: "git_sha"})
	}
	// sha512 integrity
	for _, m := range sha512IntegrityRe.FindAllStringIndex(text, -1) {
		safeSpans = append(safeSpans, Span{Start: m[0], End: m[1], Kind: "integrity_hash"})
	}
	// base64 image chunks
	for _, m := range base64ImageRe.FindAllStringIndex(text, -1) {
		safeSpans = append(safeSpans, Span{Start: m[0], End: m[1], Kind: "base64_image"})
	}

	if len(safeSpans) == 0 {
		return spans
	}

	// Filter out any span contained within a safe span
	var filtered []Span
	for _, s := range spans {
		excluded := false
		for _, safe := range safeSpans {
			if s.Start >= safe.Start && s.End <= safe.End {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// shannonEntropy calculates the Shannon entropy of a string.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := make(map[rune]float64)
	total := 0

	for _, r := range s {
		freq[r]++
		total++
	}

	var entropy float64
	for _, count := range freq {
		p := count / float64(total)
		entropy -= p * math.Log2(p)
	}

	// Normalize by max possible entropy for the observed runes
	// This better distinguishes high-entropy strings from low-entropy ones
	if len(freq) > 1 {
		maxEntropy := math.Log2(float64(total))
		if maxEntropy > 0 {
			entropy = entropy / maxEntropy * 8.0 // normalize to ~0-8 range
		}
	}

	// Adjust for string length: longer strings need higher raw entropy threshold
	// to be suspicious
	scale := 1.0
	if total >= 32 {
		// Longer strings need more varied characters
		scale = 1.0 + float64(total-32)/128.0
		if scale > 2.0 {
			scale = 2.0
		}
	}

	return entropy * float64(utf8.RuneCountInString(s))
}

// entropyThreshold returns the minimum entropy for a candidate of given length.
func entropyThreshold(candidate string) float64 {
	length := utf8.RuneCountInString(candidate)
	if length < 8 {
		return 4.0
	}
	if length < 16 {
		return 3.5
	}
	return 3.0
}