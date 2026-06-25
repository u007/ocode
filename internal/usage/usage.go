// Package usage provides LLM token usage tracking and aggregation.
// Records are stored as newline-delimited JSON in a per-project directory
// so they survive session restarts and can be queried by date range.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/pricing"
)

// Record represents a single LLM API call.
type Record struct {
	Timestamp        time.Time `json:"t"`
	Model            string    `json:"m"`
	Provider         string    `json:"p,omitempty"`
	PromptTokens     int64     `json:"pt"`
	CompletionTokens int64     `json:"ct"`
	CacheReadTokens  int64     `json:"crt,omitempty"`
	TotalTokens      int64     `json:"tt"`
	Spend            float64   `json:"sp,omitempty"`
}

// ModelSummary aggregates usage for a single model.
type ModelSummary struct {
	Model            string  `json:"model"`
	RequestCount     int     `json:"request_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Spend            float64 `json:"spend"`
}

// Summary holds the full usage summary over a time range.
type Summary struct {
	TotalRequests         int            `json:"total_requests"`
	TotalPromptTokens     int64          `json:"total_prompt_tokens"`
	TotalCompletionTokens int64          `json:"total_completion_tokens"`
	TotalCacheReadTokens  int64          `json:"total_cache_read_tokens"`
	TotalTokens           int64          `json:"total_tokens"`
	TotalSpend            float64        `json:"total_spend"`
	ByModel               []ModelSummary `json:"by_model"`
	StartTime             time.Time      `json:"start_time"`
	EndTime               time.Time      `json:"end_time"`
	Days                  int            `json:"days"`
}

// DateRange describes a time range for filtering.
type DateRange struct {
	Label string
	From  func() (time.Time, time.Time)
}

// Predefined date ranges.
var DateRanges = []DateRange{
	{Label: "Last hour", From: func() (time.Time, time.Time) {
		return time.Now().Add(-1 * time.Hour), time.Now()
	}},
	{Label: "Today", From: func() (time.Time, time.Time) {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), now
	}},
	{Label: "This week (last 7 days)", From: func() (time.Time, time.Time) {
		return time.Now().Add(-7 * 24 * time.Hour), time.Now()
	}},
	{Label: "This month (last 30 days)", From: func() (time.Time, time.Time) {
		return time.Now().Add(-30 * 24 * time.Hour), time.Now()
	}},
	{Label: "Last month", From: func() (time.Time, time.Time) {
		now := time.Now()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastMonth := firstOfMonth.AddDate(0, -1, 0)
		return lastMonth, firstOfMonth.Add(-time.Nanosecond)
	}},
	{Label: "Last 3 months", From: func() (time.Time, time.Time) {
		now := time.Now()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		threeMonthsAgo := firstOfMonth.AddDate(0, -3, 0)
		return threeMonthsAgo, firstOfMonth.Add(-time.Nanosecond)
	}},
	{Label: "All time", From: func() (time.Time, time.Time) {
		return time.Time{}, time.Now()
	}},
}

var (
	mu sync.Mutex
	// dataDirFn is the function used to locate the usage data directory.
	// Exported as a var so tests can override it.
	dataDirFn = defaultDataDir
)

// defaultDataDir returns the platform-appropriate data directory for usage records.
func defaultDataDir() (string, error) {
	return paths.UsageDir()
}

func dataDir() (string, error) {
	return dataDirFn()
}

func recordsPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "records.jsonl"), nil
}

// RecordUsage writes a single usage record to the JSON-lines file.
// It is safe for concurrent calls.
func RecordUsage(t time.Time, model string, provider string, promptTokens, completionTokens, cacheReadTokens, totalTokens int64, spend float64) error {
	rec := Record{
		Timestamp:        t,
		Model:            model,
		Provider:         provider,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CacheReadTokens:  cacheReadTokens,
		TotalTokens:      totalTokens,
		Spend:            spend,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("usage: marshal record: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()

	path, err := recordsPath()
	if err != nil {
		return err
	}

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("usage: create directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("usage: open records file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("usage: write record: %w", err)
	}
	return nil
}

// RecordUsageFromTokens is a convenience wrapper that accepts individual
// token counts and computes the spend using the pricing table.
func RecordUsageFromTokens(t time.Time, model string, promptTokens, completionTokens, cacheReadTokens int64) error {
	spend := 0.0
	if mp, ok := pricing.Lookup(model); ok {
		spend = (float64(promptTokens)*mp.InputPerMillion/1_000_000 +
			float64(completionTokens)*mp.OutputPerMillion/1_000_000)
	}
	total := promptTokens + completionTokens
	return RecordUsage(t, model, "", promptTokens, completionTokens, cacheReadTokens, total, spend)
}

// Query reads all records within the given time range.
// If from is zero (time.Time{}), all records up to to are returned.
func Query(from, to time.Time) ([]Record, error) {
	path, err := recordsPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Record{}, nil
		}
		return nil, fmt.Errorf("usage: open records file: %w", err)
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Skip corrupt lines
			continue
		}
		if !from.IsZero() && rec.Timestamp.Before(from) {
			continue
		}
		if rec.Timestamp.After(to) {
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("usage: scan records: %w", err)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	return records, nil
}

// Summarize aggregates records into a Summary grouped by model.
func Summarize(records []Record) Summary {
	type acc struct {
		count            int
		promptTokens     int64
		completionTokens int64
		cacheReadTokens  int64
		totalTokens      int64
		spend            float64
	}
	byModel := make(map[string]*acc)
	var total Summary

	modelOrder := make([]string, 0)

	for _, rec := range records {
		a, ok := byModel[rec.Model]
		if !ok {
			a = &acc{}
			byModel[rec.Model] = a
			modelOrder = append(modelOrder, rec.Model)
		}
		a.count++
		a.promptTokens += rec.PromptTokens
		a.completionTokens += rec.CompletionTokens
		a.cacheReadTokens += rec.CacheReadTokens
		a.totalTokens += rec.TotalTokens
		a.spend += rec.Spend

		total.TotalRequests++
		total.TotalPromptTokens += rec.PromptTokens
		total.TotalCompletionTokens += rec.CompletionTokens
		total.TotalCacheReadTokens += rec.CacheReadTokens
		total.TotalTokens += rec.TotalTokens
		total.TotalSpend += rec.Spend
	}

	sort.Strings(modelOrder)
	total.ByModel = make([]ModelSummary, 0, len(modelOrder))
	for _, name := range modelOrder {
		a := byModel[name]
		total.ByModel = append(total.ByModel, ModelSummary{
			Model:            name,
			RequestCount:     a.count,
			PromptTokens:     a.promptTokens,
			CompletionTokens: a.completionTokens,
			CacheReadTokens:  a.cacheReadTokens,
			TotalTokens:      a.totalTokens,
			Spend:            a.spend,
		})
	}

	if len(records) > 0 {
		total.StartTime = records[0].Timestamp
		total.EndTime = records[len(records)-1].Timestamp
		total.Days = int(total.EndTime.Sub(total.StartTime).Hours()/24) + 1
	}

	return total
}

// FormatSummary produces a human-readable table of usage data.
func FormatSummary(summary Summary) string {
	var b strings.Builder

	dateRange := "All time"
	if !summary.StartTime.IsZero() {
		dateRange = fmt.Sprintf("%s to %s",
			summary.StartTime.Format("2006-01-02"),
			summary.EndTime.Format("2006-01-02"))
		if summary.Days <= 1 {
			dateRange = summary.StartTime.Format("2006-01-02 15:04")
		}
	}
	fmt.Fprintf(&b, "≡ Usage Summary: %s\n\n", dateRange)

	if summary.TotalRequests == 0 {
		b.WriteString("No usage records found in this period.\n")
		return b.String()
	}

	// Per-model table
	fmt.Fprintf(&b, "%-30s %5s  %12s  %12s  %12s  %12s  %10s\n",
		"Model", "Calls", "Input Tokens", "Cached", "Output Tokens", "Total Tokens", "Cost")
	b.WriteString(strings.Repeat("─", 105))
	b.WriteString("\n")

	for _, ms := range summary.ByModel {
		fmt.Fprintf(&b, "%-30s %5d  %12s  %12s  %12s  %12s  %10s\n",
			shortModelName(ms.Model),
			ms.RequestCount,
			formatInt(ms.PromptTokens),
			formatInt(ms.CacheReadTokens),
			formatInt(ms.CompletionTokens),
			formatInt(ms.TotalTokens),
			formatCost(ms.Spend),
		)
	}

	b.WriteString(strings.Repeat("─", 105))
	b.WriteString("\n")

	fmt.Fprintf(&b, "%-30s %5d  %12s  %12s  %12s  %12s  %10s\n",
		"Total",
		summary.TotalRequests,
		formatInt(summary.TotalPromptTokens),
		formatInt(summary.TotalCacheReadTokens),
		formatInt(summary.TotalCompletionTokens),
		formatInt(summary.TotalTokens),
		formatCost(summary.TotalSpend),
	)

	return b.String()
}

func shortModelName(model string) string {
	// Strip provider prefix like "openai/" or "anthropic/"
	if idx := strings.Index(model, "/"); idx >= 0 && idx < len(model)-1 {
		model = model[idx+1:]
	}
	// Strip version suffix for display
	if strings.HasPrefix(model, "claude-") {
		// Keep claude model names as-is, they're distinctive enough
		return model
	}
	if len(model) > 28 {
		model = model[:25] + "..."
	}
	return model
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	// Add thousands separators
	parts := make([]string, 0)
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

func formatCost(v float64) string {
	if v == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.4f", v)
}
