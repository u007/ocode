package redact

import (
	"os"
	"time"
)

// Scanner is the interface for tier-2 local model scanning.
type Scanner interface {
	Scan(maskedText string) ([]Span, error)
}

// RedactorConfig holds the redaction configuration.
type RedactorConfig struct {
	Enabled    bool
	Model      string
	FailMode   string // "block" or "warn"
	CustomWords []string
}

// Redactor is the main facade for secret redaction.
type Redactor struct {
	registry *Registry
	config   RedactorConfig
	vault    func() string // returns vault file path
	scanner  Scanner
}

// NewRedactor creates a new Redactor with the given config.
func NewRedactor(config RedactorConfig, vaultPath string, scanner Scanner) *Redactor {
	return &Redactor{
		config:   config,
		vault: func() string {
			return vaultPath
		},
		scanner: scanner,
	}
}

// Enabled returns whether redaction is enabled.
func (r *Redactor) Enabled() bool {
	return r.config.Enabled
}

// SetEnabled enables or disables redaction at runtime.
func (r *Redactor) SetEnabled(enabled bool) {
	r.config.Enabled = enabled
}

// SetRegistry sets the registry (e.g., loaded from vault).
func (r *Redactor) SetRegistry(reg *Registry) {
	r.registry = reg
}

// Registry returns the current registry.
func (r *Redactor) Registry() *Registry {
	return r.registry
}

// RedactChat performs chat-mode redaction: detect + register + substitute.
// Vault is persisted before returning to ensure crash-safety.
func (r *Redactor) RedactChat(text string) (string, error) {
	if !r.config.Enabled || r.registry == nil {
		return text, nil
	}

	// Tier-1: known format + keyword/entropy detection
	spans := Detect(text, r.config.CustomWords, DetectOpts{FileContent: false})

	// Register detected secrets
	for _, span := range spans {
		value := text[span.Start:span.End]
		r.registry.GetOrAssign(value, span.Kind, "chat")
	}

	// Substitute all registered values
	masked := r.registry.Substitute(text)

	// Tier-2: local model scan (if scanner is configured)
	var err error
	if r.scanner != nil {
		scannerSpans, scanErr := r.scanner.Scan(masked)
		if scanErr != nil {
			err = ErrScannerUnavailable
			// Still return tier-1 masked text
			return masked, err
		}
		// Register scanner-found secrets (they're in masked text, so we need
		// to find the actual values in the original text)
		for _, span := range scannerSpans {
			// The scanner returns spans in the masked text; we need the original values
			// For now, skip if the span is already masked
			if !TokenPattern.MatchString(masked[span.Start:span.End]) {
				value := masked[span.Start:span.End]
				r.registry.GetOrAssign(value, "model", "scanner")
			}
		}
		// Re-substitute with any new entries
		masked = r.registry.Substitute(text)
	}

	// Persist vault before returning (ordering invariant)
	if err := r.persistVault(); err != nil {
		return "", err
	}

	return masked, nil
}

// RedactFile performs file-mode redaction: detect known formats only + register + substitute.
func (r *Redactor) RedactFile(text string) (string, error) {
	if !r.config.Enabled || r.registry == nil {
		return text, nil
	}

	// Tier-1: file mode (no keyword/entropy heuristics)
	spans := Detect(text, r.config.CustomWords, DetectOpts{FileContent: true})

	// Register detected secrets
	for _, span := range spans {
		value := text[span.Start:span.End]
		r.registry.GetOrAssign(value, span.Kind, "file")
	}

	// Substitute all registered values
	masked := r.registry.Substitute(text)

	// Persist vault before returning
	if err := r.persistVault(); err != nil {
		return "", err
	}

	return masked, nil
}

// Render resolves tokens back to real values for display.
func (r *Redactor) Render(text string) string {
	if r.registry == nil {
		return text
	}
	return r.registry.Resolve(text)
}

// persistVault saves the current registry to disk.
func (r *Redactor) persistVault() error {
	path := r.vault()
	if path == "" {
		return nil
	}
	return SaveVault(path, r.registry)
}

// ErrScannerUnavailable is returned when the tier-2 scanner fails.
var ErrScannerUnavailable = ScannerError{}

// ScannerError wraps scanner errors.
type ScannerError struct {
	Err error
}

func (e ScannerError) Error() string {
	if e.Err != nil {
		return "scanner unavailable: " + e.Err.Error()
	}
	return "scanner unavailable"
}

func (e ScannerError) Unwrap() error {
	return e.Err
}

// IsScannerUnavailable checks if an error is ErrScannerUnavailable.
func IsScannerUnavailable(err error) bool {
	_, ok := err.(ScannerError)
	if ok {
		return true
	}
	// Also check pointer type
	_, ok = err.(*ScannerError)
	return ok
}

// SecretRef represents a secret used in a tool call.
type SecretRef struct {
	Index  int
	Kind   string
	Value  string // masked preview
}

// MaskedPreview returns a masked preview of a value (first 3 + last 2 chars).
func MaskedPreview(value string) string {
	if len(value) < 8 {
		// Too short - mask with just asterisks
		return "***"
	}
	return string([]byte(value)[:3]) + "***" + string([]byte(value[len(value)-2:]))
}

// Init initializes the redactor with a fresh nonce.
func (r *Redactor) Init() error {
	r.registry = NewRegistry(NewNonce())
	return r.persistVault()
}

// InitFromVault loads an existing vault or initializes fresh.
func InitFromVault(vaultPath string) (*Registry, error) {
	reg, err := LoadVault(vaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh vault
			return NewRegistry(NewNonce()), nil
		}
		return nil, err
	}
	return reg, nil
}

// NewSessionRedactor creates a redactor for a new or existing session.
func NewSessionRedactor(cfg RedactorConfig, sessionID, projectSlug, vaultBase string) (*Redactor, error) {
	vaultPath := VaultPath(vaultBase, projectSlug, sessionID)
	reg, err := InitFromVault(vaultPath)
	if err != nil {
		return nil, err
	}

	r := NewRedactor(cfg, vaultPath, nil)
	r.SetRegistry(reg)
	return r, nil
}

// Placeholder: time.Now() will be used for FirstSeenAt
var timeNow = time.Now
