package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

func TestCronUpdateEndpoint(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	baseNow := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return baseNow })

	srv := &Server{
		mux:     http.NewServeMux(),
		workDir: dir,
	}
	srv.SetScheduler(svc)

	id, err := svc.AddJob(scheduler.Job{
		Name:     "original",
		Schedule: scheduler.Schedule{Kind: scheduler.KindEvery, EveryMs: int64(time.Hour / time.Millisecond)},
		Payload:  scheduler.Payload{Message: "original message"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	disabled := false
	newName := "renamed"
	newMessage := "updated message"
	newSchedule := map[string]any{
		"kind":     scheduler.KindEvery,
		"every_ms": int64(30 * time.Minute / time.Millisecond),
	}
	body, _ := json.Marshal(map[string]any{
		"enabled":    disabled,
		"name":       newName,
		"message":    newMessage,
		"schedule":   newSchedule,
		"notes":      "updated notes",
		"owner":      "/workdir",
		"deliver_to": "telegram",
		"perm_mode":  scheduler.PermLocked,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/cron/"+id, bytes.NewReader(body))
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}

	var updated scheduler.Job
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.ID != id {
		t.Fatalf("id mismatch: %s != %s", updated.ID, id)
	}
	if updated.Name != newName || updated.Payload.Message != newMessage {
		t.Fatalf("unexpected job after patch: %+v", updated)
	}
	if updated.Enabled {
		t.Fatal("expected job to be disabled")
	}
	if updated.Payload.Notes != "updated notes" || updated.Payload.Owner != "/workdir" || updated.Payload.DeliverTo != "telegram" || updated.Payload.PermMode != scheduler.PermLocked {
		t.Fatalf("payload not updated: %+v", updated.Payload)
	}
	wantNext := baseNow.Add(30 * time.Minute).UnixMilli()
	if updated.State.NextRunAtMs != wantNext {
		t.Fatalf("next run mismatch: want %d got %d", wantNext, updated.State.NextRunAtMs)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/cron/missing", bytes.NewReader(body))
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing patch: %d %s", w.Code, w.Body.String())
	}
}
