package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

func TestCronTargetsEndpoints(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	svc := scheduler.NewService(storePath)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(svc.Stop)

	srv := &Server{
		scheduler:        svc,
		schedulerOutbox:  scheduler.NewOutbox(storePath),
		schedulerTargets: scheduler.NewTargets(storePath),
		mux:              http.NewServeMux(),
	}
	srv.mux.HandleFunc("GET /api/cron/targets", srv.handleCronTargetsList)
	srv.mux.HandleFunc("POST /api/cron/targets", srv.handleCronTargetsSet)

	// Initial GET should return empty map.
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/cron/targets", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list initial: %d %s", w.Code, w.Body.String())
	}
	var initialResp struct {
		Targets map[string]int64 `json:"targets"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &initialResp)
	if len(initialResp.Targets) != 0 {
		t.Fatalf("want empty initial targets, got %+v", initialResp.Targets)
	}

	// POST a new mapping.
	body, _ := json.Marshal(map[string]any{"workdir": "/x", "chat_id": 99})
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/cron/targets", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}

	// GET should now return the mapping.
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/cron/targets", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &initialResp)
	if initialResp.Targets["/x"] != 99 {
		t.Fatalf("want /x=99, got %+v", initialResp.Targets)
	}
}
