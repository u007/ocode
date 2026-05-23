package agent

import (
	"strings"
	"testing"
)

func TestParseAgentContent_HonorsModelField(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\nmodel: anthropic/claude-haiku-4-5\n---\nbody"
	def, diags := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatalf("expected def, got diags: %+v", diags)
	}
	if def.Model != "anthropic/claude-haiku-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-haiku-4-5", def.Model)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "model") {
			t.Errorf("unexpected diagnostic for model field: %+v", d)
		}
	}
}

func TestParseAgentContent_WarnsForUnappliedTemperatureTopP(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\ntemperature: 0.2\ntop_p: 0.9\n---\nbody"
	def, diags := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatalf("expected def, got nil")
	}
	var sawTemp, sawTopP bool
	for _, d := range diags {
		if strings.Contains(d.Message, "temperature") {
			sawTemp = true
		}
		if strings.Contains(d.Message, "top_p") {
			sawTopP = true
		}
	}
	if !sawTemp || !sawTopP {
		t.Errorf("expected diagnostics for temperature and top_p; got %+v", diags)
	}
}
