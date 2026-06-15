package redact

import (
	"fmt"
	"strings"
)

// ScanAndMask scans the given content using the provided Scanner, registers
// any newly identified secrets into the Registry, and substitutes all registered
// values in-place. Returns the masked content.
//
// This is the single entry-point for LLM-based scanning. It ensures that every
// finding is registered AND substituted immediately, never relying on the
// tier-1 safety net's Detect pass (which may not match novel secrets).
//
// The caller is responsible for passing the already-tier-1-masked content so
// the Scanner only sees previously-registered tokens (not raw secrets).
func ScanAndMask(content string, s Scanner, reg *Registry) (string, error) {
	if s == nil || reg == nil {
		return content, nil
	}
	if strings.TrimSpace(content) == "" {
		return content, nil
	}

	spans, err := s.Scan(content)
	if err != nil {
		return content, fmt.Errorf("ScanAndMask: scanner error: %w", err)
	}

	for _, span := range spans {
		if span.Start < 0 || span.End > len(content) || span.Start >= span.End {
			continue
		}
		val := content[span.Start:span.End]
		// Skip values that are already registered as tokens.
		if TokenPattern.MatchString(val) {
			continue
		}
		reg.GetOrAssign(val, span.Kind, "scanner")
	}

	// Substitute all registered values — covers both the new findings
	// and any previously-registered secrets.
	return reg.Substitute(content), nil
}
