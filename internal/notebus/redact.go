package notebus

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/redact"
)

// RedactBody returns a scrubbed form of text with any
// detected secret spans replaced by [REDACTED:<kind>]. The
// function is the canonical adapter between the
// `redact.Detect` API and the bus's SetRedactor callback
// signature. It is exported so production code can wire
// it directly:
//
//	bus.SetRedactor(notebus.RedactBody)
//
// The function is pure and safe to call from the bus owner
// goroutine. It does no allocation beyond the output
// strings.Builder. Tier-1 only (regex + keyword + entropy);
// tier-2 LLM scanning is not invoked here — it is too
// expensive to run per-append and is reserved for full-
// document reviews.
func RedactBody(text string) string {
	if text == "" {
		return text
	}
	spans := redact.Detect(text, nil, redact.DetectOpts{FileContent: true})
	if len(spans) == 0 {
		return text
	}
	// Sort by start (defensive — redact.Detect should
	// already return sorted spans, but the API does not
	// guarantee it).
	sortSpans(spans)
	var b strings.Builder
	b.Grow(len(text))
	cursor := 0
	for _, s := range spans {
		if s.Start < cursor {
			// Overlapping; skip the latter span.
			continue
		}
		b.WriteString(text[cursor:s.Start])
		fmt.Fprintf(&b, "[REDACTED:%s]", s.Kind)
		cursor = s.End
	}
	b.WriteString(text[cursor:])
	return b.String()
}

func sortSpans(spans []redact.Span) {
	// Insertion sort: spans are usually <10 in count.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j-1].Start > spans[j].Start; j-- {
			spans[j-1], spans[j] = spans[j], spans[j-1]
		}
	}
}
