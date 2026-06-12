package models

import (
	"testing"
)

func TestRequestyModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"openai/gpt-4o", "gpt-4o"},
		{"anthropic/claude-3-opus", "claude-3-opus"},
		{"google/gemini-pro", "gemini-pro"},
		{"model-without-slash", "model-without-slash"},
		{"provider/", "provider/"},          // edge case: trailing slash
		{"/model", "/model"},                // edge case: leading slash only
		{"", ""},                            // edge case: empty string
		{"a/b/c", "b/c"},                   // multiple slashes: returns after first
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := requestyModelName(tt.input)
			if got != tt.expected {
				t.Errorf("requestyModelName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFilterByProvider(t *testing.T) {
	models := []ModelEntry{
		{ID: "openai/gpt-4o", Name: "gpt-4o"},
		{ID: "anthropic/claude-3-opus", Name: "claude-3-opus"},
		{ID: "google/gemini-pro", Name: "gemini-pro"},
	}

	// Requesty provider should skip filtering
	t.Run("requesty skips filtering", func(t *testing.T) {
		got := filterByProvider(models, "requesty")
		if len(got) != len(models) {
			t.Errorf("expected %d models, got %d", len(models), len(got))
		}
	})

	// Other providers should filter by ID
	t.Run("openai filters correctly", func(t *testing.T) {
		got := filterByProvider(models, "openai")
		if len(got) != 1 {
			t.Fatalf("expected 1 model, got %d", len(got))
		}
		if got[0].ID != "openai/gpt-4o" {
			t.Errorf("expected openai/gpt-4o, got %s", got[0].ID)
		}
	})

	// Empty provider should return all
	t.Run("empty provider returns all", func(t *testing.T) {
		got := filterByProvider(models, "")
		if len(got) != len(models) {
			t.Errorf("expected %d models, got %d", len(models), len(got))
		}
	})

	// No matches should return empty
	t.Run("no matches returns empty", func(t *testing.T) {
		got := filterByProvider(models, "nonexistent")
		if len(got) != 0 {
			t.Errorf("expected 0 models, got %d", len(got))
		}
	})
}
