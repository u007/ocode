package commandctx

import (
	"strings"
	"testing"
)

// TestInitInterpolatesFocus pins the /init prompt contract shared by the TUI
// and the server's /api/command-context/init endpoint. Both call this function,
// so web `/init <focus>` and TUI `/init <focus>` now produce identical prompts
// (the web previously wrote a static AGENTS.md stub instead).
func TestInitInterpolatesFocus(t *testing.T) {
	const focus = "the billing module"

	got := Init([]string{"the", "billing", "module"})
	if !strings.Contains(got, focus) {
		t.Fatalf("focus missing from /init prompt:\n%s", got)
	}
	if strings.Contains(got, "$ARGUMENTS") {
		t.Errorf("placeholder not substituted:\n%s", got)
	}
	if !strings.Contains(got, "AGENTS.md") {
		t.Errorf("/init prompt should name AGENTS.md:\n%s", got)
	}
}

// TestInitNoArgsMatchesTemplate pins that a bare /init interpolates an empty
// focus rather than leaving the raw placeholder behind.
func TestInitNoArgsMatchesTemplate(t *testing.T) {
	got := Init(nil)
	if strings.Contains(got, "$ARGUMENTS") {
		t.Fatalf("bare /init left the placeholder unsubstituted:\n%s", got)
	}
	// The template itself still carries the literal placeholder; the empty-focus
	// result must be exactly that template with the token removed.
	want := strings.ReplaceAll(initializePromptTemplate, "$ARGUMENTS", "")
	if got != want {
		t.Errorf("bare /init should equal the raw template with $ARGUMENTS removed")
	}
}
