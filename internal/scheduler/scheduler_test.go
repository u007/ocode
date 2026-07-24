package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedNow is a deterministic clock anchor for tests.
var fixedNow = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	s := NewService(filepath.Join(dir, "jobs.json"))
	s.SetClock(func() time.Time { return fixedNow })
	return s
}

func TestComputeNextRun(t *testing.T) {
	s := newTestService(t)
	from := fixedNow

	// at -> exact timestamp
	at := &Job{Schedule: Schedule{Kind: KindAt, AtMs: from.Add(2 * time.Hour).UnixMilli()}}
	nr, err := s.computeNextRun(at, from)
	if err != nil {
		t.Fatalf("at: %v", err)
	}
	if nr != at.Schedule.AtMs {
		t.Fatalf("at: want %d got %d", at.Schedule.AtMs, nr)
	}

	// every -> from + interval
	ev := &Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(10 * time.Minute / time.Millisecond)}}
	nr, err = s.computeNextRun(ev, from)
	if err != nil {
		t.Fatalf("every: %v", err)
	}
	if nr != from.Add(10*time.Minute).UnixMilli() {
		t.Fatalf("every: want %d got %d", from.Add(10*time.Minute).UnixMilli(), nr)
	}

	// cron -> next 09:00 UTC after a 10:00 date
	cr := &Job{Schedule: Schedule{Kind: KindCron, Expr: "0 9 * * *", TZ: "UTC"}}
	nr, err = s.computeNextRun(cr, from)
	if err != nil {
		t.Fatalf("cron: %v", err)
	}
	want := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC).UnixMilli()
	if nr < want || nr > want+maxJitterMs {
		t.Fatalf("cron: want ~%d (±jitter) got %d", want, nr)
	}

	// cron with bad expr -> error
	bad := &Job{Schedule: Schedule{Kind: KindCron, Expr: "not a cron"}}
	if _, err := s.computeNextRun(bad, from); err == nil {
		t.Fatal("cron: expected error for bad expr")
	}
}

func TestAddJobValidation(t *testing.T) {
	s := newTestService(t)

	// invalid schedule
	if _, err := s.AddJob(Job{Schedule: Schedule{Kind: ""}}); err == nil {
		t.Fatal("expected error for empty schedule kind")
	}

	// valid every job
	id, err := s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)}, Payload: Payload{Message: "hi"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(id) != idLen {
		t.Fatalf("id len: want %d got %d", idLen, len(id))
	}

	// cap enforcement
	s.SetMaxJobs(1)
	if _, err := s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)}, Payload: Payload{Message: "x"}}); err == nil {
		t.Fatal("expected job-limit error")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	s1 := NewService(path)
	s1.SetClock(func() time.Time { return fixedNow })
	id, err := s1.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)}, Payload: Payload{Message: "persist me"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Re-open from the same store path.
	s2 := NewService(path)
	s2.SetClock(func() time.Time { return fixedNow })
	if err := s2.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s2.Stop()
	j := s2.GetJob(id)
	if j == nil {
		t.Fatal("job not reloaded from disk")
	}
	if j.Payload.Message != "persist me" {
		t.Fatalf("payload mismatch: %q", j.Payload.Message)
	}
	if j.State.NextRunAtMs == 0 {
		t.Fatal("NextRunAtMs not computed on reload")
	}
}

func TestOneShotAutoDelete(t *testing.T) {
	s := newTestService(t)
	var fired int
	s.SetOnJob(func(_ context.Context, _ *Job) error { fired++; return nil })

	id, err := s.AddJob(Job{Schedule: Schedule{Kind: KindAt, AtMs: fixedNow.Add(time.Hour).UnixMilli()}, Payload: Payload{Message: "once"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Force it due.
	s.jobs[0].State.NextRunAtMs = fixedNow.UnixMilli()

	s.executeJob(&s.jobs[0])
	if fired != 1 {
		t.Fatalf("onJob fired %d times, want 1", fired)
	}
	if s.GetJob(id) != nil {
		t.Fatal("one-shot job should have been deleted after firing")
	}
}

func TestRecurringReschedule(t *testing.T) {
	s := newTestService(t)
	var fired int
	s.SetOnJob(func(_ context.Context, _ *Job) error { fired++; return nil })

	_, err := s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(10 * time.Minute / time.Millisecond)}, Payload: Payload{Message: "loop"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	j := &s.jobs[0]
	j.State.NextRunAtMs = fixedNow.UnixMilli() // due now

	s.executeJob(j)
	if fired != 1 {
		t.Fatalf("onJob fired %d times, want 1", fired)
	}
	if j.State.Runs != 1 {
		t.Fatalf("runs: want 1 got %d", j.State.Runs)
	}
	wantNext := fixedNow.Add(10 * time.Minute).UnixMilli()
	if j.State.NextRunAtMs != wantNext {
		t.Fatalf("next run: want %d got %d", wantNext, j.State.NextRunAtMs)
	}
	if j.State.LastStatus != "ok" {
		t.Fatalf("last status: want ok got %q", j.State.LastStatus)
	}
	// Still present (recurring).
	if s.GetJob(j.ID) == nil {
		t.Fatal("recurring job should persist")
	}
}

func TestSevenDayExpiry(t *testing.T) {
	s := newTestService(t)
	var fired int
	s.SetOnJob(func(_ context.Context, _ *Job) error { fired++; return nil })

	_, err := s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)}, Payload: Payload{Message: "expiring"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	j := &s.jobs[0]
	j.CreatedAtMs = fixedNow.Add(-8 * 24 * time.Hour).UnixMilli() // older than 7 days
	j.State.NextRunAtMs = fixedNow.UnixMilli()                    // due

	s.executeJob(j)
	if fired != 1 {
		t.Fatalf("onJob fired %d times, want 1", fired)
	}
	if s.GetJob(j.ID) != nil {
		t.Fatal("every-job older than 7 days should be auto-deleted")
	}
}

func TestTickFiresDueJobs(t *testing.T) {
	s := newTestService(t)
	var ids []string
	s.SetOnJob(func(_ context.Context, j *Job) error { ids = append(ids, j.ID); return nil })

	_, err := s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Hour / time.Millisecond)}, Payload: Payload{Message: "a"}})
	if err != nil {
		t.Fatalf("add a: %v", err)
	}
	_, err = s.AddJob(Job{Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Hour / time.Millisecond)}, Payload: Payload{Message: "b"}})
	if err != nil {
		t.Fatalf("add b: %v", err)
	}
	// Only the first job is due.
	s.jobs[0].State.NextRunAtMs = fixedNow.UnixMilli()
	s.jobs[1].State.NextRunAtMs = fixedNow.Add(time.Hour).UnixMilli()

	s.tick()
	if len(ids) != 1 || ids[0] != s.jobs[0].ID {
		t.Fatalf("tick fired %v, want [:%s]", ids, s.jobs[0].ID)
	}
	// First job rescheduled to the future.
	if s.jobs[0].State.NextRunAtMs <= fixedNow.UnixMilli() {
		t.Fatal("first job should have been rescheduled to the future")
	}
}

func TestExternalReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	s := NewService(path)
	s.SetClock(func() time.Time { return fixedNow })
	if err := s.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Simulate another process (the TUI) writing a job to the store.
	ext := Store{Version: 1, Jobs: []Job{{
		ID:          "external1",
		Name:        "from-tui",
		Schedule:    Schedule{Kind: KindEvery, EveryMs: int64(time.Hour / time.Millisecond)},
		Payload:     Payload{Message: "external"},
		CreatedAtMs: fixedNow.UnixMilli(),
		Enabled:     true,
	}}}
	data, _ := json.MarshalIndent(ext, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write ext: %v", err)
	}
	// Make its mtime strictly after our load time.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	s.syncFromDisk()
	found := false
	for _, j := range s.ListJobs() {
		if j.ID == "external1" {
			found = true
		}
	}
	if !found {
		t.Fatal("external job written by another process was not picked up")
	}
}

func TestPanicIsolation(t *testing.T) {
	s := newTestService(t)
	var errOut error
	s.SetOnJob(func(_ context.Context, _ *Job) error { panic("boom") })

	_, err := s.AddJob(Job{Schedule: Schedule{Kind: KindAt, AtMs: fixedNow.Add(time.Hour).UnixMilli()}, Payload: Payload{Message: "boom"}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	j := &s.jobs[0]
	j.State.NextRunAtMs = fixedNow.UnixMilli()

	// Must not propagate the panic.
	s.executeJob(j)
	if j.State.LastStatus != "error" {
		t.Fatalf("expected error status after panic, got %q", j.State.LastStatus)
	}
	if j.State.LastError == "" {
		t.Fatal("expected LastError to record the panic")
	}
	_ = errOut
}

func TestUpdateJob(t *testing.T) {
	s := newTestService(t)

	id, err := s.AddJob(Job{
		Schedule: Schedule{Kind: KindEvery, EveryMs: int64(time.Minute / time.Millisecond)},
		Payload:  Payload{Message: "original"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	disabled := false
	updated, err := s.UpdateJob(id, JobPatch{Enabled: &disabled})
	if err != nil {
		t.Fatalf("update enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatal("want disabled after update")
	}

	newName := "renamed"
	newPayload := Payload{Message: "updated message"}
	updated, err = s.UpdateJob(id, JobPatch{Name: &newName, Payload: &newPayload})
	if err != nil {
		t.Fatalf("update name/payload: %v", err)
	}
	if updated.Name != "renamed" || updated.Payload.Message != "updated message" {
		t.Fatalf("want renamed/updated message, got %+v", updated)
	}
	if updated.Enabled {
		t.Fatal("want enabled to remain false after unrelated update")
	}

	reenabled := true
	updated, err = s.UpdateJob(id, JobPatch{Enabled: &reenabled})
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if !updated.Enabled {
		t.Fatal("want enabled after re-enable")
	}
	if updated.State.NextRunAtMs == 0 {
		t.Fatal("want NextRunAtMs recomputed on re-enable")
	}

	before := s.GetJob(id).State.NextRunAtMs
	newSchedule := Schedule{Kind: KindEvery, EveryMs: int64(2 * time.Hour / time.Millisecond)}
	updated, err = s.UpdateJob(id, JobPatch{Schedule: &newSchedule})
	if err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if updated.State.NextRunAtMs == before {
		t.Fatal("want NextRunAtMs recomputed after schedule change")
	}
	wantNext := fixedNow.UnixMilli() + newSchedule.EveryMs
	if updated.State.NextRunAtMs != wantNext {
		t.Fatalf("want next run %d, got %d", wantNext, updated.State.NextRunAtMs)
	}

	badSchedule := Schedule{Kind: KindEvery, EveryMs: 0}
	if _, err := s.UpdateJob(id, JobPatch{Schedule: &badSchedule}); err == nil {
		t.Fatal("expected error for invalid schedule")
	}

	if _, err := s.UpdateJob("nonexistent", JobPatch{Enabled: &disabled}); err == nil {
		t.Fatal("expected error for unknown id")
	}
}
