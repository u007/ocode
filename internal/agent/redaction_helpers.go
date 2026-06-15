package agent

import "github.com/u007/ocode/internal/redact"

// redactText applies tier-1 redaction to text using the session registry.
// It registers any discovered spans before substituting so later outputs can
// resolve the same OCSEC tokens consistently.
func redactText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.Detect(text, nil, redact.DetectOpts{FileContent: false})
	if len(spans) == 0 {
		return text
	}
	for _, span := range spans {
		value := text[span.Start:span.End]
		reg.GetOrAssign(value, span.Kind, "agent")
	}
	return reg.Substitute(text)
}
