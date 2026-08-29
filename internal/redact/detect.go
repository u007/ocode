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
	// FileContent disables the generic keyword+entropy heuristic (which is
	// prone to false positives on arbitrary file content) and restricts that
	// family to known-format matches and custom words only. Assignment-style
	// env-var detection (addSpansFromEnvMatches) runs in BOTH modes, because
	// config files (.env, YAML) are a primary secret-hiding spot and the
	// detection is name-gated (low false-positive).
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

	// Env-var / assignment-style secrets (both modes). Line-anchored so the
	// match is an actual `KEY = value` assignment, not a substring inside prose.
	// Group 1 = variable name. The value is captured without its surrounding
	// quotes (so quotes survive in the redacted output) via one of three
	// alternatives: double-quoted inner (group 2), single-quoted inner (group 3),
	// or an unquoted run (group 4) that stops at whitespace — so a trailing
	// `# comment` is excluded and the value can contain `:@/#$%=+` and env
	// interpolation (`${OTHER}`). Unquoted values need >=4 chars to align with the
	// strong-name minimum; quoted values may be any non-empty length.
	envAssignRe = regexp.MustCompile(`(?im)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*[:=]\s*(?:"([^"]*)"|'([^']*)'|([^\s"']{4,}))`)
)

// envSecretTokens are trailing underscore-delimited name segments that mark a
// variable as a secret. A name matches if it equals a token exactly OR ends in
// `_<TOKEN>` (so `MONKEY` does NOT match `KEY`, but `ENCRYPTION_KEY` and
// `AWS_SECRET_ACCESS_KEY` do). This mirrors the suffix families already
// recognized by gate.sensitivePrefixRe so tier-1 redaction aligns with the
// tier-2 scan trigger.
var envSecretTokens = []string{
	"SECRET", "SECRET_KEY", "PRIVATE", "PRIVATE_KEY", "PASSWORD", "PASSWD",
	"PWD", "PASSPHRASE", "TOKEN", "CREDENTIAL", "CREDENTIALS", "ENCRYPTION",
	"ENCRYPTION_KEY", "AUTH", "AUTH_TOKEN", "AUTH_KEY", "CLIENT_SECRET",
	"SESSION", "SESSION_SECRET", "ACCESS_TOKEN", "ACCESS_KEY", "API_KEY",
	"APIKEY", "KEY", "SALT",
}

// envSecretWeakTokens are trailing segments that may hold a secret but also
// commonly hold low-entropy identifiers (PROJECT_ID, ACCOUNT_ID, CLIENT_ID).
// These are only redacted when the value is high-entropy, to avoid masking
// ordinary identifiers.
var envSecretWeakTokens = []string{
	"ID",
}

// isEnvSecretName reports whether a variable name looks secret. matched is true
// when the (uppercased) name equals a token or ends in `_<TOKEN>`; strong is
// true for the high-confidence tokens (redact regardless of value entropy).
func isEnvSecretName(name string) (matched, strong bool) {
	u := strings.ToUpper(name)
	for _, tok := range envSecretWeakTokens {
		if u == tok || strings.HasSuffix(u, "_"+tok) {
			return true, false
		}
	}
	for _, tok := range envSecretTokens {
		if u == tok || strings.HasSuffix(u, "_"+tok) {
			return true, true
		}
	}
	return false, false
}

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

	// Assignment-style env-var secrets (both modes)
	addSpansFromEnvMatches(&spans, text, envAssignRe.FindAllStringSubmatchIndex(text, -1))

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

// addSpansFromEnvMatches appends spans for env-var/assignment-detected secrets.
// The variable name is gated by isEnvSecretName; the *value* is what gets
// redacted. The value arrives in one of three capture groups (see envAssignRe):
// group 2 = double-quoted inner, group 3 = single-quoted inner, group 4 =
// unquoted run. Strong names (e.g. *_SECRET, *_KEY, *_TOKEN) are redacted
// whenever the value is non-trivial (len >= 4). Weak names (e.g. *_ID) are only
// redacted when the value is high-entropy, to avoid masking ordinary identifiers
// like PROJECT_ID / ACCOUNT_ID / CLIENT_ID.
func addSpansFromEnvMatches(spans *[]Span, text string, matches [][]int) {
	for _, m := range matches {
		if len(m) < 10 {
			continue
		}
		// m[0]:m[1] = full match, m[2]:m[3] = name, value in groups 2/3/4.
		name := text[m[2]:m[3]]

		// Pick the value group that actually matched (others are -1).
		valStart, valEnd := -1, -1
		switch {
		case m[4] >= 0 && m[5] >= 0:
			valStart, valEnd = m[4], m[5] // double-quoted inner
		case m[6] >= 0 && m[7] >= 0:
			valStart, valEnd = m[6], m[7] // single-quoted inner
		case m[8] >= 0 && m[9] >= 0:
			valStart, valEnd = m[8], m[9] // unquoted run
		}
		if valStart < 0 {
			continue
		}
		value := text[valStart:valEnd]

		matched, strong := isEnvSecretName(name)
		if !matched {
			continue
		}
		if strong {
			if len(value) < 4 {
				continue
			}
		} else {
			// Weak name (e.g. *_ID): only redact genuinely secret-looking
			// values. Require length, high entropy, AND at least one letter so
			// purely-numeric identifiers (CLIENT_ID=1234567890,
			// PROJECT_ID=prod) are not masked — numeric strings have entropy
			// ~3.32 which would otherwise clear the entropy bar.
			if len(value) < 12 || perCharEntropy(value) < 3.0 || !hasLetter(value) {
				continue
			}
		}

		*spans = append(*spans, Span{Start: valStart, End: valEnd, Kind: "env_secret: " + name})
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

	// Filter out any span contained within a safe span. Name-gated
	// env-var assignment spans (env_secret: *) are never discarded: a long
	// base64/hex secret assigned to e.g. ENCRYPTION_KEY can coincidentally
	// resemble a Git SHA or base64 image chunk, but the name gate already
	// validated it as a secret, so the safe-pattern guard must not override it.
	var filtered []Span
	for _, s := range spans {
		if strings.HasPrefix(s.Kind, "env_secret:") {
			filtered = append(filtered, s)
			continue
		}
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

// perCharEntropy returns the Shannon entropy per character of s (0-8 range for
// the observed alphabet), independent of string length. Used by the env-var
// detector's weak-name gate, where the length-scaled shannonEntropy would make
// any multi-character string clear a fixed threshold.
func perCharEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := make(map[rune]float64)
	for _, r := range s {
		freq[r]++
	}
	var e float64
	n := float64(len(s))
	for _, c := range freq {
		p := c / n
		e -= p * math.Log2(p)
	}
	return e
}

// hasLetter reports whether s contains at least one ASCII letter. Used to keep
// purely-numeric identifiers (CLIENT_ID=1234567890) from being masked by the
// weak-name gate.
func hasLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
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
