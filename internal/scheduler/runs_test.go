package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunHistoryAppendListAndGet(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rh := NewRunHistory(storePath)

	now := time.Now().UTC().Truncate(time.Millisecond)
	// Append 3 runs for job123, 1 for other job
	for i := 0; i < 3; i++ {
		rec := RunRecord{
			JobID:      "job123",
			JobName:    "my job",
			StartedAt:  now.Add(-time.Duration(i) * time.Hour),
			FinishedAt: now.Add(-time.Duration(i)*time.Hour + 2*time.Second),
			DurationMs: 2000,
			Status:     "ok",
			Input:      "do thing",
			Output:     "done",
			Logs: []RunLogEntry{
				{At: now.Add(-time.Duration(i) * time.Hour), Level: "info", Message: "started"},
				{At: now.Add(-time.Duration(i)*time.Hour + 2*time.Second), Level: "info", Message: "finished in 2000ms"},
			},
		}
		if err := rh.Append(rec); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	other := RunRecord{
		JobID:      "other",
		JobName:    "other job",
		StartedAt:  now,
		FinishedAt: now.Add(time.Second),
		DurationMs: 1000,
		Status:     "error",
		Input:      "bad input",
		Error:      "boom",
		Logs:       []RunLogEntry{{At: now, Level: "error", Message: "failed"}},
	}
	if err := rh.Append(other); err != nil {
		t.Fatalf("append other: %v", err)
	}

	// List filtered by job123, newest first
	runs, total, err := rh.List("job123", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(runs) != 3 {
		t.Fatalf("expected 3 runs total=%d len=%d", total, len(runs))
	}
	// Newest first: StartedAt descending — first should be now (i=0)
	if !runs[0].StartedAt.Equal(now) {
		t.Fatalf("newest first: got %v want %v", runs[0].StartedAt, now)
	}
	for _, r := range runs {
		if r.DurationMs != 2000 {
			t.Fatalf("duration want 2000 got %d", r.DurationMs)
		}
		if r.Input != "do thing" || r.Output != "done" {
			t.Fatalf("input/output mismatch %+v", r)
		}
		if len(r.Logs) != 2 {
			t.Fatalf("logs len want 2 got %d", len(r.Logs))
		}
		if r.Logs[0].At.IsZero() || r.Logs[1].At.IsZero() {
			t.Fatalf("log missing datetime")
		}
	}
	// Pagination: limit 1, offset 1 should give second newest
	paged, total2, err := rh.List("job123", 1, 1)
	if err != nil {
		t.Fatalf("paged list: %v", err)
	}
	if total2 != 3 || len(paged) != 1 {
		t.Fatalf("paged total=%d len=%d", total2, len(paged))
	}
	if !paged[0].StartedAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("paged second newest want %v got %v", now.Add(-time.Hour), paged[0].StartedAt)
	}
	// Get single
	got, err := rh.Get("job123", runs[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != runs[0].ID {
		t.Fatalf("get mismatch got=%v want %s", got, runs[0].ID)
	}
	// Get with wrong jobID should not match
	got2, err := rh.Get("other", runs[0].ID)
	if err != nil {
		t.Fatalf("get wrong job: %v", err)
	}
	if got2 != nil {
		t.Fatalf("get with wrong jobID should be nil, got %+v", got2)
	}
}

func TestRunHistoryEmptyAndMissing(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rh := NewRunHistory(storePath)
	runs, total, err := rh.List("nonexistent", 10, 0)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if total != 0 || len(runs) != 0 {
		t.Fatalf("expected empty, got total=%d len=%d", total, len(runs))
	}
	got, err := rh.Get("nonexistent", "nope")
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil got %+v", got)
	}
	// Ensure file does not exist yet
	if _, err := os.Stat(rh.Path()); !os.IsNotExist(err) {
		t.Fatalf("runs file should not exist, err %v", err)
	}
}

func TestRunHistoryDispatcherIntegration(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	rh := NewRunHistory(storePath)
	d := &Dispatcher{
		RunHistory: rh,
		Runner:     stubRunner{result: "hello from agent"},
	}
	job := &Job{ID: "job123", Name: "test-job", Payload: Payload{Message: "do thing"}}
	if err := d.OnJob(context.Background(), job); err != nil {
		t.Fatalf("OnJob: %v", err)
	}
	runs, total, err := rh.List("job123", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("expected 1 run total=%d len=%d", total, len(runs))
	}
	r := runs[0]
	if r.Input != "do thing" || r.Output != "hello from agent" || r.Status != "ok" {
		t.Fatalf("dispatcher record mismatch %+v", r)
	}
	if r.DurationMs < 0 {
		t.Fatalf("duration negative %d", r.DurationMs)
	}
	if len(r.Logs) != 2 {
		t.Fatalf("logs want 2 got %d", len(r.Logs))
	}
	if r.Logs[0].At.IsZero() || r.Logs[1].At.IsZero() {
		t.Fatalf("logs missing datetime")
	}
	// Error path
	dErr := &Dispatcher{
		RunHistory: rh,
		Runner:     stubRunner{err: os.ErrInvalid},
	}
	job2 := &Job{ID: "job123", Name: "test-job", Payload: Payload{Message: "fail input"}}
	if err := dErr.OnJob(context.Background(), job2); err == nil {
		t.Fatalf("expected error")
	}
	runs2, total2, _ := rh.List("job123", 10, 0)
	if total2 != 2 {
		t.Fatalf("after error total want 2 got %d", total2)
	}
	if runs2[0].Status != "error" || runs2[0].Error == "" {
		t.Fatalf("error status want error got %+v", runs2[0])
	}
	if runs2[0].Logs[1].Level != "error" {
		t.Fatalf("error log level want error got %s", runs2[0].Logs[1].Level)
	}
}

type stubRunner struct {
	result string
	err    error
}

func (s stubRunner) RunScheduledJob(_ context.Context, _ *Job) (string, error) {
	return s.result, s.err
}
