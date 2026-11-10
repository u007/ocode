package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestHTRRoutingPromptGatedOnConfig pins that the fragment follows the
// htr_enabled config flag and is a SYSTEM message (it sits in the cached
// prefix). The true case runs with no token and no daemon, matching the
// user's requirement that the prompt is present even when htrcli is
// unavailable.
func TestHTRRoutingPromptGatedOnConfig(t *testing.T) {
	resetPromptOverrides(t, nil)

	ag := &Agent{}
	ag.config = &config.Config{Ocode: config.OcodeConfig{Browser: config.BrowserConfig{HTREnabled: true}}}
	msgs := ag.BasePromptMessages()
	if !containsPromptMarker(msgs, promptHTREnabledMarker) {
		t.Fatalf("expected the %s fragment when htr_enabled is true", promptHTREnabledMarker)
	}
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, promptHTREnabledMarker) && m.Role != "system" {
			t.Fatalf("HTR fragment role = %q, want system", m.Role)
		}
		if strings.HasPrefix(m.Content, promptHTREnabledMarker) {
			if !strings.Contains(m.Content, "command -v htrcli") {
				t.Fatalf("HTR fragment missing actionable content: %q", m.Content)
			}
		}
	}

	ag.config = &config.Config{Ocode: config.OcodeConfig{Browser: config.BrowserConfig{HTREnabled: false}}}
	if containsPromptMarker(ag.BasePromptMessages(), promptHTREnabledMarker) {
		t.Fatalf("expected no %s fragment once htr_enabled is false", promptHTREnabledMarker)
	}

	// Nil config must not panic and must omit the fragment.
	ag.config = nil
	if containsPromptMarker(ag.BasePromptMessages(), promptHTREnabledMarker) {
		t.Fatalf("expected no %s fragment with nil config", promptHTREnabledMarker)
	}
}
