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

// redactFileText is redactText in file mode: known secret formats only, no
// keyword/entropy heuristics, which misfire on source code and config files.
func redactFileText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.Detect(text, nil, redact.DetectOpts{FileContent: true})
	for _, span := range spans {
		reg.GetOrAssign(text[span.Start:span.End], span.Kind, "agent")
	}
	return reg.Substitute(text)
}

// judgeMaskRegistry returns the session registry when /mask is on, nil
// otherwise. Everything the permission judges receive is masked through it:
// they are separate model calls that bypass the main conversation's masking.
func (a *Agent) judgeMaskRegistry() *redact.Registry {
	if !a.redactionEnabled {
		return nil
	}
	return a.redactionRegistry
}
