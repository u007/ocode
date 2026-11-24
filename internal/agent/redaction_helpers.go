package agent

import "github.com/u007/ocode/internal/redact"

// redactText applies tier-1 secret redaction to text using the session
// registry. It registers any discovered spans before substituting so later
// outputs can resolve the same OCSEC tokens consistently.
//
// Secrets-only by construction: it calls DetectSecrets, which has no PII
// input, so every judge/guard call site that uses it cannot mask PII. The
// PII-capable variant is redactWithPII.
func redactText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.DetectSecrets(text, nil, redact.DetectOpts{FileContent: false})
	if len(spans) == 0 {
		return text
	}
	for _, span := range spans {
		value := text[span.Start:span.End]
		reg.GetOrAssign(value, span.Kind, "agent")
	}
	return reg.Substitute(text)
}

// redactWithPII is redactText plus the opt-in PII groups. The error comes only
// from PII detection and is FATAL for its callers: no fallback to unmasked
// text, in either fail_mode.
func redactWithPII(text string, reg *redact.Registry, cats []redact.PIICategory) (string, error) {
	if reg == nil || text == "" {
		return text, nil
	}
	spans, err := redact.Detect(text, nil, redact.DetectOpts{FileContent: false, PIICategories: cats})
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return text, nil
	}
	for _, span := range spans {
		value := text[span.Start:span.End]
		reg.GetOrAssign(value, span.Kind, "agent")
	}
	return reg.Substitute(text), nil
}

// piiRedactForToolResult is redactWithPII in production. It is a var only so a
// test can force a PII detection failure and prove scanToolResult propagates it.
var piiRedactForToolResult = redactWithPII

// redactFileText is redactText in file mode: known secret formats only, no
// keyword/entropy heuristics, which misfire on source code and config files.
// Stays secrets-only — file content reaching a judge must never be PII-masked.
func redactFileText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.DetectSecrets(text, nil, redact.DetectOpts{FileContent: true})
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
