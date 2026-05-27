package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndQuery(t *testing.T) {
	// Use a temporary directory to avoid polluting real data
	tmpDir := t.TempDir()
	origDir := dataDirFn
	dataDirFn = func() (string, error) {
		return filepath.Join(tmpDir, "usage"), nil
	}
	defer func() { dataDirFn = origDir }()

	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	// Record a few entries
	err := RecordUsage(now, "gpt-4o", "openai", 100, 50, 10, 150, 0.001)
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	err = RecordUsage(now.Add(1*time.Hour), "gpt-4o-mini", "openai", 200, 100, 0, 300, 0.0002)
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	err = RecordUsage(now.Add(2*time.Hour), "claude-3-5-sonnet-20241022", "anthropic", 150, 75, 30, 225, 0.002)
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	// Query all
	records, err := Query(time.Time{}, now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Query subset
	records, err = Query(now.Add(30*time.Minute), now.Add(90*time.Minute))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record in time window, got %d", len(records))
	}
	if records[0].Model != "gpt-4o-mini" {
		t.Fatalf("expected gpt-4o-mini, got %s", records[0].Model)
	}
}

func TestSummarize(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	records := []Record{
		{Timestamp: now, Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, Spend: 0.001},
		{Timestamp: now.Add(1 * time.Hour), Model: "gpt-4o", PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300, Spend: 0.002},
		{Timestamp: now.Add(2 * time.Hour), Model: "gpt-4o-mini", PromptTokens: 50, CompletionTokens: 25, TotalTokens: 75, Spend: 0.0001},
	}

	summary := Summarize(records)

	if summary.TotalRequests != 3 {
		t.Fatalf("expected 3 total requests, got %d", summary.TotalRequests)
	}
	if summary.TotalPromptTokens != 350 {
		t.Fatalf("expected 350 total prompt tokens, got %d", summary.TotalPromptTokens)
	}
	if summary.TotalCompletionTokens != 175 {
		t.Fatalf("expected 175 total completion tokens, got %d", summary.TotalCompletionTokens)
	}
	if len(summary.ByModel) != 2 {
		t.Fatalf("expected 2 models, got %d", len(summary.ByModel))
	}

	// Check gpt-4o summary
	if summary.ByModel[0].Model != "gpt-4o" {
		t.Fatalf("expected first model gpt-4o, got %s", summary.ByModel[0].Model)
	}
	if summary.ByModel[0].RequestCount != 2 {
		t.Fatalf("expected 2 requests for gpt-4o, got %d", summary.ByModel[0].RequestCount)
	}
}

func TestFormatSummary(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	records := []Record{
		{Timestamp: now, Model: "gpt-4o", PromptTokens: 1000, CompletionTokens: 500, CacheReadTokens: 100, TotalTokens: 1500, Spend: 0.005},
	}

	summary := Summarize(records)
	output := FormatSummary(summary)

	if !contains(output, "gpt-4o") {
		t.Fatalf("expected output to contain model name gpt-4o, got:\n%s", output)
	}
	if !contains(output, "1,000") {
		t.Fatalf("expected output to contain 1,000, got:\n%s", output)
	}
	if !contains(output, "$0.0050") {
		t.Fatalf("expected output to contain cost, got:\n%s", output)
	}
}

func TestEmptySummary(t *testing.T) {
	summary := Summarize([]Record{})
	output := FormatSummary(summary)
	if !contains(output, "No usage records") {
		t.Fatalf("expected empty message, got:\n%s", output)
	}
}

func TestRecordUsageFromTokens(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := dataDirFn
	dataDirFn = func() (string, error) {
		return filepath.Join(tmpDir, "usage"), nil
	}
	defer func() { dataDirFn = origDir }()

	now := time.Now()

	// This model is in the pricing table
	err := RecordUsageFromTokens(now, "gpt-4o", 1000000, 500000, 100000)
	if err != nil {
		t.Fatalf("RecordUsageFromTokens failed: %v", err)
	}

	records, err := Query(time.Time{}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Spend <= 0 {
		t.Fatalf("expected positive spend for known model, got %f", records[0].Spend)
	}
}

func TestDateRanges(t *testing.T) {
	// Verify date ranges don't crash
	for _, dr := range DateRanges {
		from, to := dr.From()
		if to.Before(from) && !from.IsZero() {
			t.Errorf("date range %q has from after to", dr.Label)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
