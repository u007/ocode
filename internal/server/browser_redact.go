package server

import (
	"net/url"
	"regexp"
	"strings"
)

// browserURLPattern finds http(s) URLs inside page text: console messages and
// exception text, which the page writes. It is broad on purpose: it stops only at
// whitespace, quotes, angle brackets, parentheses, brackets, braces and backticks,
// so a URL is redacted rather than missed.
var browserURLPattern = regexp.MustCompile("https?://[^\\s\"'<>()\\[\\]{}`]+")

// redactBrowserURL keeps a URL's scheme, host and path. A query or fragment can
// carry a token or a session, so each is replaced by a marker that shows it was
// elided. Userinfo is dropped with the host it sits in front of, because only
// the host is kept. A URL with no host (data:, blob:) keeps only its scheme. A
// URL that does not parse is replaced whole.
func redactBrowserURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable URL redacted]"
	}
	if u.Host == "" {
		return u.Scheme + ":…"
	}
	out := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?…"
	}
	if u.Fragment != "" {
		out += "#…"
	}
	return out
}

// redactBrowserURLsInText redacts every http(s) URL in s, leaving the rest of the
// text as it is. Sentence punctuation the pattern swept up after a URL ("...?x=1,")
// is kept, so only the URL itself changes.
func redactBrowserURLsInText(s string) string {
	return browserURLPattern.ReplaceAllStringFunc(s, func(m string) string {
		core := strings.TrimRight(m, ".,;:!?")
		return redactBrowserURL(core) + m[len(core):]
	})
}
