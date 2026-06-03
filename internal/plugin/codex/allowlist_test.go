package codex

import "testing"

func TestIsAllowed_ExplicitModels(t *testing.T) {
	allowed := []string{
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini",
		"gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2",
	}
	for _, m := range allowed {
		if !isAllowed(m) {
			t.Errorf("expected %q to be allowed", m)
		}
	}
}

func TestIsAllowed_RejectedModels(t *testing.T) {
	rejected := []string{"gpt-4o", "gpt-4.1", "gpt-3.5", "gpt-5", "claude-3-opus"}
	for _, m := range rejected {
		if isAllowed(m) {
			t.Errorf("expected %q to be rejected", m)
		}
	}
}

func TestIsAllowed_SemanticFilter(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.51", false}, // multi-digit minor not matched
		{"gpt-5.40", false}, // multi-digit minor not matched
		{"gpt-5.39", false}, // 5.39 < 5.4
		{"gpt-5.6", true},   // 5.6 > 5.4
		{"gpt-6.0", true},   // 6.0 > 5.4
		{"gpt-6", false},    // no minor version
	}
	for _, c := range cases {
		got := isAllowed(c.model)
		if got != c.want {
			t.Errorf("isAllowed(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

func TestIsAllowed_WithProviderPrefix(t *testing.T) {
	if !isAllowed("openai/gpt-5.4") {
		t.Error("expected openai/gpt-5.4 to be allowed")
	}
	if isAllowed("openai/gpt-4o") {
		t.Error("expected openai/gpt-4o to be rejected")
	}
}
