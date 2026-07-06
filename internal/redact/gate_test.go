package redact

import "testing"

func TestWarrantsLLMScanKeywords(t *testing.T) {
	cases := []struct {
		input string
		want  bool
		label string
	}{
		{"my password is secret123", true, "keyword: password"},
		{"here is my token: abc", true, "keyword: token"},
		{"the api_key is sk-abc123def456ghi789", true, "keyword: api_key"},
		{"secret = value", true, "keyword: secret"},
		{"pass=hunter2", true, "keyword: pass"},
		{"api-key: xyz", true, "keyword: api-key"},
		{"my APIKEY is abc", true, "keyword: apikey"},
		{"the quick brown fox jumps over the lazy dog", false, "benign prose"},
		{"just a normal sentence about cats", false, "benign normal"},
		{"I love programming in Go", false, "benign tech"},
		{"", false, "empty string"},
	}
	for _, tc := range cases {
		got := WarrantsLLMScan(tc.input)
		if got != tc.want {
			t.Errorf("WarrantsLLMScan(%q) = %v, want %v (%s)", tc.input, got, tc.want, tc.label)
		}
	}
}

func TestWarrantsLLMScanPrefixes(t *testing.T) {
	cases := []struct {
		input string
		want  bool
		label string
	}{
		{"export AWS_SECRET_ACCESS_KEY=AKIA1234567890123456", true, "prefix: AWS_"},
		{"export OPENAI_API_KEY=sk-test123", true, "prefix: OPENAI_"},
		{"ANTHROPIC_API_KEY=sk-ant-123", true, "prefix: ANTHROPIC_"},
		{"GEMINI_KEY=abc", true, "prefix: GEMINI_"},
		{"MY_APP_SECRET=xyz", true, "suffix: _SECRET"},
		{"DATABASE_TOKEN=abc", true, "suffix: _TOKEN"},
		{"SERVICE_API_KEY=abc", true, "suffix: _API_KEY"},
		{"OPENROUTER_KEY=or-abc", true, "suffix: _KEY (and prefix: OPENROUTER_)"},
		{"DEEPINFRA_KEY=abc", true, "suffix: _KEY (and prefix: DEEPINFRA_)"},
		{"SECRETMAN_KEY=abc", true, "suffix: _KEY (secret in prefix)"},
		{"OPENROUTER_API_KEY=or-abc", true, "prefix: OPENROUTER_"},
		{"DEEPINFRA_TOKEN=abc", true, "prefix: DEEPINFRA_"},
		{"just a normal variable", false, "benign"},
	}
	for _, tc := range cases {
		got := WarrantsLLMScan(tc.input)
		if got != tc.want {
			t.Errorf("WarrantsLLMScan(%q) = %v, want %v (%s)", tc.input, got, tc.want, tc.label)
		}
	}
}

func TestWarrantsLLMScanQuickScan(t *testing.T) {
	// A bare AKIA value (no keyword) should still trigger via QuickScan
	input := "AKIAIOSFODNN7EXAMPLE"
	if !WarrantsLLMScan(input) {
		t.Errorf("WarrantsLLMScan(%q) = false, want true (QuickScan value-pattern)", input)
	}
}

func TestWarrantsLLMScanCaseInsensitive(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"PASSWORD=secret", true},
		{"My Token is here", true},
		{"the SECRET value", true},
		{"normal text", false},
	}
	for _, tc := range cases {
		got := WarrantsLLMScan(tc.input)
		if got != tc.want {
			t.Errorf("WarrantsLLMScan(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
