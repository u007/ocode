package config

import (
	"encoding/json"
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

// TestCompactConfigPatchApplyToCoversEveryField is exhaustive on purpose.
// applyTo is the whole merge contract — a field left out of it is silently
// UNWRITABLE (a Settings form edit or a sidebar toggle would look accepted and
// change nothing), which no partial-coverage test can catch. One subtest per
// field sets it against a non-zero base and asserts ONLY that field moved.
func TestCompactConfigPatchApplyToCoversEveryField(t *testing.T) {
	base := CompactConfig{
		Enabled:                         false,
		SummaryProvider:                 "base-provider",
		SummaryModel:                    "base-model",
		TokenThreshold:                  0.11,
		KeepRecentTurns:                 1,
		KeepRecentTokens:                2,
		MinMessages:                     3,
		SummaryTimeoutSeconds:           4,
		SummaryFirstTokenTimeoutSeconds: 5,
		SummaryMaxRetries:               6,
		MaxSummaryInputTokens:           7,
	}
	fresh := func() CompactConfig { c := base; return c }

	bTrue, bStr, bFloat, bInt := true, "written", 0.99, 42

	tests := []struct {
		name  string
		patch CompactConfigPatch
		check func(t *testing.T, got CompactConfig)
	}{
		{"enabled", CompactConfigPatch{Enabled: &bTrue}, func(t *testing.T, g CompactConfig) {
			if !g.Enabled {
				t.Error("enabled not applied")
			}
		}},
		{"summary_provider", CompactConfigPatch{SummaryProvider: &bStr}, func(t *testing.T, g CompactConfig) {
			if g.SummaryProvider != bStr {
				t.Errorf("summary_provider = %q", g.SummaryProvider)
			}
		}},
		{"summary_model", CompactConfigPatch{SummaryModel: &bStr}, func(t *testing.T, g CompactConfig) {
			if g.SummaryModel != bStr {
				t.Errorf("summary_model = %q", g.SummaryModel)
			}
		}},
		{"token_threshold", CompactConfigPatch{TokenThreshold: &bFloat}, func(t *testing.T, g CompactConfig) {
			if g.TokenThreshold != bFloat {
				t.Errorf("token_threshold = %v", g.TokenThreshold)
			}
		}},
		{"keep_recent_turns", CompactConfigPatch{KeepRecentTurns: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.KeepRecentTurns != bInt {
				t.Errorf("keep_recent_turns = %d", g.KeepRecentTurns)
			}
		}},
		{"keep_recent_tokens", CompactConfigPatch{KeepRecentTokens: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.KeepRecentTokens != bInt {
				t.Errorf("keep_recent_tokens = %d", g.KeepRecentTokens)
			}
		}},
		{"min_messages", CompactConfigPatch{MinMessages: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.MinMessages != bInt {
				t.Errorf("min_messages = %d", g.MinMessages)
			}
		}},
		{"summary_timeout_seconds", CompactConfigPatch{SummaryTimeoutSeconds: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.SummaryTimeoutSeconds != bInt {
				t.Errorf("summary_timeout_seconds = %d", g.SummaryTimeoutSeconds)
			}
		}},
		{"summary_first_token_timeout_seconds", CompactConfigPatch{SummaryFirstTokenTimeoutSeconds: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.SummaryFirstTokenTimeoutSeconds != bInt {
				t.Errorf("summary_first_token_timeout_seconds = %d", g.SummaryFirstTokenTimeoutSeconds)
			}
		}},
		{"summary_max_retries", CompactConfigPatch{SummaryMaxRetries: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.SummaryMaxRetries != bInt {
				t.Errorf("summary_max_retries = %d", g.SummaryMaxRetries)
			}
		}},
		{"max_summary_input_tokens", CompactConfigPatch{MaxSummaryInputTokens: &bInt}, func(t *testing.T, g CompactConfig) {
			if g.MaxSummaryInputTokens != bInt {
				t.Errorf("max_summary_input_tokens = %d", g.MaxSummaryInputTokens)
			}
		}},
	}
	if len(tests) != 11 {
		t.Fatalf("expected one subtest per CompactConfig field, got %d — add the new one", len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fresh()
			if tt.patch.IsEmpty() {
				t.Fatalf("patch with %s set must not report IsEmpty", tt.name)
			}
			tt.patch.applyTo(&got)
			tt.check(t, got)
		})
	}
}

