package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

func TestCronRunsEndpoints(t *testing.T) {
	dir := t.TempDir()
	storePath := dir + "/jobs.json"
	_ = os.WriteFile(storePath, []byte(`{"version":1,"jobs":[]}`), 0644)

	rh := scheduler.NewRunHistory(storePath)
	now := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 3; i++ {
		rec := scheduler.RunRecord{
			JobID:      "job123",
			JobName:    "my job",
			StartedAt:  now.Add(-time.Duration(i) * time.Hour),
			FinishedAt: now.Add(-time.Duration(i)*time.Hour + 2*time.Second),
			DurationMs: 2000,
			Status:     "ok",
			Input:      "do thing",
			Output:     "done",
			Logs: []scheduler.RunLogEntry{
				{At: now.Add(-time.Duration(i) * time.Hour), Level: "info", Message: "started"},
				{At: now.Add(-time.Duration(i)*time.Hour + 2*time.Second), Level: "info", Message: "finished in 2000ms"},
			},
		}
		if err := rh.Append(rec); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	srv := &Server{
		workDir:          dir,
		mux:              http.NewServeMux(),
		handler:          NewHandler(),
		schedulerRuns:    rh,
		schedulerOutbox:  scheduler.NewOutbox(storePath),
		schedulerTargets: scheduler.NewTargets(storePath),
	}
	srv.mux.HandleFunc("GET /api/cron/{id}/runs", srv.handleCronRuns)
	srv.mux.HandleFunc("GET /api/cron/{id}/runs/{runId}", srv.handleCronRunDetail)

	// List
	req := httptest.NewRequest("GET", "/api/cron/job123/runs?limit=10&offset=0", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list status %d body %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Runs  []scheduler.RunRecord `json:"runs"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if listResp.Total != 3 || len(listResp.Runs) != 3 {
		t.Fatalf("expected 3 runs got total=%d len=%d", listResp.Total, len(listResp.Runs))
	}
	if listResp.Runs[0].Logs[0].At.IsZero() {
		t.Fatalf("log missing datetime")
	}
	if listResp.Runs[0].DurationMs != 2000 || listResp.Runs[0].Input != "do thing" {
		t.Fatalf("first run mismatch %+v", listResp.Runs[0])
	}

	// Pagination
	req2 := httptest.NewRequest("GET", "/api/cron/job123/runs?limit=1&offset=1", nil)
	w2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("paged status %d body %s", w2.Code, w2.Body.String())
	}
	var paged struct {
		Runs  []scheduler.RunRecord `json:"runs"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &paged); err != nil {
		t.Fatalf("unmarshal paged: %v", err)
	}
	if paged.Total != 3 || len(paged.Runs) != 1 {
		t.Fatalf("paged expected total 3 len 1 got %d %d", paged.Total, len(paged.Runs))
	}

	// Get single
	runID := listResp.Runs[0].ID
	req3 := httptest.NewRequest("GET", "/api/cron/job123/runs/"+runID, nil)
	w3 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("get status %d body %s", w3.Code, w3.Body.String())
	}
	var single scheduler.RunRecord
	if err := json.Unmarshal(w3.Body.Bytes(), &single); err != nil {
		t.Fatalf("unmarshal single: %v", err)
	}
	if single.ID != runID || single.Input != "do thing" || single.DurationMs != 2000 {
		t.Fatalf("single mismatch %+v", single)
	}

	// Not found
	req4 := httptest.NewRequest("GET", "/api/cron/job123/runs/notfound", nil)
	w4 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w4, req4)
	if w4.Code != 404 {
		t.Fatalf("expected 404 for missing run, got %d body %s", w4.Code, w4.Body.String())
	}

	// Empty job
	req5 := httptest.NewRequest("GET", "/api/cron/empty/runs?limit=10&offset=0", nil)
	w5 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w5, req5)
	if w5.Code != 200 {
		t.Fatalf("empty list status %d body %s", w5.Code, w5.Body.String())
	}
	var empty struct {
		Runs  []scheduler.RunRecord `json:"runs"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w5.Body.Bytes(), &empty); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if empty.Total != 0 || len(empty.Runs) != 0 {
		t.Fatalf("empty expected 0 got total=%d len=%d", empty.Total, len(empty.Runs))
	}
}
