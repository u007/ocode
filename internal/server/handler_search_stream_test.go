package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseSSEFrames splits a raw SSE body into frames, skipping comment frames
// and blank separators. Returns event name -> data JSON (already trimmed).
func parseSSEFrames(t *testing.T, body string) []struct {
	Event string
	Data  string
} {
	t.Helper()
	var out []struct {
		Event string
		Data  string
	}
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" || strings.HasPrefix(frame, ":") {
			continue
		}
		var event, data string
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data += strings.TrimPrefix(line, "data: ")
			}
		}
		out = append(out, struct {
			Event string
			Data  string
		}{Event: event, Data: data})
	}
	return out
}

func flattenStreamResults(t *testing.T, frames []struct {
	Event string
	Data  string
}) ([]FileSearchResult, FileSearchStreamDone) {
	t.Helper()
	var results []FileSearchResult
	var done FileSearchStreamDone
	foundDone := false
	for _, f := range frames {
		switch f.Event {
		case "result":
			var batch struct {
				Results []FileSearchResult `json:"results"`
			}
			if err := json.Unmarshal([]byte(f.Data), &batch); err != nil {
				t.Fatalf("bad result frame %q: %v", f.Data, err)
			}
			results = append(results, batch.Results...)
		case "done":
			foundDone = true
			if err := json.Unmarshal([]byte(f.Data), &done); err != nil {
				t.Fatalf("bad done frame %q: %v", f.Data, err)
			}
		default:
			t.Fatalf("unexpected frame event %q", f.Event)
		}
	}
	if !foundDone {
		t.Fatal("stream ended without a done frame")
	}
	if done.Total != len(results) {
		t.Fatalf("done.total %d != streamed %d", done.Total, len(results))
	}
	return results, done
}

func TestHandleFileSearchStreamStreamsBatchesBeforeDone(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// 25 matching lines in a.txt + 5 in b.txt = 30: one full batch (25) plus a
	// trailing partial batch (5), so at least two result frames precede done.
	var a strings.Builder
	for i := 0; i < 25; i++ {
		a.WriteString("hello stream line\n")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte(a.String()), 0644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString("hello other file\n")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/search/stream?q="+url.QueryEscape("hello"), nil)
	h.HandleFileSearchStream(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("stream failed %d: %s", w.Code, w.Body.String())
	}
	frames := parseSSEFrames(t, w.Body.String())
	if len(frames) < 3 {
		t.Fatalf("expected multiple frames (result batches + done), got %d: %s", len(frames), w.Body.String())
	}
	if frames[len(frames)-1].Event != "done" {
		t.Fatalf("last frame must be done, got %q", frames[len(frames)-1].Event)
	}
	results, done := flattenStreamResults(t, frames)
	if len(results) != 30 {
		t.Fatalf("expected 30 results, got %d", len(results))
	}
	if done.Capped {
		t.Fatal("capped should be false for 25 results")
	}
	// Paths are anchored to the workdir.
	for _, res := range results {
		if strings.Contains(res.Path, tmpDir) {
			t.Fatalf("result path %q must be relative to the searched root", res.Path)
		}
		if !strings.Contains(res.Text, "hello") {
			t.Fatalf("result line %q does not contain the query", res.Text)
		}
	}
}

func TestHandleFileSearchStreamParityWithPagedEndpoint(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("alpha beta\ngamma beta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("beta only\n"), 0644); err != nil {
		t.Fatal(err)
	}
	q := url.QueryEscape("beta")

	// Paged endpoint (all results + offset 0) for the expected set.
	w1 := httptest.NewRecorder()
	h.HandleFileSearch(w1, httptest.NewRequest("GET", "/api/files/search?q="+q, nil))
	var paged FileSearchResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &paged); err != nil {
		t.Fatalf("decode paged: %v", err)
	}

	w2 := httptest.NewRecorder()
	h.HandleFileSearchStream(w2, httptest.NewRequest("GET", "/api/files/search/stream?q="+q, nil))
	streamed, done := flattenStreamResults(t, parseSSEFrames(t, w2.Body.String()))
	if done.Capped {
		t.Fatal("unexpected cap")
	}
	if len(streamed) != len(paged.Results) {
		t.Fatalf("streamed %d != paged %d", len(streamed), len(paged.Results))
	}
	for i := range streamed {
		if streamed[i] != paged.Results[i] {
			t.Fatalf("result %d mismatch: stream %+v vs paged %+v", i, streamed[i], paged.Results[i])
		}
	}
}

func TestHandleFileSearchStreamRespectsLimitCap(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("cap line match\n")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "big.txt"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/search/stream?q="+url.QueryEscape("match")+"&limit=10", nil)
	h.HandleFileSearchStream(w, r)
	results, done := flattenStreamResults(t, parseSSEFrames(t, w.Body.String()))
	if len(results) != 10 {
		t.Fatalf("expected exactly 10 results with limit=10, got %d", len(results))
	}
	if !done.Capped {
		t.Fatal("capped should be true when the limit truncated the scan")
	}
}

func TestHandleFileSearchStreamEmptyQueryEmitsDoneOnly(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("anything\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.HandleFileSearchStream(w, httptest.NewRequest("GET", "/api/files/search/stream?q=", nil))
	frames := parseSSEFrames(t, w.Body.String())
	if len(frames) != 1 || frames[0].Event != "done" {
		t.Fatalf("expected a single done frame, got %d frames: %s", len(frames), w.Body.String())
	}
	var done FileSearchStreamDone
	if err := json.Unmarshal([]byte(frames[0].Data), &done); err != nil {
		t.Fatal(err)
	}
	if done.Total != 0 || done.Capped {
		t.Fatalf("expected done{total:0}, got %+v", done)
	}
}

func TestHandleFileSearchStreamCancelledContextEmitsNoDone(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the walk starts
	r := httptest.NewRequest("GET", "/api/files/search/stream?q="+url.QueryEscape("hello"), nil)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleFileSearchStream(w, r)
	if strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("cancelled stream must not emit done, got: %s", w.Body.String())
	}
}