// TestCompactConfigPatchApplyToLeavesAbsentFieldsAndWritesExplicitZero pins
// the distinction the whole design rests on: ABSENT means "leave alone",
// explicit zero means "write". Getting it backwards either silently drops a
// user's reset (zero treated as absent) or silently reverts a clear (absent
// treated as zero) — the second is the dangerous one, because
// summaryModelPatch("") must be able to return to the auto fallback.
func TestCompactConfigPatchApplyToLeavesAbsentFieldsAndWritesExplicitZero(t *testing.T) {
	base := CompactConfig{
		Enabled:         true,
		SummaryProvider: "openai",
		SummaryModel:    "gpt-4o-mini",
		TokenThreshold:  0.85,
		MinMessages:     8,
	}

	absent := base
	var nothing CompactConfigPatch
	if !nothing.IsEmpty() {
		t.Fatal("a zero patch must report IsEmpty")
	}
	nothing.applyTo(&absent)
	if absent != base {
		t.Errorf("an empty patch changed the config: %+v", absent)
	}

	zeroStr, zeroInt, zeroFloat, falseBool := "", 0, 0.0, false
	explicit := base
	CompactConfigPatch{
		SummaryProvider: &zeroStr,
		SummaryModel:    &zeroStr,
		TokenThreshold:  &zeroFloat,
		MinMessages:     &zeroInt,
		Enabled:         &falseBool,
	}.applyTo(&explicit)
	if explicit.SummaryProvider != "" || explicit.SummaryModel != "" ||
		explicit.TokenThreshold != 0 || explicit.MinMessages != 0 || explicit.Enabled {
		t.Errorf("explicit zeros must be written: %+v", explicit)
	}
}

// TestCompactConfigPatchJSONRoundTripIsAbsentAware guards the wire contract: a
// key the client omitted must arrive nil (merge-safe), and a key it sent as ""
// or 0 must arrive non-nil pointing at that zero.
func TestCompactConfigPatchJSONRoundTripIsAbsentAware(t *testing.T) {
	parsed, err := jsonPatchForTest(`{"enabled":false,"summary_model":"","token_threshold":0}`)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Enabled == nil || *parsed.Enabled {
		t.Errorf("enabled = %v, want non-nil false", parsed.Enabled)
	}
	if parsed.SummaryModel == nil || *parsed.SummaryModel != "" {
		t.Errorf("summary_model = %v, want non-nil pointing at \"\"", parsed.SummaryModel)
	}
	if parsed.TokenThreshold == nil || *parsed.TokenThreshold != 0 {
		t.Errorf("token_threshold = %v, want non-nil pointing at 0", parsed.TokenThreshold)
	}
	// Absent keys stay nil.
	if parsed.SummaryProvider != nil || parsed.MinMessages != nil ||
		parsed.KeepRecentTurns != nil || parsed.KeepRecentTokens != nil ||
		parsed.SummaryTimeoutSeconds != nil || parsed.SummaryFirstTokenTimeoutSeconds != nil ||
		parsed.SummaryMaxRetries != nil || parsed.MaxSummaryInputTokens != nil {
		t.Errorf("absent keys must stay nil, got %+v", parsed)
	}
	if parsed.IsEmpty() {
		t.Error("a patch with present keys must not report IsEmpty")
	}
}

// jsonPatchForTest decodes a request body the way the HTTP handler does, so the
// absent-vs-nil contract is pinned at the JSON boundary and not just on a
// hand-built struct.
func jsonPatchForTest(body string) (CompactConfigPatch, error) {
	var p CompactConfigPatch
	err := json.Unmarshal([]byte(body), &p)
	return p, err
}
