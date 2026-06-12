package redact

// NetHook provides the chokepoint safety net for secret redaction.
// It scans all message contents before sending to the LLM and redacts
// known-format secrets.
type NetHook struct {
	Registry *Registry
	Enabled  bool
	// OnTripwire is called once per session when a high-confidence secret
	// is detected but redaction is disabled.
	OnTripwire func(kinds []string)
	// tripwireFired tracks if the tripwire has already fired this session
	tripwireFired bool
}

// ScanText scans a single text string and returns it with known-format secrets redacted.
func (nh *NetHook) ScanText(text string) string {
	if nh == nil || !nh.Enabled || nh.Registry == nil {
		return text
	}
	return nh.redactKnownFormats(text)
}

// redactKnownFormats applies known-format detection and substitution.
func (nh *NetHook) redactKnownFormats(text string) string {
	// Only detect known formats (not keyword/entropy) - system prompts are file-like
	spans := Detect(text, nil, DetectOpts{FileContent: true})

	if len(spans) == 0 {
		return text
	}

	// Register and substitute
	for _, span := range spans {
		value := text[span.Start:span.End]
		nh.Registry.GetOrAssign(value, span.Kind, "net")
	}

	return nh.Registry.Substitute(text)
}

// FireTripwire fires the tripwire callback if not already fired.
func (nh *NetHook) FireTripwire(kinds []string) {
	if nh == nil || nh.tripwireFired || nh.OnTripwire == nil {
		return
	}
	nh.tripwireFired = true
	nh.OnTripwire(kinds)
}

// ResetTripwire allows the tripwire to fire again (for testing).
func (nh *NetHook) ResetTripwire() {
	if nh != nil {
		nh.tripwireFired = false
	}
}

// NetHookDisabled returns a disabled NetHook (no-op).
func NetHookDisabled() *NetHook {
	return &NetHook{Enabled: false}
}

// NetHookEnabled returns an enabled NetHook with the given registry.
func NetHookEnabled(reg *Registry) *NetHook {
	return &NetHook{
		Registry: reg,
		Enabled:  true,
	}
}
