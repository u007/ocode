package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// useTempUsageDir points the ledger at a fresh directory for the duration of
// the test (the same isolation pattern as usage_test.go).
func useTempUsageDir(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir := dataDirFn
	dataDirFn = func() (string, error) {
		return filepath.Join(tmpDir, "usage"), nil
	}
	t.Cleanup(func() { dataDirFn = origDir })
}

func TestLastModelForSessionEmptyLedger(t *testing.T) {
	useTempUsageDir(t)

	if got, err := LastModelForSession("ses_missing"); err != nil || got != "" {
		t.Fatalf("LastModelForSession on an empty ledger = %q, %v; want empty, nil", got, err)
	}
	if got, err := LastModelForSession(""); err != nil || got != "" {
		t.Fatalf("LastModelForSession(\"\") = %q, %v; want empty, nil", got, err)
	}
}

func TestLastModelForSessionNewestWinsAndQualifiesProvider(t *testing.T) {
	useTempUsageDir(t)
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

	// Untagged rows must never be attributed to a session.
	if err := RecordUsage(now, "title-model", "openai", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record untagged: %v", err)
	}
	if err := RecordUsageForSession(now, "ses_a", "nemotron-3.5-super-vl:free", "openrouter", 10, 5, 0, 15, 0); err != nil {
		t.Fatalf("record a1: %v", err)
	}
	if err := RecordUsageForSession(now.Add(time.Minute), "ses_a", "deepseek-v4.1-flash", "opencode-go", 10, 5, 0, 15, 0); err != nil {
		t.Fatalf("record a2: %v", err)
	}
	// A row whose model already carries the provider prefix is not doubled.
	if err := RecordUsageForSession(now, "ses_b", "openai/gpt-4o", "openai", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record b: %v", err)
	}

	if got, err := LastModelForSession("ses_a"); err != nil || got != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("ses_a = %q, %v; want the newest row's provider-qualified model", got, err)
	}
	if got, err := LastModelForSession("ses_b"); err != nil || got != "openai/gpt-4o" {
		t.Fatalf("ses_b = %q, %v; want the prefix not doubled", got, err)
	}
}

// TestLastModelForSessionReadsAppendedRows pins the incremental scan: the index
// must pick up rows appended after a previous lookup instead of serving a
// process-lifetime snapshot.
func TestLastModelForSessionReadsAppendedRows(t *testing.T) {
	useTempUsageDir(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

	if err := RecordUsageForSession(now, "ses_c", "first-model", "prov", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if got, _ := LastModelForSession("ses_c"); got != "prov/first-model" {
		t.Fatalf("ses_c first = %q", got)
	}
	if err := RecordUsageForSession(now.Add(time.Minute), "ses_c", "second-model", "prov", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record second: %v", err)
	}
	if got, _ := LastModelForSession("ses_c"); got != "prov/second-model" {
		t.Fatalf("ses_c after append = %q, want the appended row", got)
	}
}

// TestLastModelForSessionWaitsForCompleteLines pins the torn-tail rule: a row
// whose trailing newline has not landed yet must be ignored (never consumed,
// never mis-parsed), and the NEXT call must pick it up once it is complete.
func TestLastModelForSessionWaitsForCompleteLines(t *testing.T) {
	useTempUsageDir(t)
	now := time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)

	if err := RecordUsageForSession(now, "ses_tail", "first-model", "prov", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if got, _ := LastModelForSession("ses_tail"); got != "prov/first-model" {
		t.Fatalf("ses_tail baseline = %q", got)
	}

	path, err := recordsPath()
	if err != nil {
		t.Fatalf("recordsPath: %v", err)
	}
	partial := `{"t":"2026-10-26T11:01:00Z","m":"second-model","p":"prov","sid":"ses_tail"}`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := f.WriteString(partial); err != nil {
		f.Close()
		t.Fatalf("write partial row: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}

	if got, _ := LastModelForSession("ses_tail"); got != "prov/first-model" {
		t.Fatalf("torn tail consumed: got %q, want the last COMPLETE row", got)
	}

	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		f.Close()
		t.Fatalf("complete row: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	if got, _ := LastModelForSession("ses_tail"); got != "prov/second-model" {
		t.Fatalf("completed tail not consumed: got %q, want prov/second-model", got)
	}
}

// TestLastModelForSessionRescansAfterShrink covers a replaced/pruned ledger:
// the append-only offset would otherwise read past the new content and serve
// the pre-rotation index forever.
func TestLastModelForSessionRescansAfterShrink(t *testing.T) {
	useTempUsageDir(t)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	if err := RecordUsageForSession(now, "ses_rot", "a-very-long-model-name-that-makes-this-row-far-longer-than-the-replacement", "prov", 1, 1, 0, 2, 0); err != nil {
		t.Fatalf("record: %v", err)
	}
	if got, _ := LastModelForSession("ses_rot"); got == "" {
		t.Fatalf("ses_rot baseline is empty")
	}
	path, err := recordsPath()
	if err != nil {
		t.Fatalf("recordsPath: %v", err)
	}
	replacement := `{"t":"2026-10-26T12:01:00Z","m":"new-model","p":"prov","sid":"ses_rot"}` + "\n"
	if err := os.WriteFile(path, []byte(replacement), 0o644); err != nil {
		t.Fatalf("replace ledger: %v", err)
	}
	if got, _ := LastModelForSession("ses_rot"); got != "prov/new-model" {
		t.Fatalf("after rotation = %q, want the replacement ledger's model", got)
	}
}
