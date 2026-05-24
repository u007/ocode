package agent

import (
	"strings"
	"testing"
)

func TestParseAgentContent_HonorsColorField(t *testing.T) {
	src := "---\ndescription: test\nmode: primary\ncolor: \"#7AA2F7\"\n---\nbody"
	def, _ := parseAgentContent(src, "fake.md")
	if def == nil {
		t.Fatal("expected def")
	}
	if def.Color != "\"#7AA2F7\"" && def.Color != "#7AA2F7" {
		t.Errorf("Color = %q, want #7AA2F7 (with or without quotes)", def.Color)
	}
}

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

// Replaced by the richer sampling_params_test.go suite. Temperature/top_p are
// now applied (not warned about) when valid; warnings only fire for invalid
// numeric values.
