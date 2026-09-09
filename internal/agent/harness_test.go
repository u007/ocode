package agent

import (
	"net/http"
	"os"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestNormalizeFakeAgent(t *testing.T) {
	tests := map[string]string{
		"":             "ocode",
		"ocode":       "ocode",
		"OpenCode":    "opencode",
		"claudecode":  "claude-code",
		"Claude_Code": "claude-code",
		"cline":       "cline",
		"Kilo":        "kilo-code",
		"codex-cli":   "codex",
	}
	for input, want := range tests {
		got, err := config.NormalizeFakeAgent(input)
		if err != nil {
			t.Errorf("NormalizeFakeAgent(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeFakeAgent(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := config.NormalizeFakeAgent("unknown"); err == nil {
		t.Error("NormalizeFakeAgent(unknown) succeeded")
	}
}

func TestApplyHarnessHeaders(t *testing.T) {
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })
	if _, err := SetActiveHarness("claude-code"); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyHarnessHeaders(req, "anthropic")
	if got := req.Header.Get("User-Agent"); got != "claude-cli/2.1.0 (external, cli)" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := req.Header.Get("X-App"); got != "cli" {
		t.Errorf("X-App = %q", got)
	}
	if got := req.Header.Get("anthropic-beta"); got != "claude-code-20250219" {
		t.Errorf("anthropic-beta = %q", got)
	}
}

func TestApplyHarnessHeadersEnvironmentOverride(t *testing.T) {
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })
	if _, err := SetActiveHarness("cline"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("OCODE_FAKE_AGENT", "kilo-code"); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyHarnessHeaders(req, "openai")
	if got := req.Header.Get("User-Agent"); got != "kilo-code/1.0.0" {
		t.Errorf("User-Agent = %q, want kilo-code fingerprint", got)
	}
}
