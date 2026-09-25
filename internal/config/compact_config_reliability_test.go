package config

import (
	"strconv"
	"testing"
)

func intPtrForCompactTest(v int) *int { return &v }

func TestDefaultCompactConfigUsesFirstTokenTimeoutDefault(t *testing.T) {
	if got := defaultCompactConfig().SummaryFirstTokenTimeoutSeconds; got != 300 {
		t.Fatalf("default summary_first_token_timeout_seconds = %d, want 300", got)
	}
}

func TestApplyCompactConfigPreservesExplicitFirstTokenTimeoutValues(t *testing.T) {
	for _, value := range []int{0, 300} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			cfg := defaultCompactConfig()
			applyCompactConfig(&cfg, compactConfigFile{
				SummaryFirstTokenTimeoutSeconds: intPtrForCompactTest(value),
			})
			if got := cfg.SummaryFirstTokenTimeoutSeconds; got != value {
				t.Fatalf("persisted first-token timeout = %d, want %d", got, value)
			}
		})
	}
}
