package notebus

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/redact"
)

// RedactBody returns a scrubbed form of text with any
// detected secret spans replaced by [REDACTED:<kind>]. The
// function is the canonical adapter between the
// `redact.DetectSecrets` API and the bus's SetRedactor
// callback signature. It is exported so production code can
// wire it directly:
//
//	bus.SetRedactor(notebus.RedactBody)
//
// The function is pure and safe to call from the bus owner
// goroutine. It does no allocation beyond the output
// strings.Builder. Tier-1 only (regex + keyword + entropy);
// tier-2 LLM scanning is not invoked here — it is too
// expensive to run per-append and is reserved for full-
// document reviews. Secrets-only by construction: the PII
// variant is RedactBodyWith.
func RedactBody(text string) string {
	return renderRedacted(text, redact.DetectSecrets(text, nil, redact.DetectOpts{FileContent: true}))
}

// RedactBodyWith is RedactBody plus the opt-in PII groups. With no categories
// it is exactly RedactBody. The error comes only from PII detection and the
// caller MUST fail closed (the bus has no registry, so it cannot fall back to
// tokenizing anything).
func RedactBodyWith(text string, cats []redact.PIICategory) (string, error) {
	if text == "" {
		return text, nil
	}
	spans, err := redact.Detect(text, nil, redact.DetectOpts{FileContent: true, PIICategories: cats})
	if err != nil {
		return "", err
	}
	return renderRedacted(text, spans), nil
}

// renderRedacted substitutes [REDACTED:<kind>] for every span in text.
func renderRedacted(text string, spans []redact.Span) string {
	if len(spans) == 0 {
		return text
	}
	// Sort by start (defensive — the detector should
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
